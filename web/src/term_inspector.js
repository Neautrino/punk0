// web/src/term_inspector.js
// The editor's terminals: one pane per group (termview.js), the right
// side's and the center's, both showing tabs of the agents page that owns
// this folder. The tabs themselves, in what order and in which group, are
// tabs.js's business (editorgroups.js); this module knows which exist
// (one 3 s poll for the page, 'termtabs:changed'), attaches a pane to one,
// starts and stops them, and types a selection into an agent's prompt.
import { $, S, api } from './state.js';
import { CENTER, RIGHT, activeEntry, groupOf, other, place } from './editorgroups.js';
import { sideShown, rightView, showRightView } from './sideviews.js';
import { on, emit } from './bus.js';
import { showToast } from './ui.js';
import { getTabLabel } from './term-util.js';
import { createTermView } from './termview.js';
import { agentIcon } from './agenticons.js';
import { openFile, switchTab, drawTabs } from './tabs.js';

let agentMenuEl = null;
const views = {}; // group -> its termview.js pane
let lastGroup = RIGHT; // the group whose terminal was shown last (Alt+T)
let workspaceTabs = [];
let pollTimer = null;

// parentURL is a path of the orchestrator that opened this editor.
export const parentURL = path => {
  const p = path.startsWith('/') ? path.slice(1) : path;
  const base = new URL(S.meta?.workbench || '/', window.location.origin);
  return new URL(p, base).href;
};

const wsURL = path => {
  const u = new URL(parentURL(path));
  u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
  return u.href;
};

// fileLinks is filelinks.js's view of this editor's folder: a path in the
// terminal opens as an editor tab, at its line.
function fileLinks() {
  if (!S.meta?.root) return null;
  return {
    root: S.meta.root,
    list: dir => api('/api/tree', { dir }).then(j => j.children),
    open: (path, line, col) => openFile(path, line ? { line, col, view: 'source' } : {}),
  };
}

// newEditorTermView binds a terminal pane of this editor in el: the
// agents page's websocket, file paths opening editor tabs.
export const newEditorTermView = el => createTermView(el, { vendorURL: parentURL, socketURL: wsURL, fileLinks });

export const termTabs = () => workspaceTabs;
export const termTab = id => workspaceTabs.find(t => t.id === id) || null;
export const isRunning = id => !!termTab(id)?.running;
// shownTerm is the tab a group's pane has attached, '' for none.
export const shownTerm = g => views[g]?.id() || '';

// attachTerm shows tab id in group g's pane. pick says the user chose it,
// which starts a stopped tab and makes it the one punk0 reopens.
export function attachTerm(g, id, { pick = false, focus = true } = {}) {
  if (!views[g]) return;
  lastGroup = g;
  views[g].attach(id, { pick, focus });
}

// detachTerm empties a group's pane, leaving note (the idle text) in its
// place; the tab itself keeps running on the server. The pane writes a
// note only when it changes, so the 3 s poll may repeat it.
export function detachTerm(g, note = '') {
  const v = views[g];
  if (!v) return;
  if (v.id()) v.attach(null);
  v.showNotice(note);
}

// workspaceID is this editor's workspace at the orchestrator, '' when it
// has none (no tabs, no drag).
export function workspaceID() {
  return S.meta?.workspace || S.meta?.workspaceId || '';
}

// loadWorkspaceTabs fetches this editor's workspace tabs into workspaceTabs,
// attaching nothing, and says whether it got them; 'termtabs:changed' tells
// tabs.js (the groups and their bars) and the status bar.
async function loadWorkspaceTabs() {
  const wsId = workspaceID();
  if (!wsId) return false;
  try {
    const res = await fetch(parentURL(`api/workspaces/${encodeURIComponent(wsId)}/tabs`));
    if (!res.ok) return false;
    const data = await res.json();
    workspaceTabs = data.tabs || [];
  } catch {
    return false;
  }
  emit('termtabs:changed');
  return true;
}

export const refreshWorkspaceTabs = () => loadWorkspaceTabs();

// The running agent tabs with that status, in tab order: what the status
// bar's needs-you and working buttons count and step through.
const agentTabs = status => workspaceTabs.filter(t => t.running && t.status === status);
export const agentTabCount = status => agentTabs(status).length;

