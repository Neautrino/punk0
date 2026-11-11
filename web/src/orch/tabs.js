// web/src/orch/tabs.js
// Tab bars of the selected workspace. In the split layout (split.js) the
// terminals' bar holds shells and agent chats and the file group's bar the
// open files; in the tabs layout the terminals' bar holds both. "+" offers a
// shell and the open file picker, then below a rule a new tab of every
// installed agent not in agents.hidden, agents.default first. The empty
// center's welcome view starts the default agent, offers the other agents in
// a menu of their own, then the shell, a file, the editor and a new
// workspace, then a few of the workspace's own tabs to open again. The file
// group's "+" opens the picker. Double-click renames a terminal tab
// after what it is for; a right click offers to stop it (tabstop.js), which
// keeps the tab to start again when opened. At the right end, the
// workspace's folder opens in the full editor.
import { $, $$, esc, keyCaps } from '../state.js';
import { emit, on } from '../bus.js';
import { call, failed, openEditor } from './api.js';
import { subscribe, reload, workspace, projectOf, tabsOf, fileTabsOf, activeTab, termTab, fileTab, selectTab, closeFile, closeChanges, closeWhatsNew } from './store.js';
import { fileKind } from './tree.js';
import { openPicker } from './picker.js';
import { getSetting, visibleAgents, defaultAgent } from './settings.js';
import { isSplit } from './split.js';
import { scrollTabsOnWheel } from '../tabwheel.js';
import { agentIcon } from '../agenticons.js';
import { getTabLabel } from '../term-util.js';
import { stopTab } from '../tabstop.js';
import { openNewWorkspace } from './newworkspace.js';
import { openAddProject } from './addproject.js';

const ICON_CLOSE = '<svg viewBox="0 0 10 10"><path d="M2 2l6 6M8 2l-6 6"/></svg>';
const ICON_CHANGES = '<svg class="o-tab-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v8M8 9h8M8 18h8"/></svg>';
const ICON_NEWS = '<svg class="o-tab-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3l2.6 5.6 6.1.7-4.5 4.2 1.2 6L12 16.6 6.6 19.5l1.2-6-4.5-4.2 6.1-.7z"/></svg>';
const TAB_STATUS = { working: 'working', waiting: 'waiting for you', idle: 'idle' };
const RESUME_MAX = 4; // the welcome view's tabs to open again; the bar has them all

let barEl, fileBarEl, addEl, menuEl, tabMenuEl, agentMenuEl, emptyEl;
let agents = []; // GET api/agents

export function initTabs() {
  barEl = $('#o-tabs');
  fileBarEl = $('#o-file-tabs');
  addEl = $('#o-new-tab');
  menuEl = $('#o-new-menu');
  tabMenuEl = $('#o-tab-menu');
  agentMenuEl = $('#o-agent-menu');
  emptyEl = $('#o-empty');
  if (addEl) {
    addEl.addEventListener('click', e => {
      e.stopPropagation();
      toggleNewMenu();
    });
  }
  for (const b of $$('.o-open-editor')) b.addEventListener('click', openInEditor);
  menuEl.addEventListener('click', e => {
    const kind = e.target.closest('[data-new-tab]')?.dataset.newTab;
    if (!kind) return;
    closeNewMenu();
    startNew(kind);
  });
  emptyEl.addEventListener('click', onWelcomeClick);
  agentMenuEl.addEventListener('click', e => {
    const kind = e.target.closest('[data-new-tab]')?.dataset.newTab;
    if (!kind) return;
    closeAgentMenu();
    startNew(kind);
  });
  // Enter with nothing focused starts the default agent, as the welcome
  // view's first button says; a focused control keeps its own Enter.
  addEventListener('keydown', e => {
    if (e.key !== 'Enter' || e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return;
    if (e.target !== document.body || emptyEl.hidden || emptyEl.dataset.ws !== 'yes') return;
    const main = $('#o-empty-main');
    if (!main) return;
    e.preventDefault();
    main.click();
  });
  document.addEventListener('mousedown', e => {
    if (!menuEl.contains(e.target) && !addEl.contains(e.target)) closeNewMenu();
    if (!tabMenuEl.contains(e.target)) closeTabMenu();
    if (!agentMenuEl.contains(e.target) && !e.target.closest?.('#o-empty-more')) closeAgentMenu();
  });
  menuEl.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeNewMenu();
  });
  tabMenuEl.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeTabMenu();
  });
  agentMenuEl.addEventListener('keydown', e => {
    if (e.key !== 'Escape') return;
    closeAgentMenu();
    $('#o-empty-more')?.focus();
  });
  tabMenuEl.addEventListener('click', onTabMenu);
  for (const el of [barEl, fileBarEl]) {
    el.addEventListener('click', onTabClick);
    el.addEventListener('dblclick', onRename);
    el.addEventListener('contextmenu', openTabMenu);
    scrollTabsOnWheel(el);
  }
  $('#o-file-new').addEventListener('click', () => openPicker());
  subscribe(renderTabs);
  on('setting:changed', key => key.startsWith('agents.') && renderAgents());
  loadAgents();
}

