// web/src/orch/sidebar.js
// Left pane, in two views behind a tab: Projects (projects, their
// workspaces, and the pull requests open for review) and Scratch (chats that
// belong to no project). Each workspace is one feature: local ones share the
// project folder, worktree ones own a folder and branch, scratch chats own a
// folder outside any project. A folder's editor (px0's single-folder view)
// runs in a child the parent starts on demand; its state is the dot.
//
// Under the header, a search field narrows the view by name; the header's
// View options (listview.js) group, sort and filter it. A chat is named after
// its first prompt (scratch.go); renaming, grouping, pinning and archiving
// change labels only. New groups and group names are edited in place.
import { $, esc } from '../state.js';
import { emit } from '../bus.js';
import { showToast } from '../ui.js';
import { call, failed, openEditor } from './api.js';
import { state, subscribe, reload, selectWorkspace, workspace, tildify, wsLabel } from './store.js';
import { getSetting } from './settings.js';
import { openNewChat, closeNewChat } from './newchat.js';
import { openNewWorkspace, closeNewWorkspace } from './newworkspace.js';
import { openAddProject, closeAddProject } from './addproject.js';
import { openRemoveDialog, closeRemoveDialog } from './removedialog.js';
import { opts, setOpt, initViewMenu, syncViewButton, closeViewMenu, isRunning, statusOf, stateOf, STATES, agentOf, agentLabel } from './listview.js';

const ICON_PLUS = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>';
const ICON_X = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18"/></svg>';
const ICON_STOP = '<svg viewBox="0 0 24 24" width="11" height="11" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>';
const ICON_PR = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="18" r="2.5"/><path d="M6 8.5v7M18 15.5V9a3 3 0 0 0-3-3h-4"/><polyline points="13 4 11 6 13 8"/></svg>';
const ICON_FOLDER = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/></svg>';
const ICON_FOLDER_OPEN = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 18V7a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v1"/><path d="M3 18l2.6-6.5A2 2 0 0 1 7.5 10H21l-2.7 7.2a2 2 0 0 1-1.9 1.3H3Z"/></svg>';
const ICON_MORE = '<svg viewBox="0 0 24 24" width="13" height="13" fill="currentColor"><circle cx="5" cy="12" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="19" cy="12" r="1.7"/></svg>';
const ICON_PENCIL = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h4L19 9l-4-4L4 16Z"/><path d="m13.5 6.5 4 4"/></svg>';
const ICON_RESTORE = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 3-6.7"/><path d="M3 4v5h5"/></svg>';
const ICON_TRASH = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12M9 7V4h6v3"/></svg>';
const ICON_PIN = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V17h14v-1.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/></svg>';
const ICON_REFRESH = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 4v5h-5"/></svg>';

const STATUS_LABEL = { waiting: 'needs you', working: 'working', idle: 'idle' };

// Folded project rows. The key once held the split's collapsed group too
// ("term"), so anything but a list counts as none.
const collapsed = new Set((() => {
  try {
    const v = JSON.parse(localStorage.getItem('punk0.collapsed'));
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
})());
let view = localStorage.getItem('punk0.sideView') === 'scratch' ? 'scratch' : 'projects';
const search = { scratch: '', projects: '' }; // lowercased search text per view

let listEl, searchEl, searchClearEl, chatMenuEl;
// What the one actions menu (#o-chat-menu) is open for: { mode: 'chat', id }
// a scratch chat, { mode: 'ws', id } a project's workspace.
let menuFor = null;
// The group name being edited in place: { kind: 'new' | 'rename', id, moveChat, error, busy }.
let editing = null;

export function initSidebar() {
  listEl = $('#o-projects');
  searchEl = $('#o-search');
  searchClearEl = $('#o-search-clear');
  chatMenuEl = $('#o-chat-menu');

  // In the Scratch view the header's + starts a chat and the folder button
  // makes a group; in Projects they add a project and a workspace.
  $('#o-add-project').addEventListener('click', () => {
    if (view === 'scratch') return newChat('', $('#o-add-project'));
    openAddProject($('#o-add-project'));
  });
  $('#o-add-feature').addEventListener('click', () => {
    if (view === 'scratch') return startNewGroup();
    const project = featureProject();
    if (project) newWorkspace(project, $('#o-add-feature'));
  });
  $('.o-side-tabs').addEventListener('click', e => {
    const v = e.target.closest('[data-view]')?.dataset.view;
    if (v && v !== view) setView(v);
  });
  initSearch();
  initViewMenu($('#o-view-btn'), $('#o-view-menu'), {
    view: () => view,
    shown: shownChoices,
    changed: renderSidebar,
  });
  listEl.addEventListener('click', onListClick);
  listEl.addEventListener('contextmenu', onListContextMenu);
  listEl.addEventListener('keydown', e => {
    if (e.target.matches('[data-edit]')) {
      if (e.key === 'Enter') {
        e.preventDefault();
        commitEdit();
      } else if (e.key === 'Escape') {
        e.preventDefault();
        cancelEdit();
      }
    } else if ((e.key === 'Enter' || e.key === ' ') && e.target.matches('[data-act=toggle-archived]')) {
      e.preventDefault();
      toggleArchived();
    }
  });
  chatMenuEl.addEventListener('click', onChatMenu);
  chatMenuEl.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeChatMenu();
  });
  document.addEventListener('mousedown', e => {
    if (!chatMenuEl.hidden && !chatMenuEl.contains(e.target) && !e.target.closest('[data-act$="-menu"]')) closeChatMenu();
    // Clicking away from a name being edited keeps it, as Enter does.
    if (editing && !e.target.closest('[data-edit]')) commitEdit();
  });
  subscribe(renderSidebar);

  // ?open=<url> comes from a workspace's holding page when a PR has no known
  // clone: ask for it in Add project.
  const target = new URLSearchParams(location.search).get('open');
  if (target) {
    history.replaceState(null, '', location.pathname);
    if (view !== 'projects') setView('projects');
    // After the first load: the flyout marks registered folders and shortens paths.
    reload().then(() => openAddProject($('#o-add-project'), target));
  }
}

