// web/src/lspmenu.js
// The language server list in the LSP menu of both status bars, the editor's
// (status.js) and the agents page's (orch/lsp.js), so a change here shows in
// both. It renders the rows of px0's GET api/lsp/servers and runs their
// buttons: Start, Stop, Start All / Stop All, Install (one of px0's own
// recipes, never a command from the page) and Copy. Each page says how to
// reach its editor backend and what to do with a fresh list; the menu around
// the rows, its head and placement, stay the page's. Depends only on
// state.js, ui.js and format.js, which both bundles already carry.
import { esc } from './state.js';
import { copyToClipboard, showToast } from './ui.js';
import { fmtBytes } from './format.js';

/**
 * lspServers ties the list to one editor backend.
 * @param {object} page
 * @param {(method: string, path: string, params?: Record<string, string>) => Promise<any>} page.request
 *   Sends to the backend, path like "api/lsp/servers", params in the query
 *   string; resolves to the reply's JSON and throws an Error with the
 *   server's message on failure.
 * @param {(data: any) => void} page.show  Takes every fresh servers reply.
 * @param {() => boolean} page.open  Whether the menu is open: a starting
 *   server is followed only while someone can see it.
 */
export function lspServers(page) {
  const installing = new Set(); // installs started here, to start once in
  let timer = 0;

  // refresh shows the current list. While a server installs, starts or
  // indexes it asks again every 1.2 s, so its row does not stay at
  // "Starting…" once it is up; an install keeps it asking with the menu
  // closed too, to start a server installed from here once it is in.
  async function refresh() {
    clearTimeout(timer);
    const data = await page.request('GET', 'api/lsp/servers');
    page.show(data);
    const list = data.servers || [];
    for (const s of list) {
      if (!installing.has(s.name) || s.state === 'installing') continue;
      installing.delete(s.name);
      if (!s.installed) continue; // failed: the row says so and shows the commands
      showToast('✓', `${s.name} installed. Starting…`);
      page.show(await page.request('POST', 'api/lsp/start', { server: s.name }));
    }
    const busy = list.some(s => s.state === 'installing') ||
      (page.open() && list.some(s => s.state === 'starting' || s.state === 'indexing'));
    if (busy) timer = setTimeout(() => refresh().catch(() => {}), 1200);
    return data;
  }

  // act runs a list button: data-act, with data-name, data-opt or data-cmd.
  async function act(btn) {
    const { act: what, name } = btn.dataset;
    if (what === 'copy') return copyToClipboard(btn.dataset.cmd, 'Command copied', btn);
    btn.disabled = true;
    try {
      if (what === 'install') {
        btn.textContent = 'Installing…';
        installing.add(name);
        await page.request('POST', 'api/lsp/install', { server: name, option: btn.dataset.opt });
        showToast('✓', `Installing ${name} in the background…`);
        await refresh();
        return;
      }
      const path = what === 'start' || what === 'start-all' ? 'api/lsp/start' : 'api/lsp/stop';
      const params = what === 'start-all' ? { server: 'all' } : what === 'stop-all' ? undefined : { server: name };
      await page.request('POST', path, params);
      await refresh(); // follows a starting server until it is up
    } catch (e) {
      installing.delete(name);
      showToast('!', e.message || 'Request failed', 4000);
      refresh().catch(() => {});
    }
  }

  return { refresh, act };
}

// lspAllButton is Stop All while any server runs, else Start All when any is
// installed, for a menu head.
export function lspAllButton(data) {
  const list = data?.servers || [];
  if (data?.anyRunning) return '<button class="lsp-menu-head-action" data-act="stop-all" title="Stop all running language servers">Stop All</button>';
  if (list.some(s => s.installed)) return '<button class="lsp-menu-head-action" data-act="start-all" title="Start all installed language servers">Start All</button>';
  return '';
}

// lspRows renders the servers. A running one shows the memory of its process
// and everything it started. One that is not installed gets Install when px0
// has a recipe it may run here; otherwise, or once an install failed, it
// shows how to get it by hand: this system's install commands with Copy, and
// a link to the server's install guide.
export function lspRows(data) {
  const list = data?.servers || [];
  if (data?.enabled === false) return '<div class="lsp-empty-note">Language servers are turned off in Settings.</div>';
  if (!list.length) return '<div class="lsp-empty-note">No language servers for the files in this folder.</div>';
  return list.map(s => {
    let dot, meta, btn = '', hint = '', manual = false;
    const stop = `<button class="lsp-btn lsp-btn-stop" data-act="stop" data-name="${esc(s.name)}" title="Stop ${esc(s.name)}">Stop</button>`;
    const start = `<button class="lsp-btn lsp-btn-start" data-act="start" data-name="${esc(s.name)}" title="Start ${esc(s.name)}">Start</button>`;
    if (s.state === 'installing') {
      dot = 'starting';
      meta = 'Installing…';
      btn = '<button class="lsp-btn" disabled>Installing…</button>';
    } else if (s.state === 'starting') { // reported as running too
      dot = 'starting';
      meta = 'Starting…';
      btn = stop;
    } else if (s.running) {
      dot = s.state === 'indexing' ? 'indexing' : 'ready';
      meta = s.memBytes ? fmtBytes(s.memBytes) : 'Running';
      hint = s.memBytes ? 'Memory of the server and the processes it started' : '';
      btn = stop;
    } else if (s.installed) {
      dot = s.state === 'failed' ? 'failed' : 'stopped';
      meta = s.state === 'failed' ? 'Failed' : 'Stopped';
      hint = s.error || '';
      btn = start;
    } else {
      dot = 'missing';
      meta = s.error ? 'Install failed' : 'Not installed';
      hint = s.error || '';
      manual = !!s.error || !(s.autoOpt >= 0);
      if (s.autoOpt >= 0) {
        btn = `<button class="lsp-btn lsp-btn-install" data-act="install" data-name="${esc(s.name)}" data-opt="${s.autoOpt}" title="Runs ${esc(s.options[s.autoOpt].cmd)}">${s.error ? 'Retry' : 'Install'}</button>`;
      }
    }
    const row = `
      <div class="lsp-server-row">
        <div class="lsp-server-info">
          <span class="lsp-dot ${dot}"></span>
          <span class="lsp-server-name">${esc(s.name)}</span>
          <span class="lsp-server-lang">${esc(s.lang || '')}</span>
        </div>
        <span class="lsp-server-meta" title="${esc(hint || meta)}">${esc(meta)}</span>
        ${btn}
      </div>`;
    return manual ? `<div class="lsp-manual">${row}${howToGet(s)}</div>` : row;
  }).join('');
}

function howToGet(s) {
  const cmds = (s.options || []).map(o => `
    <div class="lsp-cmd">
      <code title="${esc(o.cmd)}">${esc(o.cmd)}</code>
      <button class="lsp-btn lsp-btn-copy" data-act="copy" data-cmd="${esc(o.cmd)}" title="Copy the command">Copy</button>
    </div>${o.hasTool ? '' : `<div class="lsp-cmd-note">Needs ${esc(o.tool)}, which is not installed</div>`}`).join('');
  const docs = s.docs ? `<a class="lsp-docs" href="${esc(s.docs)}" target="_blank" rel="noopener">Install guide ↗</a>` : '';
  return `<div class="lsp-get">${cmds}${docs}</div>`;
}
