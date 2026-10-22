// web/src/orch/tabs.js
// Tab bars of the selected workspace. In the split layout (split.js) the
// terminals' bar holds shells and agent chats and the file group's bar the
// open files; in the tabs layout the terminals' bar holds both. "+" offers a
// new tab of every installed agent not in agents.hidden, a shell, or the
// open file picker, agents.default first; the empty center's welcome view
// offers the same, and the file group's "+" opens the picker. Double-click
// renames a terminal tab after what it is for. At the right end, the
// workspace's folder opens in the full editor.
import { $, $$, esc } from '../state.js';
import { on } from '../bus.js';
import { call, failed, openEditor } from './api.js';
import { subscribe, reload, workspace, tabsOf, fileTabsOf, activeTab, termTab, fileTab, selectTab, closeFile } from './store.js';
import { fileKind } from './tree.js';
import { openPicker } from './picker.js';
import { getSetting, visibleAgents, defaultAgent } from './settings.js';
import { isSplit } from './split.js';

const ICON_CLOSE = '<svg viewBox="0 0 10 10"><path d="M2 2l6 6M8 2l-6 6"/></svg>';
const TAB_STATUS = { working: 'working', waiting: 'waiting for you', idle: 'idle' };

let barEl, fileBarEl, addEl, menuEl;
let agents = []; // GET api/agents

export function initTabs() {
  barEl = $('#o-tabs');
  fileBarEl = $('#o-file-tabs');
  addEl = $('#o-new-tab');
  menuEl = $('#o-new-menu');
  for (const b of $$('.o-open-editor')) b.addEventListener('click', () => {
    const w = workspace();
    if (w && !w.missing) openEditor(w.href);
  });
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
  for (const el of [barEl, fileBarEl]) {
    el.addEventListener('click', onTabClick);
    el.addEventListener('dblclick', onRename);
    el.addEventListener('wheel', onTabWheel, { passive: false });
  }
  $('#o-file-new').addEventListener('click', () => openPicker());
  subscribe(renderTabs);
  on('setting:changed', key => key.startsWith('agents.') && renderAgents());
  loadAgents();
}

// A mouse wheel scrolls a bar with more tabs than fit sideways. Horizontal
// deltas (trackpads, Shift+wheel) already scroll it natively.
function onTabWheel(e) {
  const el = e.currentTarget;
  if (e.ctrlKey || Math.abs(e.deltaY) <= Math.abs(e.deltaX) || el.scrollWidth <= el.clientWidth) return;
  e.preventDefault();
  const step = e.deltaMode === WheelEvent.DOM_DELTA_LINE ? 40 : e.deltaMode === WheelEvent.DOM_DELTA_PAGE ? el.clientWidth : 1;
  el.scrollLeft += e.deltaY * step;
}

async function loadAgents() {
  const res = await call('GET', 'api/agents');
  if (res.ok) agents = res.data || [];
  renderAgents();
}

// renderAgents fills the "+" menu and the welcome view with the agents a new
// tab can run, agents.default first.
function renderAgents() {
  const visible = visibleAgents(agents);
  const first = defaultAgent(visible);
  visible.sort((a, b) => (b.id === first) - (a.id === first));
  const icon = a => `<span class="o-menu-kind${a.id === 'claude' ? ' claude' : ''}">${a.id === 'claude' ? '✳' : a.id === 'shell' ? '$' : '◆'}</span>`;
  const what = a => a.id === 'shell' ? 'your own shell, in a real terminal' : `runs <code>${esc(a.id)}</code> in this folder`;
  $('#o-new-agents').innerHTML = visible.map(a =>
    `<button type="button" role="menuitem" data-new-tab="${esc(a.id)}">${icon(a)}${esc(a.label)}</button>`).join('');
  $('#o-empty-agents').innerHTML = visible.map(a =>
    `<button type="button" data-new-tab="${esc(a.id)}">${icon(a)}<span>New ${esc(a.label)}${a.id === 'shell' ? '' : ' chat'}</span><span class="wb-faint">${what(a)}</span></button>`).join('');
}

function renderTabs() {
  const w = workspace();
  const off = !w || w.missing;
  addEl.disabled = $('#o-file-new').disabled = off;
  for (const b of $$('.o-open-editor')) b.disabled = off;
  if (isSplit()) {
    barEl.innerHTML = tabsHTML(w?.tabs || [], termTab());
    fileBarEl.innerHTML = tabsHTML(fileTabsOf(w), fileTab());
  } else {
    barEl.innerHTML = tabsHTML(tabsOf(w), activeTab());
    fileBarEl.innerHTML = '';
  }
  for (const el of [barEl, fileBarEl]) el.querySelector('.tab.active')?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
}

function tabsHTML(tabs, active) {
  return tabs.map(t => `
    <div class="tab${t.id === active?.id ? ' active' : ''}" data-tab="${esc(t.id)}" title="${esc(tabTitle(t))}" role="tab" aria-selected="${t.id === active?.id}">
      ${t.kind === 'file' ? `<span class="o-tab-ic" data-t="${fileKind(t.name)}"></span>` : `<span class="o-tab-dot${t.running ? ' on' : ''}${t.status ? ' ' + esc(t.status) : ''}"></span>
      <span class="o-tab-kind">${t.kind === 'claude' ? '✳' : t.kind === 'shell' ? '$' : '◆'}</span>`}
      <span class="tn">${esc(t.name)}</span>
      <button type="button" class="x" data-act="close" title="Close tab" aria-label="Close tab">${ICON_CLOSE}</button>
    </div>`).join('');
}

function tabTitle(t) {
  if (t.kind === 'file') return t.path;
  const what = t.kind === 'claude' ? 'Claude Code' : t.kind === 'shell' ? 'Shell' : t.name;
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

// group is the tabs of t's bar and the one shown from them: in the split
// layout its own group's, else all of them.
function group(t) {
  const w = workspace();
  if (!isSplit()) return { tabs: tabsOf(w), shown: activeTab() };
  return t.kind === 'file' ? { tabs: fileTabsOf(w), shown: fileTab() } : { tabs: w?.tabs || [], shown: termTab() };
}

// neighbour is the tab of tabs to show once t closes: the one before it,
// else after.
function neighbour(t, tabs) {
  const i = tabs.findIndex(x => x.id === t.id);
  return (tabs[i - 1] || tabs[i + 1])?.id;
}

async function onTabClick(e) {
  const t = tabAt(e);
  if (!t) return;
  const { tabs, shown } = group(t);
  // Re-rendering the active tab would swallow the second click of a rename.
  if (!e.target.closest('[data-act=close]')) return t.id !== shown?.id && selectTab(t.id);
  const next = t.id === shown?.id ? neighbour(t, tabs) : null;
  if (t.kind === 'file') {
    closeFile(t.path);
  } else {
    if (t.running && getSetting('terminal.confirmCloseRunning') !== false &&
      !confirm(`Close "${t.name}"? Its shell and anything running in it stop.`)) return;
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
