// The admin panel at /admin (server/admin in Go, the same page): admins log
// in with a one-time code the relay sends to their Mava account as an
// end-to-end encrypted message. Sessions, codes and settings live in the
// store's key-value table, since edge instances share no memory.

import { cat, hex, unhex, utf8, randomBytes, u32be, b64 } from './util.js';
import { noiseXSeal } from './crypto.js';
import { buildData, dataPrologue } from './packet.js';
import { tripPanelState, currentTrip } from './trips.js';

// The network owner's account: an admin of every relay built from this code
// (server/admin.OwnerID).
export const OWNER_ID = 'ad2107c8e944bf2a99b3f0cdb2c40903';
const COOKIE = 'mava_admin';
const SESSION_TTL = 12 * 3600 * 1000;
const CODE_TTL = 10 * 60 * 1000;

function adminJSON(status, v, headers = {}) {
  return new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store', ...headers } });
}
const adminFail = (status, msg) => adminJSON(status, { error: msg });

function parseAccountID(s) {
  const id = String(s || '').trim().toLowerCase();
  if (!/^[0-9a-f]{32}$/.test(id)) throw new Error('An account id is 32 hex characters (Mava → Me).');
  return id;
}

function cookieOf(request, name) {
  for (const part of (request.headers.get('Cookie') || '').split(';')) {
    const [k, ...v] = part.trim().split('=');
    if (k === name) return v.join('=');
  }
  return '';
}

async function sessionOf(ctx, request) {
  const tok = cookieOf(request, COOKIE);
  if (!/^[0-9a-f]{64}$/.test(tok)) return null;
  const id = await ctx.store.kvGet('session:' + tok);
  return id && (await ctx.admins()).includes(id) ? id : null;
}

const PAGE_HEADERS = {
  'Content-Type': 'text/html; charset=utf-8',
  'X-Frame-Options': 'DENY',
  'Cache-Control': 'no-store',
};

/** Routes /admin…; null if the path isn't the panel's. */
export async function adminRoute(ctx, request, url) {
  const path = url.pathname;
  const method = request.method;
  if (method === 'GET' && (path === '/admin' || path === '/admin/')) {
    return new Response(PANEL_HTML, { headers: { ...PAGE_HEADERS, 'Referrer-Policy': 'no-referrer',
      'Content-Security-Policy': "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'" } });
  }
  if (method === 'GET' && path === '/admin/trip') {
    return new Response(TRIPMAP_HTML.replace('/*TRIPCRYPTO*/', TRIPCRYPTO_JS), { headers: { ...PAGE_HEADERS, 'Referrer-Policy': 'strict-origin',
      'Content-Security-Policy': "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data: https://tile.openstreetmap.org; base-uri 'none'; form-action 'none'; frame-ancestors 'none'" } });
  }
  if (!path.startsWith('/admin/api/')) return null;
  const name = path.slice('/admin/api/'.length);
  if (method === 'POST') {
    if (request.headers.get('X-Mava-Panel') !== '1') return adminFail(403, 'missing X-Mava-Panel header');
    let body = {};
    try { body = await request.json(); } catch (e) { /* empty */ }
    if (name === 'code') return sendCode(ctx, body);
    if (name === 'login') return login(ctx, body, url);
    const you = await sessionOf(ctx, request);
    if (!you) return adminFail(401, 'log in');
    if (name === 'logout') {
      await ctx.store.kvDel('session:' + cookieOf(request, COOKIE));
      return new Response(null, { status: 204, headers: { 'Set-Cookie': `${COOKIE}=; Path=/admin; Max-Age=0; HttpOnly; SameSite=Strict` } });
    }
    if (name === 'settings') return saveSettings(ctx, body);
    if (name === 'admins') return changeAdmins(ctx, body);
    if (name === 'restart') return adminJSON(200, { info: 'An edge relay has nothing to restart: saved settings apply at once.' });
    return adminFail(404, 'not on an edge relay');
  }
  if (method === 'GET') {
    const you = await sessionOf(ctx, request);
    if (!you) return adminFail(401, 'log in');
    if (name === 'state') return state(ctx, you, url);
    if (name === 'trip') return adminJSON(200, await tripPanelState(ctx));
    if (name === 'trip/reports') return tripReports(ctx, url);
  }
  return adminFail(404, 'not found');
}

async function sendCode(ctx, body) {
  let id;
  try { id = parseAccountID(body.id); } catch (e) { return adminFail(400, e.message); }
  // Same answer for non-admins: the panel doesn't tell who is an admin.
  const sent = { info: 'If this account is an admin of this relay, a code is on its way to it in Mava. It can take a minute.' };
  if (!(await ctx.admins()).includes(id)) return adminJSON(200, sent);
  if (await ctx.store.kvGet('code-wait:' + id)) return adminFail(429, 'A code was just sent. Wait a minute before asking again.');
  const pub = await ctx.relay.publicFor(id);
  if (!pub) return adminFail(409, "This relay doesn't know this account's keys yet. Open Mava with this relay in its relay list (it announces itself), then try again.");
  const code = Array.from(randomBytes(8), (b) => String(b % 10)).join('');
  await ctx.store.kvPut('code:' + id, JSON.stringify({ code, tries: 0 }), CODE_TTL);
  await ctx.store.kvPut('code-wait:' + id, '1', 60 * 1000);
  const me = await ctx.identity();
  const text = `Admin code for relay ${ctx.cfg.name}: ${code} (valid 10 min)`;
  const inner = cat(Uint8Array.of(0), u32be(Math.floor(Date.now() / 1000)), utf8(text));
  const idBytes = randomBytes(8), destBytes = unhex(id);
  const sealed = await noiseXSeal(me.xPriv, me.xPub, pub.x, dataPrologue(idBytes, destBytes), cat(me.edPub, inner));
  await ctx.relay.ingestOne(buildData(idBytes, destBytes, sealed, true), 'local', '', Date.now());
  return adminJSON(200, sent);
}

