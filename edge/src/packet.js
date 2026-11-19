// Packets as relays see them (docs/protocol.md §2, core/packet, core/announce):
// headers, keys, announces, receipts, mailbox-fetch signatures, proof of work.

import { cat, hex, unhex, utf8, readU32be, MAX_PACKET } from './util.js';
import { sha256, verifyDomain } from './crypto.js';

export const T_DATA = 0, T_RECEIPT = 1, T_ANNOUNCE = 2;
const VERSION = 1;
const HEADER = 26;
const CTX_FRAGMENTED = 4;

/** Parses a raw packet: {type, ctx, id (hex), dest (hex), frag, body} or null. */
export function decodePacket(raw) {
  if (raw.length < HEADER || raw.length > MAX_PACKET || raw[0] >> 6 !== VERSION) return null;
  const p = {
    type: (raw[0] >> 3) & 7, ctx: raw[0] & 7,
    idBytes: raw.subarray(2, 10), destBytes: raw.subarray(10, 26),
    frag: 0, body: raw.subarray(HEADER),
  };
  if (p.ctx & CTX_FRAGMENTED) {
    if (p.body.length < 1) return null;
    const idx = p.body[0] >> 4, count = (p.body[0] & 15) + 1;
    if (idx >= count) return null;
    p.frag = idx;
    p.body = p.body.subarray(1);
  }
  p.id = hex(p.idBytes);
  p.dest = hex(p.destBytes);
  return p;
}

/** The packet's store key: id and fragment index. */
export function packetKey(p) { return { pid: p.id, frag: p.frag }; }

/** Public identity from X || Ed (64 bytes): {x, ed, hash}. */
export async function publicFrom(b) {
  if (b.length !== 64) return null;
  return { x: b.slice(0, 32), ed: b.slice(32, 64), hash: hex((await sha256(b)).subarray(0, 16)) };
}

/** Verifies an announce: {pub, time, name, flags, relay} or null (core/announce Parse). */
export async function parseAnnounce(p) {
  if (p.type !== T_ANNOUNCE) return null;
  const b = p.body;
  if (b.length < 64 + 4 + 1 + 64) return null;
  const adLen = b[68];
  if (adLen > 64 || b.length !== 69 + adLen + 64) return null;
  const pub = await publicFrom(b.subarray(0, 64));
  if (!pub || pub.hash !== p.dest) return null;
  const signed = b.subarray(0, 69 + adLen), sig = b.subarray(69 + adLen);
  if (!(await verifyDomain(pub.ed, 'MESHMSG-ANNC', cat(p.destBytes, signed), sig))) return null;
  const a = { pub, time: readU32be(b, 64), name: '', flags: 0, relay: '' };
  let ad = b.subarray(69, 69 + adLen);
  while (ad.length > 0) {
    if (ad.length < 2 || ad.length < 2 + ad[1]) return null;
    const tag = ad[0], val = ad.subarray(2, 2 + ad[1]);
    if (tag === 1) a.name = new TextDecoder().decode(val);
    if (tag === 2 && val.length) a.flags = val[0];
    ad = ad.subarray(2 + ad[1]);
  }
  return a;
}

export const FLAG_DELEGATE = 1;

/** The deterministic packet ID of the receipt for acked (hex). */
export async function receiptID(ackedHex) {
  return hex((await sha256(cat(utf8('MESHMSG-RCPT-ID'), unhex(ackedHex)))).subarray(0, 8));
}

/** The packet a receipt acknowledges (hex), checking its ID; null if malformed. */
export async function receiptAcked(p) {
  if (p.type !== T_RECEIPT || p.body.length !== 8 + 64) return null;
  const acked = hex(p.body.subarray(0, 8));
  return (await receiptID(acked)) === p.id ? acked : null;
}

/** Checks a receipt's signature against the recipient's announced key. */
export async function verifyReceipt(p, pub) {
  if (pub.hash !== p.dest) return false;
  return verifyDomain(pub.ed, 'MESHMSG-RCPT', cat(p.body.subarray(0, 8), p.destBytes), p.body.subarray(8));
}

/** Checks a mailbox-fetch signature over nonce || dest. */
export function verifyFetch(pub, nonce, sig) {
  return verifyDomain(pub.ed, 'MESHMSG-FETCH', cat(utf8(nonce), unhex(pub.hash)), sig);
}

/** Proof of work: SHA-256("MESHMSG-POW" || body || nonce(8, BE)) has need leading zero bits. */
export async function checkPoW(body, nonceStr, need) {
  if (need <= 0) return true;
  if (!/^\d{1,20}$/.test(nonceStr || '')) return false;
  let n = BigInt(nonceStr);
  if (n >= 1n << 64n) return false;
  const nb = new Uint8Array(8);
  for (let i = 7; i >= 0; i--) { nb[i] = Number(n & 255n); n >>= 8n; }
  const d = await sha256(cat(utf8('MESHMSG-POW'), body, nb));
  let bits = 0;
  for (const byte of d) {
    if (byte === 0) { bits += 8; continue; }
    bits += Math.clz32(byte) - 24;
    break;
  }
  return bits >= need;
}

/** Builds a raw DATA packet (one frame, not fragmented). */
export function buildData(idBytes, destBytes, body, priority, maxHops = 8) {
  const flags = (VERSION << 6) | (T_DATA << 3) | (priority ? 1 : 0);
  return cat(Uint8Array.of(flags, maxHops & 15), idBytes, destBytes, body);
}

/** The Noise prologue of a DATA packet: flags & 0xF8 || id || dest. */
export function dataPrologue(idBytes, destBytes) {
  return cat(Uint8Array.of((VERSION << 6) | (T_DATA << 3)), idBytes, destBytes);
}
