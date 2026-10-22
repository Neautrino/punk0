// web/src/orch/listview.js
// How the sidebar lists each view, chosen in its View options menu: how rows
// are grouped and sorted, which agents or projects show, and what is hidden.
// Saved per view in this browser. The text search is separate (sidebar.js)
// and reaches rows these filters hide; the selected workspace always shows.
import { esc } from '../state.js';
import { call } from './api.js';

const KEY = 'punk0.listView';

const DEFAULTS = {
  scratch: { groupBy: 'group', sort: 'active', hiddenAgents: [], hideSleeping: false, hideMine: false, showArchived: false },
  projects: { groupBy: 'project', hiddenProjects: [], hideSleeping: false, hideLocal: false, hideMissing: false, hideReviews: false },
};

// The menu's choices per view. Show is filled in from what the list holds.
const MENU = {
  scratch: {
    groupBy: [['group', 'Group'], ['date', 'Date'], ['agent', 'Agent'], ['status', 'Status'], ['none', 'None']],
    sort: [['active', 'Last used'], ['created', 'Created'], ['name', 'Name']],
    show: { key: 'hiddenAgents', title: 'Agents' },
    filters: [['hideSleeping', 'Hide sleeping'], ['hideMine', 'Hide chats in my folders'], ['showArchived', 'Show archived']],
  },
  projects: {
    groupBy: [['project', 'Project'], ['status', 'Status'], ['none', 'None']],
    show: { key: 'hiddenProjects', title: 'Projects' },
    filters: [['hideSleeping', 'Hide sleeping'], ['hideLocal', 'Hide local workspaces'], ['hideMissing', 'Hide missing folders'], ['hideReviews', 'Hide reviews']],
  },
};

function load() {
  let saved = {};
  try {
    saved = JSON.parse(localStorage.getItem(KEY)) || {};
  } catch {}
  return {
    scratch: { ...DEFAULTS.scratch, ...saved.scratch },
    projects: { ...DEFAULTS.projects, ...saved.projects },
  };
}

export const opts = load();
const save = () => localStorage.setItem(KEY, JSON.stringify(opts));

export function setOpt(view, key, value) {
  opts[view][key] = value;
  save();
}

// activeCount is how many filters are on in a view: each hidden agent or
// project, and each filter toggle. Grouping and sorting are not filters.
export function activeCount(view) {
  const o = opts[view];
  return o[MENU[view].show.key].length + MENU[view].filters.filter(([k]) => o[k]).length;
}

// clearFilters resets what activeCount counts, leaving grouping and sorting.
function clearFilters(view) {
  const o = opts[view];
  o[MENU[view].show.key] = [];
  for (const [k] of MENU[view].filters) o[k] = false;
  save();
}

/* ---------- what a row is ---------- */

// The tab status that most needs a look wins a workspace's state.
const URGENCY = ['waiting', 'working', 'idle'];
export const STATES = [['waiting', 'Needs you'], ['working', 'Working'], ['idle', 'Idle'], ['running', 'Running'], ['stopped', 'Stopped']];

export const isRunning = w => w.backend === 'running' || w.tabs.some(t => t.running);

// statusOf is the most urgent agent status among a workspace's running tabs.
export function statusOf(w) {
  const statuses = w.tabs.filter(t => t.running).map(t => t.status);
  return URGENCY.find(s => statuses.includes(s)) || '';
}

// stateOf places a workspace in the Status grouping.
export const stateOf = w => statusOf(w) || (isRunning(w) ? 'running' : 'stopped');

// A chat's agent is its first tab's; a chat whose tabs were all closed has none.
export const agentOf = w => w.tabs[0]?.kind || 'none';

const agentLabels = { shell: 'Shell', none: 'No tab' };
export const agentLabel = id => agentLabels[id] || id;

/* ---------- the menu ---------- */

let btn, menu, countEl, ctx;

