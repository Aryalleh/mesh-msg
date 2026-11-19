// The relay's HTTP API (server/api in Go), for edge runtimes: the same
// paths, binary bodies and headers, so phones, desktops, boards and other
// relays use an edge relay like any other.

import { cat, hex, unhex, randomBytes, encodeFrames, decodeFrames, decodeGateway, decodeSync, encodeSync, sleep, utf8 } from './util.js';
import { x25519, X25519_BASE, edPublic, signDomain } from './crypto.js';
import { checkPoW } from './packet.js';
import { D1Store } from './store-d1.js';
import { S3Store } from './store-s3.js';
import { Relay } from './relay.js';
import { tripGet, tripPut, tripDelete } from './trips.js';
import { adminRoute, OWNER_ID } from './admin.js';
import { maintenance } from './federate.js';

export const EDGE_VERSION = '0.1.0';
const MAX_BODY = 4 << 20;
const PAGE = 500;

const CORS_HEADERS = 'Content-Type, X-PoW, X-Origin, X-Region, X-Nonce, X-Signature, X-Readers, X-Account, X-Day, Authorization';

function list(v) {
  if (Array.isArray(v)) return v.map(String).map((s) => s.trim()).filter(Boolean);
  return String(v || '').split(/[\s,]+/).map((s) => s.trim()).filter(Boolean);
}

/** Relay settings from the platform's variables (Cloudflare env / the Arvan bundle's CONFIG). */
export function edgeConfig(env) {
  let peers = env.PEERS || [];
  if (typeof peers === 'string') { try { peers = JSON.parse(peers || '[]'); } catch (e) { peers = []; } }
  return {
    name: String(env.RELAY_NAME || 'edge'),
    region: String(env.RELAY_REGION || 'global'),
    publicURL: String(env.PUBLIC_URL || ''),
    admins: list(env.ADMINS).map((a) => a.toLowerCase()).filter((a) => /^[0-9a-f]{32}$/.test(a)),
    powBits: Number(env.POW_BITS ?? 16),
    ttlHours: Number(env.TTL_HOURS ?? 168),
    syncTokens: list(env.SYNC_TOKENS),
    peers: (Array.isArray(peers) ? peers : []).filter((p) => p && p.name && p.url),
    maxWait: Number(env.MAX_WAIT ?? 25),
    pollMs: Number(env.POLL_MS ?? 2000),
    releaseUpstream: String(env.RELEASE_UPSTREAM || 'https://eu-1.mfadem.ir'),
  };
}

/** Per-request context: store, settings, identity and admins. */
export class EdgeCtx {
  constructor(store, cfg, platform, waitUntil) {
    this.store = store;
    this.cfg = cfg;
    this.platform = platform;
    this.waitUntil = waitUntil || ((p) => p.catch(() => {}));
    this.relay = new Relay(store, { name: cfg.name, region: cfg.region, ttlMs: () => this._ttlMs });
    this._ttlMs = cfg.ttlHours * 3600 * 1000;
  }

  async settings() {
    if (!this._settings) {
      let saved = {};
      try { saved = JSON.parse((await this.store.kvGet('settings')) || '{}'); } catch (e) { saved = {}; }
      this._settings = {
        saved,
        powBits: saved.pow_bits ?? this.cfg.powBits,
        ttlHours: saved.ttl_hours ?? this.cfg.ttlHours,
      };
      this._ttlMs = this._settings.ttlHours * 3600 * 1000;
    }
    return this._settings;
  }

  async admins() {
    if (!this._admins) {
      let saved = [];
      try { saved = JSON.parse((await this.store.kvGet('admins')) || '[]'); } catch (e) { saved = []; }
      this._admins = [...new Set([OWNER_ID, ...this.cfg.admins, ...saved])];
    }
    return this._admins;
  }

  /** The relay's own key (made on first use, kept in the store). */
  async identity() {
    if (!edgeIdentityCache.has(this.store)) {
      const seedHex = await this.store.kvPutIfAbsent('identity', hex(randomBytes(64)));
      const seed = unhex(seedHex);
      const xPriv = seed.slice(0, 32), edSeed = seed.slice(32);
      const xPub = x25519(xPriv, X25519_BASE), edPub = await edPublic(edSeed);
      await this.store.kvPutIfAbsent('started', String(Date.now()));
      edgeIdentityCache.set(this.store, { xPriv, xPub, edSeed, edPub, bytes: cat(xPub, edPub) });
    }
    return edgeIdentityCache.get(this.store);
  }
}
const edgeIdentityCache = new WeakMap();

function respond(body, status = 200, headers = {}) { return new Response(body, { status, headers }); }
function jsonOK(v) { return respond(JSON.stringify(v), 200, { 'Content-Type': 'application/json' }); }
function textErr(status, msg) { return respond(msg + '\n', status, { 'Content-Type': 'text/plain; charset=utf-8' }); }
function framesOK(body, cursor, more) {
  const h = { 'Content-Type': 'application/x-meshmsg-frames', 'X-Cursor': String(cursor) };
  if (more) h['X-More'] = '1';
  return respond(body, 200, h);
}

