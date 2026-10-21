// web/src/tree.js
// Files sidebar of the selected workspace: lazy folder expansion; clicking a
// file opens it in a tab. The active file tab's row stays highlighted.
import { subscribe, workspace, projectOf, activeTab, openFile } from './store.js';

let treeEl, rootEl, footEl, branchEl;
let ws = '';
let activePath = null;

const GIT_STATUS = {
  M: ['st-mod', 'M'],
  A: ['st-add', 'A'],
  D: ['st-del', 'D'],
  U: ['st-untr', 'U'],
  R: ['st-ren', 'R'],
  '!': ['st-conf', '!'],
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

export function initTree(container) {
  treeEl = container.querySelector('#tree');
  rootEl = container.querySelector('#files-root');
  footEl = container.querySelector('#files-foot');
  branchEl = container.querySelector('#files-branch');
  treeEl.addEventListener('click', onClick);
  container.querySelector('#btn-collapse').addEventListener('click', collapseAll);
  subscribe(render);
}

function render() {
  const w = workspace();
  if ((w?.id || '') !== ws) {
    ws = w?.id || '';
    treeEl.innerHTML = '';
    if (ws) loadTree('', treeEl, 0);
  }
  const p = w && projectOf(w.id);
  rootEl.textContent = w ? (p.scratch ? w.name : p.name) : '';
  rootEl.title = w?.dir || '';
  const branch = w?.kind === 'worktree' ? w.branch : w?.kind === 'local' ? p.branch : '';
  footEl.classList.toggle('hidden', !branch);
  branchEl.textContent = branch || '';

  const t = activeTab();
  activePath = t?.kind === 'file' ? t.path : null;
  markActive();
}

function markActive() {
  for (const el of treeEl.querySelectorAll('.tr.sel')) el.classList.remove('sel');
  if (activePath === null) return;
  treeEl.querySelector(`[data-file="${CSS.escape(activePath)}"]`)?.classList.add('sel');
}

async function loadTree(dir, targetEl, depth) {
  const from = ws;
  try {
    const res = await fetch(`/api/workspaces/${encodeURIComponent(from)}/tree?dir=${encodeURIComponent(dir)}`);
    if (!res.ok || from !== ws) return; // drop answers for a workspace switched away from
    renderNodes((await res.json()).children, targetEl, depth);
    markActive();
  } catch (err) {
    console.error('Failed to load tree:', dir, err);
  }
}

function renderNodes(nodes, container, depth) {
  const pad = 8 + depth * 12;
  container.innerHTML = nodes.map((n) => {
    const ignored = n.ignored ? ' ignored' : '';
    if (n.dir) {
      return `
        <div class="tr dir${ignored}${n.dirty ? ' dirty' : ''}" data-dir="${escapeHtml(n.path)}" data-depth="${depth}" style="padding-left: ${pad}px">
          <span class="ar"></span>
          <span class="nm">${escapeHtml(n.name)}</span>
        </div>
        <div class="kids"></div>`;
    }
    const st = GIT_STATUS[n.status];
    return `
      <div class="tr file${ignored}${st ? ` git-${n.status}` : ''}" data-file="${escapeHtml(n.path)}" title="${escapeHtml(n.path)}" style="padding-left: ${pad + 17}px">
        <span class="ic" data-t="${fileKind(n.name)}"></span>
        <span class="nm">${escapeHtml(n.name)}</span>
        ${st ? `<span class="gs ${st[0]}">${st[1]}</span>` : ''}
      </div>`;
  }).join('');
}

function onClick(e) {
  const dirRow = e.target.closest('.tr.dir');
  if (dirRow) {
    const kids = dirRow.nextElementSibling;
    const open = dirRow.classList.toggle('open');
    kids.classList.toggle('open', open);
    if (open && !kids.hasChildNodes()) loadTree(dirRow.dataset.dir, kids, +dirRow.dataset.depth + 1);
    return;
  }
  const fileRow = e.target.closest('.tr.file');
  if (fileRow) openFile(fileRow.dataset.file);
}

function collapseAll() {
  for (const el of treeEl.querySelectorAll('.open')) el.classList.remove('open');
}

function escapeHtml(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}
