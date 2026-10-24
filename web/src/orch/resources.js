// web/src/orch/resources.js
// The agents page's resource button and panel (respanel.js), at the status
// bar's right end, fed from GET /api/resources. Each payload also goes out
// on the bus: the LSP item shows its language servers' share.
import { $ } from '../state.js';
import { emit } from '../bus.js';
import { createResPanel } from '../respanel.js';
import { call } from './api.js';

export { fmtRAM } from '../respanel.js';

let panel = null;

export function initResources() {
  panel = createResPanel($('#st-res'), async () => {
    const res = await call('GET', 'api/resources');
    return res.ok ? res.data : null;
  }, data => emit('resources', data));
}

// refreshResources runs one poll, the page's 3 s tick and after the LSP
// menu stops a folder's servers.
export function refreshResources() {
  return panel?.refresh();
}
