// Federation from the edge (relayclient.SyncOnce in Go): on a schedule, pull
// what each peer relay has and push what we have. Pushing needs the peer's
// sync token (we don't spend CPU on proof of work); without one, the peer
// pulls from us instead (list this relay among its peers).

import { encodeSync, decodeSync } from './util.js';

const SYNC_PAGE = 500;
const SYNC_ROUNDS = 4;

async function peerFetch(ctx, peer, path, opts = {}) {
  const headers = { 'X-Origin': ctx.cfg.name, 'X-Region': ctx.cfg.region, ...(opts.headers || {}) };
  if (peer.token) headers.Authorization = 'Bearer ' + peer.token;
  const ac = new AbortController();
  const t = setTimeout(() => ac.abort(), 25000);
  try {
    return await fetch(peer.url.replace(/\/+$/, '') + path, { ...opts, headers, signal: ac.signal });
  } finally {
    clearTimeout(t);
  }
}

/** One sync round with one peer; returns {pulled, pushed}. */
export async function syncPeer(ctx, peer) {
  const ckey = 'cursor:' + peer.name;
  const cur = JSON.parse((await ctx.store.kvGet(ckey)) || '{"a":0,"b":0}');
  let pulled = 0, pushed = 0;
  // Theirs → ours.
  for (let i = 0; i < SYNC_ROUNDS; i++) {
    const r = await peerFetch(ctx, peer, `/v1/sync/pull?after=${cur.b}&limit=${SYNC_PAGE}`);
    if (!r.ok) throw new Error(`pull from ${peer.name}: HTTP ${r.status}`);
    const items = decodeSync(new Uint8Array(await r.arrayBuffer()));
    pulled += await ctx.relay.ingestSync(items, 'peer:' + peer.name);
    cur.b = Number(r.headers.get('X-Cursor') || cur.b);
    if (r.headers.get('X-More') !== '1') break;
  }
  // Ours → theirs (only with a token).
  if (peer.token) {
    for (let i = 0; i < SYNC_ROUNDS; i++) {
      const { items, cursor, more } = await ctx.relay.pull(cur.a, SYNC_PAGE, 'peer:' + peer.name);
      if (items.length) {
        const r = await peerFetch(ctx, peer, '/v1/sync/push', {
          method: 'POST', body: encodeSync(items), headers: { 'Content-Type': 'application/x-meshmsg-frames' },
        });
        if (!r.ok) throw new Error(`push to ${peer.name}: HTTP ${r.status}`);
        pushed += items.length;
      }
      cur.a = cursor;
      if (!more) break;
    }
  }
  await ctx.store.kvPut(ckey, JSON.stringify(cur));
  return { pulled, pushed };
}

/** Periodic work: expire old packets, sync with peers. */
export async function maintenance(ctx) {
  const out = { expired: 0, peers: {} };
  try { out.expired = await ctx.relay.expire(); } catch (e) { out.expireError = String(e); }
  for (const peer of ctx.cfg.peers) {
    try { out.peers[peer.name] = await syncPeer(ctx, peer); } catch (e) { out.peers[peer.name] = { error: String(e.message || e) }; }
  }
  return out;
}
