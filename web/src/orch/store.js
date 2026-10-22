// web/src/orch/store.js
// The project tree from GET /api/projects, the pull requests from
// GET /api/wb/state, and what is selected. The sidebar, tab bar and panes all
// render from here. The tree is small, so every change reloads all of it.
import { call } from './api.js';

const listeners = new Set();

const loadJSON = (key, empty) => {
  try {
    return JSON.parse(localStorage.getItem(key)) || empty;
  } catch {
    return empty;
  }
};
const saveJSON = (key, value) => {
  try {
    localStorage.setItem(key, typeof value === 'string' ? value : JSON.stringify(value));
  } catch {}
};

export const state = {
  projects: [],
  reviews: [], // pull request items from /api/wb/state
  wb: { cwd: '', home: '', sep: '/', version: '' },
  ws: localStorage.getItem('punk0.ws') || '', // selected workspace id
  // workspace id -> { term, file, front }: its last used terminal tab and
  // file tab, and which of the two was used last.
  tabs: loadJSON('punk0.tabs', {}),
  files: loadJSON('punk0.files', {}), // workspace id -> open file paths
  // The split layout's collapsed group: '', 'term' (files maximized) or
  // 'file' (file panel hidden). Selecting a tab of that group shows it again.
  collapsed: localStorage.getItem('punk0.collapsed') || '',
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
    // An archived chat cannot stay selected: its tabs are stopped.
    const sel = workspace();
    if (!sel || sel.archived) {
      const all = state.projects.flatMap(pr => pr.workspaces).filter(w => !w.archived);
      const first = all.find(w => !projectOf(w.id)?.scratch) || all[0];
      if (first) return selectWorkspace(first.id);
      if (sel) return selectWorkspace('');
    }
    const sig = JSON.stringify([state.projects, state.reviews, state.wb.version]);
    if (sig !== lastSig) {
      lastSig = sig;
      rerender();
    }
  })().finally(() => { reloading = null; });
  return reloading;
}

// pruneFiles forgets the open files of workspaces that no longer exist.
function pruneFiles() {
  if (!state.projects.length) return; // not loaded yet: keep what is stored
  const ids = new Set(state.projects.flatMap(p => p.workspaces.map(w => w.id)));
  const stale = Object.keys(state.files).filter(id => !ids.has(id));
  if (!stale.length) return;
  for (const id of stale) delete state.files[id];
  saveJSON('punk0.files', state.files);
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

// wsLabel is what a workspace is called on the page: a scratch chat's title,
// else its name.
export const wsLabel = w => w.title || w.name;

export const fileTabId = path => `file:${path}`;
const kindOf = id => (id.startsWith('file:') ? 'file' : 'term');

// tabsOf lists a workspace's tabs: its shells and Claude chats from the
// server, then the files open in it, which only this browser remembers.
export function tabsOf(w) {
  if (!w) return [];
  return [...w.tabs, ...fileTabsOf(w)];
}

export function fileTabsOf(w) {
  return (state.files[w?.id] || []).map(path => ({
    id: fileTabId(path),
    kind: 'file',
    path,
    name: path.slice(path.lastIndexOf('/') + 1),
  }));
}

// used is the remembered tabs of workspace id; an entry stored in another
// shape counts as none.
function used(id) {
  const e = state.tabs[id];
  return e && typeof e === 'object' ? e : (state.tabs[id] = {});
}

// termTab is the selected workspace's last used terminal tab, else its first.
export function termTab(w = workspace()) {
  if (!w) return null;
  const id = used(w.id).term;
  return w.tabs.find(t => t.id === id) || w.tabs[0] || null;
}

// fileTab is the selected workspace's last used file tab, else its first.
export function fileTab(w = workspace()) {
  const files = fileTabsOf(w);
  const id = w && used(w.id).file;
  return files.find(t => t.id === id) || files[0] || null;
}

// activeTab is the tab in front: of the selected workspace's last used
// terminal and file tabs, the one used last.
export function activeTab() {
  const w = workspace();
  const t = termTab(w), f = fileTab(w);
  return (w && used(w.id).front === 'file' ? f || t : t || f) || null;
}

export function selectWorkspace(id) {
  state.ws = id;
  saveJSON('punk0.ws', id);
  rerender();
}

export function selectTab(id) {
  const kind = kindOf(id);
  Object.assign(used(state.ws), { [kind]: id, front: kind });
  saveJSON('punk0.tabs', state.tabs);
  if (state.collapsed === kind) return setCollapsed('');
  rerender();
}

// setCollapsed collapses one group of the split layout ('term' or 'file'),
// or none ('').
export function setCollapsed(kind) {
  state.collapsed = kind;
  saveJSON('punk0.collapsed', kind);
  rerender();
}

// openFile shows path in a tab of the selected workspace, reusing its tab
// if it is already open.
export function openFile(path) {
  const open = (state.files[state.ws] ||= []);
  if (!open.includes(path)) {
    open.push(path);
    saveJSON('punk0.files', state.files);
  }
  selectTab(fileTabId(path));
}

// closeFile closes path's tab; closing the last one also ends a collapsed
// split, so the next file opens beside the terminals again.
export function closeFile(path) {
  const open = (state.files[state.ws] || []).filter(p => p !== path);
  state.files[state.ws] = open;
  saveJSON('punk0.files', state.files);
  if (!open.length && state.collapsed) return setCollapsed('');
  rerender();
}

// tildify shortens a path under the home folder the parent reports.
export function tildify(p) {
  const { home, sep } = state.wb;
  return home && (p === home || p.startsWith(home + sep)) ? '~' + p.slice(home.length) : p;
}