function setView(v) {
  view = v;
  localStorage.setItem('punk0.sideView', v);
  closeAddProject(); // its + may belong to the other view
  closeChatMenu();
  closeNewChat();
  closeNewWorkspace();
  closeRemoveDialog();
  closeViewMenu();
  editing = null;
  syncSearch();
  renderSidebar();
}

/* ---------- search ---------- */

function initSearch() {
  searchEl.addEventListener('input', () => {
    search[view] = searchEl.value.trim().toLowerCase();
    searchClearEl.hidden = !searchEl.value;
    renderSidebar();
  });
  searchEl.addEventListener('keydown', e => {
    if (e.key !== 'Escape') return;
    if (!searchEl.value) return searchEl.blur();
    clearSearch();
  });
  searchClearEl.addEventListener('click', () => {
    clearSearch();
    searchEl.focus();
  });
  syncSearch();
}

function clearSearch() {
  searchEl.value = '';
  search[view] = '';
  searchClearEl.hidden = true;
  renderSidebar();
}

// Each view keeps its own search.
function syncSearch() {
  searchEl.value = search[view];
  searchEl.placeholder = view === 'scratch' ? 'Search chats' : 'Search projects';
  searchEl.title = view === 'scratch' ? 'Chat titles and group names' : 'Project, workspace and branch names';
  searchClearEl.hidden = !search[view];
}

const textMatch = (...s) => !search[view] || s.some(x => x && x.toLowerCase().includes(search[view]));

// shownChoices is the View options menu's Show section: the agents chats
// run, or the projects.
function shownChoices() {
  if (view === 'scratch') {
    const ids = [...new Set((scratchProject()?.workspaces || []).map(agentOf))];
    return ids.map(id => [id, agentLabel(id)]).sort((a, b) => a[1].localeCompare(b[1]));
  }
  return state.projects.filter(p => !p.scratch).map(p => [p.id, p.name]);
}

// featureProject is where the header's new workspace button adds one: the
// selected workspace's project, else the first project.
function featureProject() {
  const projects = state.projects.filter(p => !p.scratch);
  return projects.find(p => p.workspaces.some(w => w.id === state.ws)) || projects[0] || null;
}

const scratchProject = () => state.projects.find(p => p.scratch);
const scratchGroups = () => scratchProject()?.groups || [];

/* ---------- rendering ---------- */

function renderSidebar() {
  for (const b of document.querySelectorAll('.o-side-tabs [data-view]')) b.setAttribute('aria-selected', String(b.dataset.view === view));
  $('#o-list-title').textContent = view === 'scratch' ? 'Chats' : 'Projects';
  const feature = $('#o-add-feature');
  feature.title = view === 'scratch' ? 'New group' : 'New workspace or worktree';
  feature.setAttribute('aria-label', feature.title);
  feature.disabled = view !== 'scratch' && !featureProject();
  const add = $('#o-add-project');
  add.title = view === 'scratch' ? 'New chat' : 'Add project';
  add.setAttribute('aria-label', add.title);
  syncViewButton();

  // The list re-renders while a name is being typed: carry the text over.
  const input = listEl.querySelector('[data-edit]');
  const typed = input && { value: input.value, start: input.selectionStart, end: input.selectionEnd };
  listEl.innerHTML = view === 'scratch' ? renderScratch(scratchProject()) : renderProjects();
  const next = listEl.querySelector('[data-edit]');
  if (next) {
    if (typed) {
      next.value = typed.value;
      next.setSelectionRange(typed.start, typed.end);
    } else next.select();
    next.focus();
  }
}

const section = (title, rows, extra = '') =>
  rows ? `<div class="o-proj o-scratch${extra}">${title ? `<div class="o-section"><span>${esc(title)}</span></div>` : ''}${rows}</div>` : '';

/* Projects */

