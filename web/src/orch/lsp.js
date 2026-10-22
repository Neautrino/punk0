// web/src/orch/lsp.js
// The selected folder's language servers, as px0's status bar shows its own:
// an item at the right corner of the middle column, its dot saying whether
// they are on, and a menu above it. With a text file in front (viewer.js's
// 'file:shown') the item is named after the file's language, and while the
// folder's servers are on it starts that language's server, as opening the
// file in the editor does, and its dot follows that server; with no file it
// shows only while servers are on, since they then cost memory. They are
// off by default per folder (records.json lsp); switching restarts the
// folder's editor, whose child then starts with or without -no-lsp. While on
// and the editor runs, the menu lists its servers from the child
// (w/<id>/api/lsp/servers) with the editor's own list (lspmenu.js): Start,
// Stop, Install and each server's memory, the same as in the editor.
import { $, esc } from '../state.js';
import { lspServers, lspRows } from '../lspmenu.js';
import { on } from '../bus.js';
import { call, failed } from './api.js';
import { subscribe, reload, workspace, projectOf, wsLabel, tildify } from './store.js';
import { fmtRAM, refreshResources } from './resources.js';
import { getSetting } from './settings.js';

let itemEl, menuEl;
let shownFor = ''; // the workspace the open menu is about
let file = null; // { ws, path, lang, missing } of the text file in front, else null
let fileServer = null; // its server's { state, server, missing } from api/lsp/warm
let warmFor = ''; // "<workspace id>:<path>" being warmed
let warmSeq = 0; // the running warm loop's number: a newer one supersedes it
let usage = []; // [{ w, bytes }]: folders whose language servers hold memory
let lastBytes = {}; // workspace id → its language servers' bytes at the last poll

export function initLsp() {
  itemEl = $('#st-lsp');
  menuEl = $('#lsp-menu');
  itemEl.addEventListener('click', e => {
    e.stopPropagation();
    toggle();
  });
  itemEl.addEventListener('keydown', e => {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    e.preventDefault();
    toggle();
  });
  menuEl.addEventListener('click', onMenu);
  document.addEventListener('mousedown', e => {
    if (!menuEl.hidden && !e.target.closest?.('#lsp-menu, #st-lsp')) close();
  });
  addEventListener('keydown', e => e.key === 'Escape' && close());
  addEventListener('resize', () => !menuEl.hidden && place());
  subscribe(draw);
  on('file:shown', f => {
    file = f;
    fileServer = null;
    warmFor = '';
    warmSeq++; // ends the old file's loop, also when no file follows
    draw();
  });
  // Every resources poll (3 s, from the parent's process table, so it keeps
  // no editor awake): the memory below each folder's editor, counted only
  // where its language servers are on.
  on('resources', r => {
    const all = r.workspaces || [];
    usage = all.map(x => ({ w: workspace(x.id), bytes: x.lsp }))
      .filter(x => x.w?.lsp && x.bytes > 0);
    // The file's server stopped after being idle: its memory went to nothing
    // while the dot said running. Shown off without a warm, which would
    // start it again and defeat the idle stop; re-showing the file warms.
    const w = workspace();
    const now = all.find(x => x.id === w?.id);
    if (now && file?.ws === w.id && lastBytes[w.id] > 0 && !(now.lsp > 0) &&
        (fileServer?.state === 'ready' || fileServer?.state === 'indexing')) {
      warmSeq++;
      fileServer = { state: 'idle', server: fileServer.server };
    }
    lastBytes = Object.fromEntries(all.map(x => [x.id, x.lsp || 0]));
    draw();
    drawFolders();
  });
  on('setting:changed', key => {
    if (key === 'lsp.enabled') {
      // Off: the editors stop their servers on their own; on again, the file
      // in front is warmed afresh rather than trusting the old reply.
      fileServer = null;
      warmFor = '';
      warmSeq++;
      draw();
    }
    if (key === 'lsp.memoryWarning') {
      draw();
      drawFolders();
    }
  });
}

// overWarning tells whether total bytes exceed the memory warning set in
// Settings (lsp.memoryWarning, in MB; 0 never warns).
function overWarning(total) {
  const mb = Number(getSetting('lsp.memoryWarning')) || 0;
  return mb > 0 && total > mb * 1024 * 1024;
}

function warningLine(total) {
  return overWarning(total) ? `\nAbove the ${getSetting('lsp.memoryWarning')} MB warning set in Settings.` : '';
}

// stateOf is the item's dot: off, or on and as far along as the editor is.
function stateOf(w) {
  if (!w.lsp) return 'off';
  return { running: 'ready', starting: 'starting', failed: 'failed' }[w.backend] || 'stopped';
}

const TITLES = {
  off: 'Language servers: off for this folder (click to manage)',
  ready: 'Language servers: on (click to manage)',
  starting: 'Language servers: on, editor starting (click to manage)',
  failed: 'Language servers: on, editor failed to start (click to manage)',
  stopped: 'Language servers: on, start with this folder\'s editor (click to manage)',
  missing: 'Language servers: on, none installed for this language (click to manage)',
};

