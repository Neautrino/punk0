// web/src/orch/lsp.js
// The selected folder's language servers, as px0's status bar shows its own:
// an LSP item at the right corner of the middle column, its dot saying
// whether they are on, and a menu above it. They are off by default per
// folder (records.json lsp); switching restarts the folder's editor, whose
// child then starts with or without -no-lsp. While on and the editor runs,
// the menu lists its servers from the child (w/<id>/api/lsp/servers) with
// px0's Start / Stop.
import { $, esc } from '../state.js';
import { call, failed } from './api.js';
import { subscribe, reload, workspace, tildify } from './store.js';

let itemEl, menuEl;
let shownFor = ''; // the workspace the open menu is about
let seq = 0;

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
};

function draw() {
  const w = workspace();
  itemEl.hidden = !w || w.missing;
  if (itemEl.hidden) return close();
  const st = stateOf(w);
  itemEl.dataset.state = st;
  itemEl.title = TITLES[st];
  // The menu follows the selection and the editor's state.
  if (!menuEl.hidden && (shownFor !== w.id || menuEl.dataset.state !== st)) render();
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

async function render() {
  const w = workspace();
  if (!w) return close();
  const my = ++seq;
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
    body = note('Off for this folder: outline and go-to-definition use punk0\'s own index. Each language server takes 300 MB to 1.5 GB of memory.');
  } else if (w.backend !== 'running') {
    body = note('On. They start when this folder\'s editor runs and a file of their language is opened.');
  } else {
    body = note('Loading…');
  }
  menuEl.innerHTML = head + `<div class="lsp-server-list">${body}</div>`;
  place();
  if (!w.lsp || w.backend !== 'running') return;
  const res = await call('GET', `${w.href}api/lsp/servers`);
  if (my !== seq || menuEl.hidden) return;
  menuEl.querySelector('.lsp-server-list').innerHTML = res.ok ? servers(res.data.servers || []) : note(esc(res.data.error));
  place();
}

// servers is px0's server list, without installing: that is the editor's.
function servers(list) {
  if (!list.length) return note('No language servers for the files in this folder.');
  return list.map(s => {
    let dot = s.state || 'stopped', meta, btn = '', hint = '';
    if (s.running) {
      dot = s.state === 'indexing' ? 'indexing' : 'ready';
      meta = s.memBytes ? `${Math.round(s.memBytes / 1048576)} MB` : 'Running';
      btn = `<button class="lsp-btn lsp-btn-stop" data-act="stop" data-name="${esc(s.name)}">Stop</button>`;
    } else if (s.state === 'starting') {
      dot = 'starting';
      meta = 'Starting…';
    } else if (s.installed) {
      meta = s.state === 'failed' ? 'Failed' : 'Stopped';
      hint = s.error || '';
      btn = `<button class="lsp-btn lsp-btn-start" data-act="start" data-name="${esc(s.name)}">Start</button>`;
    } else {
      dot = 'missing';
      meta = 'Not installed';
      hint = 'Install it from the editor\'s LSP menu';
    }
    return `
      <div class="lsp-server-row">
        <div class="lsp-server-info">
          <span class="lsp-dot ${esc(dot)}"></span>
          <span class="lsp-server-name">${esc(s.name)}</span>
          <span class="lsp-server-lang">${esc(s.lang || '')}</span>
        </div>
        <span class="lsp-server-meta" title="${esc(hint || meta)}">${esc(meta)}</span>
        ${btn}
      </div>`;
  }).join('');
}

async function onMenu(e) {
  const b = e.target.closest('button[data-act]');
  const w = workspace();
  if (!b || !w) return;
  e.stopPropagation();
  b.disabled = true;
  const act = b.dataset.act;
  if (act === 'on' || act === 'off') {
    const res = await call('POST', `api/workspaces/${encodeURIComponent(w.id)}/lsp`, { on: act === 'on' });
    if (!res.ok) failed(res);
    await reload();
    return render();
  }
  const res = await call('POST', `${w.href}api/lsp/${act}`, { server: b.dataset.name });
  if (!res.ok) failed(res);
  render();
}