function renderProjects() {
  const o = opts.projects;
  const searching = !!search.projects;
  const all = state.projects.filter(p => !p.scratch);
  if (!all.length) return `
    <div class="o-side-empty">
      <p>No projects yet.</p>
      <button type="button" class="wb-btn wb-primary" data-act="add-project">Add project</button>
    </div>`;
  const projects = all.filter(p => searching || !o.hiddenProjects.includes(p.id));
  // Hide sleeping keeps each project's local workspace, its main checkout,
  // so the project stays in the list to start work from.
  const keep = w => w.id === state.ws || searching ||
    ((!o.hideLocal || w.kind !== 'local') && (!o.hideMissing || !w.missing) && (!o.hideSleeping || isRunning(w) || w.kind === 'local'));
  const shown = p => {
    const own = textMatch(p.name);
    return p.workspaces.filter(w => keep(w) && (own || textMatch(w.name, w.branch)));
  };
  const filtered = searching || o.hideLocal || o.hideMissing || o.hideSleeping;
  let html = '';
  if (o.groupBy === 'project') {
    html = projects.map(p => {
      const ws = shown(p);
      return filtered && !ws.length ? '' : renderProject(p, ws, filtered);
    }).join('');
  } else {
    const rows = projects.flatMap(p => shown(p).map(w => [p, w]));
    if (o.groupBy === 'status') {
      html = STATES.map(([st, label]) => section(label, rows.filter(([, w]) => stateOf(w) === st).map(([p, w]) => renderWorkspace(w, p)).join(''))).join('');
    } else {
      html = section('', rows.map(([p, w]) => renderWorkspace(w, p)).join(''));
    }
  }
  if (searching || !o.hideReviews) html += renderReviews(o.hideSleeping && !searching);
  return html.trim() ? html : '<p class="o-note">Nothing matches.</p>';
}

function renderProject(p, ws, filtered) {
  // A search or filter shows every match, folded projects included.
  const open = filtered || !collapsed.has(p.id);
  return `
    <div class="o-proj" data-project="${esc(p.id)}">
      <div class="o-proj-row" data-act="toggle" title="${esc(tildify(p.dir))}" aria-expanded="${open}">
        <span class="o-proj-ico">${open ? ICON_FOLDER_OPEN : ICON_FOLDER}</span>
        <span class="o-proj-name">${esc(p.name)}</span>
        <span class="o-proj-branch">${esc(p.branch || '')}</span>
        <span class="o-acts">
          <button type="button" class="mini" data-act="refresh-project" title="Refresh: look at the folder again (after git init, a new branch…)" aria-label="Refresh project">${ICON_REFRESH}</button>
          <button type="button" class="mini" data-act="add-ws" title="New workspace" aria-label="New workspace">${ICON_PLUS}</button>
          <button type="button" class="mini o-del" data-act="del-project" title="Remove project from punk0" aria-label="Remove project">${ICON_X}</button>
        </span>
      </div>
      ${open ? renderTree(p, ws) : ''}
    </div>`;
}

// renderTree lists a project's shown workspaces nested by parent, any depth.
// A workspace whose parent is not shown (filtered out, or gone) sits at the
// top level, so a filter never hides a match.
function renderTree(p, ws) {
  const ids = new Set(ws.map(w => w.id));
  const branch = (w, depth) => renderWorkspace(w, null, depth) +
    ws.filter(c => c.parent === w.id).map(c => branch(c, depth + 1)).join('');
  return ws.filter(w => !w.parent || !ids.has(w.parent)).map(w => branch(w, 0)).join('');
}

/* Scratch */

const SORTS = {
  active: (a, b) => (b.active || 0) - (a.active || 0),
  created: (a, b) => (b.created || b.active || 0) - (a.created || a.active || 0),
  name: (a, b) => wsLabel(a).localeCompare(wsLabel(b)),
};

// Scratch lists its pinned chats first, then the rest the way View options
// groups them, then, when shown, the archived ones. A pinned chat shows only
// under Pinned, wherever it is filed.
function renderScratch(p) {
  if (!p) return '';
  const o = opts.scratch;
  const searching = !!search.scratch;
  const groups = p.groups || [];
  if (!p.workspaces.length && !groups.length && !editing) return `
    <div class="o-side-empty">
      <p>No scratch chats yet.</p>
      <p class="wb-faint">An agent chat in a folder of its own, outside any project: research, one-off scripts, prototypes.</p>
      <button type="button" class="wb-btn wb-primary" data-act="new-chat">New chat</button>
    </div>`;
  scratchDir = p.dir;
  const groupName = w => groups.find(g => g.id === w.group)?.name;
  const match = w => textMatch(wsLabel(w), w.name, groupName(w));
  const keep = w => w.id === state.ws || searching ||
    ((!o.hideSleeping || isRunning(w)) && (!o.hideMine || ownFolder(w)) && !o.hiddenAgents.includes(agentOf(w)));
  const sort = SORTS[o.sort] || SORTS.active;
  const live = p.workspaces.filter(w => !w.archived && keep(w) && match(w)).sort(sort);
  const rest = live.filter(w => !w.pinned);
  let html = section('Pinned', live.filter(w => w.pinned).map(renderChat).join(''));
  switch (o.groupBy) {
    case 'group': {
      const ids = new Set(groups.map(g => g.id));
      const filtered = searching || o.hideSleeping || o.hideMine || o.hiddenAgents.length > 0;
      html += (editing?.kind === 'new' ? renderNewGroup() : '') +
        groups.map(g => renderGroup(g, rest.filter(w => w.group === g.id), filtered)).join('') +
        renderDated(rest.filter(w => !ids.has(w.group)));
      break;
    }
    case 'date':
      html += renderDated(rest);
      break;
    case 'agent': {
      const agents = [...new Set(rest.map(agentOf))].sort((a, b) => agentLabel(a).localeCompare(agentLabel(b)));
      html += agents.map(a => section(agentLabel(a), rest.filter(w => agentOf(w) === a).map(renderChat).join(''))).join('');
      break;
    }
    case 'status':
      html += STATES.map(([st, label]) => section(label, rest.filter(w => stateOf(w) === st).map(renderChat).join(''))).join('');
      break;
    default:
      html += section('', rest.map(renderChat).join(''));
  }
  if (o.showArchived || searching) html += renderArchived(p.workspaces.filter(w => w.archived && match(w)).sort(sort));
  return html.trim() ? html : '<p class="o-note">Nothing matches.</p>';
}