// fileState is the dot and title for the file's own server, from the last
// warm reply, or null to fall back on the folder's.
function fileState(s) {
  if (!s) return null;
  // Allowed for the folder but turned off by lsp.enabled: warm keeps asking,
  // so turning them back on recovers once the editor notices.
  if (s.paused) return ['off', 'Language servers are turned off in Settings'];
  if (s.state === 'idle') return ['off', `${s.server || 'The language server'} stopped after being idle; it starts again when you open a file of its language`];
  switch (s.state) {
    case 'ready': return ['ready', `${s.server} is running (click to manage)`];
    case 'indexing': return ['indexing', `${s.server} is indexing (click to manage)`];
    case 'starting': return ['starting', `${s.server || 'The language server'} is starting (click to manage)`];
    case 'failed': return ['failed', `Language server failed (${s.server}) (click to manage)`];
  }
  if (s.missing) return ['missing', TITLES.missing];
  if (s.server) return ['off', `${s.server} is stopped: Start it in the menu`];
  return ['off', 'No language server for this language'];
}

function draw() {
  const w = workspace();
  const total = usage.reduce((n, x) => n + x.bytes, 0);
  // Shown for a file, while this folder's servers are on, or while any
  // folder's servers hold memory: they are what costs gigabytes. With
  // language servers off in Settings none runs, so there is nothing to show
  // and nothing to warm.
  itemEl.hidden = getSetting('lsp.enabled') === false || !w || w.missing || (!file && !w.lsp && !total);
  if (itemEl.hidden) return close();
  const folder = stateOf(w);
  const own = file && w.lsp ? fileState(fileServer) : null;
  // The editor reports a language it has no server for only while they are on.
  const st = own ? own[0] : file?.missing && w.lsp ? 'missing' : folder;
  itemEl.dataset.state = st;
  const name = file ? langName(file.lang) : 'LSP';
  const warn = overWarning(total);
  itemEl.innerHTML = esc(name) + (total ? `<span class="wb-lsp-ram${warn ? ' warn' : ''}">${fmtRAM(total)}</span>` : '');
  const ram = total ? `\nLanguage servers use ${fmtRAM(total)} in ${usage.length} folder${usage.length === 1 ? '' : 's'}.${warningLine(total)}` : '';
  itemEl.title = (file ? `${name}. ` : '') + (own ? own[1] : TITLES[st]) + ram;
  // Only the selected folder's own file: on a workspace switch this runs
  // before the viewer announces the new folder's file (or none), and the old
  // path must not wake the new folder's editor and server.
  if (file && file.ws === w.id && w.lsp) warm(w);
  // The menu follows the selection and the editor's state.
  if (!menuEl.hidden && (shownFor !== w.id || menuEl.dataset.state !== folder)) render();
}

// warm starts the server for the file in front, as the editor does when it
// opens a file (GET api/lsp/warm), and follows it until it is up, so the dot
// turns green on its own. A server stopped from the menu stays stopped: the
// editor reports it off rather than starting it. A newer warm, for another
// file or a re-check of this one, supersedes a running loop (warmSeq).
async function warm(w) {
  const key = `${w.id}:${file.path}`;
  if (warmFor === key) return;
  warmFor = key;
  const my = ++warmSeq;
  const path = file.path;
  for (let i = 0; i < 25; i++) {
    const q = new URLSearchParams({ path, wait: i ? '1200' : '1' });
    const res = await call('GET', `${w.href}api/lsp/warm?${q}`);
    if (my !== warmSeq || !res.ok) return;
    fileServer = res.data;
    draw();
    // Paused asks on without counting: the editor takes up to 2 s to notice
    // the setting turned back on.
    if (res.data.paused) i--;
    else if (res.data.state !== 'starting' && res.data.state !== 'indexing') return;
    await new Promise(r => setTimeout(r, 900));
    if (my !== warmSeq) return;
  }
}

// langName is how the item names a highlighter's language. Some are lower
// case ("markdown"), and plain text is "plaintext".
function langName(lang) {
  if (lang === 'plaintext') return 'Plain Text';
  return lang === lang.toLowerCase() ? lang.charAt(0).toUpperCase() + lang.slice(1) : lang;
}

function toggle() {
  if (!menuEl.hidden) return close();
  menuEl.hidden = false;
  itemEl.setAttribute('aria-expanded', 'true');
  render();
}

function close() {
  if (menuEl.hidden) return;
  menuEl.hidden = true;
  itemEl.setAttribute('aria-expanded', 'false');
  shownFor = '';
}

// place opens the menu upward from the item, right edges aligned, as px0's.
function place() {
  const r = itemEl.getBoundingClientRect();
  menuEl.style.bottom = `${innerHeight - r.top + 6}px`;
  menuEl.style.right = `${Math.max(8, innerWidth - r.right)}px`;
  menuEl.style.left = 'auto';
}

const note = text => `<div class="lsp-empty-note">${text}</div>`;

