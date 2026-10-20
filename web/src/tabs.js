// web/src/tabs.js
// Tab bar of the selected workspace: every tab is a shell in the workspace's
// folder, a claude tab one that runs Claude Code. Double-click renames a tab
// after what it is for.
import { subscribe, reload, workspace, activeTab, selectTab, api, esc } from './store.js';

let barEl, addEls;

export function initTabs(container) {
  barEl = container.querySelector('#tabs');
  addEls = container.querySelectorAll('[data-new-tab]');
  for (const el of addEls) el.addEventListener('click', () => add(el.dataset.newTab));
  barEl.addEventListener('click', onClick);
  barEl.addEventListener('dblclick', onRename);
  subscribe(render);
}

function render() {
  const w = workspace();
  const active = activeTab();
  for (const el of addEls) el.disabled = !w || w.missing;
  barEl.innerHTML = (w ? w.tabs : []).map((t) => `
    <div class="tab${t.id === active?.id ? ' active' : ''}" data-tab="${esc(t.id)}" title="${t.kind === 'claude' ? 'Claude Code' : 'Shell'}: double-click to rename">
      <span class="dot${t.running ? ' on' : ''}"></span>
      <span class="tab-kind">${t.kind === 'claude' ? '✳' : '$'}</span>
      <span class="tab-name">${esc(t.name)}</span>
      <button type="button" class="icon-btn del" data-act="close" title="Close tab">×</button>
    </div>`).join('');
}

function tabOf(e) {
  const id = e.target.closest('[data-tab]')?.dataset.tab;
  return workspace()?.tabs.find((t) => t.id === id) || null;
}

async function onClick(e) {
  const t = tabOf(e);
  if (!t) return;
  // Re-rendering the active tab would swallow the second click of a rename.
  if (!e.target.closest('[data-act=close]')) return t.id !== activeTab()?.id && selectTab(t.id);
  if (t.running && !confirm(`Close "${t.name}"? Its shell and anything running in it stop.`)) return;
  const res = await api('DELETE', `/api/tabs/${encodeURIComponent(t.id)}`);
  if (!res.ok) return alert(res.data.error);
  reload();
}

async function onRename(e) {
  const t = tabOf(e);
  if (!t || e.target.closest('[data-act=close]')) return;
  const name = prompt('Tab name', t.name)?.trim();
  if (!name || name === t.name) return;
  const res = await api('PATCH', `/api/tabs/${encodeURIComponent(t.id)}`, { name });
  if (!res.ok) return alert(res.data.error);
  reload();
}

async function add(kind) {
  const w = workspace();
  if (!w) return;
  const res = await api('POST', `/api/workspaces/${encodeURIComponent(w.id)}/tabs`, { kind });
  if (!res.ok) return alert(res.data.error);
  await reload();
  selectTab(res.data.id);
}