// renderGroup lists a group's chats. While searching or filtering, a group
// with nothing to show is left out.
function renderGroup(g, chats, filtered) {
  if (filtered && !chats.length && editing?.id !== g.id) return '';
  const open = filtered || !collapsed.has(g.id);
  const name = editing?.kind === 'rename' && editing.id === g.id
    ? editField(g.name)
    : `<span class="o-proj-name">${esc(g.name)}</span><span class="o-count">${chats.length || ''}</span>
        <span class="o-acts">
          <button type="button" class="mini" data-act="new-chat" title="New chat in ${esc(g.name)}" aria-label="New chat in this group">${ICON_PLUS}</button>
          <button type="button" class="mini" data-act="rename-group" title="Rename group" aria-label="Rename group">${ICON_PENCIL}</button>
          <button type="button" class="mini o-del" data-act="del-group" title="Delete group (its chats stay)" aria-label="Delete group">${ICON_X}</button>
        </span>`;
  return `
    <div class="o-proj o-chat-group" data-group="${esc(g.id)}">
      <div class="o-proj-row" data-act="toggle" aria-expanded="${open}">
        <span class="o-proj-ico">${open ? ICON_FOLDER_OPEN : ICON_FOLDER}</span>
        ${name}
      </div>
      ${editing?.kind === 'rename' && editing.id === g.id ? editError() : ''}
      ${!open ? '' : chats.length ? chats.map(renderChat).join('') : '<p class="o-note o-group-empty">Empty. Move a chat here from its ⋯ menu.</p>'}
    </div>`;
}

// The new group being named, at the top of the groups.
function renderNewGroup() {
  return `
    <div class="o-proj o-chat-group o-group-new">
      <div class="o-proj-row">
        <span class="o-proj-ico">${ICON_FOLDER}</span>
        ${editField('')}
      </div>
      ${editError()}
    </div>`;
}

const editField = value =>
  `<input class="o-inline-input" data-edit type="text" maxlength="40" value="${esc(value)}" placeholder="Group name" aria-label="Group name" spellcheck="false">`;
const editError = () => (editing?.error ? `<p class="wb-msg o-inline-msg" role="alert">${esc(editing.error)}</p>` : '');

// renderDated lists chats under when they were last used; they come sorted.
function renderDated(chats) {
  const today = new Date().setHours(0, 0, 0, 0);
  const buckets = [
    ['Today', w => (w.active || 0) >= today],
    ['Previous 7 days', w => (w.active || 0) >= today - 6 * 864e5],
    ['Older', () => true],
  ];
  let html = '';
  for (const [title, inBucket] of buckets) {
    html += section(title, chats.filter(inBucket).map(renderChat).join(''));
    chats = chats.filter(w => !inBucket(w));
  }
  return html;
}

let archivedOpen = localStorage.getItem('punk0.archivedOpen') === '1';

function renderArchived(chats) {
  if (!chats.length) return '';
  const open = !!search.scratch || archivedOpen;
  return `
    <div class="o-proj o-scratch o-archived">
      <div class="o-section o-fold" data-act="toggle-archived" role="button" tabindex="0" aria-expanded="${open}">
        <span>Archived</span><span class="o-count">${chats.length}</span>
      </div>
      ${open ? chats.map(renderArchivedChat).join('') : ''}
    </div>`;
}

// when labels a chat's last use without ticking: the time today, the
// weekday this week, else the date.
function when(ms) {
  if (!ms) return '';
  const d = new Date(ms);
  const today = new Date().setHours(0, 0, 0, 0);
  if (ms >= today) return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  if (ms >= today - 6 * 864e5) return d.toLocaleDateString([], { weekday: 'short' });
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
}

// A chat in a folder of yours, rather than one punk0 made, shows the
// folder's name: deleting the chat leaves that folder alone.
let scratchDir = '';
const ownFolder = w => w.dir.startsWith(scratchDir + state.wb.sep);
const folderChip = w => ownFolder(w) || wsLabel(w) === w.name ? ''
  : `<span class="o-chip" title="Runs in your folder ${esc(tildify(w.dir))}">${esc(w.name)}</span>`;

