// web/src/tree.js
// File tree of the selected workspace: lazy folder expansion and file selection.
import { openFile } from './viewer.js';

let treeEl = null;
let activeRow = null;
let ws = '';

const GIT_STATUS = {
  M: ['st-mod', 'M'],
  A: ['st-add', 'A'],
  D: ['st-del', 'D'],
  U: ['st-untr', 'U'],
  R: ['st-ren', 'R'],
  '!': ['st-conf', '!'],
};

export function initTree(container) {
  treeEl = container;
  treeEl.addEventListener('click', handleTreeClick);
}

export function setTreeWorkspace(id) {
  ws = id;
  activeRow = null;
  treeEl.innerHTML = '';
  if (id) loadTree('', treeEl, 0);
}

async function loadTree(dir, targetEl, depth) {
  const from = ws;
  try {
    const res = await fetch(`/api/workspaces/${encodeURIComponent(from)}/tree?dir=${encodeURIComponent(dir)}`);
    if (!res.ok) return;
    const j = await res.json();
    if (from === ws) renderNodes(j.children, targetEl, depth); // drop answers for a workspace switched away from
  } catch (err) {
    console.error('Failed to load tree:', dir, err);
  }
}

function renderNodes(nodes, container, depth) {
  const pad = 12 + depth * 14;
  container.innerHTML = nodes.map(n => {
    const igClass = n.ignored ? ' ignored' : '';
    const dirtyClass = n.dirty ? ' dirty' : '';

    if (n.dir) {
      return `
        <div class="tree-node dir${igClass}${dirtyClass}">
          <div class="tree-row dir-row" data-dir="${escapeHtml(n.path)}" style="padding-left: ${pad}px">
            <span class="tree-arrow">▶</span>
            <span class="tree-name">${escapeHtml(n.name)}</span>
          </div>
          <div class="tree-children hidden" data-parent="${escapeHtml(n.path)}" data-depth="${depth + 1}"></div>
        </div>
      `;
    }

    const st = GIT_STATUS[n.status];
    const badge = st ? `<span class="git-badge ${st[0]}">${st[1]}</span>` : '';

    return `
      <div class="tree-node file${igClass}">
        <div class="tree-row file-row" data-file="${escapeHtml(n.path)}" style="padding-left: ${pad + 12}px">
          <span class="tree-name">${escapeHtml(n.name)}</span>
          ${badge}
        </div>
      </div>
    `;
  }).join('');
}

async function handleTreeClick(e) {
  const dirRow = e.target.closest('.dir-row');
  if (dirRow) {
    const node = dirRow.closest('.tree-node');
    const kids = node.querySelector('.tree-children');
    const arrow = dirRow.querySelector('.tree-arrow');

    if (kids.classList.contains('hidden')) {
      kids.classList.remove('hidden');
      arrow.textContent = '▼';
      if (!kids.hasChildNodes()) {
        const dir = dirRow.dataset.dir;
        const depth = parseInt(kids.dataset.depth, 10) || 1;
        await loadTree(dir, kids, depth);
      }
    } else {
      kids.classList.add('hidden');
      arrow.textContent = '▶';
    }
    return;
  }

  const fileRow = e.target.closest('.file-row');
  if (fileRow) {
    if (activeRow) activeRow.classList.remove('active');
    fileRow.classList.add('active');
    activeRow = fileRow;
    const path = fileRow.dataset.file;
    openFile(path);
  }
}

function escapeHtml(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}
