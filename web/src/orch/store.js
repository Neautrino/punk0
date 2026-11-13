// web/src/orch/store.js
// The project tree from GET /api/projects, the pull requests from
// GET /api/wb/state, and what is selected. The sidebar, tab bar and panes all
// render from here. The tree is small, so every change reloads all of it.
import { call, failed } from './api.js';
import { getSetting } from './settings.js';
import { reconcile, moveTab as moveInGroups, canonicalTerms, movesToMatch, idKind, TERM, FILE } from './groups.js';

const listeners = new Set();

// What a page shows is its own: the selection, open files and the split's
// collapsed group live in sessionStorage, so two browser tabs on punk0 never
// overwrite each other, and a reload keeps what that tab showed.
const page = sessionStorage;
const loadJSON = (key, empty) => {
  try {
    return JSON.parse(page.getItem(key)) || empty;
  } catch {
    return empty;
  }
};
const loadText = key => {
  try {
    return page.getItem(key) || '';
  } catch {
    return '';
  }
};
const saveJSON = (key, value) => {
  try {
    page.setItem(key, typeof value === 'string' ? value : JSON.stringify(value));
  } catch {}
};

// initialWs is the workspace to select on load. A page `punk0 <dir>` opened
// carries ?ws=<id>: that one, saved as if clicked, with the parameter dropped
// so a reload keeps whatever is selected by then. Else this tab's last
// selected; a new tab has none and takes punk0's last used (reload).
function initialWs() {
  const params = new URLSearchParams(location.search);
  const id = params.get('ws');
  if (!id) return loadText('punk0.ws');
  params.delete('ws');
  const q = params.toString();
  history.replaceState(history.state, '', location.pathname + (q ? '?' + q : '') + location.hash);
  saveJSON('punk0.ws', id);
  return id;
}

export const state = {
  projects: [],
  reviews: [], // pull request items from /api/wb/state
  wb: { cwd: '', home: '', sep: '/', version: '' },
  ws: initialWs(), // selected workspace id
  // workspace id -> its tab groups (groups.js); the page's own tabs, with
  // no workspace selected, are under ''.
  groups: loadJSON('punk0.groups', {}),
  files: loadJSON('punk0.files', {}), // workspace id -> open file paths
  diffs: loadJSON('punk0.fileDiff', {}), // workspace id -> open file paths shown as their diff
  changes: loadJSON('punk0.changes', {}), // workspace id -> true while its Changes tab is open
  whatsNew: loadText('punk0.whatsNew'), // version the What's New tab shows, '' while it is closed
  // The split layout's collapsed group as text: '', '0' or '1'
  // (collapsedGroup). Selecting a tab of that group shows it again.
  collapsed: loadText('punk0.splitCollapsed'),
};

export function subscribe(fn) {
  listeners.add(fn);
}

// rerender runs every subscriber; for changes outside the store, such as
// the center switching between the split and tabs layouts.
export function rerender() {
  for (const fn of listeners) fn();
}

let lastSig = '';
let reloading = null;
let settled = false; // the first full list has placed this page's selection
let picked = false; // the user chose what is selected since the last takePick

// reload fetches both lists and re-renders only when something changed, so
// the periodic refresh cannot swallow a click or a double-click mid-press.
// Calls made while one is in flight share it.
export function reload() {
  reloading ||= (async () => {
    const [p, wb] = await Promise.all([call('GET', 'api/projects'), call('GET', 'api/wb/state')]);
    if (p.ok) state.projects = p.data || [];
    if (wb.ok) {
      const { items, ...rest } = wb.data;
      state.reviews = items || [];
      state.wb = rest;
    }
    pruneFiles();
    let moved = false;
    // A page with no selection of its own opens where the user last picked
    // or typed, in any page (Records.UseTab); with none, nothing opens.
    if (!settled && p.ok && wb.ok) {
      settled = true;
      if (!state.ws && state.wb.lastWorkspace) {
        state.ws = state.wb.lastWorkspace;
        saveJSON('punk0.ws', state.ws);
      }
      moved = true;
    }
    // A workspace that is gone, or an archived chat (its tabs are stopped),
    // cannot stay selected; nothing is chosen in its place.
    const sel = workspace();
    if (p.ok && state.ws && (!sel || sel.archived)) {
      state.ws = '';
      saveJSON('punk0.ws', '');
      moved = true;
    }
    const sig = JSON.stringify([state.projects, state.reviews, state.wb.version, state.wb.update, state.wb.whatsNew]);
    if (sig !== lastSig || moved) {
      lastSig = sig;
      rerender();
    }
  })().finally(() => { reloading = null; });
  return reloading;
}