function renderChat(w) {
  return `
    <div class="o-ws-row o-chat${w.id === state.ws ? ' active' : ''}${w.missing ? ' missing' : ''}" data-ws="${esc(w.id)}" title="${esc(tildify(w.dir))}">
      <span class="o-ws-name">${esc(wsLabel(w))}</span>
      ${folderChip(w)}${badgeOf(w)}<span class="o-when">${esc(when(w.active))}</span>
      <span class="o-acts">
        <button type="button" class="mini o-pin${w.pinned ? ' on' : ''}" data-act="pin" title="${w.pinned ? 'Unpin' : 'Pin to the top'}" aria-label="${w.pinned ? 'Unpin chat' : 'Pin chat'}" aria-pressed="${!!w.pinned}">${ICON_PIN}</button>
        <button type="button" class="mini" data-act="chat-menu" title="Rename, pin, move to a group, archive" aria-label="Chat actions" aria-haspopup="menu">${ICON_MORE}</button>
      </span>
    </div>`;
}

function renderArchivedChat(w) {
  return `
    <div class="o-ws-row o-chat o-chat-archived" data-ws="${esc(w.id)}" title="${esc(tildify(w.dir))}">
      <span class="o-ws-name">${esc(wsLabel(w))}</span>${folderChip(w)}
      <span class="o-when">${esc(when(w.active))}</span>
      <span class="o-acts">
        <button type="button" class="mini" data-act="restore" title="Restore chat" aria-label="Restore chat">${ICON_RESTORE}</button>
        <button type="button" class="mini o-del" data-act="del-ws" title="Delete chat" aria-label="Delete chat">${ICON_TRASH}</button>
      </span>
    </div>`;
}

// badgeOf shows a workspace's running tabs, or their most urgent status.
function badgeOf(w) {
  const st = statusOf(w);
  const running = w.tabs.filter(t => t.running).length;
  return running ? `<span class="o-badge${st ? ' ' + st : ''}" title="${running} running${st ? ', ' + STATUS_LABEL[st] : ''}">${st ? esc(STATUS_LABEL[st]) : running}</span>` : '';
}

const BACKEND_TITLE = {
  stopped: 'Editor stopped: starts when opened',
  starting: 'Editor starting',
  running: 'Editor running',
  failed: 'Editor failed to start',
};

// whereLine is a workspace row's second line: the branch it is on and where
// -- ⌂ the project folder, whose branch every workspace there shares and
// follows as it changes, or ⑂ a worktree of its own -- and whether it was
// adopted or its folder is gone. A chat's folder has neither.
function whereLine(w) {
  const branch = w.branch === 'HEAD' ? 'detached' : w.branch;
  let text;
  let title;
  if (w.kind === 'local') {
    text = `⌂ ${branch ? `${branch} · ` : ''}project folder`;
    title = `Runs in the project folder${branch ? `, on ${branch} as it is now` : ''}`;
  } else if (w.kind === 'worktree') {
    text = `⑂ ${branch || 'detached'}`;
    title = `${w.adopted ? 'Adopted worktree' : 'Own worktree'} on ${branch ? 'branch ' + branch : 'a detached HEAD'}`;
    if (w.adopted) {
      text += ' · adopted';
      title += ': made outside punk0 and used where it is, so deleting the workspace leaves its folder';
    }
  } else return '';
  const missing = w.missing ? ' · <span class="o-ws-gone" title="Folder deleted outside punk0">missing</span>' : '';
  return `<span class="o-ws-sub" title="${esc(title)}">${esc(text)}${missing}</span>`;
}

// renderWorkspace is a project's workspace row; outside its project's
// group (p given), it is named with the project. depth indents a child
// under its parent. Its editor opens from the tab bar, its language servers
// from the status bar (lsp.js).
function renderWorkspace(w, p, depth = 0) {
  const name = p && p.name !== w.name ? `${p.name} / ${w.name}` : w.name;
  const sub = whereLine(w);
  return `
    <div class="o-ws-row${sub ? ' o-two' : ''}${w.id === state.ws ? ' active' : ''}${w.missing ? ' missing' : ''}${p ? ' o-flat' : ''}" data-ws="${esc(w.id)}" title="${esc(tildify(w.dir))}"${depth ? ` style="--depth:${depth}"` : ''}>
      <span class="o-backend ${esc(w.backend || 'stopped')}" title="${esc(BACKEND_TITLE[w.backend] || w.backend)}"></span>
      <span class="o-ws-text"><span class="o-ws-name">${esc(name)}</span>${sub}</span>
      ${badgeOf(w)}
      <span class="o-acts">
        <button type="button" class="mini" data-act="ws-menu" title="Rename or delete" aria-label="Workspace actions" aria-haspopup="menu">${ICON_MORE}</button>
      </span>
    </div>`;
}

function renderReviews(runningOnly) {
  const items = state.reviews.filter(it => (!runningOnly || it.state === 'running') && textMatch(it.title, it.repo, String(it.number)));
  if (!items.length) return '';
  return `
    <div class="o-proj o-reviews">
      <div class="o-section"><span>Reviews</span></div>
      ${items.map(renderReview).join('')}
    </div>`;
}

function renderReview(it) {
  const title = it.title || `#${it.number}`;
  const sub = it.state === 'starting' && it.progress ? it.progress : `${it.repo}#${it.number}`;
  const st = it.state === 'stopped' && !it.error ? '' :
    `<span class="wb-state ${esc(it.state)}" title="${esc(it.error || it.state)}"></span>`;
  return `
    <div class="o-pr-row" data-pr="${esc(it.id)}" title="${esc(it.url || '')}">
      <span class="o-pr-ico">${ICON_PR}</span>
      <span class="o-pr-text"><span class="o-pr-title">${esc(title)}</span><span class="o-pr-sub">${esc(sub)}</span></span>
      ${st}
      <span class="o-acts">
        ${it.state === 'running' ? `<button type="button" class="mini" data-act="pr-stop" title="Stop its editor" aria-label="Stop">${ICON_STOP}</button>` : ''}
        <button type="button" class="mini o-del" data-act="pr-remove" title="Remove from the list" aria-label="Remove">${ICON_X}</button>
      </span>
    </div>`;
}

