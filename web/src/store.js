// web/src/store.js
// The project tree from GET /api/projects and what is selected in it. The
// sidebar, tab bar and panes all render from here; every change reloads the
// whole tree, which is small.

const listeners = new Set();

export const state = {
  projects: [],
  ws: localStorage.getItem('punk0.ws') || '', // selected workspace id
  tabs: JSON.parse(localStorage.getItem('punk0.tabs') || '{}'), // workspace id -> last tab id
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
  if (!workspace()) {
    const first = state.projects.flatMap((p) => p.workspaces)[0];
    selectWorkspace(first ? first.id : '');
    return;
  }
  emit();
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

// activeTab is the selected workspace's last used tab, else its first.
export function activeTab() {
  const w = workspace();
  if (!w) return null;
  return w.tabs.find((t) => t.id === state.tabs[w.id]) || w.tabs[0] || null;
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