// takePick reports whether the user chose the selection since it was last
// asked, and clears it: the terminal attached for a choice tells punk0 so,
// one reopened by a page load does not.
export function takePick() {
  const p = picked;
  picked = false;
  return p;
}

// pruneFiles forgets the open files, diffs and Changes tabs of workspaces
// that no longer exist.
function pruneFiles() {
  if (!state.projects.length) return; // not loaded yet: keep what is stored
  const ids = new Set(state.projects.flatMap(p => p.workspaces.map(w => w.id)));
  for (const [key, map] of [['punk0.files', state.files], ['punk0.fileDiff', state.diffs], ['punk0.changes', state.changes]]) {
    const stale = Object.keys(map).filter(id => !ids.has(id));
    if (!stale.length) continue;
    for (const id of stale) delete map[id];
    saveJSON(key, map);
  }
}

// workspace returns the workspace with id, or the selected one.
export function workspace(id = state.ws) {
  for (const p of state.projects) {
    const w = p.workspaces.find(x => x.id === id);
    if (w) return w;
  }
  return null;
}

export function projectOf(wsId) {
  return state.projects.find(p => p.workspaces.some(w => w.id === wsId)) || null;
}

// hasGit says whether workspace w is a git checkout, so it has changes to show.
export function hasGit(w) {
  return !!w && !w.missing && !!(projectOf(w.id)?.git || w.branch);
}

// wsLabel is what a workspace is called on the page: a scratch chat's title,
// else its name.
export const wsLabel = w => w.title || w.name;

export const fileTabId = path => `file:${path}`;
// The Changes tab (changes.js): the workspace's diff, one per workspace.
export const CHANGES_TAB = 'changes';
// The What's New tab (whatsnew.js): one version's release notes. It belongs
// to the page, not a workspace: it goes to whichever group files were last
// shown in, whatever workspace is selected, until it is closed.
export const WHATSNEW_TAB = 'whatsnew';

// tabsOf lists a workspace's tabs: its shells and Claude chats from the
// server, then its Changes tab and the files open in it, which only this
// browser remembers. With no workspace only the page's own tab is there.
export function tabsOf(w) {
  if (!w) return fileTabsOf(null);
  return [...w.tabs, ...fileTabsOf(w)];
}

// fileTabsOf lists the tabs only this page knows: the What's New tab
// (whatsNew set to its version), the Changes tab (changes set, no path),
// then the files; a file shown as its diff has diff set.
export function fileTabsOf(w) {
  const diffs = state.diffs[w?.id] || [];
  const files = (state.files[w?.id] || []).map(path => ({
    id: fileTabId(path),
    kind: 'file',
    path,
    name: path.slice(path.lastIndexOf('/') + 1),
    diff: diffs.includes(path),
  }));
  const lead = [];
  if (state.whatsNew) lead.push({ id: WHATSNEW_TAB, kind: 'file', whatsNew: state.whatsNew, name: "What's New" });
  if (state.changes[w?.id]) lead.push({ id: CHANGES_TAB, kind: 'file', changes: true, name: 'Changes' });
  return [...lead, ...files];
}

// adopted holds the workspaces whose last used tab this page has taken
// (w.lastTab, Records.LastTab): once per workspace per page load, so a tab
// used in another page later does not move what this one shows.
const adopted = new Set();

// liveOf is what exists now for workspace w: its tabs in the server's
// order, this page's file tabs, which terminals run, and the tab to adopt.
function liveOf(w) {
  const tabs = w?.tabs || [];
  let lastTab = '';
  if (w && !adopted.has(w.id)) {
    adopted.add(w.id);
    lastTab = w.lastTab || '';
  }
  return {
    terms: tabs.map(t => t.id),
    files: fileTabsOf(w).map(t => t.id),
    running: new Set(tabs.filter(t => t.running).map(t => t.id)),
    lastTab,
  };
}

