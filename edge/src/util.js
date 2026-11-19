// Byte helpers and the relay's binary codecs (core/packet frames, core/wire
// gateway and sync bodies). Plain functions: the build joins every module
// into one script, so names are unique across src/.

const TE = new TextEncoder();
const TD = new TextDecoder();

export function utf8(s) { return TE.encode(s); }
export function fromUtf8(b) { return TD.decode(b); }

export function cat(...parts) {
  let n = 0;
  for (const p of parts) n += p.length;
  const out = new Uint8Array(n);
  let i = 0;
  for (const p of parts) { out.set(p, i); i += p.length; }
  return out;
}

export function hex(b) {
  let s = '';
  for (let i = 0; i < b.length; i++) s += b[i].toString(16).padStart(2, '0');
  return s;
}

export function unhex(s) {
  if (typeof s !== 'string' || s.length % 2 || /[^0-9a-fA-F]/.test(s)) throw new Error('bad hex');
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.substr(i * 2, 2), 16);
  return out;
}

export function b64(b) {
  let s = '';
  for (let i = 0; i < b.length; i += 0x8000) s += String.fromCharCode.apply(null, b.subarray(i, i + 0x8000));
  return btoa(s);
}

export function unb64(s) {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function b64url(b) { return b64(b).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, ''); }
export function unb64url(s) {
  s = s.replace(/-/g, '+').replace(/_/g, '/');
  while (s.length % 4) s += '=';
  return unb64(s);
}

export function equalBytes(a, b) {
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a[i] ^ b[i];
  return d === 0;
}

export function randomBytes(n) {
  const b = new Uint8Array(n);
  crypto.getRandomValues(b);
  return b;
}

export function u32be(n) { return Uint8Array.of((n >>> 24) & 255, (n >>> 16) & 255, (n >>> 8) & 255, n & 255); }
export function readU32be(b, i) { return ((b[i] << 24) | (b[i + 1] << 16) | (b[i + 2] << 8) | b[i + 3]) >>> 0; }

export const MAX_PACKET = 64 * 1024;

// Frames: len(4, big-endian) || raw, repeated (core/packet frame.go).
export function encodeFrames(raws) {
  return cat(...raws.flatMap((r) => [u32be(r.length), r]));
}

export function decodeFrames(b) {
  const out = [];
  let i = 0;
  while (i < b.length) {
    if (b.length - i < 4) throw new Error('short frame');
    const n = readU32be(b, i);
    if (n > MAX_PACKET) throw new Error('frame too large');
    if (b.length - i - 4 < n) throw new Error('short frame');
    out.push(b.slice(i + 4, i + 4 + n));
    i += 4 + n;
  }
  return out;
}

// Gateway body: count(2) || count × dest(16) || frames (core/wire).
export function decodeGateway(b) {
  if (b.length < 2) throw new Error('short gateway body');
  const n = (b[0] << 8) | b[1];
  if (b.length < 2 + n * 16) throw new Error('short gateway serve list');
  const serve = [];
  for (let i = 0; i < n; i++) serve.push(hex(b.subarray(2 + i * 16, 18 + i * 16)));
  return { serve, upload: decodeFrames(b.subarray(2 + n * 16)) };
}

// Sync items: frames of region_len(1) || region || raw (core/wire).
export function encodeSync(items) {
  return encodeFrames(items.map((it) => {
    const r = utf8(it.region || '').subarray(0, 255);
    return cat(Uint8Array.of(r.length), r, it.raw);
  }));
}

export function decodeSync(b) {
  return decodeFrames(b).map((f) => {
    if (f.length < 1 || f.length < 1 + f[0]) throw new Error('bad sync item');
    return { region: fromUtf8(f.subarray(1, 1 + f[0])), raw: f.slice(1 + f[0]) };
  });
}

export function sleep(ms) { return new Promise((r) => setTimeout(r, ms)); }
