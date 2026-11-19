// The relay's trip (docs/trip.md), as server/trips does it: one signed trip
// card at GET /v1/trip, set and ended by the relay's admins who lead it.

import { cat, unhex, utf8, fromUtf8, u32be, b64url } from './util.js';
import { sha256, verifyDomain } from './crypto.js';
import { publicFrom } from './packet.js';

/** The exact text of a top-level JSON member's value (the signed card bytes). */
export function jsonMemberText(text, member) {
  let i = text.indexOf('{');
  if (i < 0) return null;
  i++;
  const skipWs = () => { while (i < text.length && /\s/.test(text[i])) i++; };
  const readString = () => {
    const start = i++;
    while (i < text.length && text[i] !== '"') i += text[i] === '\\' ? 2 : 1;
    i++;
    return JSON.parse(text.slice(start, i));
  };
  const skipValue = () => {
    skipWs();
    const start = i;
    if (text[i] === '"') { readString(); return text.slice(start, i); }
    if (text[i] === '{' || text[i] === '[') {
      let depth = 0;
      while (i < text.length) {
        const ch = text[i];
        if (ch === '"') { readString(); continue; }
        if (ch === '{' || ch === '[') depth++;
        if (ch === '}' || ch === ']') { depth--; if (depth === 0) { i++; break; } }
        i++;
      }
      return text.slice(start, i);
    }
    while (i < text.length && !/[,}\s]/.test(text[i])) i++;
    return text.slice(start, i);
  };
  for (;;) {
    skipWs();
    if (text[i] !== '"') return null;
    const name = readString();
    skipWs();
    if (text[i++] !== ':') return null;
    const v = skipValue();
    if (name === member) return v;
    skipWs();
    if (text[i] === ',') { i++; continue; }
    return null;
  }
}

/** Verifies a signed trip card (core/trip Signed.Open); returns {card, hash} or throws. */
export async function openTripCard(text) {
  const o = JSON.parse(text);
  const cardText = jsonMemberText(text, 'card');
  if (!cardText || typeof o.sig !== 'string') throw new Error('trip: not a signed card');
  const c = JSON.parse(cardText);
  const now = Date.now() / 1000;
  const nameBytes = utf8(c.name || '').length;
  if (c.v !== 1) throw new Error('trip: unknown card version');
  if (!String(c.name || '').trim() || nameBytes > 64) throw new Error('trip: name must be 1–64 bytes');
  if (!Array.isArray(c.leaders) || c.leaders.length < 1 || c.leaders.length > 16) throw new Error('trip: 1–16 leaders');
  if (!(c.every >= 60 && c.every <= 3600)) throw new Error('trip: report interval must be 60–3600 s');
  if (!(c.ends > c.made && c.ends - c.made <= 90 * 86400)) throw new Error('trip: must end after it starts, within 90 days');
  const seen = new Set();
  for (const l of c.leaders) {
    const id = String(l.id || '').toLowerCase();
    if (!/^[0-9a-f]{32}$/.test(id) || seen.has(id) || utf8(l.name || '').length > 64) throw new Error('trip: bad or repeated leader id');
    seen.add(id);
  }
  if (!/^[0-9a-f]{128}$/.test(c.key || '')) throw new Error('trip: key');
  const pub = await publicFrom(unhex(c.key));
  if (!(await verifyDomain(pub.ed, 'mesh-msg trip v1', utf8(cardText), unhex(o.sig)))) throw new Error('trip: bad card signature');
  return { card: c, hash: pub.hash, ended: now >= c.ends };
}

/** Checks the account signature of a trip request; returns the account hash or null. */
export async function tripSigner(request, domain, what) {
  try {
    const pk = unhex(request.headers.get('X-Account') || '');
    const sig = unhex(request.headers.get('X-Signature') || '');
    const day = Number(request.headers.get('X-Day'));
    const today = Math.floor(Date.now() / 86400000);
    if (!Number.isInteger(day) || day + 1 < today || day > today + 1) return null;
    const pub = await publicFrom(pk);
    if (!pub || !(await verifyDomain(pub.ed, domain, cat(u32be(day), what), sig))) return null;
    return pub.hash;
  } catch (e) {
    return null;
  }
}

export async function currentTrip(ctx) {
  const t = await ctx.store.kvGet('trip');
  if (!t) return null;
  try {
    const r = await openTripCard(t);
    return r.ended ? null : { ...r, text: t };
  } catch (e) {
    return null;
  }
}

function tripText(status, body) { return new Response(body, { status, headers: { 'Content-Type': 'text/plain; charset=utf-8' } }); }

export async function tripGet(ctx) {
  const t = await currentTrip(ctx);
  if (!t) return tripText(404, 'no trip on this relay\n');
  return new Response(t.text, { headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-cache' } });
}

export async function tripPut(ctx, request) {
  const body = new Uint8Array(await request.arrayBuffer());
  if (body.length > 32 << 10) return tripText(400, 'bad body');
  const text = fromUtf8(body);
  let r;
  try { r = await openTripCard(text); } catch (e) { return tripText(400, e.message); }
  const acct = await tripSigner(request, 'MESHMSG-TRIP-PUT', await sha256(body));
  if (!acct) return tripText(401, 'signed request required');
  const admins = await ctx.admins();
  if (!admins.includes(acct)) return tripText(403, "only this relay's admins can run a trip on it");
  const c = r.card;
  if (r.ended) return tripText(400, 'this trip has already ended');
  if (c.made > Date.now() / 1000 + 86400) return tripText(400, 'the card is dated in the future');
  if (!c.leaders.some((l) => l.id.toLowerCase() === acct)) return tripText(403, "you are not one of this trip's leaders");
  for (const l of c.leaders) {
    if (!admins.includes(l.id.toLowerCase())) {
      return tripText(403, `every leader must be an admin of this relay; ${l.name || l.id} is not (add them in the relay's admin panel)`);
    }
  }
  const cur = await currentTrip(ctx);
  if (cur && cur.card.key === c.key && c.made <= cur.card.made) return tripText(409, 'the relay has a newer card of this trip');
  await ctx.store.kvPut('trip', text);
  return new Response(null, { status: 204 });
}

export async function tripDelete(ctx, request, url) {
  const h = (url.searchParams.get('trip') || '').toLowerCase();
  if (!/^[0-9a-f]{32}$/.test(h)) return tripText(400, 'trip= required');
  const acct = await tripSigner(request, 'MESHMSG-TRIP-DEL', unhex(h));
  if (!acct) return tripText(401, 'signed request required');
  if (!(await ctx.admins()).includes(acct)) return tripText(403, "only this relay's admins can end its trip");
  const cur = await currentTrip(ctx);
  if (!cur || cur.hash !== h) return tripText(404, 'this trip is not on the relay');
  await ctx.store.kvDel('trip');
  return new Response(null, { status: 204 });
}

/** The admin panel's view of the trip (server/trips PanelState). */
export async function tripPanelState(ctx) {
  const t = await currentTrip(ctx);
  if (!t) return {};
  const admins = await ctx.admins();
  return {
    card: t.card, hash: t.hash, signed: JSON.parse(t.text), link: 'meshtrip:' + b64url(utf8(t.text)),
    leaders: t.card.leaders.map((l) => ({ ...l, admin: admins.includes(l.id.toLowerCase()) })),
  };
}