async function loadAgents() {
  const res = await call('GET', 'api/agents');
  if (res.ok) agents = res.data || [];
  renderAgents();
}

// renderAgents fills the "+" menu and the welcome view with the agents a new
// tab can run, agents.default first. The menu puts the shell over Open file,
// above the rule, and the agents below it. The welcome view starts the
// default one, lists the others in its Another agent… menu and offers the
// shell after them.
function renderAgents() {
  const visible = visibleAgents(agents);
  const first = defaultAgent(visible);
  visible.sort((a, b) => (b.id === first ? 1 : 0) - (a.id === first ? 1 : 0));
  const icon = a => agentIcon(a.id, 'o-menu-kind');
  const item = a => `<button type="button" role="menuitem" data-new-tab="${esc(a.id)}">${icon(a)}${esc(a.label)}</button>`;
  const chats = visible.filter(a => a.id !== 'shell');
  $('#o-new-shell').innerHTML = visible.filter(a => a.id === 'shell').map(item).join('');
  $('#o-new-sep').hidden = !chats.length;
  $('#o-new-agents').innerHTML = chats.map(item).join('');

  const main = visible.find(a => a.id === first);
  const others = chats.filter(a => a.id !== first);
  const shell = first !== 'shell' && visible.find(a => a.id === 'shell');
  const title = a => a.id === 'shell' ? 'New shell' : `New ${a.label} chat`;
  $('#o-empty-agents').innerHTML = [
    main && `<button type="button" id="o-empty-main" class="o-welcome-main" data-new-tab="${esc(main.id)}">${icon(main)}<span>${esc(title(main))}</span><span class="wb-faint">${keyCaps('Enter')}</span></button>`,
    others.length && `<button type="button" id="o-empty-more" aria-haspopup="menu" aria-expanded="false"><svg class="o-welcome-ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg><span>Another agent…</span><span class="wb-faint">${others.length} more</span></button>`,
    shell && `<button type="button" data-new-tab="shell">${icon(shell)}<span>New shell</span></button>`,
  ].filter(Boolean).join('');
  agentMenuEl.innerHTML = others.map(item).join('');
  closeAgentMenu();
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
  // A scratch chat is a folder of its own, with no project to branch from.
  $('#o-empty-nw').hidden = !w || !!projectOf(w.id)?.scratch;
  renderResume(w);
}

