// The relay's logic (server/relay in Go): an untrusted store-and-forward
// mailbox that federates with other relays and serves gateways.

import { hex, unhex, randomBytes, utf8 } from './util.js';
import { hmac256 } from './crypto.js';
import {
  decodePacket, packetKey, parseAnnounce, receiptAcked, receiptID, verifyReceipt, verifyFetch,
  T_DATA, T_RECEIPT, T_ANNOUNCE, FLAG_DELEGATE,
} from './packet.js';

const CHALLENGE_TTL = 5 * 60 * 1000;
const CARRIER_MIN_AGE = 10 * 60 * 1000;

export class Relay {
  /** store: D1Store | S3Store; opts: {name, region, ttlMs: () => number} */
  constructor(store, opts) {
    this.store = store;
    this.opts = opts;
    this.annCache = new Map(); // per request: dest → {a, item} | null
  }

  async announceFor(dest) {
    if (this.annCache.has(dest)) return this.annCache.get(dest);
    const item = await this.store.latestAnnounce(dest);
    let out = null;
    if (item) {
      const p = decodePacket(item.raw);
      const a = p && (await parseAnnounce(p));
      if (a) out = { a, item };
    }
    this.annCache.set(dest, out);
    return out;
  }

  async publicFor(dest) { const r = await this.announceFor(dest); return r ? r.a.pub : null; }

  async delegated(dest) { const r = await this.announceFor(dest); return !!(r && r.a.flags & FLAG_DELEGATE); }

  /** Ingests raw packets; returns how many were new. */
  async ingest(raws, origin, region) {
    const now = Date.now();
    let n = 0;
    for (const raw of raws) if ((await this.ingestOne(raw, origin, region, now)) === 'accepted') n++;
    return n;
  }

  async ingestOne(raw, origin, region, now) {
    const p = decodePacket(raw);
    if (!p) return 'rejected';
    const key = packetKey(p);
    if (await this.store.has(key.pid, key.frag)) return 'duplicate';
    const item = {
      ...key, type: p.type, dest: p.dest, raw, origin, region: '', received: now, expires: now + this.opts.ttlMs(),
    };
    switch (p.type) {
      case T_ANNOUNCE: {
        const a = await parseAnnounce(p);
        if (!a) return 'rejected';
        const old = await this.announceFor(p.dest);
        if (old) {
          if (a.time <= old.a.time) return 'duplicate'; // keep only newer announces
          if (!region) region = old.item.region;
        }
        item.region = region || '';
        this.annCache.delete(p.dest);
        break;
      }
      case T_RECEIPT: {
        const acked = await receiptAcked(p);
        if (!acked) return 'rejected';
        const pub = await this.publicFor(p.dest);
        if (pub) {
          if (!(await verifyReceipt(p, pub))) return 'rejected';
          await this.store.deleteData(acked);
        }
        break;
      }
      case T_DATA:
        if (await this.store.has(await receiptID(p.id), 0)) return 'duplicate'; // already delivered
        break;
      default:
        return 'rejected'; // hop-local types never reach relays
    }
    const { added } = await this.store.put(item);
    return added ? 'accepted' : 'duplicate';
  }

  /** Federation / bridge items: keep the home region each one carries. */
  async ingestSync(items, origin) {
    const now = Date.now();
    let n = 0;
    for (const it of items) if ((await this.ingestOne(it.raw, origin, it.region, now)) === 'accepted') n++;
    return n;
  }

  // Mailbox challenges are stateless (no store write per fetch): the nonce
  // carries its time and an HMAC with the relay's secret, and is good for 5
  // minutes.
  async challengeKey() {
    if (!this.chKey) this.chKey = unhex(await this.store.kvPutIfAbsent('challenge-key', hex(randomBytes(32))));
    return this.chKey;
  }

  async newChallenge() {
    const ts = Math.floor(Date.now() / 1000).toString(16).padStart(10, '0');
    const r = hex(randomBytes(8));
    const mac = hex((await hmac256(await this.challengeKey(), utf8(ts + r))).subarray(0, 12));
    return ts + r + mac;
  }

  async authorizeFetch(dest, nonce, sig) {
    if (await this.delegated(dest)) return true;
    if (!/^[0-9a-f]{50}$/.test(nonce || '')) return false;
    const ts = nonce.slice(0, 10), r = nonce.slice(10, 26), mac = nonce.slice(26);
    const want = hex((await hmac256(await this.challengeKey(), utf8(ts + r))).subarray(0, 12));
    if (want !== mac || Date.now() - parseInt(ts, 16) * 1000 > CHALLENGE_TTL) return false;
    const pub = await this.publicFor(dest);
    return !!pub && sig.length === 64 && verifyFetch(pub, nonce, sig);
  }

  async mailbox(dest, after, limit) {
    const { items, cursor } = await this.store.byDest(dest, after, limit);
    return { raws: items.map((i) => i.raw), cursor };
  }

  async receipts(ids) {
    const out = [];
    for (const id of ids) {
      const it = await this.store.get(await receiptID(id), 0);
      if (it) out.push(it.raw);
    }
    return out;
  }

  /** Everything after cursor not from excludeOrigin (federation / bridges). */
  async pull(after, limit, excludeOrigin) {
    const { items, cursor, more } = await this.store.scan(after, limit, { excludeOrigin });
    return { items: items.map((i) => ({ raw: i.raw, region: i.region })), cursor, more };
  }

  /**
   * Packets for a gateway: receipts, DATA for served destinations that
   * delegated fetching, and (carrier) undelivered DATA of this region.
   */
  async gateway(req) {
    const serve = new Set();
    for (const h of req.serve) if (await this.delegated(h)) serve.add(h);
    const { items, cursor } = await this.store.scan(req.after, 500, { excludeOrigin: req.origin, types: [T_DATA, T_RECEIPT] });
    const now = Date.now();
    const out = [];
    let cur = req.after;
    let quota = req.quota;
    for (const it of items) {
      if (out.length >= req.limit) return { raws: out, cursor: cur };
      let include = false;
      if (it.type === T_RECEIPT || serve.has(it.dest)) {
        include = true;
      } else if (req.carrier && it.type === T_DATA) {
        const ann = await this.announceFor(it.dest);
        const home = ann ? ann.item.region : '';
        if (!(ann && home && home !== this.opts.region)) {
          // Online recipients fetch directly first; stop at the quota (retried next call).
          if (now - it.received < CARRIER_MIN_AGE || quota < it.raw.length) return { raws: out, cursor: cur };
          include = true;
          quota -= it.raw.length;
        }
      }
      if (include) out.push(it.raw);
      cur = it.seq;
    }
    return { raws: out, cursor };
  }

  expire() { return this.store.expire(Date.now()); }
}

