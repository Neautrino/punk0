// web/src/tabs.js
// Tab bar of the selected workspace: shells, Claude Code chats and open
// files. "+" offers a new Claude or shell tab, or the open file picker.
// Double-click renames a terminal tab after what it is for.
import { subscribe, reload, workspace, tabsOf, activeTab, selectTab, closeFile, api, esc } from './store.js';
import { fileKind } from './tree.js';
import { openPicker } from './picker.js';

let barEl, addEl, menuEl;

export function initTabs(container) {
  barEl = container.querySelector('#tabs');
  addEl = container.querySelector('#btn-new-tab');
  menuEl = container.querySelector('#new-tab-menu');
  addEl.addEventListener('click', toggleMenu);
  menuEl.addEventListener('click', (e) => {
    const kind = e.target.closest('[data-new-tab]')?.dataset.newTab;
    if (!kind) return;
    closeMenu();
    kind === 'file' ? openPicker() : add(kind);
  });
  document.addEventListener('mousedown', (e) => {
    if (!menuEl.contains(e.target) && !addEl.contains(e.target)) closeMenu();
  });
  document.addEventListener('keydown', (e) => e.key === 'Escape' && closeMenu());
  barEl.addEventListener('click', onClick);
  barEl.addEventListener('dblclick', onRename);
  subscribe(render);
}

function render() {
  const w = workspace();
  const active = activeTab();
  addEl.disabled = !w || w.missing;
  barEl.innerHTML = tabsOf(w).map((t) => `
    <div class="tab${t.id === active?.id ? ' active' : ''}" data-tab="${esc(t.id)}" title="${esc(tabTitle(t))}">
      ${t.kind === 'file' ? `<span class="ic" data-t="${fileKind(t.name)}"></span>` : `<span class="dot${t.running ? ' on' : ''}"></span>
      <span class="tab-kind">${t.kind === 'claude' ? '✳' : '$'}</span>`}
      <span class="tab-name">${esc(t.name)}</span>
      <button type="button" class="icon-btn del" data-act="close" title="Close tab">×</button>
    </div>`).join('');
  barEl.querySelector('.tab.active')?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
}

function tabTitle(t) {
  if (t.kind === 'file') return t.path;
  return `${t.kind === 'claude' ? 'Claude Code' : 'Shell'}: double-click to rename`;
}

function toggleMenu() {
  if (!menuEl.classList.contains('hidden')) return closeMenu();
  const pane = menuEl.parentElement.getBoundingClientRect(); // a hidden menu has no offsetParent
  const btn = addEl.getBoundingClientRect();
  menuEl.style.left = `${Math.max(4, btn.left - pane.left)}px`;
  menuEl.style.top = `${btn.bottom - pane.top + 4}px`;
  menuEl.classList.remove('hidden');
  menuEl.querySelector('button').focus();
}

function closeMenu() {
  menuEl.classList.add('hidden');
}

function tabOf(e) {
  const id = e.target.closest('[data-tab]')?.dataset.tab;
  return tabsOf(workspace()).find((t) => t.id === id) || null;
}

// neighbour is the tab to show once t closes: the one before it, else after.
function neighbour(t) {
  const tabs = tabsOf(workspace());
  const i = tabs.findIndex((x) => x.id === t.id);
  return (tabs[i - 1] || tabs[i + 1])?.id;
}

async function onClick(e) {
  const t = tabOf(e);
  if (!t) return;
  // Re-rendering the active tab would swallow the second click of a rename.
  if (!e.target.closest('[data-act=close]')) return t.id !== activeTab()?.id && selectTab(t.id);
  const next = t.id === activeTab()?.id ? neighbour(t) : null;
  if (t.kind === 'file') {
    closeFile(t.path);
  } else {
    if (t.running && !confirm(`Close "${t.name}"? Its shell and anything running in it stop.`)) return;
    const res = await api('DELETE', `/api/tabs/${encodeURIComponent(t.id)}`);
    if (!res.ok) return alert(res.data.error);
    await reload();
  }
  if (next) selectTab(next);
}

async function onRename(e) {
  const t = tabOf(e);
  if (!t || t.kind === 'file' || e.target.closest('[data-act=close]')) return;
  const name = prompt('Tab name', t.name)?.trim();
  if (!name || name === t.name) return;
  const res = await api('PATCH', `/api/tabs/${encodeURIComponent(t.id)}`, { name });
  if (!res.ok) return alert(res.data.error);
  reload();
}

async function add(kind) {
  const w = workspace();
  if (!w) return;
  const res = await api('POST', `/api/workspaces/${encodeURIComponent(w.id)}/tabs`, { kind });
  if (!res.ok) return alert(res.data.error);
  await reload();
  selectTab(res.data.id);
}
