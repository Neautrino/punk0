// web/src/orch/changes.js
// Diffs against HEAD, untracked files included (w/<id>/api/changes). The
// Changes tab shows every file the selected workspace changed, one after
// another, each under a header with its status, path and +/- lines that
// folds it away; a file tab switched to its diff (the path row's switch, or
// a click in the files pane's changed-files view) shows that file's alone.
// Each tab group draws in its own viewport, so one group can show the
// Changes tab while the other shows a file's diff. Beside another group the
// diff is one column; with the group maximized it is side by side, or one
// column, as last picked with the switch in the path row
// (punk0.diffLayout). The files tree's "± All changes" row (#o-all-changes,
// shown by tree.js) opens the Changes tab; a diff reloads each time it
// comes into view, and with its refresh button.
import { $, $$, esc } from '../state.js';
import { call } from './api.js';
import { subscribe, workspace, hasGit, openChanges, openFile } from './store.js';
import { view } from './split.js';
import { GIT_STATUS } from './tree.js';
import { hunkHeader, unifiedTable, splitTable, fileLayout } from '../difftable.js';

const LAYOUT_KEY = 'punk0.diffLayout';

const ICON_FOLD = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>';
const ICON_OPEN = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3h7v7M21 3l-9 9M19 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2h5"/></svg>';

// One pane per tab group: its elements, and what its diff shows.
// shown: a diff is in view; ws/href: the workspace of data and its backend;
// only: the file whose diff is shown, '' for the Changes tab; loads is
// bumped per load, so only the latest one paints.
const panes = [];

export function initChanges() {
  $$('.o-group').forEach((group, g) => {
    const p = {
      vp: $('.o-diff-vp', group),
      titleEl: $('.o-file-title', group),
      metaEl: $('.o-file-meta', group),
      actsEl: $('.o-changes-acts', group),
      layoutEl: $('.o-diff-layout', group),
      modeEl: $('.o-file-mode', group),
      g, shown: false, ws: '', href: '', only: '', data: null, error: '', loads: 0, maximized: false,
      folded: new Set(), // paths folded away, for ws
    };
    panes[g] = p;
    $('.o-changes-refresh', group).addEventListener('click', () => load(p));
    p.layoutEl.addEventListener('click', e => {
      const mode = e.target.closest('[data-layout]')?.dataset.layout;
      if (!mode || mode === layout(p)) return;
      try { localStorage.setItem(LAYOUT_KEY, mode); } catch {}
      for (const q of panes) if (q.shown) paint(q);
    });
    p.modeEl.addEventListener('click', e => {
      const mode = e.target.closest('[data-mode]')?.dataset.mode;
      const f = view().groups[g]?.tab;
      if (mode && f?.path) openFile(f.path, mode);
    });
    p.vp.addEventListener('click', e => onClick(p, e));
  });

  $('#o-all-changes').addEventListener('click', () => {
    for (const p of panes) if (p.shown && !p.only) load(p);
    openChanges();
  });
  subscribe(syncModes);
}

// syncModes offers the source / diff switch in each group showing a file,
// in a folder with git to diff against.
function syncModes() {
  const w = workspace();
  const v = view();
  for (const p of panes) {
    const f = v.groups[p.g]?.shown ? v.groups[p.g].tab : null;
    p.modeEl.hidden = !f?.path || !hasGit(w);
    for (const b of p.modeEl.querySelectorAll('[data-mode]')) {
      const on = (b.dataset.mode === 'diff') === !!f?.diff;
      b.classList.toggle('active', on);
      b.setAttribute('aria-pressed', String(on));
    }
  }
}

// layout is how a pane's diff is drawn now: one column beside another
// group, else as last picked, side by side at first.
function layout(p) {
  if (!p.maximized) return 'unified';
  return localStorage.getItem(LAYOUT_KEY) === 'unified' ? 'unified' : 'split';
}