async function readBody(request) {
  const b = new Uint8Array(await request.arrayBuffer());
  if (b.length > MAX_BODY) throw new Error('body too large');
  return b;
}

function origin(request, prefix) {
  let o = request.headers.get('X-Origin') || '';
  if (!o) return prefix + 'anon';
  if (o.length > 64) o = o.slice(0, 64);
  return prefix + o;
}

async function powOK(ctx, request, body) {
  const s = await ctx.settings();
  if (s.powBits <= 0 || body.length === 0) return true;
  return checkPoW(body, request.headers.get('X-PoW'), s.powBits);
}

function syncAuthorized(ctx, request) {
  const h = request.headers.get('Authorization') || '';
  if (!h.startsWith('Bearer ')) return null; // fall back to proof of work
  return ctx.cfg.syncTokens.includes(h.slice(7));
}

/** Long poll: calls fetch until it returns something or wait (s) elapses. */
async function waitFor(ctx, url, fetchOnce) {
  const wait = Math.min(Math.max(Number(url.searchParams.get('wait') || 0), 0), ctx.cfg.maxWait) * 1000;
  const deadline = Date.now() + wait;
  for (;;) {
    const r = await fetchOnce();
    if (r.found || Date.now() + ctx.cfg.pollMs > deadline) return r;
    await sleep(ctx.cfg.pollMs);
  }
}

