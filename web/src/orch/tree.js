// web/src/orch/tree.js
// Right pane: the selected workspace's files, from its folder's editor
// backend (w/<id>/api/tree), expanded lazily. Clicking a file opens it in a
// tab; the active file tab's row stays highlighted. Rows use the app's tree
// classes (style.css), so both views look alike.
import { $, esc } from '../state.js';
import { call } from './api.js';
import { subscribe, workspace, projectOf, openFile } from './store.js';
import { view } from './split.js';

/* git status letter -> CSS class + label, as in the app's tree. */
const GIT_STATUS = {
  M: ['git-M', 'modified'], A: ['git-A', 'added'], D: ['git-D', 'deleted'],
  U: ['git-untracked', 'untracked'], R: ['git-R', 'renamed'],
  C: ['git-A', 'copied'], '!': ['git-M', 'unmerged'],
};

/* A colour family per file kind, drawn in CSS. Emoji or icon fonts would be at
   the mercy of whatever the viewer has installed. */
const FILE_KIND = {
  go: 'code', js: 'code', mjs: 'code', cjs: 'code', ts: 'code', tsx: 'code', jsx: 'code',
  py: 'code', rb: 'code', rs: 'code', java: 'code', kt: 'code', c: 'code', h: 'code',
  cc: 'code', cpp: 'code', hpp: 'code', cs: 'code', php: 'code', swift: 'code',
  lua: 'code', ex: 'code', exs: 'code', scala: 'code', dart: 'code', sh: 'code',
  bash: 'code', zsh: 'code', sql: 'code',
  json: 'data', yaml: 'data', yml: 'data', toml: 'data', ini: 'data', xml: 'data',
  csv: 'data', env: 'data', lock: 'data', mod: 'data', sum: 'data',
  md: 'doc', markdown: 'doc', txt: 'doc', rst: 'doc', adoc: 'doc',
  html: 'web', htm: 'web', css: 'web', scss: 'web', less: 'web', svg: 'web', vue: 'web',
  png: 'img', jpg: 'img', jpeg: 'img', gif: 'img', webp: 'img', ico: 'img', avif: 'img',
};

export function fileKind(name) {
  const i = name.lastIndexOf('.');
  return (i > 0 && FILE_KIND[name.slice(i + 1).toLowerCase()]) || 'other';
}

let treeEl, footEl, branchEl;
let treeWs = ''; // workspace id the tree shows
let treeHref = ''; // its folder's backend, "w/<id>/"
let activePath = null;

export function initTree() {
  treeEl = $('#o-tree');
  footEl = $('#o-files-foot');
  branchEl = $('#o-files-branch');
  treeEl.addEventListener('click', onTreeClick);
  $('#o-tree-collapse').addEventListener('click', collapseTree);
  $('#o-tree-refresh').addEventListener('click', refreshTree);
  subscribe(renderTree);
}

function renderTree() {
  const w = workspace();
  if ((w?.id || '') !== treeWs) {
    treeWs = w?.id || '';
    treeHref = w?.href || '';
    treeEl.innerHTML = '';
    if (w?.missing) treeEl.innerHTML = '<p class="o-note">The folder of this workspace is gone.</p>';
    else if (w) drawDir('', treeEl, 0);
  }
  const p = w && projectOf(w.id);
  const branch = w?.kind === 'worktree' ? w.branch : w?.kind === 'local' ? p.branch : '';
  footEl.hidden = !branch;
  branchEl.textContent = branch || '';

  activePath = view().file?.path ?? null;
  markActive();
}

function markActive() {
  for (const el of treeEl.querySelectorAll('.tr.sel')) el.classList.remove('sel');
  if (activePath === null) return;
  treeEl.querySelector(`[data-file="${CSS.escape(activePath)}"]`)?.classList.add('sel');
}

// drawDir lists dir into container. The first request may start the
// folder's backend, which can take a few seconds; the root says so meanwhile.
async function drawDir(dir, container, depth) {
  const from = treeWs;
  if (!dir && !container.hasChildNodes()) container.innerHTML = '<p class="o-note">Loading…</p>';
  const res = await call('GET', `${treeHref}api/tree?` + new URLSearchParams({ dir }));
  if (from !== treeWs) return false; // the workspace was switched away from
  if (!res.ok) {
    container.innerHTML = `<p class="o-note o-err">${esc(res.data.error)}</p>`;
    return false;
  }
  const pad = 8 + depth * 12;
  container.innerHTML = res.data.children.map(c => {
    // Ignored by .gitignore: still browsable, dimmed.
    const ig = c.ignored ? ' ignored' : '';
    if (c.dir) {
      return `<div class="tw"><div class="tr dir${ig}${c.dirty ? ' dirty' : ''}" data-dir="${esc(c.path)}" data-depth="${depth}" style="padding-left:${pad}px" title="${esc(c.path)}">` +
        `<span class="ar"></span><span class="nm">${esc(c.name)}</span></div><div class="kids"></div></div>`;
    }
    const g = GIT_STATUS[c.status];
    const badge = g ? `<span class="gs" title="git: ${g[1]}">${esc(c.status)}</span>` : '';
    return `<div class="tr file${ig}${g ? ' dirty ' + g[0] : ''}" data-file="${esc(c.path)}" style="padding-left:${pad + 12}px" title="${esc(c.path)}">` +
      `<span class="ic" data-t="${fileKind(c.name)}"></span><span class="nm">${esc(c.name)}</span>${badge}</div>`;
  }).join('') || (dir ? '' : '<p class="o-note">No files.</p>');
  markActive();
  return true;
}

function onTreeClick(e) {
  const dirRow = e.target.closest('.tr.dir');
  if (dirRow) {
    const kids = dirRow.nextElementSibling;
    const open = dirRow.classList.toggle('open');
    kids.classList.toggle('open', open);
    if (open && !kids.hasChildNodes()) drawDir(dirRow.dataset.dir, kids, +dirRow.dataset.depth + 1);
    return;
  }
  const fileRow = e.target.closest('.tr.file');
  if (fileRow) openFile(fileRow.dataset.file);
}

function collapseTree() {
  for (const el of treeEl.querySelectorAll('.open')) el.classList.remove('open');
}

// refreshTree redraws the tree, reopening the folders that were open, outer
// ones first so their rows exist when the inner ones are looked up.
async function refreshTree() {
  if (!treeWs || workspace()?.missing) return;
  const from = treeWs;
  const open = [...treeEl.querySelectorAll('.tr.dir.open')].map(r => r.dataset.dir);
  if (!(await drawDir('', treeEl, 0))) return;
  for (const dir of open) {
    if (from !== treeWs) return;
    const row = treeEl.querySelector(`.tr.dir[data-dir="${CSS.escape(dir)}"]`);
    if (!row) continue;
    row.classList.add('open');
    row.nextElementSibling.classList.add('open');
    await drawDir(dir, row.nextElementSibling, +row.dataset.depth + 1);
  }
}