// initViewMenu wires the View options button. ctx.view() is the sidebar's
// view, ctx.shown() lists [id, label] pairs for its Show section, and
// ctx.changed() re-renders the list.
export function initViewMenu(button, menuEl, context) {
  btn = button;
  menu = menuEl;
  ctx = context;
  countEl = btn.querySelector('.o-view-count');
  btn.addEventListener('click', () => (menu.hidden ? open() : close()));
  menu.addEventListener('click', onPick);
  menu.addEventListener('keydown', e => {
    if (e.key === 'Escape') {
      close();
      btn.focus();
    }
  });
  document.addEventListener('mousedown', e => {
    if (!menu.hidden && !menu.contains(e.target) && !btn.contains(e.target)) close();
  });
  call('GET', 'api/agents').then(res => {
    if (!res.ok) return;
    for (const a of res.data) agentLabels[a.id] = a.label;
    ctx.changed();
  });
}

// syncViewButton shows the active filter count on the button.
export function syncViewButton() {
  const n = activeCount(ctx.view());
  countEl.textContent = n || '';
  countEl.hidden = !n;
  btn.classList.toggle('on', n > 0);
  btn.title = n ? `View options (${n} filter${n === 1 ? '' : 's'} on)` : 'View options';
  if (!menu.hidden) render();
}

export function closeViewMenu() {
  close();
}

function open() {
  render();
  menu.hidden = false;
  btn.setAttribute('aria-expanded', 'true');
  const r = btn.getBoundingClientRect();
  menu.style.top = `${r.bottom + 4}px`;
  menu.style.left = `${Math.max(4, Math.min(r.right - menu.offsetWidth, innerWidth - menu.offsetWidth - 4))}px`;
  menu.querySelector('button')?.focus();
}

function close() {
  if (!menu || menu.hidden) return;
  menu.hidden = true;
  btn.setAttribute('aria-expanded', 'false');
}

function render() {
  const view = ctx.view();
  const o = opts[view];
  const m = MENU[view];
  const radio = (key, [val, label]) =>
    `<button type="button" role="menuitemradio" aria-checked="${o[key] === val}" data-opt="${key}" data-val="${esc(val)}">${esc(label)}</button>`;
  const check = (key, label, on) =>
    `<button type="button" role="menuitemcheckbox" aria-checked="${on}" data-opt="${key}">${esc(label)}</button>`;
  const shown = ctx.shown();
  const hidden = o[m.show.key];
  let html = '<div class="o-menu-label">Group by</div>' + m.groupBy.map(c => radio('groupBy', c)).join('');
  if (m.sort) html += '<div class="o-menu-sep" role="separator"></div><div class="o-menu-label">Sort</div>' + m.sort.map(c => radio('sort', c)).join('');
  if (shown.length) {
    html += `<div class="o-menu-sep" role="separator"></div><div class="o-menu-label">Show ${esc(m.show.title.toLowerCase())}</div>` +
      shown.map(([id, label]) => `<button type="button" role="menuitemcheckbox" aria-checked="${!hidden.includes(id)}" data-show="${esc(id)}">${esc(label)}</button>`).join('');
  }
  html += '<div class="o-menu-sep" role="separator"></div><div class="o-menu-label">Filters</div>' +
    m.filters.map(([k, label]) => check(k, label, !!o[k])).join('');
  const n = activeCount(view);
  html += `<div class="o-menu-sep" role="separator"></div><button type="button" role="menuitem" data-clear ${n ? '' : 'disabled'}>Clear filters${n ? ` (${n})` : ''}</button>`;
  menu.innerHTML = html;
}

function onPick(e) {
  const b = e.target.closest('button');
  if (!b || b.disabled) return;
  const view = ctx.view();
  const o = opts[view];
  if (b.hasAttribute('data-clear')) clearFilters(view);
  else if (b.dataset.show) {
    const key = MENU[view].show.key;
    const id = b.dataset.show;
    setOpt(view, key, o[key].includes(id) ? o[key].filter(x => x !== id) : [...o[key], id]);
  } else if (b.dataset.val) setOpt(view, b.dataset.opt, b.dataset.val);
  else setOpt(view, b.dataset.opt, !o[b.dataset.opt]);
  // Keep focus on the same choice across the re-render.
  const sel = b.dataset.show ? `[data-show="${CSS.escape(b.dataset.show)}"]`
    : b.dataset.val ? `[data-opt="${b.dataset.opt}"][data-val="${CSS.escape(b.dataset.val)}"]`
    : b.dataset.opt ? `[data-opt="${b.dataset.opt}"]` : '[data-clear]';
  ctx.changed();
  render();
  menu.querySelector(sel)?.focus();
}
