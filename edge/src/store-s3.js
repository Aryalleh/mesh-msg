// The relay's store in an S3-compatible bucket (Arvan Object Storage), for
// edge platforms without a database. Every packet is one object; its
// metadata is in the object's name, so listings answer most queries
// without reading objects:
//
//   i/<seq>_<type>_<pid>_<frag>_<dest>_<origin tag>   the packet (JSON: raw, origin, region, received, expires)
//   k/<pid>_<frag>                                    → the i/ name (duplicates, receipts, purges)
//   d/<dest>/<seq>                                    → the i/ name (mailboxes; DATA only)
//   a/<dest>                                          → the i/ name of the newest announce
//   kv/<key>                                          small values (identity, settings, codes, trip)
//
// seq is the time in microseconds (16 digits) plus a random part: objects
// written at the same moment from different edge locations get distinct,
// almost ordered numbers. Readers only look at objects older than a few
// seconds (SETTLE), so a slightly late writer is not skipped by a cursor.

import { b64, hex, unb64, utf8 } from './util.js';
import { sha256, hmac256 } from './crypto.js';

const SETTLE_MS = 4000;
const S3_PAGE = 1000;

async function sha256hex(b) { return hex(await sha256(typeof b === 'string' ? utf8(b) : b)); }

function pad16(n) { return String(n).padStart(16, '0'); }

async function originTag(origin) { return (await sha256hex(origin || '')).slice(0, 12); }

function parseItemName(name) {
  // i/<seq>_<type>_<pid>_<frag>_<dest>_<tag>
  const m = /^i\/(\d{16})_(\d)_([0-9a-f]{16})_(\d{1,2})_([0-9a-f]{32})_([0-9a-f]{12})$/.exec(name);
  if (!m) return null;
  return { name, seq: Number(m[1]), type: Number(m[2]), pid: m[3], frag: Number(m[4]), dest: m[5], tag: m[6] };
}

export class S3Store {
  /** cfg: {endpoint: "https://s3.ir-thr-at1.arvanstorage.ir", region, bucket, accessKey, secretKey} */
  constructor(cfg) { this.cfg = cfg; }

  async init() {}

  // ---- S3 requests, signed with AWS Signature Version 4 ----
  async s3(method, key, { query = {}, body = null, headers = {} } = {}) {
    const c = this.cfg;
    const base = new URL(c.endpoint.replace(/\/+$/, ''));
    const path = '/' + c.bucket + (key ? '/' + key.split('/').map(encodeURIComponent).join('/') : '');
    const now = new Date();
    const amzDate = now.toISOString().replace(/[-:]/g, '').replace(/\.\d{3}/, '');
    const day = amzDate.slice(0, 8);
    const payload = body === null ? new Uint8Array(0) : (typeof body === 'string' ? utf8(body) : body);
    const payloadHash = await sha256hex(payload);
    const qs = Object.keys(query).sort().map((k) => `${encodeURIComponent(k)}=${encodeURIComponent(query[k])}`).join('&');
    const h = { host: base.host, 'x-amz-content-sha256': payloadHash, 'x-amz-date': amzDate };
    for (const [hk, hv] of Object.entries(headers)) h[hk.toLowerCase()] = hv;
    const names = Object.keys(h).sort();
    const canonHeaders = names.map((k) => `${k}:${String(h[k]).trim()}\n`).join('');
    const signed = names.join(';');
    const canonical = [method, path, qs, canonHeaders, signed, payloadHash].join('\n');
    const region = c.region || 'us-east-1';
    const scope = `${day}/${region}/s3/aws4_request`;
    const toSign = ['AWS4-HMAC-SHA256', amzDate, scope, await sha256hex(canonical)].join('\n');
    let k = await hmac256(utf8('AWS4' + c.secretKey), utf8(day));
    k = await hmac256(k, utf8(region));
    k = await hmac256(k, utf8('s3'));
    k = await hmac256(k, utf8('aws4_request'));
    const sig = hex(await hmac256(k, utf8(toSign)));
    const auth = `AWS4-HMAC-SHA256 Credential=${c.accessKey}/${scope}, SignedHeaders=${signed}, Signature=${sig}`;
    const hdrs = { ...h, authorization: auth };
    delete hdrs.host;
    const res = await fetch(base.origin + path + (qs ? '?' + qs : ''), { method, headers: hdrs, body: body === null ? undefined : payload });
    return res;
  }

  async getText(key) {
    const r = await this.s3('GET', key);
    if (r.status === 404) return null;
    if (!r.ok) throw new Error(`s3 GET ${key}: ${r.status}`);
    return r.text();
  }

  async putText(key, text) {
    const r = await this.s3('PUT', key, { body: text, headers: { 'content-type': 'application/octet-stream' } });
    if (!r.ok) throw new Error(`s3 PUT ${key}: ${r.status}`);
  }

  async del(key) {
    const r = await this.s3('DELETE', key);
    if (!r.ok && r.status !== 404) throw new Error(`s3 DELETE ${key}: ${r.status}`);
  }

  /** Object names under prefix after startAfter, in order. */
  async list(prefix, startAfter, max) {
    const query = { 'list-type': '2', prefix, 'max-keys': String(Math.min(max, S3_PAGE)) };
    if (startAfter) query['start-after'] = startAfter;
    const r = await this.s3('GET', '', { query });
    if (!r.ok) throw new Error(`s3 LIST ${prefix}: ${r.status}`);
    const xml = await r.text();
    const keys = [...xml.matchAll(/<Key>([^<]*)<\/Key>/g)].map((m) => m[1].replace(/&amp;/g, '&'));
    return { keys, truncated: /<IsTruncated>true<\/IsTruncated>/.test(xml) };
  }

