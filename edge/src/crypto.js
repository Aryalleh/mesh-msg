// Crypto for the edge relay: SHA-2 and HMAC from WebCrypto; Ed25519 from
// WebCrypto when the runtime has it, else in JavaScript (BigInt); X25519 and
// ChaCha20-Poly1305 in JavaScript (only the admin panel's login code needs
// them); Noise_X_25519_ChaChaPoly_SHA256 sealing as core/e2e does it.

import { cat, equalBytes, randomBytes, utf8 } from './util.js';

const subtle = crypto.subtle;

export async function sha256(b) { return new Uint8Array(await subtle.digest('SHA-256', b)); }
export async function sha512(b) { return new Uint8Array(await subtle.digest('SHA-512', b)); }

export async function hmac256(key, data) {
  const k = await subtle.importKey('raw', key, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
  return new Uint8Array(await subtle.sign('HMAC', k, data));
}

// ---- field arithmetic mod 2^255-19 ----
const P = (1n << 255n) - 19n;
const md = (a) => { const r = a % P; return r < 0n ? r + P : r; };
function powmod(b, e, m = P) {
  let r = 1n;
  b %= m; if (b < 0n) b += m;
  while (e > 0n) { if (e & 1n) r = (r * b) % m; b = (b * b) % m; e >>= 1n; }
  return r;
}
const inv = (a) => powmod(a, P - 2n);
export function le2n(b) { let r = 0n; for (let i = b.length - 1; i >= 0; i--) r = (r << 8n) | BigInt(b[i]); return r; }
export function n2le(n, len) { const o = new Uint8Array(len); for (let i = 0; i < len; i++) { o[i] = Number(n & 255n); n >>= 8n; } return o; }

// ---- X25519 (RFC 7748) ----
export function x25519(k, u) {
  const ks = Uint8Array.from(k); ks[0] &= 248; ks[31] &= 127; ks[31] |= 64;
  const sc = le2n(ks);
  const ub = Uint8Array.from(u); ub[31] &= 127;
  const x1 = md(le2n(ub));
  let x2 = 1n, z2 = 0n, x3 = x1, z3 = 1n, swap = 0n;
  for (let t = 254; t >= 0; t--) {
    const kt = (sc >> BigInt(t)) & 1n;
    swap ^= kt;
    if (swap) { [x2, x3] = [x3, x2]; [z2, z3] = [z3, z2]; }
    swap = kt;
    const A = md(x2 + z2), AA = md(A * A), B = md(x2 - z2), BB = md(B * B), E = md(AA - BB);
    const C = md(x3 + z3), D = md(x3 - z3), DA = md(D * A), CB = md(C * B);
    x3 = md((DA + CB) * (DA + CB));
    z3 = md(x1 * md((DA - CB) * (DA - CB)));
    x2 = md(AA * BB);
    z2 = md(E * md(AA + 121665n * E));
  }
  if (swap) { [x2, x3] = [x3, x2]; [z2, z3] = [z3, z2]; }
  return n2le(md(x2 * inv(z2)), 32);
}
export const X25519_BASE = (() => { const b = new Uint8Array(32); b[0] = 9; return b; })();

// ---- ChaCha20-Poly1305 (RFC 8439) ----
function chachaBlock(key, counter, nonce) {
  const c = new Uint32Array(16);
  c[0] = 0x61707865; c[1] = 0x3320646e; c[2] = 0x79622d32; c[3] = 0x6b206574;
  const kv = new DataView(key.buffer, key.byteOffset, 32);
  for (let i = 0; i < 8; i++) c[4 + i] = kv.getUint32(i * 4, true);
  c[12] = counter;
  const nv = new DataView(nonce.buffer, nonce.byteOffset, 12);
  for (let i = 0; i < 3; i++) c[13 + i] = nv.getUint32(i * 4, true);
  const x = c.slice();
  const rotl = (v, n) => (v << n) | (v >>> (32 - n));
  const qr = (a, b, cc, d) => {
    x[a] += x[b]; x[d] = rotl(x[d] ^ x[a], 16);
    x[cc] += x[d]; x[b] = rotl(x[b] ^ x[cc], 12);
    x[a] += x[b]; x[d] = rotl(x[d] ^ x[a], 8);
    x[cc] += x[d]; x[b] = rotl(x[b] ^ x[cc], 7);
  };
  for (let i = 0; i < 10; i++) {
    qr(0, 4, 8, 12); qr(1, 5, 9, 13); qr(2, 6, 10, 14); qr(3, 7, 11, 15);
    qr(0, 5, 10, 15); qr(1, 6, 11, 12); qr(2, 7, 8, 13); qr(3, 4, 9, 14);
  }
  const out = new Uint8Array(64), ov = new DataView(out.buffer);
  for (let i = 0; i < 16; i++) ov.setUint32(i * 4, (x[i] + c[i]) >>> 0, true);
  return out;
}
function chachaXor(key, nonce, data) {
  const out = new Uint8Array(data.length);
  for (let off = 0, ctr = 1; off < data.length; off += 64, ctr++) {
    const ks = chachaBlock(key, ctr, nonce);
    for (let i = 0; i < 64 && off + i < data.length; i++) out[off + i] = data[off + i] ^ ks[i];
  }
  return out;
}
function poly1305(key, msg) {
  const r = le2n(key.subarray(0, 16)) & 0x0ffffffc0ffffffc0ffffffc0fffffffn;
  const s = le2n(key.subarray(16, 32));
  const p = (1n << 130n) - 5n;
  let acc = 0n;
  for (let i = 0; i < msg.length; i += 16) {
    const blk = msg.subarray(i, Math.min(i + 16, msg.length));
    acc = ((acc + le2n(blk) + (1n << BigInt(8 * blk.length))) * r) % p;
  }
  return n2le((acc + s) & ((1n << 128n) - 1n), 16);
}
function macData(aad, ct) {
  const pad = (n) => (16 - (n % 16)) % 16;
  const m = new Uint8Array(aad.length + pad(aad.length) + ct.length + pad(ct.length) + 16);
  m.set(aad, 0);
  m.set(ct, aad.length + pad(aad.length));
  const lv = new DataView(m.buffer, m.length - 16);
  lv.setUint32(0, aad.length, true); lv.setUint32(8, ct.length, true);
  return m;
}
export function aeadSeal(key, nonce, pt, aad) {
  const ct = chachaXor(key, nonce, pt);
  const tag = poly1305(chachaBlock(key, 0, nonce).subarray(0, 32), macData(aad, ct));
  return cat(ct, tag);
}
export function aeadOpen(key, nonce, data, aad) {
  if (data.length < 16) return null;
  const ct = data.subarray(0, data.length - 16);
  const tag = poly1305(chachaBlock(key, 0, nonce).subarray(0, 32), macData(aad, ct));
  if (!equalBytes(tag, data.subarray(data.length - 16))) return null;
  return chachaXor(key, nonce, ct);
}

// ---- Noise_X_25519_ChaChaPoly_SHA256, initiator (core/e2e Seal, first contact) ----
const NOISE_X = utf8('Noise_X_25519_ChaChaPoly_SHA256');
const NONCE0 = new Uint8Array(12);
async function hkdf2(ck, ikm) {
  const t = await hmac256(ck, ikm);
  const o1 = await hmac256(t, Uint8Array.of(1));
  const o2 = await hmac256(t, cat(o1, Uint8Array.of(2)));
  return [o1, o2];
}

/** Seals payload from the static key (sPriv, sPub) to the recipient's X25519 key rs. */
export async function noiseXSeal(sPriv, sPub, rs, prologue, payload) {
  let h = new Uint8Array(32); h.set(NOISE_X);
  let ck = h.slice();
  h = await sha256(cat(h, prologue));
  h = await sha256(cat(h, rs));
  const ePriv = randomBytes(32);
  const ePub = x25519(ePriv, X25519_BASE);
  h = await sha256(cat(h, ePub));
  let k;
  [ck, k] = await hkdf2(ck, x25519(ePriv, rs));
  const encS = aeadSeal(k, NONCE0, sPub, h);
  h = await sha256(cat(h, encS));
  [ck, k] = await hkdf2(ck, x25519(sPriv, rs));
  const ct = aeadSeal(k, NONCE0, payload, h);
  return cat(ePub, encS, ct);
}

/** Opens a Noise X message to (priv, pub); returns {senderX, payload} or null. */
export async function noiseXOpen(priv, pub, prologue, msg) {
  if (msg.length < 96) return null;
  let h = new Uint8Array(32); h.set(NOISE_X);
  let ck = h.slice();
  h = await sha256(cat(h, prologue));
  h = await sha256(cat(h, pub));
  const re = msg.subarray(0, 32);
  h = await sha256(cat(h, re));
  let k;
  [ck, k] = await hkdf2(ck, x25519(priv, re));
  const encS = msg.subarray(32, 80);
  const rs = aeadOpen(k, NONCE0, encS, h);
  if (!rs) return null;
  h = await sha256(cat(h, encS));
  [ck, k] = await hkdf2(ck, x25519(priv, rs));
  const pt = aeadOpen(k, NONCE0, msg.subarray(80), h);
  return pt ? { senderX: rs, payload: pt } : null;
}

// ---- Ed25519 (RFC 8032) ----
const L = (1n << 252n) + 27742317777372353535851937790883648493n;
const D = md(-121665n * inv(121666n));
const SQRT_M1 = powmod(2n, (P - 1n) / 4n);
const BY = md(4n * inv(5n));
const BX = recoverX(BY, 0);
const BASE = [BX, BY, 1n, md(BX * BY)];

function recoverX(y, sign) {
  const y2 = md(y * y);
  const x2 = md((y2 - 1n) * inv(D * y2 + 1n));
  if (x2 === 0n) return sign ? null : 0n;
  let x = powmod(x2, (P + 3n) / 8n);
  if (md(x * x - x2) !== 0n) x = md(x * SQRT_M1);
  if (md(x * x - x2) !== 0n) return null;
  if (Number(x & 1n) !== sign) x = P - x;
  return x;
}
function edAdd(p, q) {
  const [X1, Y1, Z1, T1] = p, [X2, Y2, Z2, T2] = q;
  const A = md((Y1 - X1) * (Y2 - X2)), B = md((Y1 + X1) * (Y2 + X2));
  const C = md(2n * D * T1 * T2), Dd = md(2n * Z1 * Z2);
  const E = B - A, F = Dd - C, G = Dd + C, H = B + A;
  return [md(E * F), md(G * H), md(F * G), md(E * H)];
}
function edMul(s, p) {
  let q = [0n, 1n, 1n, 0n];
  for (; s > 0n; s >>= 1n) { if (s & 1n) q = edAdd(q, p); p = edAdd(p, p); }
  return q;
}
function edEncode(p) {
  const zi = inv(p[2]);
  const x = md(p[0] * zi), y = md(p[1] * zi);
  const b = n2le(y, 32);
  b[31] |= Number(x & 1n) << 7;
  return b;
}
function edDecode(b) {
  if (b.length !== 32) return null;
  const c = Uint8Array.from(b);
  const sign = c[31] >> 7; c[31] &= 127;
  const y = le2n(c);
  if (y >= P) return null;
  const x = recoverX(y, sign);
  return x === null ? null : [x, y, 1n, md(x * y)];
}

async function jsVerify(pub, msg, sig) {
  if (sig.length !== 64 || pub.length !== 32) return false;
  const A = edDecode(pub), R = edDecode(sig.subarray(0, 32));
  const S = le2n(sig.subarray(32));
  if (!A || !R || S >= L) return false;
  const k = le2n(await sha512(cat(sig.subarray(0, 32), pub, msg))) % L;
  return equalBytes(edEncode(edMul(S, BASE)), edEncode(edAdd(R, edMul(k, A))));
}

async function jsKeys(seed) {
  const h = await sha512(seed);
  const a = Uint8Array.from(h.subarray(0, 32)); a[0] &= 248; a[31] &= 127; a[31] |= 64;
  const s = le2n(a);
  return { s, prefix: h.subarray(32), pub: edEncode(edMul(s, BASE)) };
}
async function jsSign(seed, msg) {
  const { s, prefix, pub } = await jsKeys(seed);
  const r = le2n(await sha512(cat(prefix, msg))) % L;
  const R = edEncode(edMul(r, BASE));
  const k = le2n(await sha512(cat(R, pub, msg))) % L;
  return cat(R, n2le((r + k * s) % L, 32));
}

// WebCrypto Ed25519 when the runtime has it (fast, native); else JavaScript.
let nativeEd = null; // null: not tested yet
async function hasNativeEd() {
  if (nativeEd !== null) return nativeEd;
  try {
    const k = await subtle.importKey('raw', new Uint8Array(32).fill(1), { name: 'Ed25519' }, false, ['verify']);
    await subtle.verify({ name: 'Ed25519' }, k, new Uint8Array(64), new Uint8Array(1));
    nativeEd = true;
  } catch (e) {
    nativeEd = false;
  }
  return nativeEd;
}
const PKCS8_ED = Uint8Array.of(0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20);

export async function edVerify(pub, msg, sig) {
  if (await hasNativeEd()) {
    try {
      const k = await subtle.importKey('raw', pub, { name: 'Ed25519' }, false, ['verify']);
      return await subtle.verify({ name: 'Ed25519' }, k, sig, msg);
    } catch (e) {
      return false;
    }
  }
  return jsVerify(pub, msg, sig);
}

export async function edSign(seed, msg) {
  if (await hasNativeEd()) {
    try {
      const k = await subtle.importKey('pkcs8', cat(PKCS8_ED, seed), { name: 'Ed25519' }, false, ['sign']);
      return new Uint8Array(await subtle.sign({ name: 'Ed25519' }, k, msg));
    } catch (e) { /* fall through to JavaScript */ }
  }
  return jsSign(seed, msg);
}

export async function edPublic(seed) { return (await jsKeys(seed)).pub; }

/** Signature under a domain tag, as identity.SignDomain / Public.Verify. */
export function verifyDomain(edPub, domain, msg, sig) { return edVerify(edPub, cat(utf8(domain), msg), sig); }
export function signDomain(edSeed, domain, msg) { return edSign(edSeed, cat(utf8(domain), msg)); }

// Exposed for tests: the JavaScript paths regardless of the runtime.
export const edJS = { verify: jsVerify, sign: jsSign };