// renderResume lists a few of workspace w's terminal tabs in the welcome
// view, which shows when none is open in this page: opening one is the
// user's choice. Running tabs come first; the tab bar has the rest.
function renderResume(w) {
  const tabs = [...(w?.tabs || [])].sort((a, b) => Number(b.running) - Number(a.running));
  if (!tabs.length) {
    $('#o-empty-tabs').innerHTML = '';
    return;
  }
  const more = tabs.length - RESUME_MAX;
  $('#o-empty-tabs').innerHTML = `<span class="o-welcome-head">Open again</span>` + tabs.slice(0, RESUME_MAX).map(t =>
    `<button type="button" data-pick-tab="${esc(t.id)}">${agentIcon(t.kind, 'o-menu-kind')}<span>${esc(getTabLabel(t))}</span><span class="wb-faint">${!t.running ? 'stopped' : TAB_STATUS[t.status] || 'running'}</span></button>`).join('') +
    (more > 0 ? `<span class="o-welcome-head">${more} more in the tab bar</span>` : '');
}

function onWelcomeClick(e) {
  const pick = e.target.closest('[data-pick-tab]')?.dataset.pickTab;
  if (pick) return selectTab(pick);
  const kind = e.target.closest('[data-new-tab]')?.dataset.newTab;
  if (kind) return startNew(kind);
  const btn = e.target.closest('[data-act], #o-empty-more');
  if (!btn) return;
  if (btn.id === 'o-empty-more') return toggleAgentMenu(btn);
  const act = btn.dataset.act;
  if (act === 'open-editor') return openInEditor();
  // Both panels open beside the sidebar, pointing at its own buttons.
  if (act === 'add-project') return openAddProject($('#o-add-project'));
  const p = workspace() && projectOf(workspace().id);
  if (act === 'new-workspace' && p && !p.scratch) openNewWorkspace(p, $('#o-add-feature'));
}

function openInEditor() {
  const w = workspace();
  if (w && !w.missing) openEditor(w.href);
}

// toggleAgentMenu drops the other agents under btn, or over it when the
// window has no room below.
function toggleAgentMenu(btn) {
  if (!agentMenuEl.hidden) return closeAgentMenu();
  agentMenuEl.hidden = false;
  const r = btn.getBoundingClientRect();
  const h = agentMenuEl.offsetHeight;
  const below = r.bottom + 4 + h <= innerHeight - 4;
  agentMenuEl.style.left = `${Math.max(4, Math.min(r.left, innerWidth - agentMenuEl.offsetWidth - 4))}px`;
  agentMenuEl.style.top = `${below ? r.bottom + 4 : Math.max(4, r.top - 4 - h)}px`;
  btn.setAttribute('aria-expanded', 'true');
  agentMenuEl.querySelector('button')?.focus();
}

function closeAgentMenu() {
  agentMenuEl.hidden = true;
  $('#o-empty-more')?.setAttribute('aria-expanded', 'false');
}

// label is what a tab says: a file its name, a terminal its own name, its
// chat's title or what runs in it (getTabLabel).
const label = t => t.kind === 'file' ? t.name : getTabLabel(t);

function tabsHTML(tabs, active) {
  return tabs.map(t => `
    <div class="tab${t.id === active?.id ? ' active' : ''}" data-tab="${esc(t.id)}" title="${esc(tabTitle(t))}" role="tab" aria-selected="${t.id === active?.id}">
      ${t.whatsNew ? ICON_NEWS : t.changes ? ICON_CHANGES : t.kind === 'file' ? `<span class="o-tab-ic" data-t="${fileKind(t.name)}"></span>` : `<span class="o-tab-dot${t.running ? ' on' : ''}${t.status ? ' ' + esc(t.status) : ''}"></span>
      ${agentIcon(t.kind, 'o-tab-kind')}`}
      <span class="tn">${esc(label(t))}</span>
      <button type="button" class="x" data-act="close" title="Close tab" aria-label="Close tab">${ICON_CLOSE}</button>
    </div>`).join('');
}

function tabTitle(t) {
  if (t.whatsNew) return `Release notes of punk0 v${t.whatsNew}`;
  if (t.changes) return 'Changes in this workspace';
  if (t.kind === 'file') return t.path;
  const st = !t.running ? 'stopped' : TAB_STATUS[t.status] || 'running';
  const what = getTabLabel({ kind: t.kind });
  return `${label(t)} (${what}, ${st}): double-click to rename`;
}