/* ---------- actions ---------- */

async function onListClick(e) {
  if (e.target.closest('[data-edit]')) return;
  const act = e.target.closest('[data-act]')?.dataset.act;
  const projectId = e.target.closest('[data-project]')?.dataset.project;
  const project = state.projects.find(p => p.id === projectId);
  const groupId = e.target.closest('[data-group]')?.dataset.group || '';
  const w = rowWorkspace(e.target, '[data-ws]');
  const prId = e.target.closest('[data-pr]')?.dataset.pr;
  if (prId) return reviewAction(state.reviews.find(r => r.id === prId), act);
  if (act === 'add-project') return openAddProject(e.target.closest('[data-act]'));
  if (act === 'add-ws') return newWorkspace(project, e.target.closest('[data-act]'));
  if (act === 'del-project') return removeProject(project, e.target.closest('[data-act]'));
  if (act === 'refresh-project') return refreshProject(project, e.target.closest('[data-act]'));
  // The group's + only shows on hover; point at its row.
  if (act === 'new-chat') return newChat(groupId, e.target.closest('.o-proj-row') || e.target.closest('[data-act]'));
  if (act === 'rename-group') return startRenameGroup(groupId);
  if (act === 'del-group') return removeGroup(groupId);
  if (act === 'toggle-archived') return toggleArchived();
  if (w && act === 'del-ws') return removeWorkspace(w, e.target.closest('[data-act]'));
  if (w && act === 'chat-menu') return chatMenuEl.hidden || menuFor?.id !== w.id ? openChatMenu(w, menuAt(e.target.closest('button'))) : closeChatMenu();
  if (w && act === 'ws-menu') return chatMenuEl.hidden || menuFor?.id !== w.id ? openWorkspaceMenu(w, menuAt(e.target.closest('button'))) : closeChatMenu();
  if (w && act === 'restore') return updateChat(w, { archived: false });
  if (w && act === 'pin') return updateChat(w, { pinned: !w.pinned });
  // A double-click renames. It is the second click's detail, not a dblclick
  // event: the first click re-renders the list, which loses dblclick.
  const second = e.detail === 2 && !e.target.closest('.o-acts');
  if (w && second && w.kind === 'folder' && !w.archived) return renameChat(w);
  if (w) return !w.archived && w.id !== state.ws && selectWorkspace(w.id);
  if (act === 'toggle') {
    const key = groupId || projectId;
    if (collapsed.has(key)) collapsed.delete(key);
    else collapsed.add(key);
    saveCollapsed();
    // The first click of a double-click on a group's name folded it; the
    // second undoes that and renames the group.
    if (second && groupId && e.target.closest('.o-proj-name')) return startRenameGroup(groupId);
    renderSidebar();
  }
}

// A right click on a project's workspace opens its menu at the pointer.
function onListContextMenu(e) {
  const at = { x: e.clientX, y: e.clientY };
  const w = rowWorkspace(e.target, '[data-ws]');
  if (w?.kind !== 'worktree' && w?.kind !== 'local') return;
  e.preventDefault();
  openWorkspaceMenu(w, at);
}

// rowWorkspace is the workspace of the row el is in, or null outside one.
// workspace() with no id means the selected one, so it is not asked then.
function rowWorkspace(el, sel) {
  const id = el.closest(sel)?.dataset.ws;
  return id ? workspace(id) : null;
}

const saveCollapsed = () => localStorage.setItem('punk0.collapsed', JSON.stringify([...collapsed]));

function toggleArchived() {
  archivedOpen = !archivedOpen;
  localStorage.setItem('punk0.archivedOpen', archivedOpen ? '1' : '');
  renderSidebar();
}

// newChat opens the New chat flyout, in a group when given, and selects the
// chat it starts. from is the button the New chat flyout points at.
async function newChat(group, from) {
  const id = await openNewChat(group, from);
  if (!id) return;
  if (group && collapsed.delete(group)) saveCollapsed();
  if (view !== 'scratch') setView('scratch');
  await reload();
  selectWorkspace(id);
}

async function updateChat(w, change) {
  const res = await call('PATCH', `api/chats/${encodeURIComponent(w.id)}`, change);
  if (!res.ok) failed(res);
  reload();
}

function renameChat(w) {
  const title = prompt('Chat name', wsLabel(w))?.trim();
  if (title && title !== wsLabel(w)) updateChat(w, { title });
}

/* Groups, named in place */

// startNewGroup adds a group row being named at the top of the groups; a
// chat moved there from its menu joins the group once it is made. Groups
// only show grouped by Group, so the list switches to that.
function startNewGroup(moveChat = '') {
  if (view !== 'scratch') setView('scratch');
  if (opts.scratch.groupBy !== 'group') setOpt('scratch', 'groupBy', 'group');
  editing = { kind: 'new', moveChat };
  renderSidebar();
}

