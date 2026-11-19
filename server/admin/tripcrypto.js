// Opens trip location reports in the leader's browser (docs/trip.md): the
// relay hands over the encrypted packets, the map key never leaves this page.
// X25519 (RFC 7748), ChaCha20-Poly1305 (RFC 8439), Noise_X_25519_ChaChaPoly_SHA256
// as the Go core seals them (core/e2e), and the report layout of core/trip.
const TripCrypto = (() => {
  const P = (1n << 255n) - 19n;
  const md = (a) => { const r = a % P; return r < 0n ? r + P : r; };
  const le2n = (b) => { let r = 0n; for (let i = b.length - 1; i >= 0; i--) r = (r << 8n) | BigInt(b[i]); return r; };
  const n2le = (n, len) => { const o = new Uint8Array(len); for (let i = 0; i < len; i++) { o[i] = Number(n & 255n); n >>= 8n; } return o; };
  function powmod(b, e) {
    let r = 1n; b = md(b);
    while (e > 0n) { if (e & 1n) r = md(r * b); b = md(b * b); e >>= 1n; }
    return r;
  }
  function x25519(k, u) {
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
    return n2le(md(x2 * powmod(z2, P - 2n)), 32);
  }
  const BASE = (() => { const b = new Uint8Array(32); b[0] = 9; return b; })();

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
  const pad16 = (n) => (16 - (n % 16)) % 16;
  function aeadOpen(key, nonce, data, aad) {
    if (data.length < 16) return null;
    const ct = data.subarray(0, data.length - 16), tag = data.subarray(data.length - 16);
    const otk = chachaBlock(key, 0, nonce).subarray(0, 32);
    const mac = new Uint8Array(aad.length + pad16(aad.length) + ct.length + pad16(ct.length) + 16);
    mac.set(aad, 0);
    mac.set(ct, aad.length + pad16(aad.length));
    const lv = new DataView(mac.buffer, mac.length - 16);
    lv.setUint32(0, aad.length, true); lv.setUint32(8, ct.length, true);
    const want = poly1305(otk, mac);
    let diff = 0;
    for (let i = 0; i < 16; i++) diff |= want[i] ^ tag[i];
    if (diff !== 0) return null;
    const pt = new Uint8Array(ct.length);
    for (let off = 0, ctr = 1; off < ct.length; off += 64, ctr++) {
      const ks = chachaBlock(key, ctr, nonce);
      for (let i = 0; i < 64 && off + i < ct.length; i++) pt[off + i] = ct[off + i] ^ ks[i];
    }
    return pt;
  }

  const cat = (...a) => { const o = new Uint8Array(a.reduce((n, x) => n + x.length, 0)); let i = 0; for (const x of a) { o.set(x, i); i += x.length; } return o; };
  const subtle = globalThis.crypto.subtle;
  const sha256 = async (b) => new Uint8Array(await subtle.digest('SHA-256', b));
  async function hmac(key, data) {
    const k = await subtle.importKey('raw', key, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
    return new Uint8Array(await subtle.sign('HMAC', k, data));
  }
  async function hkdf2(ck, ikm) {
    const t = await hmac(ck, ikm);
    const o1 = await hmac(t, Uint8Array.of(1));
    const o2 = await hmac(t, cat(o1, Uint8Array.of(2)));
    return [o1, o2];
  }
  const NONCE0 = new Uint8Array(12);
  const NAME = new TextEncoder().encode('Noise_X_25519_ChaChaPoly_SHA256');

  // openX decrypts a Noise X message to the static key priv/pub.
  async function openX(priv, pub, prologue, msg) {
    if (msg.length < 96) return null;
    let h = new Uint8Array(32); h.set(NAME);
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
    if (!pt) return null;
    return { senderX: rs, payload: pt };
  }

  const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
  const unhex = (s) => { const o = new Uint8Array(s.length / 2); for (let i = 0; i < o.length; i++) o[i] = parseInt(s.substr(i * 2, 2), 16); return o; };

  // openReport opens one raw packet (Uint8Array) for the trip; null if it is
  // not a report this key opens.
  async function openReport(priv, pub, raw) {
    if (raw.length < 26 + 96 + 32 + 5) return null;
    if ((raw[0] >> 6) !== 1 || ((raw[0] >> 3) & 7) !== 0 || (raw[0] & 4)) return null; // v1 DATA, not fragmented
    const prologue = cat(Uint8Array.of(raw[0] & 0xf8), raw.subarray(2, 26));
    const o = await openX(priv, pub, prologue, raw.subarray(26));
    if (!o || o.payload.length < 32 + 5) return null;
    const ed = o.payload.subarray(0, 32), inner = o.payload.subarray(32);
    if (inner[0] !== 10) return null; // e2e.ContentTrip
    const dv = new DataView(inner.buffer, inner.byteOffset, inner.length);
    const sent = dv.getUint32(1, false);
    const c = inner.subarray(5);
    if (c.length < 16 || c[0] !== 1) return null; // a report
    const cv = new DataView(c.buffer, c.byteOffset, c.length);
    const nameLen = c[15];
    const id = (await sha256(cat(o.senderX, ed))).subarray(0, 16);
    return {
      id: hex(raw.subarray(2, 10)), member: hex(id), key: hex(cat(o.senderX, ed)), sent,
      flags: c[1], lat: cv.getInt32(2, false) / 1e7, lon: cv.getInt32(6, false) / 1e7,
      accuracy: cv.getUint16(10, false), fixAge: cv.getUint16(12, false),
      battery: c[14] === 255 ? -1 : c[14],
      name: new TextDecoder().decode(c.subarray(16, 16 + nameLen)),
    };
  }

  return { x25519, BASE, openX, openReport, hex, unhex, aeadOpen, chachaBlock, poly1305 };
})();
if (typeof module !== 'undefined') module.exports = TripCrypto;