function toggleNewMenu() {
  if (!menuEl.hidden) return closeNewMenu();
  menuEl.hidden = false; // a hidden menu has no offsetParent or width
  const pane = menuEl.offsetParent.getBoundingClientRect();
  const btn = addEl.getBoundingClientRect();
  menuEl.style.left = `${Math.max(4, Math.min(btn.left - pane.left, pane.width - menuEl.offsetWidth - 4))}px`;
  menuEl.style.top = `${btn.bottom - pane.top + 4}px`;
  menuEl.querySelector('button')?.focus();
}

function closeNewMenu() {
  menuEl.hidden = true;
}

// openTabMenu is a terminal tab's right-click menu at the pointer: Stop while
// it runs, and what opening it again does.
function openTabMenu(e) {
  const t = tabAt(e);
  if (!t || t.kind === 'file' || t.whatsNew || t.changes) return;
  e.preventDefault();
  const note = t.kind === 'shell'
    ? 'Opening the tab again starts a new shell.'
    : 'Opening the tab again starts it; the agent picks its chat up where it can.';
  tabMenuEl.dataset.tab = t.id;
  tabMenuEl.innerHTML = `<button type="button" role="menuitem" data-act="stop"${t.running ? '' : ' disabled'}>${t.running ? 'Stop' : 'Stopped'}</button>
    <div class="o-menu-sep" role="separator"></div>
    <div class="o-menu-label">${note}</div>`;
  tabMenuEl.hidden = false;
  tabMenuEl.style.left = `${Math.max(4, Math.min(e.clientX, innerWidth - tabMenuEl.offsetWidth - 4))}px`;
  tabMenuEl.style.top = `${Math.max(4, Math.min(e.clientY, innerHeight - tabMenuEl.offsetHeight - 4))}px`;
  tabMenuEl.querySelector('button:not(:disabled)')?.focus();
}

function closeTabMenu() {
  tabMenuEl.hidden = true;
}

async function onTabMenu(e) {
  if (!e.target.closest('[data-act=stop]')) return;
  closeTabMenu();
  const t = tabsOf(workspace()).find(x => x.id === tabMenuEl.dataset.tab);
  if (t?.running && await stopTab(document.baseURI, { id: t.id, name: label(t) })) reload();
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
// else after. A stopped terminal is passed over: showing it would start it.
function neighbour(t, tabs) {
  const open = tabs.filter(x => x.id === t.id || x.kind === 'file' || x.running);
  const i = open.findIndex(x => x.id === t.id);
  return (open[i - 1] || open[i + 1])?.id;
}

async function onTabClick(e) {
  const t = tabAt(e);
  if (!t) return;
  const { tabs, shown } = group(t);
  // Re-rendering the active tab would swallow the second click of a rename.
  if (!e.target.closest('[data-act=close]')) return t.id !== shown?.id && selectTab(t.id);
  const next = t.id === shown?.id ? neighbour(t, tabs) : null;
  if (t.whatsNew) {
    closeWhatsNew();
  } else if (t.changes) {
    closeChanges();
  } else if (t.kind === 'file') {
    closeFile(t.path);
  } else {
    if (t.running && getSetting('terminal.confirmCloseRunning') !== false &&
      !confirm(`Close "${label(t)}"? Its shell and anything running in it stop.`)) return;
    const res = await call('DELETE', `api/tabs/${encodeURIComponent(t.id)}`);
    if (!res.ok) return failed(res);
    await reload();
    // Chats mode lists a chat as open while a tab resumes it: that tab is
    // gone now, and the list would say otherwise until its next poll.
    emit('orch:tabclosed');
  }
  if (next) selectTab(next);
}

async function onRename(e) {
  const t = tabAt(e);
  if (!t || t.kind === 'file' || e.target.closest('[data-act=close]')) return;
  const name = prompt('Tab name', label(t))?.trim();
  if (!name || name === label(t)) return;
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
