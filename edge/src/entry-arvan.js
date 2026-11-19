// ---- ArvanCloud Edge Computing entry (service worker) ----
// The installer replaces the CONFIG object below with this relay's settings
// (name, region, admins, the Object Storage bucket and its keys). Arvan's
// edge has no scheduler: expiry and federation run now and then, after a
// request, at most once a minute per instance.
import { EdgeCtx, edgeConfig, handleEdge, makeStore, runMaintenance } from './api.js';

const CONFIG = /*MAVA_CONFIG*/ {} /*END_MAVA_CONFIG*/;

let lastMaintenance = 0;

function arvanCtx(event) {
  const wait = typeof event.waitUntil === 'function' ? (p) => event.waitUntil(p) : (p) => { p.catch(() => {}); };
  return new EdgeCtx(makeStore(CONFIG), edgeConfig(CONFIG), 'arvan', wait);
}

addEventListener('fetch', (event) => {
  const ctx = arvanCtx(event);
  event.respondWith(handleEdge(ctx, event.request).then((res) => {
    if (Date.now() - lastMaintenance > 60000) {
      lastMaintenance = Date.now();
      ctx.waitUntil(runMaintenance(new EdgeCtx(ctx.store, ctx.cfg, 'arvan', ctx.waitUntil)));
    }
    return res;
  }));
});