const groupKey = w => w?.id || '';

function saveGroups(key, s) {
  state.groups[key] = s;
  saveJSON('punk0.groups', state.groups);
}

// pending is a tab selected before it was in a group: a new or resumed tab
// the next list brings in. It is shown as soon as it appears.
let pending = '';

// groupsOf is workspace w's tab groups (groups.js), brought up to date with
// the tabs that exist now and saved when that changed anything. Bringing
// them up to date never moves a tab on the server: only a drop or a Move
// menu item does, so two pages cannot push an order back and forth.
export function groupsOf(w = workspace()) {
  const key = groupKey(w);
  const had = state.groups[key]?.groups?.length || 0;
  const s = reconcile(state.groups[key], liveOf(w), { beside: getSetting('layout.fileView') !== 'tabs' });
  if (pending) {
    const g = s.groups.findIndex(x => x.ids.includes(pending));
    if (g >= 0) {
      s.groups[g].active = pending;
      s.focus = g;
      s.last[idKind(pending)] = g;
      pending = '';
    }
  }
  if (JSON.stringify(s) !== JSON.stringify(state.groups[key])) saveGroups(key, s);
  // A second group that closed takes any collapse with it, or the next tab
  // opened beside the first would be hidden.
  if (had === 2 && s.groups.length === 1 && state.collapsed) {
    state.collapsed = '';
    saveJSON('punk0.splitCollapsed', '');
  }
  return s;
}

// focusGroup makes group g the focused one: a click in its body, as a click
// on one of its tabs does. The status bar, Go to line and the LSP item
// follow the focused group's file.
export function focusGroup(g) {
  const w = workspace();
  const s = groupsOf(w);
  if (s.focus === g || !s.groups[g]) return;
  s.focus = g;
  saveGroups(groupKey(w), s);
  rerender();
}

// tabIn is the tab group g shows, or null while this page has opened none
// of its tabs.
export function tabIn(g, w = workspace()) {
  const id = groupsOf(w).groups[g]?.active;
  return (id && tabsOf(w).find(t => t.id === id)) || null;
}

// focusedGroup is the group last clicked or dropped into: the one whose
// file the status bar follows and whose terminal the keyboard reaches.
export const focusedGroup = (w = workspace()) => groupsOf(w).focus;

// groupOf is the group holding tab id, or -1.
export const groupOf = (id, w = workspace()) => groupsOf(w).groups.findIndex(g => g.ids.includes(id));

// frontTab is the tab in front: the focused group's, else the other's.
export function frontTab() {
  const w = workspace();
  const s = groupsOf(w);
  return tabIn(s.focus, w) || tabIn(1 - s.focus, w);
}

// showable says whether a tab may be shown as it is: a file always, a
// terminal only while it runs, since showing a stopped one starts it.
const showable = w => id => idKind(id) === FILE || !!(w?.tabs || []).find(t => t.id === id)?.running;

// openIn says the next new tab of kind belongs to group g: the group's "+"
// menu records it before the tab exists, and the placement rules follow.
export function openIn(g, kind) {
  const w = workspace();
  const key = groupKey(w);
  const s = groupsOf(w);
  s.last[kind === 'file' ? FILE : TERM] = g;
  saveGroups(key, s);
}

// moveTab puts tab id in group to, ahead of before ('' = last): a drop or a
// Move menu item. The server holds the terminals in the order the two
// groups read in (canonicalTerms), so the editor's bar follows; the moves
// that bring it there go first and the page's copy of the order follows
// them, since bringing the groups up to date re-sorts the terminals by the
// server's order.
export async function moveTab(id, to, before) {
  const w = workspace();
  const key = groupKey(w);
  const s = moveInGroups(groupsOf(w), id, to, before, showable(w));
  const desired = canonicalTerms(s);
  const moves = movesToMatch((w?.tabs || []).map(t => t.id), desired);
  for (const m of moves) {
    const res = await call('POST', `api/tabs/${encodeURIComponent(m.id)}/move`, { before: m.before });
    if (!res.ok) {
      failed(res);
      return reload(); // the bars draw the order the server kept
    }
  }
  // The groups are saved against the order the server now holds, not the
  // one the last poll brought.
  if (moves.length) {
    const rank = new Map(desired.map((x, i) => [x, i]));
    w.tabs.sort((a, b) => (rank.get(a.id) ?? Infinity) - (rank.get(b.id) ?? Infinity));
  }
  saveGroups(key, s);
  picked ||= idKind(id) === TERM;
  // Dropping into a collapsed group brings it back.
  if (state.collapsed === String(s.focus)) setCollapsed(-1);
  else rerender();
}