async function login(ctx, body, url) {
  let id;
  try { id = parseAccountID(body.id); } catch (e) { return adminFail(400, e.message); }
  const raw = await ctx.store.kvGet('code:' + id);
  const pc = raw ? JSON.parse(raw) : null;
  let ok = false;
  if (pc && pc.tries < 5 && (await ctx.admins()).includes(id)) {
    ok = String(body.code || '').trim() === pc.code;
    if (ok) await ctx.store.kvDel('code:' + id);
    else await ctx.store.kvPut('code:' + id, JSON.stringify({ code: pc.code, tries: pc.tries + 1 }), CODE_TTL);
  }
  if (!ok) return adminFail(401, 'Wrong or expired code. After 5 wrong tries, ask for a new one.');
  const tok = hex(randomBytes(32));
  await ctx.store.kvPut('session:' + tok, id, SESSION_TTL);
  const secure = url.protocol === 'https:' ? '; Secure' : '';
  return adminJSON(200, { admin: id }, { 'Set-Cookie': `${COOKIE}=${tok}; Path=/admin; Max-Age=${SESSION_TTL / 1000}; HttpOnly; SameSite=Strict${secure}` });
}

async function state(ctx, you, url) {
  const me = await ctx.identity();
  const s = await ctx.settings();
  const counts = await ctx.store.count();
  const started = Number((await ctx.store.kvGet('started')) || Date.now());
  const trip = await tripPanelState(ctx);
  return adminJSON(200, {
    you, owner: OWNER_ID, admins: await ctx.admins(), edge: ctx.platform,
    info: {
      Name: ctx.cfg.name, Region: ctx.cfg.region, Version: EDGE_VERSION + ' (' + ctx.platform + ')', PubKey: hex(me.bytes),
      Listen: url.origin, Peers: ctx.cfg.peers.map((p) => p.name), Started: new Date(started).toISOString(),
    },
    uptime: Math.floor((Date.now() - started) / 1000),
    stats: { Accepted: counts.total < 0 ? '—' : counts.total, Duplicate: '—', Rejected: '—', Purged: '—' },
    current: { pow_bits: s.powBits, ttl_hours: s.ttlHours, log: false, media_max_mb: 0, turn_enabled: false, media_accounts: [] },
    saved: s.saved, secure: url.protocol === 'https:',
    media: { on: false, used: 0, cap: 0, count: 0, files: [] },
    ledger: {}, turn: null, trip: trip.card ? trip : null,
  });
}

async function saveSettings(ctx, body) {
  const s = await ctx.settings();
  const saved = { ...s.saved };
  for (const [k, lo, hi] of [['pow_bits', 0, 28], ['ttl_hours', 1, 720]]) {
    if (body[k] === undefined || body[k] === null) { delete saved[k]; continue; }
    const v = Number(body[k]);
    if (!Number.isInteger(v) || v < lo || v > hi) return adminFail(400, `${k} must be ${lo}…${hi}`);
    saved[k] = v;
  }
  await ctx.store.kvPut('settings', JSON.stringify(saved));
  return adminJSON(200, { info: 'Saved; it applies at once. (Media and TURN are not available on an edge relay.)' });
}

async function changeAdmins(ctx, body) {
  let list = JSON.parse((await ctx.store.kvGet('admins')) || '[]');
  try {
    if (body.add) list.push(parseAccountID(body.add));
    if (body.remove) {
      const id = parseAccountID(body.remove);
      if (id === OWNER_ID) return adminFail(403, "The owner can't be removed.");
      if (ctx.cfg.admins.includes(id)) return adminFail(403, "This admin is set in the relay's configuration (installer); change it there.");
      list = list.filter((a) => a !== id);
    }
  } catch (e) {
    return adminFail(400, e.message);
  }
  list = [...new Set(list)].filter((a) => a !== OWNER_ID);
  await ctx.store.kvPut('admins', JSON.stringify(list));
  return adminJSON(200, { admins: await ctx.admins() });
}

async function tripReports(ctx, url) {
  const t = await currentTrip(ctx);
  if (!t) return adminFail(404, 'no trip');
  const after = Number(url.searchParams.get('after') || 0) || 0;
  const limit = 200;
  const { raws, cursor } = await ctx.relay.mailbox(t.hash, after, limit);
  return adminJSON(200, { packets: raws.map(b64), cursor, more: raws.length === limit });
}