// nextAgentTab shows the tab after the shown one with that status, so
// clicking again walks through all of them, in the group holding it.
export function nextAgentTab(status) {
  const list = agentTabs(status);
  if (!list.length) return;
  const shown = shownTerm(S.groups.focus) || shownTerm(other(S.groups.focus));
  const { id } = list[(list.findIndex(t => t.id === shown) + 1) % list.length];
  if (id === shown) showTerminal();
  else openTab(id);
}

// watchWorkspaceTabs refreshes the tabs every 3 s while the page is
// visible: the bars, the status bar's counts, and each group's view of
// what it holds (tabs.js).
export function watchWorkspaceTabs() {
  if (pollTimer || !workspaceID()) return;
  const tick = () => {
    if (document.hidden) return;
    loadWorkspaceTabs();
  };
  pollTimer = setInterval(tick, 3000);
  document.addEventListener('visibilitychange', tick);
  tick();
}

// addTab starts a kind tab in workspace wsId, else this editor's (a folder
// backend id: the orchestrator picks its workspace). New terminals open on
// the right side.
export async function addTab(kind, wsId = workspaceID()) {
  if (!wsId) {
    showToast('!', 'No workspace ID available');
    return;
  }

  try {
    const res = await fetch(parentURL(`api/workspaces/${encodeURIComponent(wsId)}/tabs`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind }),
    });
    if (res.ok) {
      const added = await res.json();
      showTermSide();
      await refreshWorkspaceTabs();
      openTab(added.id);
    } else {
      const err = await res.json().catch(() => ({}));
      showToast('!', err.error || `Failed to create ${kind} tab`);
    }
  } catch (err) {
    showToast('!', `Failed to create ${kind} tab: ` + err.message);
  }
}

export async function removeTab(tabId) {
  try {
    const res = await fetch(parentURL(`api/tabs/${encodeURIComponent(tabId)}`), {
      method: 'DELETE',
    });
    if (res.ok) {
      for (const g of [CENTER, RIGHT]) if (shownTerm(g) === tabId) detachTerm(g);
      await refreshWorkspaceTabs();
    } else {
      const err = await res.json().catch(() => ({}));
      showToast('!', err.error || 'Failed to close tab');
    }
  } catch (err) {
    showToast('!', 'Failed to close tab: ' + err.message);
  }
}

const sleep = ms => new Promise(r => setTimeout(r, ms));

async function socketOpen(g, ms = 5000) {
  for (let t = 0; t < ms; t += 100) {
    if (views[g].open()) return true;
    await sleep(100);
  }
  return false;
}

/* A tab the selection can go to: the one the focused group shows if it
   runs an agent, else the other group's, else the first agent tab, running
   ones first. A plain shell is never a target: it would run the pasted
   lines as commands. */
function chatTab() {
  const isAgent = t => t.kind !== 'shell';
  for (const g of [S.groups.focus, other(S.groups.focus)]) {
    const shown = termTab(shownTerm(g));
    if (shown && isAgent(shown)) return shown;
  }
  return workspaceTabs.find(t => isAgent(t) && t.running) || workspaceTabs.find(isAgent) || null;
}

/* Add to Chat: types text into an agent's prompt through xterm's paste, which
   brackets it when the agent asked for bracketed paste (Claude Code does), so
   the newlines stay in the input and nothing is sent until the user presses
   Enter. Control characters are dropped so the text cannot end the paste. */
export async function sendToActiveTerminal(text) {
  await refreshWorkspaceTabs();
  const tab = chatTab();
  if (!tab) {
    showToast('!', 'Start an agent tab first (+ in the terminal\'s row)');
    return false;
  }
  if (tab.status === 'waiting') {
    showToast('!', getTabLabel(tab) + ' is waiting for your answer');
    return false;
  }
  const wasRunning = tab.running;
  const g = groupOf(S.groups, tab.id) || RIGHT;
  // The text must land where the user can see it answered.
  if (shownTerm(g) !== tab.id || !views[g].open()) openTab(tab.id);
  else if (g === RIGHT) showTermSide();
  else views[g].focus();
  if (!await views[g].ready().catch(() => null) || !await socketOpen(g)) {
    showToast('!', 'Could not connect to ' + getTabLabel(tab));
    return false;
  }
  if (!wasRunning) {
    // Just started: give the agent time to draw its prompt before typing into
    // it. Claude reports idle through its hooks; others report nothing.
    for (let t = 0; t < 20000; t += 500) {
      await sleep(500);
      if (tab.kind !== 'claude' && t >= 2500) break;
      await refreshWorkspaceTabs();
      if (workspaceTabs.find(x => x.id === tab.id)?.status === 'idle') break;
    }
  }
  const clean = text.replace(/\r\n?/g, '\n').replace(/[\x00-\x08\x0b-\x1f\x7f]/g, '');
  views[g].paste(clean + '\n');
  showToast('✓', 'Added to ' + getTabLabel(tab));
  return true;
}

