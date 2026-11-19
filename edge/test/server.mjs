// Runs the built edge relay (edge/dist) in Node for tests: Cloudflare's D1
// stood in by node:sqlite, or the S3 store against an in-memory fake S3.
//
//   node edge/test/server.mjs [--store d1|s3] [--entry cloudflare|arvan] [--config '{"RELAY_NAME":"x",…}']
//
// Prints "listening http://127.0.0.1:<port>" when ready. POST /__test/maintenance
// runs the scheduled work (expiry, federation) and returns its report.

import http from 'node:http';
import { readFileSync, writeFileSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { DatabaseSync } from 'node:sqlite';

const here = dirname(fileURLToPath(import.meta.url));
const args = process.argv.slice(2);
const opt = (name, def) => { const i = args.indexOf('--' + name); return i >= 0 ? args[i + 1] : def; };
const storeKind = opt('store', 'd1');
const entry = opt('entry', 'cloudflare');
const env = JSON.parse(opt('config', '{}'));

// ---- D1 on node:sqlite ----
class D1Stmt {
  constructor(db, sql, args = []) { this.db = db; this.sql = sql; this.args = args; }
  bind(...a) { return new D1Stmt(this.db, this.sql, a); }
  async all() { return { results: this.db.prepare(this.sql).all(...this.args) }; }
  async first() { return this.db.prepare(this.sql).get(...this.args) ?? null; }
  async run() { return this.runSync(); }
  runSync() {
    const r = this.db.prepare(this.sql).run(...this.args);
    return { meta: { changes: Number(r.changes), last_row_id: Number(r.lastInsertRowid) } };
  }
}
class D1 {
  constructor() { this.db = new DatabaseSync(':memory:'); }
  prepare(sql) { return new D1Stmt(this.db, sql); }
  async batch(stmts) {
    this.db.exec('BEGIN');
    try { const r = stmts.map((s) => s.runSync()); this.db.exec('COMMIT'); return r; } catch (e) { this.db.exec('ROLLBACK'); throw e; }
  }
}

// ---- a fake S3 (path style, ListObjectsV2) ----
function startFakeS3() {
  const objects = new Map();
  const srv = http.createServer((req, res) => {
    const u = new URL(req.url, 'http://x');
    if (!/^AWS4-HMAC-SHA256 Credential=test-access\//.test(req.headers.authorization || '')) { res.writeHead(403); res.end(); return; }
    const [, bucket, ...rest] = u.pathname.split('/');
    const key = rest.map(decodeURIComponent).join('/');
    if (bucket !== 'mava') { res.writeHead(404); res.end(); return; }
    const chunks = [];
    req.on('data', (c) => chunks.push(c));
    req.on('end', () => {
      if (req.method === 'PUT') { objects.set(key, Buffer.concat(chunks)); res.writeHead(200); res.end(); return; }
      if (req.method === 'DELETE') { objects.delete(key); res.writeHead(204); res.end(); return; }
      if (req.method === 'GET' && key === '' && u.searchParams.get('list-type') === '2') {
        const prefix = u.searchParams.get('prefix') || '', after = u.searchParams.get('start-after') || '';
        const max = Number(u.searchParams.get('max-keys') || 1000);
        const keys = [...objects.keys()].filter((k) => k.startsWith(prefix) && k > after).sort();
        const page = keys.slice(0, max);
        const xml = `<?xml version="1.0"?><ListBucketResult><IsTruncated>${keys.length > max}</IsTruncated>` +
          page.map((k) => `<Contents><Key>${k.replace(/&/g, '&amp;')}</Key></Contents>`).join('') + '</ListBucketResult>';
        res.writeHead(200, { 'Content-Type': 'application/xml' }); res.end(xml); return;
      }
      if (req.method === 'GET') {
        if (!objects.has(key)) { res.writeHead(404); res.end(); return; }
        res.writeHead(200); res.end(objects.get(key)); return;
      }
      res.writeHead(400); res.end();
    });
  });
  return new Promise((r) => srv.listen(0, '127.0.0.1', () => r(`http://127.0.0.1:${srv.address().port}`)));
}

if (storeKind === 'd1') env.DB = new D1();
else env.S3 = { endpoint: await startFakeS3(), region: 'ir-thr-at1', bucket: 'mava', accessKey: 'test-access', secretKey: 'test-secret' };
if (storeKind === 's3') env.POLL_MS = env.POLL_MS ?? 1000;

// ---- the bundle ----
let fetchHandler, maintenance;
if (entry === 'cloudflare') {
  const mod = await import(pathToFileURL(join(here, '..', 'dist', 'cloudflare.js')).href);
  const exec = { waitUntil: (p) => p.catch(() => {}) };
  fetchHandler = (req) => mod.default.fetch(req, env, exec);
  maintenance = () => new Promise((resolve) => mod.default.scheduled({}, env, { waitUntil: (p) => resolve(p) }));
} else {
  // The Arvan bundle as its installer writes it: CONFIG filled in, run as a service worker.
  const cfg = { ...env };
  delete cfg.DB;
  const src = readFileSync(join(here, '..', 'dist', 'arvan.js'), 'utf8')
    .replace(/\/\*MAVA_CONFIG\*\/[\s\S]*?\/\*END_MAVA_CONFIG\*\//, JSON.stringify(cfg));
  const file = join(mkdtempSync(join(tmpdir(), 'mava-arvan-')), 'arvan.mjs');
  writeFileSync(file, src);
  let listener;
  globalThis.addEventListener = (type, fn) => { if (type === 'fetch') listener = fn; };
  await import(pathToFileURL(file).href);
  fetchHandler = (req) => new Promise((resolve, reject) => {
    listener({ request: req, respondWith: (p) => Promise.resolve(p).then(resolve, reject), waitUntil: (p) => p.catch(() => {}) });
  });
  maintenance = async () => ({ note: 'arvan runs maintenance after requests' });
}

const server = http.createServer(async (req, res) => {
  const chunks = [];
  for await (const c of req) chunks.push(c);
  const body = Buffer.concat(chunks);
  const url = `http://${req.headers.host}${req.url}`;
  try {
    if (req.url === '/__test/maintenance' && req.method === 'POST') {
      const out = await maintenance();
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify(out));
      return;
    }
    const headers = new Headers();
    for (const [k, v] of Object.entries(req.headers)) headers.set(k, Array.isArray(v) ? v.join(', ') : v);
    const request = new Request(url, { method: req.method, headers, body: ['GET', 'HEAD'].includes(req.method) ? undefined : body });
    const r = await fetchHandler(request);
    const h = {};
    r.headers.forEach((v, k) => { h[k] = v; });
    res.writeHead(r.status, h);
    res.end(Buffer.from(await r.arrayBuffer()));
  } catch (e) {
    res.writeHead(500); res.end(String(e && e.stack || e));
  }
});
server.listen(Number(opt('port', 0)), '127.0.0.1', () => {
  console.log(`listening http://127.0.0.1:${server.address().port}`);
});