export function selectWorkspace(id) {
  state.ws = id;
  picked = true;
  saveJSON('punk0.ws', id);
  rerender();
}

// selectTab shows tab id in the group holding it and focuses that group; a
// tab of a collapsed group brings the group back. A tab no group holds yet
// (one just started, or a chat being resumed) is shown once the next list
// places it.
export function selectTab(id) {
  const w = workspace();
  const key = groupKey(w);
  const s = groupsOf(w);
  const g = s.groups.findIndex(x => x.ids.includes(id));
  pending = g >= 0 ? '' : id;
  if (g >= 0) {
    s.groups[g].active = id;
    s.focus = g;
    s.last[idKind(id)] = g;
    saveGroups(key, s);
  }
  picked ||= idKind(id) === TERM;
  if (g >= 0 && state.collapsed === String(g)) return setCollapsed(-1);
  rerender();
}

// collapsedGroup is the group the split layout hides, or -1 for neither.
export const collapsedGroup = () => (state.collapsed === '0' ? 0 : state.collapsed === '1' ? 1 : -1);

// setCollapsed collapses group g of the split layout, or neither with -1.
export function setCollapsed(g) {
  state.collapsed = g === 0 || g === 1 ? String(g) : '';
  saveJSON('punk0.splitCollapsed', state.collapsed);
  rerender();
}

// openFile shows path in a tab of the selected workspace, reusing its tab
// if it is already open. mode 'diff' shows the file's diff, 'source' its
// text; without one, an open tab keeps what it shows and a new one shows
// the text.
export function openFile(path, mode) {
  const open = (state.files[state.ws] ||= []);
  if (!open.includes(path)) {
    open.push(path);
    saveJSON('punk0.files', state.files);
  }
  const diffs = state.diffs[state.ws] || [];
  if (mode && (mode === 'diff') !== diffs.includes(path)) {
    state.diffs[state.ws] = mode === 'diff' ? [...diffs, path] : diffs.filter(p => p !== path);
    saveJSON('punk0.fileDiff', state.diffs);
  }
  selectTab(fileTabId(path));
}

// openChanges shows the selected workspace's Changes tab, opening it first.
export function openChanges() {
  if (!state.changes[state.ws]) {
    state.changes[state.ws] = true;
    saveJSON('punk0.changes', state.changes);
  }
  selectTab(CHANGES_TAB);
}

// closeFile closes path's tab, and closeChanges the Changes tab. Losing its
// last tab closes the second group (groupsOf), so the next file opens
// beside the terminals again.
export function closeFile(path) {
  state.files[state.ws] = (state.files[state.ws] || []).filter(p => p !== path);
  saveJSON('punk0.files', state.files);
  if (state.diffs[state.ws]?.includes(path)) {
    state.diffs[state.ws] = state.diffs[state.ws].filter(p => p !== path);
    saveJSON('punk0.fileDiff', state.diffs);
  }
  rerender();
}

export function closeChanges() {
  delete state.changes[state.ws];
  saveJSON('punk0.changes', state.changes);
  rerender();
}

// openWhatsNew shows the release notes of version ver in the page's one
// What's New tab.
export function openWhatsNew(ver) {
  state.whatsNew = ver;
  saveJSON('punk0.whatsNew', ver);
  selectTab(WHATSNEW_TAB);
}

export function closeWhatsNew() {
  state.whatsNew = '';
  saveJSON('punk0.whatsNew', '');
  rerender();
}

// tildify shortens a path under the home folder the parent reports.
export function tildify(p) {
  const { home, sep } = state.wb;
  return home && (p === home || p.startsWith(home + sep)) ? '~' + p.slice(home.length) : p;
}
