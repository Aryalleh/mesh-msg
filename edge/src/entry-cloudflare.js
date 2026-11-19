// ---- Cloudflare Workers entry (wrangler.toml: D1 binding "DB", cron trigger) ----
import { EdgeCtx, edgeConfig, handleEdge, makeStore, runMaintenance } from './api.js';

function cfCtx(env, exec) {
  return new EdgeCtx(makeStore(env), edgeConfig(env), 'cloudflare', (p) => exec.waitUntil(p));
}

export default {
  async fetch(request, env, exec) {
    return handleEdge(cfCtx(env, exec), request);
  },
  async scheduled(event, env, exec) {
    exec.waitUntil(runMaintenance(cfCtx(env, exec)));
  },
};
