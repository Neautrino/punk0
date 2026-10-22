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
  tabs: loadJSON('punk0.tabs', {}), // workspace id -> last tab id
  files: loadJSON('punk0.files', {}), // workspace id -> open file paths
};

export function subscribe(fn) {
  listeners.add(fn);
}

function notify() {
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
      notify();
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

// tabsOf lists a workspace's tabs: its shells and Claude chats from the
// server, then the files open in it, which only this browser remembers.
export function tabsOf(w) {
  if (!w) return [];
  const files = (state.files[w.id] || []).map(path => ({
    id: fileTabId(path),
    kind: 'file',
    path,
    name: path.slice(path.lastIndexOf('/') + 1),
  }));
  return [...w.tabs, ...files];
}

// activeTab is the selected workspace's last used tab, else its first.
export function activeTab() {
  const tabs = tabsOf(workspace());
  return tabs.find(t => t.id === state.tabs[state.ws]) || tabs[0] || null;
}

export function selectWorkspace(id) {
  state.ws = id;
  saveJSON('punk0.ws', id);
  notify();
}

export function selectTab(id) {
  state.tabs[state.ws] = id;
  saveJSON('punk0.tabs', state.tabs);
  notify();
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

export function closeFile(path) {
  state.files[state.ws] = (state.files[state.ws] || []).filter(p => p !== path);
  saveJSON('punk0.files', state.files);
  notify();
}

// tildify shortens a path under the home folder the parent reports.
export function tildify(p) {
  const { home, sep } = state.wb;
  return home && (p === home || p.startsWith(home + sep)) ? '~' + p.slice(home.length) : p;
}
