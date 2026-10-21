// web/src/orch/tabs.js
// Tab bar of the selected workspace: shells, Claude Code chats and open
// files. "+" offers a new Claude or shell tab, or the open file picker; the
// empty center's welcome view offers the same. Double-click renames a
// terminal tab after what it is for.
import { $, esc } from '../state.js';
import { call, failed } from './api.js';
import { subscribe, reload, workspace, tabsOf, activeTab, selectTab, closeFile } from './store.js';
import { fileKind } from './tree.js';
import { openPicker } from './picker.js';

const ICON_CLOSE = '<svg viewBox="0 0 10 10"><path d="M2 2l6 6M8 2l-6 6"/></svg>';
const TAB_STATUS = { working: 'working', waiting: 'waiting for you', idle: 'idle' };

let barEl, addEl, menuEl;

export function initTabs() {
  barEl = $('#o-tabs');
  addEl = $('#o-new-tab');
  menuEl = $('#o-new-menu');
  addEl.addEventListener('click', toggleNewMenu);
  menuEl.addEventListener('click', e => {
    const kind = e.target.closest('[data-new-tab]')?.dataset.newTab;
    if (!kind) return;
    closeNewMenu();
    startNew(kind);
  });
  $('#o-empty').addEventListener('click', e => {
    const kind = e.target.closest('[data-new-tab]')?.dataset.newTab;
    if (kind) startNew(kind);
  });
  document.addEventListener('mousedown', e => {
    if (!menuEl.contains(e.target) && !addEl.contains(e.target)) closeNewMenu();
  });
  menuEl.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeNewMenu();
  });
  barEl.addEventListener('click', onTabClick);
  barEl.addEventListener('dblclick', onRename);
  subscribe(renderTabs);
}

function renderTabs() {
  const w = workspace();
  const active = activeTab();
  addEl.disabled = !w || w.missing;
  barEl.innerHTML = tabsOf(w).map(t => `
    <div class="tab${t.id === active?.id ? ' active' : ''}" data-tab="${esc(t.id)}" title="${esc(tabTitle(t))}" role="tab" aria-selected="${t.id === active?.id}">
      ${t.kind === 'file' ? `<span class="o-tab-ic" data-t="${fileKind(t.name)}"></span>` : `<span class="o-tab-dot${t.running ? ' on' : ''}${t.status ? ' ' + esc(t.status) : ''}"></span>
      <span class="o-tab-kind">${t.kind === 'claude' ? '✳' : '$'}</span>`}
      <span class="tn">${esc(t.name)}</span>
      <button type="button" class="x" data-act="close" title="Close tab" aria-label="Close tab">${ICON_CLOSE}</button>
    </div>`).join('');
  barEl.querySelector('.tab.active')?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
}

function tabTitle(t) {
  if (t.kind === 'file') return t.path;
  const what = t.kind === 'claude' ? 'Claude Code' : 'Shell';
  const st = !t.running ? 'stopped' : TAB_STATUS[t.status] || 'running';
  return `${what}, ${st}: double-click to rename`;
}

function toggleNewMenu() {
  if (!menuEl.hidden) return closeNewMenu();
  menuEl.hidden = false; // a hidden menu has no offsetParent or width
  const pane = menuEl.offsetParent.getBoundingClientRect();
  const btn = addEl.getBoundingClientRect();
  menuEl.style.left = `${Math.max(4, Math.min(btn.left - pane.left, pane.width - menuEl.offsetWidth - 4))}px`;
  menuEl.style.top = `${btn.bottom - pane.top + 4}px`;
  menuEl.querySelector('button').focus();
}

function closeNewMenu() {
  menuEl.hidden = true;
}

function tabAt(e) {
  const id = e.target.closest('[data-tab]')?.dataset.tab;
  return tabsOf(workspace()).find(t => t.id === id) || null;
}

// neighbour is the tab to show once t closes: the one before it, else after.
function neighbour(t) {
  const tabs = tabsOf(workspace());
  const i = tabs.findIndex(x => x.id === t.id);
  return (tabs[i - 1] || tabs[i + 1])?.id;
}

async function onTabClick(e) {
  const t = tabAt(e);
  if (!t) return;
  // Re-rendering the active tab would swallow the second click of a rename.
  if (!e.target.closest('[data-act=close]')) return t.id !== activeTab()?.id && selectTab(t.id);
  const next = t.id === activeTab()?.id ? neighbour(t) : null;
  if (t.kind === 'file') {
    closeFile(t.path);
  } else {
    if (t.running && !confirm(`Close "${t.name}"? Its shell and anything running in it stop.`)) return;
    const res = await call('DELETE', `api/tabs/${encodeURIComponent(t.id)}`);
    if (!res.ok) return failed(res);
    await reload();
  }
  if (next) selectTab(next);
}

async function onRename(e) {
  const t = tabAt(e);
  if (!t || t.kind === 'file' || e.target.closest('[data-act=close]')) return;
  const name = prompt('Tab name', t.name)?.trim();
  if (!name || name === t.name) return;
  const res = await call('PATCH', `api/tabs/${encodeURIComponent(t.id)}`, { name });
  if (!res.ok) return failed(res);
  reload();
}

function startNew(kind) {
  if (kind === 'file') openPicker();
  else addTab(kind);
}

async function addTab(kind) {
  const w = workspace();
  if (!w) return;
  const res = await call('POST', `api/workspaces/${encodeURIComponent(w.id)}/tabs`, { kind });
  if (!res.ok) return failed(res);
  await reload();
  selectTab(res.data.id);
}