function render() {
  const w = workspace();
  if (!w) return close();
  shownFor = w.id;
  const st = stateOf(w);
  menuEl.dataset.state = st;
  const head = `
    <div class="lsp-menu-head">
      <span>Language Servers</span>
      <button class="lsp-menu-head-action" data-act="${w.lsp ? 'off' : 'on'}" title="Restarts this folder's editor">${w.lsp ? 'Turn off' : 'Turn on'}</button>
    </div>
    <div class="lsp-folder" title="${esc(w.dir)}">${esc(tildify(w.dir))}</div>`;
  let body;
  if (!w.lsp) {
    body = note('Off for this folder: outline and go-to-definition use punk0\'s own index. Each language server takes 300 MB to 1.5 GB of memory, and every folder runs its own: each worktree is a folder.');
  } else if (w.backend !== 'running') {
    body = note('On. They start when this folder\'s editor runs and a file of their language is opened.');
  } else {
    body = note('Loading…');
  }
  menuEl.innerHTML = head + `<div class="lsp-server-list">${body}</div><div class="wb-lsp-folders"></div>`;
  drawFolders();
  place();
  if (w.lsp && w.backend === 'running') {
    listFor(w).refresh().catch(e => {
      if (shownFor === w.id && !menuEl.hidden) menuEl.querySelector('.lsp-server-list').innerHTML = note(esc(e.message));
    });
  }
}

// drawFolders lists, under the folder's own servers, every folder whose
// servers hold memory, biggest first, each with Stop: worktrees each run
// their own, so this is where gigabytes add up. Stopped servers stay off
// until Start in that folder's list, or until its editor starts again.
function drawFolders() {
  const el = !menuEl.hidden && menuEl.querySelector('.wb-lsp-folders');
  if (!el) return;
  if (!usage.length) {
    el.innerHTML = '';
    return;
  }
  const total = usage.reduce((n, x) => n + x.bytes, 0);
  const rows = [...usage].sort((a, b) => b.bytes - a.bytes).map(({ w, bytes }) => {
    const p = projectOf(w.id);
    const name = p && !p.scratch ? `${p.name} / ${w.name}` : wsLabel(w);
    return `<div class="wb-lsp-folder-row${w.id === shownFor ? ' current' : ''}">
      <span class="wb-lsp-folder-name" title="${esc(w.dir)}">${esc(name)}</span>
      <span class="lsp-server-meta">${fmtRAM(bytes)}</span>
      <button class="lsp-btn lsp-btn-stop" data-act="stop-folder" data-ws="${esc(w.id)}" title="Stop this folder's language servers">Stop</button>
    </div>`;
  }).join('');
  const warn = overWarning(total);
  el.innerHTML = `<div class="wb-lsp-folders-head"><span>All folders</span><span${warn ? ` class="warn" title="${esc(warningLine(total).trim())}"` : ''}>${fmtRAM(total)}</span></div>${rows}`;
}

// One server list (lspmenu.js, the editor's own) per folder editor, so an
// install keeps polling its own backend while another workspace is shown.
const lists = new Map(); // workspace id -> lspServers
let listSig = ''; // the servers last shown in the menu, to see a change

function listFor(w) {
  let list = lists.get(w.id);
  if (list) return list;
  list = lspServers({
    request: async (method, path, params) => {
      const q = params ? '?' + new URLSearchParams(params) : '';
      const res = await call(method, `${w.href}${path}${q}`);
      if (!res.ok) throw new Error(res.data.error);
      return res.data;
    },
    show: data => {
      if (shownFor !== w.id || menuEl.hidden) return;
      menuEl.querySelector('.lsp-server-list').innerHTML = lspRows(data);
      place();
      // A server started, stopped or installed: ask again for the file's
      // own. Only on a change, not on every poll of an unchanged list.
      const sig = (data.servers || []).map(s => `${s.name}:${s.state}:${s.installed}`).join();
      if (sig !== listSig) {
        listSig = sig;
        warmFor = '';
        draw();
      }
    },
    open: () => !menuEl.hidden && shownFor === w.id,
  });
  lists.set(w.id, list);
  return list;
}

async function onMenu(e) {
  const b = e.target.closest('button[data-act]');
  const w = workspace();
  if (!b || !w) return;
  e.stopPropagation();
  const act = b.dataset.act;
  if (act === 'stop-folder') return stopFolder(b);
  if (act !== 'on' && act !== 'off') return listFor(w).act(b);
  b.disabled = true;
  const res = await call('POST', `api/workspaces/${encodeURIComponent(w.id)}/lsp`, { on: act === 'on' });
  if (!res.ok) failed(res);
  await reload();
  render();
}

async function stopFolder(b) {
  const fw = workspace(b.dataset.ws);
  if (!fw) return;
  b.disabled = true;
  const res = await call('POST', `${fw.href}api/lsp/stop`);
  if (!res.ok) failed(res);
  await refreshResources();
  if (fw.id === shownFor) render();
}