function startRenameGroup(id) {
  if (!scratchGroups().some(g => g.id === id)) return;
  editing = { kind: 'rename', id };
  renderSidebar();
}

function cancelEdit() {
  editing = null;
  renderSidebar();
}

// commitEdit saves the name being edited; an empty or unchanged one cancels.
// A refused name (a duplicate) stays in place with the reason under it.
async function commitEdit() {
  const ed = editing;
  const input = listEl.querySelector('[data-edit]');
  if (!ed || ed.busy || !input) return;
  const name = input.value.trim();
  const group = ed.kind === 'rename' && scratchGroups().find(g => g.id === ed.id);
  if (!name || (group && group.name === name)) return cancelEdit();
  ed.busy = true;
  const res = ed.kind === 'new'
    ? await call('POST', 'api/chat-groups', { name })
    : await call('PATCH', `api/chat-groups/${encodeURIComponent(ed.id)}`, { name });
  ed.busy = false;
  if (editing !== ed) return;
  if (!res.ok) {
    ed.error = res.data.error;
    renderSidebar();
    return;
  }
  editing = null;
  if (ed.kind === 'new' && ed.moveChat) {
    await call('PATCH', `api/chats/${encodeURIComponent(ed.moveChat)}`, { group: res.data.id });
  }
  await reload();
  renderSidebar();
}

async function removeGroup(id) {
  const g = scratchGroups().find(x => x.id === id);
  if (!g || !confirm(`Delete group "${g.name}"? Its chats are kept, in no group.`)) return;
  const res = await call('DELETE', `api/chat-groups/${encodeURIComponent(id)}`);
  if (!res.ok) failed(res);
  reload();
}

// menuAt is where a menu opened from button el goes: under it.
function menuAt(el) {
  const r = el.getBoundingClientRect();
  return { x: r.left, y: r.bottom + 4 };
}

// showChatMenu fills the one actions menu for what (menuFor) and opens it at
// at, kept inside the window.
function showChatMenu(what, html, at) {
  menuFor = what;
  chatMenuEl.innerHTML = html;
  chatMenuEl.hidden = false;
  chatMenuEl.style.left = `${Math.max(4, Math.min(at.x, innerWidth - chatMenuEl.offsetWidth - 4))}px`;
  chatMenuEl.style.top = `${Math.max(4, Math.min(at.y, innerHeight - chatMenuEl.offsetHeight - 4))}px`;
  chatMenuEl.querySelector('button')?.focus();
}

// The chat actions menu: rename, pin, file under a group, archive.
function openChatMenu(w, at) {
  const item = (g, name) => `<button type="button" role="menuitemradio" aria-checked="${(w.group || '') === g}" data-menu="move" data-group="${esc(g)}">${esc(name)}</button>`;
  showChatMenu({ mode: 'chat', id: w.id }, `
    <button type="button" role="menuitem" data-menu="rename">Rename…</button>
    <button type="button" role="menuitem" data-menu="pin">${w.pinned ? 'Unpin' : 'Pin to the top'}</button>
    <div class="o-menu-sep" role="separator"></div>
    <div class="o-menu-label">Group</div>
    ${item('', 'No group')}
    ${scratchGroups().map(g => item(g.id, g.name)).join('')}
    <button type="button" role="menuitem" data-menu="move-new">New group…</button>
    <div class="o-menu-sep" role="separator"></div>
    <button type="button" role="menuitem" data-menu="archive">Archive</button>`, at);
}

// The workspace actions menu: rename, delete.
function openWorkspaceMenu(w, at) {
  showChatMenu({ mode: 'ws', id: w.id }, `
    <button type="button" role="menuitem" data-menu="ws-rename">Rename…</button>
    <div class="o-menu-sep" role="separator"></div>
    <button type="button" role="menuitem" data-menu="ws-delete">Delete…</button>`, at);
}

// renameWorkspace asks for a new label; the folder and branch stay.
async function renameWorkspace(w) {
  const name = prompt('Workspace name', w.name)?.trim();
  if (!name || name === w.name) return;
  const res = await call('PATCH', `api/workspaces/${encodeURIComponent(w.id)}`, { name });
  if (!res.ok) failed(res);
  reload();
}

function closeChatMenu() {
  if (!chatMenuEl || chatMenuEl.hidden) return;
  chatMenuEl.hidden = true;
  menuFor = null;
}

async function onChatMenu(e) {
  const b = e.target.closest('[data-menu]');
  const what = menuFor;
  if (!b || !what) return;
  closeChatMenu();
  const act = b.dataset.menu;
  const w = workspace(what.id);
  if (!w) return;
  if (what.mode === 'ws') {
    if (act === 'ws-rename') return renameWorkspace(w);
    // The ⋯ that opened the menu hides with the row's hover: the dialog
    // points at the row.
    return removeWorkspace(w, listEl.querySelector(`[data-ws="${CSS.escape(w.id)}"]`));
  }
  if (act === 'rename') return renameChat(w);
  if (act === 'archive') return updateChat(w, { archived: true });
  if (act === 'pin') return updateChat(w, { pinned: !w.pinned });
  if (act === 'move-new') return startNewGroup(w.id);
  const group = b.dataset.group;
  if (group && collapsed.delete(group)) saveCollapsed();
  if (group !== (w.group || '')) updateChat(w, { group });
}

