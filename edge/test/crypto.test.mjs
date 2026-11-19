// Edge relay crypto against Node's own (node --test edge/test).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import nodeCrypto from 'node:crypto';
import { x25519, X25519_BASE, edJS, edVerify, edSign, edPublic, noiseXSeal, noiseXOpen, aeadSeal, aeadOpen } from '../src/crypto.js';
import { hex, unhex, randomBytes } from '../src/util.js';

test('ed25519 RFC 8032 vector 1, JavaScript and native', async () => {
  const seed = unhex('9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60');
  const pub = 'd75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a';
  const sig = 'e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e065224901555fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b';
  assert.equal(hex(await edPublic(seed)), pub);
  assert.equal(hex(await edJS.sign(seed, new Uint8Array(0))), sig);
  assert.equal(hex(await edSign(seed, new Uint8Array(0))), sig);
  assert.ok(await edJS.verify(unhex(pub), new Uint8Array(0), unhex(sig)));
  assert.ok(await edVerify(unhex(pub), new Uint8Array(0), unhex(sig)));
  const bad = unhex(sig); bad[5] ^= 1;
  assert.ok(!(await edJS.verify(unhex(pub), new Uint8Array(0), bad)));
  assert.ok(!(await edVerify(unhex(pub), new Uint8Array(0), bad)));
});

test('ed25519 random messages agree with node:crypto', async () => {
  for (let i = 0; i < 5; i++) {
    const { publicKey, privateKey } = nodeCrypto.generateKeyPairSync('ed25519');
    const msg = randomBytes(i * 37);
    const sig = nodeCrypto.sign(null, msg, privateKey);
    const raw = publicKey.export({ format: 'der', type: 'spki' }).subarray(12);
    assert.ok(await edJS.verify(new Uint8Array(raw), msg, new Uint8Array(sig)));
    const seed = privateKey.export({ format: 'der', type: 'pkcs8' }).subarray(16);
    assert.equal(hex(await edJS.sign(new Uint8Array(seed), msg)), hex(sig));
  }
});

test('x25519 agrees with node:crypto', () => {
  const a = nodeCrypto.generateKeyPairSync('x25519');
  const b = nodeCrypto.generateKeyPairSync('x25519');
  const aPriv = new Uint8Array(a.privateKey.export({ format: 'der', type: 'pkcs8' }).subarray(16));
  const bPub = new Uint8Array(b.publicKey.export({ format: 'der', type: 'spki' }).subarray(12));
  const aPub = new Uint8Array(a.publicKey.export({ format: 'der', type: 'spki' }).subarray(12));
  assert.equal(hex(x25519(aPriv, X25519_BASE)), hex(aPub));
  const shared = nodeCrypto.diffieHellman({ privateKey: a.privateKey, publicKey: b.publicKey });
  assert.equal(hex(x25519(aPriv, bPub)), hex(shared));
});

test('chacha20-poly1305 agrees with node:crypto', () => {
  const key = randomBytes(32), nonce = randomBytes(12), aad = randomBytes(13), pt = randomBytes(150);
  const c = nodeCrypto.createCipheriv('chacha20-poly1305', key, nonce, { authTagLength: 16 });
  c.setAAD(aad);
  const want = Buffer.concat([c.update(pt), c.final(), c.getAuthTag()]);
  assert.equal(hex(aeadSeal(key, nonce, pt, aad)), hex(want));
  assert.equal(hex(aeadOpen(key, nonce, aeadSeal(key, nonce, pt, aad), aad)), hex(pt));
});

test('noise X seal opens', async () => {
  const s = randomBytes(32), r = randomBytes(32);
  const sPub = x25519(s, X25519_BASE), rPub = x25519(r, X25519_BASE);
  const msg = await noiseXSeal(s, sPub, rPub, Uint8Array.of(1, 2, 3), Uint8Array.of(9, 9));
  const o = await noiseXOpen(r, rPub, Uint8Array.of(1, 2, 3), msg);
  assert.equal(hex(o.senderX), hex(sPub));
  assert.equal(hex(o.payload), '0909');
});