// Go's encoding/json escapes these; signatures cover Go-style JSON.
function goJSON(v) {
  return JSON.stringify(v).replace(/[<>&\u2028\u2029]/g, (c) => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0'));
}

async function relayList(ctx, url) {
  const me = await ctx.identity();
  const pub = hex(me.bytes);
  const urls = ctx.cfg.publicURL ? [ctx.cfg.publicURL] : [url.origin];
  const relays = [{ name: ctx.cfg.name, region: ctx.cfg.region, urls, pubkey: pub }];
  const issued = Math.floor(Date.now() / 1000);
  const payload = goJSON({ relays, issued });
  const sig = await signDomain(me.edSeed, 'MESHMSG-RELAYS', utf8(payload));
  return jsonOK({ relays, issued, pubkey: pub, signature: hex(sig) });
}

const releaseCache = new Map(); // channel → {at, body}
async function release(ctx, url) {
  const ch = url.searchParams.get('channel') || 'stable';
  if (ch !== 'beta' && ch !== 'stable') return textErr(400, 'unknown channel');
  const c = releaseCache.get(ch);
  if (c && Date.now() - c.at < 10 * 60 * 1000) return respond(c.body, 200, { 'Content-Type': 'application/json', 'Cache-Control': 'public, max-age=600' });
  try {
    const r = await fetch(ctx.cfg.releaseUpstream.replace(/\/+$/, '') + '/v1/release?channel=' + ch);
    if (r.ok) {
      const body = await r.text();
      releaseCache.set(ch, { at: Date.now(), body });
      return respond(body, 200, { 'Content-Type': 'application/json', 'Cache-Control': 'public, max-age=600' });
    }
  } catch (e) { /* below */ }
  return textErr(404, 'no release manifest yet');
}

async function route(ctx, request, url) {
  const m = request.method, p = url.pathname;
  const q = (name, def = 0) => { const v = Number(url.searchParams.get(name)); return Number.isFinite(v) && url.searchParams.has(name) ? v : def; };

  if (m === 'GET' && p === '/v1/info') {
    const me = await ctx.identity();
    const s = await ctx.settings();
    return jsonOK({ name: ctx.cfg.name, region: ctx.cfg.region, pow_bits: s.powBits, version: EDGE_VERSION, pubkey: hex(me.bytes) });
  }
  if (m === 'GET' && p === '/v1/relays') return relayList(ctx, url);
  if (m === 'POST' && p === '/v1/packets') {
    const body = await readBody(request);
    if (!(await powOK(ctx, request, body))) return textErr(402, 'proof of work required');
    let raws;
    try { raws = decodeFrames(body); } catch (e) { return textErr(400, 'bad frames'); }
    await ctx.settings();
    return jsonOK({ accepted: await ctx.relay.ingest(raws, origin(request, 'client:'), ctx.cfg.region) });
  }
  if (m === 'POST' && p === '/v1/receipts/query') {
    const body = await readBody(request);
    if (body.length % 8 || body.length / 8 > 1000) return textErr(400, 'body must be up to 1000 packet IDs');
    const ids = [];
    for (let i = 0; i < body.length; i += 8) ids.push(hex(body.subarray(i, i + 8)));
    return framesOK(encodeFrames(await ctx.relay.receipts(ids)), 0, false);
  }
  if (m === 'GET' && p === '/v1/challenge') return respond(await ctx.relay.newChallenge(), 200, { 'Content-Type': 'text/plain' });
  if (m === 'GET' && p.startsWith('/v1/mailbox/')) {
    const dest = p.slice('/v1/mailbox/'.length).toLowerCase();
    if (!/^[0-9a-f]{32}$/.test(dest)) return textErr(400, 'bad destination');
    let sig;
    try { sig = unhex(request.headers.get('X-Signature') || ''); } catch (e) { sig = new Uint8Array(0); }
    if (!(await ctx.relay.authorizeFetch(dest, request.headers.get('X-Nonce') || '', sig))) return textErr(401, 'unauthorized');
    const after = q('after');
    const r = await waitFor(ctx, url, async () => {
      const x = await ctx.relay.mailbox(dest, after, PAGE);
      return { ...x, found: x.raws.length > 0 };
    });
    return framesOK(encodeFrames(r.raws), r.cursor, false);
  }
  if (m === 'POST' && p === '/v1/gateway') {
    const body = await readBody(request);
    let g;
    try { g = decodeGateway(body); } catch (e) { return textErr(400, 'bad gateway body'); }
    const org = origin(request, 'gw:');
    await ctx.settings();
    if (g.upload.length > 0) {
      if (!(await powOK(ctx, request, body))) return textErr(402, 'proof of work required');
      await ctx.relay.ingest(g.upload, org, ctx.cfg.region);
    }
    const req = { origin: org, serve: g.serve, after: q('after'), carrier: url.searchParams.get('carrier') === '1', quota: q('quota'), limit: PAGE };
    const r = await waitFor(ctx, url, async () => {
      ctx.relay.annCache.clear();
      const x = await ctx.relay.gateway(req);
      return { ...x, found: x.raws.length > 0 };
    });
    return framesOK(encodeFrames(r.raws), r.cursor, false);
  }
  if (m === 'POST' && p === '/v1/sync/push') {
    const body = await readBody(request);
    const tok = syncAuthorized(ctx, request);
    if (tok === false) return textErr(401, 'bad token');
    if (tok === null && !(await powOK(ctx, request, body))) return textErr(402, 'proof of work required');
    let items;
    try { items = decodeSync(body); } catch (e) { return textErr(400, 'bad sync body'); }
    await ctx.settings();
    return jsonOK({ accepted: await ctx.relay.ingestSync(items, origin(request, 'peer:')) });
  }
  if (m === 'GET' && p === '/v1/sync/pull') {
    if (syncAuthorized(ctx, request) === false) return textErr(401, 'bad token');
    const after = q('after'), limit = Math.min(Math.max(q('limit', PAGE), 1), 2000);
    const ex = origin(request, 'peer:');
    const r = await waitFor(ctx, url, async () => {
      const x = await ctx.relay.pull(after, limit, ex);
      return { ...x, found: x.items.length > 0 };
    });
    return framesOK(encodeSync(r.items), r.cursor, r.more);
  }
  if (m === 'GET' && p === '/v1/release') return release(ctx, url);
  if (m === 'GET' && p === '/v1/trip') return tripGet(ctx);
  if (m === 'PUT' && p === '/v1/trip') return tripPut(ctx, request);
  if (m === 'DELETE' && p === '/v1/trip') return tripDelete(ctx, request, url);
  if (p === '/admin' || p.startsWith('/admin/')) {
    const r = await adminRoute(ctx, request, url);
    if (r) return r;
  }
  if (m === 'GET' && p === '/') return respond(`Mava relay ${ctx.cfg.name} (edge, ${ctx.platform}). Add its address to Mava; admins: /admin\n`, 200, { 'Content-Type': 'text/plain; charset=utf-8' });
  return textErr(404, 'not found');
}

/** Handles one request (both platforms). */
export async function handleEdge(ctx, request) {
  const url = new URL(request.url);
  const cors = { 'Access-Control-Allow-Origin': '*', 'Access-Control-Expose-Headers': 'X-Cursor, X-More' };
  if (request.method === 'OPTIONS') {
    return respond(null, 204, { ...cors, 'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE', 'Access-Control-Allow-Headers': CORS_HEADERS, 'Access-Control-Max-Age': '86400' });
  }
  let res;
  try {
    res = await route(ctx, request, url);
  } catch (e) {
    res = textErr(e.message === 'body too large' ? 413 : 500, e.message === 'body too large' ? e.message : 'relay error: ' + (e.message || e));
  }
  const h = new Headers(res.headers);
  for (const [k, v] of Object.entries(cors)) h.set(k, v);
  if (!h.has('Cache-Control')) h.set('Cache-Control', 'no-store');
  return new Response(res.body, { status: res.status, headers: h });
}

/** Expiry and federation (a cron trigger, or now and then on platforms without one). */
export async function runMaintenance(ctx) {
  await ctx.settings();
  return maintenance(ctx);
}

export function makeStore(env) {
  if (env.DB) return new D1Store(env.DB);
  if (env.S3 && env.S3.bucket) return new S3Store(env.S3);
  throw new Error('no store: bind a D1 database as DB (Cloudflare) or set S3 (endpoint, region, bucket, accessKey, secretKey)');
}