async function reviewAction(it, act) {
  if (!it) return;
  if (!act) return openEditor(it.href);
  const res = await call('POST', act === 'pr-stop' ? 'api/wb/stop' : 'api/wb/remove', { id: it.id });
  if (!res.ok) failed(res);
  reload();
}

// removeProject asks what to remove with the project: for one punk0 created
// or cloned, its folder, which is moved to punk0's trash and only once its
// name is typed back. Its agents' chats are never touched; they are deleted
// in Chats mode, one at a time. from is the ✕ the dialog points at.
async function removeProject(p, from) {
  const options = [];
  if (p.owned) {
    options.push({
      id: 'files',
      label: 'Also delete the project folder',
      note: `${tildify(p.dir)} is moved to punk0's trash folder. Refused while the repository still has worktrees checked out.`,
      checked: false,
    });
  }
  const answers = await openRemoveDialog({
    heading: 'Remove project',
    go: { off: 'Remove project', on: 'Remove project' },
    what: `Remove <b>${esc(p.name)}</b> from punk0? Its tabs stop. ${p.owned
      ? 'Nothing is deleted unless you say so below.'
      : 'No files are deleted, and its worktrees come back if you add it again.'}`,
    options,
    confirm: p.owned ? p.name : '',
  }, from);
  if (!answers) return;
  const q = new URLSearchParams();
  if (answers.files) {
    q.set('files', '1');
    q.set('confirm', answers.confirm);
  }
  const res = await call('DELETE', `api/projects/${encodeURIComponent(p.id)}${query(q)}`);
  if (!res.ok) failed(res);
  else if (res.data?.note) showToast('!', res.data.note, 6000);
  reload();
  emit('orch:removed');
}

// removeWorkspace asks what to delete with the workspace: a worktree offers
// its folder, ticked for the worktrees punk0 made and clear for ones it
// adopted in place. The agents' chats are never touched; a removed
// workspace's chats stay listed in Chats mode. Uncommitted changes come back
// as 409 dirty, which asks once more and deletes them.
async function removeWorkspace(w, from) {
  const branchNote = getSetting('workspace.deleteMergedBranch')
    ? `Branch ${w.branch} is deleted if merged, else kept.`
    : `Branch ${w.branch} is kept.`;
  const options = [];
  if (w.kind === 'worktree') {
    options.push({
      id: 'files',
      label: 'Also delete the worktree folder',
      note: w.missing
        ? 'The folder is already gone; git stops listing the worktree.'
        : `${tildify(w.dir)} is removed with git worktree remove.${w.branch ? ` ${branchNote}` : ''}`,
      checked: !w.adopted,
    });
  }
  // Nothing else goes with it: the chats of its folder are the agents' own.
  const what = {
    worktree: `Delete workspace <b>${esc(w.name)}</b>? Its tabs stop${w.adopted ? ', and the worktree stays where it is unless you say otherwise' : ''}.`,
    local: `Delete workspace <b>${esc(w.name)}</b>? Its tabs stop. The project folder is not touched.`,
    folder: ownFolder(w)
      ? `Delete chat <b>${esc(wsLabel(w))}</b>? Its tabs stop and the folder punk0 made for it, ${esc(tildify(w.dir))}, is deleted with it.`
      : `Delete chat <b>${esc(wsLabel(w))}</b>? Its tabs stop. Your folder ${esc(tildify(w.dir))} is not touched.`,
  }[w.kind];
  const answers = await openRemoveDialog({
    heading: w.kind === 'folder' ? 'Delete chat' : 'Delete workspace',
    go: { off: 'Delete', on: 'Delete' },
    what,
    options,
    confirm: '',
  }, from);
  if (!answers) return;
  const q = new URLSearchParams();
  if (answers.files) q.set('files', '1');
  const path = `api/workspaces/${encodeURIComponent(w.id)}`;
  let res = await call('DELETE', path + query(q));
  if (res.status === 409 && res.data?.dirty) {
    if (!confirm(`"${w.name}" has uncommitted changes. Delete them with the worktree?`)) return;
    q.set('force', '1');
    res = await call('DELETE', path + query(q));
  }
  if (!res.ok) failed(res);
  else if (res.data?.note) showToast('!', res.data.note, 6000);
  reload();
  emit('orch:removed');
}

// query is a search string for params, empty when there are none.
function query(params) {
  const s = params.toString();
  return s ? `?${s}` : '';
}

/* ---------- new workspaces ---------- */

// newWorkspace opens the New workspace panel for project, pointing at from;
// the panel selects what it makes.
function newWorkspace(project, from) {
  if (collapsed.delete(project.id)) saveCollapsed();
  openNewWorkspace(project, from);
}

// refreshProject has punk0 look at the project folder again: git is asked
// afresh and the folder's editor restarts, so changes made outside punk0
// (git init, a new branch) show without waiting.
async function refreshProject(project, btn) {
  if (!project) return;
  btn.classList.add('o-spin');
  const res = await call('POST', `api/projects/${encodeURIComponent(project.id)}/refresh`);
  await reload();
  btn.classList.remove('o-spin');
  if (!res.ok) failed(res);
}
