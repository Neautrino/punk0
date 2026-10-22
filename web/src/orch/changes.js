// web/src/orch/changes.js
// The Changes tab: every file the selected workspace changed against HEAD,
// untracked ones included (w/<id>/api/changes), one after another, each
// under a header with its status, path and +/- lines that folds it away.
// Beside the terminals the diff is one column; with the files maximized it
// is side by side, or one column, as last picked with the switch in the
// path row (punk0.diffLayout). The files pane's Diff button opens the tab;
// it reloads each time it comes into view, and with its refresh button.
import { $, esc } from '../state.js';
import { call } from './api.js';
import { subscribe, workspace, hasGit, openChanges, openFile } from './store.js';
import { GIT_STATUS } from './tree.js';
import { hunkHeader, unifiedTable, splitTable } from '../difftable.js';

const LAYOUT_KEY = 'punk0.diffLayout';

const ICON_FOLD = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>';
const ICON_OPEN = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3h7v7M21 3l-9 9M19 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2h5"/></svg>';

let vp, titleEl, metaEl, actsEl, layoutEl, diffBtn;
let shown = false; // the tab is in view
let ws = ''; // workspace id of data
let href = ''; // its folder's backend, "w/<id>/"
let data = null; // the last /api/changes response for ws, null until loaded
let error = '';
let loads = 0; // bumped per load, so only the latest one paints
let maximized = false;
const folded = new Set(); // paths folded away, for ws

export function initChanges() {
  vp = $('#o-diff-vp');
  titleEl = $('#o-file-title');
  metaEl = $('#o-file-meta');
  actsEl = $('#o-changes-acts');
  layoutEl = $('#o-diff-layout');
  diffBtn = $('#o-tree-diff');

  diffBtn.addEventListener('click', () => {
    if (shown) load();
    openChanges();
  });
  $('#o-changes-refresh').addEventListener('click', load);
  layoutEl.addEventListener('click', e => {
    const mode = e.target.closest('[data-layout]')?.dataset.layout;
    if (!mode || mode === layout()) return;
    try { localStorage.setItem(LAYOUT_KEY, mode); } catch {}
    paint();
  });
  vp.addEventListener('click', onClick);
  subscribe(() => {
    const w = workspace();
    diffBtn.disabled = !hasGit(w);
  });
}

// layout is how the diff is drawn now: one column beside the terminals,
// else as last picked, side by side at first.
function layout() {
  if (!maximized) return 'unified';
  return localStorage.getItem(LAYOUT_KEY) === 'unified' ? 'unified' : 'split';
}

// showChanges is called on every render while the tab is in front; max says
// the files are maximized. Coming into view reloads.
export function showChanges(w, max) {
  if (shown && w.id === ws) {
    if (max !== maximized) {
      maximized = max;
      paint();
    }
    return;
  }
  if (w.id !== ws) {
    ws = w.id;
    href = w.href;
    data = null;
    error = '';
    folded.clear();
  }
  shown = true;
  actsEl.hidden = false;
  titleEl.textContent = 'Changes';
  maximized = max;
  paint();
  load();
}

export function hideChanges() {
  shown = false;
  actsEl.hidden = true;
}

async function load() {
  const n = ++loads;
  const from = ws;
  metaEl.textContent = 'Loading…';
  const res = await call('GET', `${href}api/changes`);
  if (n !== loads || from !== ws) return; // a later load, or another workspace
  data = res.ok ? res.data : data;
  error = res.ok ? '' : res.data.error;
  if (shown) paint();
}

function paint() {
  const mode = layout();
  layoutEl.hidden = !maximized;
  for (const b of layoutEl.querySelectorAll('[data-layout]')) {
    const on = b.dataset.layout === mode;
    b.classList.toggle('active', on);
    b.setAttribute('aria-pressed', String(on));
  }
  const top = vp.scrollTop;
  vp.replaceChildren();
  if (error) return note(error, true);
  if (!data) return note('Loading…');
  const files = data.files || [];
  let add = 0, del = 0;
  for (const f of files) {
    add += f.add;
    del += f.del;
  }
  metaEl.innerHTML = files.length
    ? `${files.length} file${files.length === 1 ? '' : 's'} <span class="up-file-add">+${add}</span> <span class="up-file-del">&minus;${del}</span>`
    : '';
  if (!data.git) return note('Not a git repository.');
  if (!files.length) return note('No changes.');
  const frag = document.createDocumentFragment();
  for (const f of files) frag.append(fileSection(f, mode));
  if (data.truncated) frag.append(noteEl(`Only the first ${files.length} changed files are shown.`));
  vp.append(frag);
  vp.scrollTop = top;
}

function fileSection(f, mode) {
  const sec = document.createElement('section');
  sec.className = 'o-chg' + (folded.has(f.path) ? ' folded' : '');
  sec.dataset.path = f.path;
  const g = GIT_STATUS[f.status] || ['git-M', f.status];
  const path = f.from ? `${esc(f.from)} → ${esc(f.path)}` : esc(f.path);
  const head = document.createElement('header');
  head.className = 'o-chg-head';
  head.title = 'Fold or unfold';
  head.innerHTML =
    `<span class="o-chg-fold">${ICON_FOLD}</span>` +
    `<span class="gs ${g[0]}" title="${esc(g[1])}">${esc(f.status)}</span>` +
    `<span class="o-chg-path">${path}</span>` +
    (f.add ? `<span class="up-file-add">+${f.add}</span>` : '') +
    (f.del ? `<span class="up-file-del">&minus;${f.del}</span>` : '') +
    (f.status === 'D' ? '' : `<button type="button" class="mini o-chg-open" data-open="${esc(f.path)}" title="Open file" aria-label="Open ${esc(f.path)}">${ICON_OPEN}</button>`);
  const body = document.createElement('div');
  body.className = 'o-chg-body';
  if (f.binary) body.append(noteEl('Binary file.'));
  else if (f.large) body.append(noteEl('Too large to show here; open the file to read it.'));
  else if (!f.hunks.length) body.append(noteEl(f.status === 'R' ? 'Renamed without changes.' : 'No changed lines.'));
  else {
    for (const h of f.hunks) {
      body.append(hunkHeader(h), mode === 'split' ? splitTable(h) : unifiedTable(h));
    }
  }
  sec.append(head, body);
  return sec;
}

function onClick(e) {
  const open = e.target.closest('[data-open]');
  if (open) return openFile(open.dataset.open);
  const head = e.target.closest('.o-chg-head');
  if (!head) return;
  const sec = head.parentElement;
  const path = sec.dataset.path;
  if (sec.classList.toggle('folded')) folded.add(path);
  else folded.delete(path);
}

function noteEl(text, err = false) {
  const p = document.createElement('p');
  p.className = 'o-note' + (err ? ' o-err' : '');
  p.textContent = text;
  return p;
}

function note(text, err = false) {
  metaEl.textContent = '';
  vp.append(noteEl(text, err));
}
