// The relay's store on Cloudflare D1 (SQLite), the same table as the Go
// relay's (core/store/sqlite.go) with hex and base64 text instead of blobs,
// plus a small key-value table (identity, settings, codes, sessions, trip).

import { b64, unb64 } from './util.js';

const D1_SCHEMA = [
  `CREATE TABLE IF NOT EXISTS items (
    seq INTEGER PRIMARY KEY AUTOINCREMENT, pid TEXT NOT NULL, frag INTEGER NOT NULL, type INTEGER NOT NULL,
    dest TEXT NOT NULL, raw TEXT NOT NULL, origin TEXT NOT NULL DEFAULT '', region TEXT NOT NULL DEFAULT '',
    received INTEGER NOT NULL, expires INTEGER NOT NULL, UNIQUE(pid, frag))`,
  `CREATE INDEX IF NOT EXISTS items_dest ON items(dest, type, seq)`,
  `CREATE INDEX IF NOT EXISTS items_expires ON items(expires)`,
  `CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, v TEXT NOT NULL, expires INTEGER NOT NULL DEFAULT 0)`,
];

function d1Item(r) {
  return {
    seq: r.seq, pid: r.pid, frag: r.frag, type: r.type, dest: r.dest, raw: unb64(r.raw),
    origin: r.origin, region: r.region, received: r.received, expires: r.expires,
  };
}

export class D1Store {
  constructor(db) { this.db = db; this.ready = null; }

  init() {
    if (!this.ready) this.ready = this.db.batch(D1_SCHEMA.map((s) => this.db.prepare(s))).catch((e) => { this.ready = null; throw e; });
    return this.ready;
  }

  async q(sql, ...args) { await this.init(); return (await this.db.prepare(sql).bind(...args).all()).results || []; }

  async put(it) {
    await this.init();
    const ins = this.db.prepare(
      'INSERT OR IGNORE INTO items(pid,frag,type,dest,raw,origin,region,received,expires) VALUES(?,?,?,?,?,?,?,?,?)',
    ).bind(it.pid, it.frag, it.type, it.dest, b64(it.raw), it.origin || '', it.region || '', it.received, it.expires);
    if (it.type === 2) { // the newest announce replaces the older ones of its destination
      const del = this.db.prepare('DELETE FROM items WHERE type=2 AND dest=? AND NOT (pid=? AND frag=?)').bind(it.dest, it.pid, it.frag);
      const res = await this.db.batch([ins, del]);
      return { added: (res[0].meta && res[0].meta.changes) > 0 };
    }
    const res = await ins.run();
    return { added: (res.meta && res.meta.changes) > 0 };
  }

  async has(pid, frag) { return (await this.q('SELECT 1 AS one FROM items WHERE pid=? AND frag=?', pid, frag)).length > 0; }

  async get(pid, frag) {
    const r = await this.q('SELECT * FROM items WHERE pid=? AND frag=?', pid, frag);
    return r.length ? d1Item(r[0]) : null;
  }

  async byDest(dest, after, limit) {
    const rows = await this.q('SELECT * FROM items WHERE dest=? AND type=0 AND seq > ? ORDER BY seq LIMIT ?', dest, after, limit);
    const items = rows.map(d1Item);
    return { items, cursor: items.length ? items[items.length - 1].seq : after };
  }

  /**
   * Items after cursor in order, at most limit looked at; filter
   * {excludeOrigin, types} drops some. cursor passes the dropped ones too.
   */
  async scan(after, limit, filter = {}) {
    const rows = await this.q('SELECT * FROM items WHERE seq > ? ORDER BY seq LIMIT ?', after, limit + 1);
    const more = rows.length > limit;
    const seen = rows.slice(0, limit);
    const items = seen.filter((r) => (!filter.excludeOrigin || r.origin !== filter.excludeOrigin) &&
      (!filter.types || filter.types.includes(r.type))).map(d1Item);
    return { items, cursor: seen.length ? seen[seen.length - 1].seq : after, more };
  }

  async latestAnnounce(dest) {
    const r = await this.q('SELECT * FROM items WHERE dest=? AND type=2 ORDER BY seq DESC LIMIT 1', dest);
    return r.length ? d1Item(r[0]) : null;
  }

  async deleteData(pid) {
    await this.init();
    const res = await this.db.prepare('DELETE FROM items WHERE pid=? AND type=0').bind(pid).run();
    return (res.meta && res.meta.changes) || 0;
  }

  async expire(now) {
    await this.init();
    const res = await this.db.batch([
      this.db.prepare('DELETE FROM items WHERE expires != 0 AND expires < ?').bind(now),
      this.db.prepare('DELETE FROM kv WHERE expires != 0 AND expires < ?').bind(now),
    ]);
    return (res[0].meta && res[0].meta.changes) || 0;
  }

  async count() {
    const r = await this.q('SELECT type, COUNT(*) AS n FROM items GROUP BY type');
    const out = { total: 0, data: 0, receipts: 0, announces: 0 };
    for (const x of r) {
      out.total += x.n;
      if (x.type === 0) out.data = x.n;
      if (x.type === 1) out.receipts = x.n;
      if (x.type === 2) out.announces = x.n;
    }
    return out;
  }

  async kvGet(k) {
    const r = await this.q('SELECT v, expires FROM kv WHERE k=?', k);
    if (!r.length || (r[0].expires && r[0].expires < Date.now())) return null;
    return r[0].v;
  }

  async kvPut(k, v, ttlMs = 0) {
    await this.init();
    await this.db.prepare('INSERT INTO kv(k,v,expires) VALUES(?,?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v, expires=excluded.expires')
      .bind(k, v, ttlMs ? Date.now() + ttlMs : 0).run();
  }

  async kvDel(k) { await this.init(); await this.db.prepare('DELETE FROM kv WHERE k=?').bind(k).run(); }

  /** Stores v under k unless k exists; returns the value now stored. */
  async kvPutIfAbsent(k, v) {
    await this.init();
    await this.db.prepare('INSERT OR IGNORE INTO kv(k,v,expires) VALUES(?,?,0)').bind(k, v).run();
    return this.kvGet(k);
  }
}