  async readItem(meta) {
    const t = await this.getText(meta.name);
    if (t === null) return null;
    const o = JSON.parse(t);
    return { ...meta, raw: unb64(o.raw), origin: o.origin, region: o.region, received: o.received, expires: o.expires };
  }

  // ---- the store interface (see store-d1.js) ----
  async put(it) {
    if (await this.has(it.pid, it.frag)) return { added: false };
    const seq = Date.now() * 1000 + Math.floor(Math.random() * 1000);
    const name = `i/${pad16(seq)}_${it.type}_${it.pid}_${it.frag}_${it.dest}_${await originTag(it.origin)}`;
    await this.putText(name, JSON.stringify({ raw: b64(it.raw), origin: it.origin || '', region: it.region || '', received: it.received, expires: it.expires }));
    await this.putText(`k/${it.pid}_${it.frag}`, name);
    if (it.type === 0) await this.putText(`d/${it.dest}/${pad16(seq)}`, name);
    if (it.type === 2) {
      const old = await this.getText(`a/${it.dest}`);
      await this.putText(`a/${it.dest}`, name);
      if (old && old !== name) await this.dropItem(parseItemName(old));
    }
    return { added: true };
  }

  async dropItem(m) {
    if (!m) return;
    await this.del(m.name);
    await this.del(`k/${m.pid}_${m.frag}`);
    if (m.type === 0) await this.del(`d/${m.dest}/${pad16(m.seq)}`);
  }

  async has(pid, frag) { return (await this.getText(`k/${pid}_${frag}`)) !== null; }

  async get(pid, frag) {
    const name = await this.getText(`k/${pid}_${frag}`);
    const m = name && parseItemName(name);
    return m ? this.readItem(m) : null;
  }

  settled() { return (Date.now() - SETTLE_MS) * 1000; }

  async byDest(dest, after, limit) {
    const { keys } = await this.list(`d/${dest}/`, after ? `d/${dest}/${pad16(after)}` : '', limit);
    const cutoff = this.settled();
    const out = [];
    let cursor = after;
    for (const k of keys) {
      const seq = Number(k.slice(k.lastIndexOf('/') + 1));
      if (seq > cutoff) break;
      const name = await this.getText(k);
      const m = name && parseItemName(name);
      const it = m && (await this.readItem(m));
      if (it) out.push(it);
      cursor = seq;
    }
    return { items: out, cursor };
  }

  /** As D1Store.scan: names carry type and origin tag, so dropped items are never read. */
  async scan(after, limit, filter = {}) {
    const { keys, truncated } = await this.list('i/', after ? `i/${pad16(after)}` : '', limit + 1);
    const cutoff = this.settled();
    const tag = filter.excludeOrigin ? await originTag(filter.excludeOrigin) : null;
    const seen = [];
    for (const k of keys) {
      const m = parseItemName(k);
      if (!m) continue;
      if (m.seq > cutoff || seen.length >= limit) break;
      seen.push(m);
    }
    const keep = seen.filter((m) => (!tag || m.tag !== tag) && (!filter.types || filter.types.includes(m.type)));
    const items = (await Promise.all(keep.map((m) => this.readItem(m)))).filter(Boolean);
    const cursor = seen.length ? seen[seen.length - 1].seq : after;
    return { items, cursor, more: seen.length >= limit && (truncated || keys.length > limit) };
  }

  async latestAnnounce(dest) {
    const name = await this.getText(`a/${dest}`);
    const m = name && parseItemName(name);
    return m ? this.readItem(m) : null;
  }

  async deleteData(pid) {
    const { keys } = await this.list(`k/${pid}_`, '', 32);
    let n = 0;
    for (const k of keys) {
      const name = await this.getText(k);
      const m = name && parseItemName(name);
      if (m && m.type === 0) { await this.dropItem(m); n++; }
    }
    return n;
  }

  async expire(now) {
    // Objects are named in time order: drop from the oldest while expired.
    let n = 0, after = '';
    for (let page = 0; page < 5; page++) {
      const { keys, truncated } = await this.list('i/', after, 200);
      for (const k of keys) {
        const m = parseItemName(k);
        after = k;
        if (!m) continue;
        const it = await this.readItem(m);
        if (!it) continue;
        if (it.expires && it.expires < now) {
          if (m.type === 2 && (await this.getText(`a/${m.dest}`)) === m.name) await this.del(`a/${m.dest}`);
          await this.dropItem(m);
          n++;
        } else if (Math.floor(m.seq / 1000) > now) {
          return n;
        }
      }
      if (!truncated) break;
    }
    return n;
  }

  async count() { return { total: -1, data: -1, receipts: -1, announces: -1 }; }

  async kvGet(k) {
    const t = await this.getText(`kv/${k}`);
    if (t === null) return null;
    const o = JSON.parse(t);
    if (o.expires && o.expires < Date.now()) return null;
    return o.v;
  }

  async kvPut(k, v, ttlMs = 0) { await this.putText(`kv/${k}`, JSON.stringify({ v, expires: ttlMs ? Date.now() + ttlMs : 0 })); }

  async kvDel(k) { await this.del(`kv/${k}`); }

  async kvPutIfAbsent(k, v) {
    const cur = await this.kvGet(k);
    if (cur !== null) return cur;
    await this.kvPut(k, v);
    return this.kvGet(k);
  }
}
