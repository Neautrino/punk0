// web/src/store.js
// The project tree from GET /api/projects and what is selected in it. The
// sidebar, tab bar and panes all render from here; every change reloads the
// whole tree, which is small.

const listeners = new Set();

export const state = {
  projects: [],
  ws: localStorage.getItem('punk0.ws') || '', // selected workspace id
  tabs: JSON.parse(localStorage.getItem('punk0.tabs') || '{}'), // workspace id -> last tab id
  files: JSON.parse(localStorage.getItem('punk0.files') || '{}'), // workspace id -> open file paths
};

export function subscribe(fn) {
  listeners.add(fn);
}

function emit() {
  for (const fn of listeners) fn();
}

export async function reload() {
  const res = await fetch('/api/projects');
  if (!res.ok) return;
  state.projects = await res.json();
  pruneFiles();
  if (!workspace()) {
    const first = state.projects.flatMap((p) => p.workspaces)[0];
    selectWorkspace(first ? first.id : '');
    return;
  }
  emit();
}

// pruneFiles forgets the open files of workspaces that no longer exist.
function pruneFiles() {
  const ids = new Set(state.projects.flatMap((p) => p.workspaces.map((w) => w.id)));
  const stale = Object.keys(state.files).filter((id) => !ids.has(id));
  if (!stale.length) return;
  for (const id of stale) delete state.files[id];
  saveFiles();
}

// workspace returns the workspace with id, or the selected one.
export function workspace(id = state.ws) {
  for (const p of state.projects) {
    const w = p.workspaces.find((w) => w.id === id);
    if (w) return w;
  }
  return null;
}

export function projectOf(wsId) {
  return state.projects.find((p) => p.workspaces.some((w) => w.id === wsId)) || null;
}

// tabsOf lists a workspace's tabs: its shells and Claude chats from the
// server, then the files open in it, which only this browser remembers.
export function tabsOf(w) {
  if (!w) return [];
  const files = (state.files[w.id] || []).map((path) => ({
    id: fileTabId(path),
    kind: 'file',
    path,
    name: path.slice(path.lastIndexOf('/') + 1),
  }));
  return [...w.tabs, ...files];
}

export const fileTabId = (path) => `file:${path}`;

// activeTab is the selected workspace's last used tab, else its first.
export function activeTab() {
  const tabs = tabsOf(workspace());
  return tabs.find((t) => t.id === state.tabs[state.ws]) || tabs[0] || null;
}

export function selectWorkspace(id) {
  state.ws = id;
  localStorage.setItem('punk0.ws', id);
  emit();
}

export function selectTab(id) {
  state.tabs[state.ws] = id;
  localStorage.setItem('punk0.tabs', JSON.stringify(state.tabs));
  emit();
}

// openFile shows path in a tab of the selected workspace, reusing its tab
// if it is already open.
export function openFile(path) {
  const open = (state.files[state.ws] ||= []);
  if (!open.includes(path)) {
    open.push(path);
    saveFiles();
  }
  selectTab(fileTabId(path));
}

export function closeFile(path) {
  state.files[state.ws] = (state.files[state.ws] || []).filter((p) => p !== path);
  saveFiles();
  emit();
}

function saveFiles() {
  localStorage.setItem('punk0.files', JSON.stringify(state.files));
}

// api sends a JSON request and returns { ok, status, data }; data holds the
// server's { error } on failure.
export async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  return { ok: res.ok, status: res.status, data: text ? JSON.parse(text) : null };
}

export function esc(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}