// showChanges is called on every render while group g shows a diff: the
// Changes tab, or path's alone; max says the group is maximized. Coming
// into view, or another file, reloads.
export function showChanges(g, w, max, path = '') {
  const p = panes[g];
  if (p.shown && w.id === p.ws && path === p.only) {
    if (max !== p.maximized) {
      p.maximized = max;
      paint(p);
    }
    return;
  }
  if (w.id !== p.ws || path !== p.only) {
    p.data = null;
    p.error = '';
  }
  if (w.id !== p.ws) {
    p.ws = w.id;
    p.href = w.href;
    p.folded.clear();
  }
  p.only = path;
  p.shown = true;
  p.actsEl.hidden = false;
  p.titleEl.textContent = path || 'Changes';
  p.maximized = max;
  paint(p);
  load(p);
}

export function hideChanges(g) {
  panes[g].shown = false;
  panes[g].actsEl.hidden = true;
}

async function load(p) {
  const n = ++p.loads;
  const from = p.ws, path = p.only;
  p.metaEl.textContent = 'Loading…';
  const res = await call('GET', `${p.href}api/changes` + (path ? '?' + new URLSearchParams({ path }) : ''));
  if (n !== p.loads || from !== p.ws || path !== p.only) return; // a later load, or another diff
  p.data = res.ok ? res.data : p.data;
  p.error = res.ok ? '' : res.data.error;
  if (p.shown) paint(p);
}

function paint(p) {
  const mode = layout(p);
  p.layoutEl.hidden = !p.maximized;
  for (const b of p.layoutEl.querySelectorAll('[data-layout]')) {
    const on = b.dataset.layout === mode;
    b.classList.toggle('active', on);
    b.setAttribute('aria-pressed', String(on));
  }
  const top = p.vp.scrollTop;
  p.vp.replaceChildren();
  p.vp.classList.toggle('o-diff-one', !!p.only);
  if (p.error) return note(p, p.error, true);
  if (!p.data) return note(p, 'Loading…');
  const files = p.data.files || [];
  let add = 0, del = 0;
  for (const f of files) {
    add += f.add;
    del += f.del;
  }
  const counts = `<span class="up-file-add">+${add}</span> <span class="up-file-del">&minus;${del}</span>`;
  p.metaEl.innerHTML = !files.length ? '' : p.only ? (add || del ? counts : '') : `${files.length} file${files.length === 1 ? '' : 's'} ${counts}`;
  if (!p.data.git) return note(p, 'Not a git repository.');
  if (!files.length) return note(p, p.only ? 'No changes in this file.' : 'No changes.');
  const frag = document.createDocumentFragment();
  if (p.only) frag.append(fileBody(files[0], mode));
  else for (const f of files) frag.append(fileSection(p, f, mode));
  if (p.data.truncated) frag.append(noteEl(`Only the first ${files.length} changed files are shown.`));
  p.vp.append(frag);
  p.vp.scrollTop = top;
}

function fileSection(p, f, mode) {
  const sec = document.createElement('section');
  sec.className = 'o-chg' + (p.folded.has(f.path) ? ' folded' : '');
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
  sec.append(head, fileBody(f, mode));
  return sec;
}

// fileBody is one file's hunks, or why there are none.
function fileBody(f, mode) {
  const body = document.createElement('div');
  body.className = 'o-chg-body';
  if (f.binary) body.append(noteEl('Binary file.'));
  else if (f.large) body.append(noteEl('Too large to show here; open the file to read it.'));
  else if (!f.hunks.length) body.append(noteEl(f.status === 'R' ? 'Renamed without changes.' : 'No changed lines.'));
  else {
    const layout = fileLayout(f.hunks, mode);
    for (const h of f.hunks) {
      body.append(hunkHeader(h), layout === 'split' ? splitTable(h) : unifiedTable(h));
    }
  }
  return body;
}

function onClick(p, e) {
  const open = e.target.closest('[data-open]');
  if (open) return openFile(open.dataset.open, 'source');
  const head = e.target.closest('.o-chg-head');
  if (!head) return;
  const sec = head.parentElement;
  const path = sec.dataset.path;
  if (sec.classList.toggle('folded')) p.folded.add(path);
  else p.folded.delete(path);
}

function noteEl(text, err = false) {
  const p = document.createElement('p');
  p.className = 'o-note' + (err ? ' o-err' : '');
  p.textContent = text;
  return p;
}

function note(p, text, err = false) {
  p.metaEl.textContent = '';
  p.vp.append(noteEl(text, err));
}