export const termSideShown = () => sideShown('right') && rightView() === 'term';

// showTermSide shows the right side on its Terminal mode, where its own
// tabs live.
export function showTermSide() {
  if (termSideShown()) onTermInspectorShown();
  else showRightView('term'); // 'side:toggled' or 'rightview:changed' calls onTermInspectorShown
}

// showTerminal puts the caret in a terminal (Alt+T): the group that showed
// one last, which may be the center.
export function showTerminal() {
  if (lastGroup === CENTER && S.center === 'term' && shownTerm(CENTER)) {
    views[CENTER].focus();
    return;
  }
  showTermSide();
}

// openTab shows tab id in the group holding it, the right side for one no
// group has yet: a chat the orchestrator has just resumed (chats.js), or
// the agent the status bar stepped to.
export function openTab(id) {
  if (!groupOf(S.groups, id)) S.groups = place(S.groups, { kind: 'term', id }, RIGHT);
  switchTab(id);
  refreshWorkspaceTabs();
}

export function onTermInspectorShown() {
  refreshWorkspaceTabs();
  setTimeout(() => {
    views[RIGHT].fit();
    const a = activeEntry(S.groups, RIGHT);
    if (a?.kind !== 'view') views[RIGHT].focus();
  }, 50);
  watchWorkspaceTabs();
}

const SUPPORTED_AGENTS = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex' },
  { id: 'opencode', label: 'OpenCode' },
  { id: 'gemini', label: 'Gemini CLI' },
  { id: 'copilot', label: 'GitHub Copilot' },
  { id: 'agy', label: 'Antigravity' },
  { id: 'cursor-agent', label: 'Cursor Agent' },
  { id: 'qwen', label: 'Qwen Code' },
  { id: 'droid', label: 'Factory Droid' },
  { id: 'kimi', label: 'Kimi Code' },
  { id: 'crush', label: 'Crush' },
  { id: 'pi', label: 'Pi' },
  { id: 'omp', label: 'Oh My Pi' },
  { id: 'aider', label: 'Aider' },
];

// agentChoices is the + menu's items for a new tab in workspace wsId (else
// this editor's): an agent each, then a shell. Chats' section headers offer
// them too, for a chat in that section's workspace.
export const agentChoices = wsId => [
  ...SUPPORTED_AGENTS.map(a => ({ icon: agentIcon(a.id, 'agent-icon'), label: a.label, run: () => addTab(a.id, wsId || undefined) })),
  { icon: agentIcon('shell', 'agent-icon'), label: 'Shell', run: () => addTab('shell', wsId || undefined) },
];

// The + menu: a tab for each agent, then a shell.
function toggleAgentMenu(e) {
  e.stopPropagation();
  if (!agentMenuEl.hidden) {
    agentMenuEl.hidden = true;
    return;
  }
  agentMenuEl.replaceChildren(...agentChoices().map(c => {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.setAttribute('role', 'menuitem');
    btn.innerHTML = `${c.icon}<span>${c.label}</span>`;
    btn.onclick = ev => {
      ev.stopPropagation();
      agentMenuEl.hidden = true;
      c.run();
    };
    return btn;
  }));
  agentMenuEl.hidden = false;
}

export function initTermInspector() {
  views[RIGHT] = newEditorTermView($('#editor-term-container'));
  views[CENTER] = newEditorTermView($('#center-term'));
  agentMenuEl = $('#term-agent-menu');
  const addBtn = $('#btn-term-add');
  addBtn.onclick = toggleAgentMenu;
  document.addEventListener('click', e => {
    if (!agentMenuEl.hidden && !agentMenuEl.contains(e.target) && !addBtn.contains(e.target)) agentMenuEl.hidden = true;
  });
  on('side:toggled', ({ side, shown }) => { if (side === 'right' && shown && rightView() === 'term') onTermInspectorShown(); });
  on('rightview:changed', ({ view }) => { if (view === 'term' && sideShown('right')) onTermInspectorShown(); });
  drawTabs();
}
