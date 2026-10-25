// web/src/orch/history.js
// The right side's Chats mode: the chats Claude Code, Codex and OpenCode
// kept on disk, read live from GET api/history. punk0 does not own these
// conversations; it lists them, so a chat started in any terminal can be
// found again and picked up here. The head holds the scope tabs (how wide
// the list is around the selected workspace), the agent picker, the search
// over titles and a ⋯ menu (Sync, Show removed). The agent picker is a
// dropdown while the pane is wide and a filter button with a menu while it
// is narrow (CSS swaps them); both set the same agent.
//
// A chat is one line: whether a tab has it open, its title, agent, age and a
// chevron. Clicking the row EXPANDS it into a preview -- first prompt, last
// turns, and the Resume / Copy id / Remove buttons -- rather than opening it:
// resuming starts a process, and a mis-click must not. Resume is
// POST api/history/open, which opens the tab on that exact session. The trash
// over the row end opens one popup for both ways of letting a chat go:
// Remove takes the row out of punk0's list (POST api/history/remove, Sync
// brings it back), and Delete permanently is the agent's own delete
// (DELETE api/history/{agent}/{id}), which closes its tabs first.
//
// Project scope groups the chats by the project's workspace they ran in, All
// scope by project, with the chats of folders in no project last under Other
// folders; each group folds on its header. The server side is history.go.
import { $, esc } from '../state.js';
import { on } from '../bus.js';
import { showToast } from '../ui.js';
import { call, failed } from './api.js';
import { state, subscribe, reload, workspace, projectOf, wsLabel, tildify, selectWorkspace, selectTab } from './store.js';
import { paneView } from './paneviews.js';
import { openRemoveDialog } from './removedialog.js';

const SCOPE_KEY = 'punk0.historyScope';
const FOLD_KEY = 'punk0.histFolded';
const AGENT_KEY = 'punk0.histAgent';
const SCOPES = ['workspace', 'project', 'all'];
const AGENT = { claude: 'Claude Code', codex: 'Codex', opencode: 'OpenCode' };
const OTHER = 'other'; // the group of folders in no project
const EMPTY = { workspace: 'No chats in this workspace yet', project: 'No chats in this project yet', all: 'No chats yet' };
const ICON_CHEV = '<svg class="o-chev" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 6 6 6-6 6"/></svg>';
const ICON_TRASH = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/></svg>';
const ICON_BACK = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 4v4h4"/></svg>';
// What a permanent delete does, in the agent's own terms.
const DELETES = {
  claude: id => `${id}.jsonl and its ${id}/ folder under ~/.claude/projects go. This cannot be undone.`,
  codex: id => `Runs codex delete ${id}. This cannot be undone.`,
  opencode: id => `Runs opencode session delete ${id}. This cannot be undone.`,
};

let scopeEl, qEl, listEl, agentEl, filterEl, agentsEl, moreEl, menuEl;
let scope = SCOPES.includes(localStorage.getItem(SCOPE_KEY)) ? localStorage.getItem(SCOPE_KEY) : 'project';
let showRemoved = false;
let query = '';
let rows = [];
let note = '';
let loading = null, pending = false;
// The workspace selected when the list was last brought up to date, null
// until the projects are loaded and it is known.
let seenWs = null;
const selected = () => (workspace() ? state.ws : null);
let typing = 0;
// The groups folded (true) or unfolded (false) by hand, by group key; the
// rest follow the default: open, but for Other folders.
const folded = loadFolded();
// Groups folded during the current search; a search shows every group with
// matches open, without touching the stored folds.
const searchFolded = new Set();
// The agent picked in the dropdown, '' for all. Kept while the list holds
// none of its chats, which then shows them all.
let agent = localStorage.getItem(AGENT_KEY) || '';
// The one expanded row, by 'agent:id', and what was read for it. Kept across
// the ten-second poll's re-renders: the preview is fetched once, never again
// by a poll, and goes only when the row does or the user closes it.
let openKey = null;
let preview = null; // { key, loading, error, data }

export function initHistory() {
  scopeEl = $('#o-hist-scope');
  qEl = $('#o-hist-q');
  listEl = $('#o-hist-list');
  agentEl = $('#o-hist-agent');
  filterEl = $('#o-hist-filter');
  agentsEl = $('#o-hist-agents');
  moreEl = $('#o-hist-more');
  menuEl = $('#o-hist-menu');

  drawScope();
  scopeEl.addEventListener('click', e => {
    const b = e.target.closest('[data-scope]');
    if (b) setScope(b.dataset.scope);
  });
  scopeEl.addEventListener('keydown', scopeKey);
  qEl.addEventListener('input', () => {
    query = qEl.value.trim();
    searchFolded.clear();
    clearTimeout(typing);
    typing = setTimeout(refresh, 150);
  });
  qEl.addEventListener('keydown', e => {
    if (e.key !== 'Escape' || !qEl.value) return;
    e.preventDefault();
    qEl.value = query = '';
    searchFolded.clear();
    refresh();
  });
  agentEl.addEventListener('change', () => setAgent(agentEl.value));
  filterEl.addEventListener('click', () => toggleMenu(filterEl, agentsEl));
  agentsEl.addEventListener('click', e => {
    const b = e.target.closest('[data-agent]');
    if (!b) return;
    closeMenu();
    filterEl.focus();
    setAgent(b.dataset.agent);
  });
  moreEl.addEventListener('click', () => toggleMenu(moreEl, menuEl));
  menuEl.addEventListener('click', onMenu);
  document.addEventListener('mousedown', e => {
    if (opened && !opened.menu.contains(e.target) && !opened.btn.contains(e.target)) closeMenu();
  });
  // Esc closes the top-most thing: an open menu first, then the preview. The
  // search box handles its own Esc and says so by taking the event.
  document.addEventListener('keydown', e => {
    if (e.key !== 'Escape' || e.defaultPrevented || !visible()) return;
    if (opened) {
      const btn = opened.btn;
      closeMenu();
      btn.focus();
      e.preventDefault();
      return;
    }
    if (!openKey) return;
    e.preventDefault();
    // The row button is looked up after the collapse: closePreview() renders
    // the list again, so the node Esc was pressed on is gone by then and
    // focus would fall to the page itself.
    const key = openKey;
    closePreview();
    rowEl(key)?.querySelector('[data-act="row"]')?.focus();
  });
  listEl.addEventListener('click', onClick);
  listEl.addEventListener('keydown', onKey);

  on('paneview:changed', () => {
    closeMenu();
    if (visible()) refresh();
  });
  // A workspace or project removed in the sidebar takes its tabs with it: the
  // list would show them as open until the next poll.
  on('orch:removed', () => visible() && refresh());
  // A tab closed in the tab bar: the chat stays listed, but its dot is filled
  // until the list is read again.
  on('orch:tabclosed', () => visible() && refresh());
  // The selected workspace decides what the narrow scopes show; All only
  // regroups.
  subscribe(() => {
    if (selected() === seenWs) return;
    seenWs = selected();
    if (!visible()) return;
    if (scope === 'all') render();
    else refresh();
  });
  // The agents write these files from outside punk0, so the list is polled
  // while it is the mode in front and the page is in view.
  setInterval(() => visible() && refresh(), 10000);
  // paneviews.js picks the saved mode before this module is wired up, so its
  // first 'paneview:changed' is already past: load it here.
  if (visible()) refresh();
}

const visible = () => paneView() === 'chats' && !document.hidden;

/* ---------- the scope tabs ---------- */

function setScope(s) {
  if (s === scope || !SCOPES.includes(s)) return;
  scope = s;
  try { localStorage.setItem(SCOPE_KEY, scope); } catch {}
  drawScope();
  refresh();
}

function drawScope() {
  for (const b of scopeEl.querySelectorAll('[data-scope]')) {
    const on = b.dataset.scope === scope;
    b.setAttribute('aria-selected', String(on));
    b.tabIndex = on ? 0 : -1;
  }
}

// The arrow keys, Home and End move along the tabs, as in any tablist.
function scopeKey(e) {
  const i = SCOPES.indexOf(e.target.closest?.('[data-scope]')?.dataset.scope);
  if (i < 0) return;
  const n = SCOPES.length;
  const to = { ArrowRight: (i + 1) % n, ArrowLeft: (i + n - 1) % n, Home: 0, End: n - 1 }[e.key];
  if (to === undefined) return;
  e.preventDefault();
  setScope(SCOPES[to]);
  scopeEl.querySelector(`[data-scope="${SCOPES[to]}"]`).focus();
}

/* ---------- the agent ---------- */

function setAgent(a) {
  agent = a;
  try { localStorage.setItem(AGENT_KEY, agent); } catch {}
  render();
}

/* ---------- the menus: the agent filter's and ⋯ ---------- */

let opened = null; // {btn, menu} while one is open

function toggleMenu(btn, menu) {
  const was = opened?.menu === menu;
  closeMenu();
  if (was) return;
  if (menu === menuEl) menu.querySelector('[data-act=removed]').setAttribute('aria-checked', String(showRemoved));
  opened = { btn, menu };
  menu.hidden = false;
  btn.setAttribute('aria-expanded', 'true');
  const r = btn.getBoundingClientRect();
  const box = menu.offsetParent?.getBoundingClientRect() || { top: 0, left: 0 };
  menu.style.top = `${r.bottom + 4 - box.top}px`;
  menu.style.left = `${Math.max(4, r.right - menu.offsetWidth - box.left)}px`;
  (menu.querySelector('[aria-checked="true"]') || menu.querySelector('button')).focus();
}

function closeMenu() {
  if (!opened) return;
  opened.menu.hidden = true;
  opened.btn.setAttribute('aria-expanded', 'false');
  opened = null;
}

function onMenu(e) {
  const act = e.target.closest('[data-act]')?.dataset.act;
  if (!act) return;
  closeMenu();
  moreEl.focus();
  if (act === 'sync') return sync();
  showRemoved = !showRemoved;
  refresh();
}

// sync re-reads the agents' folders and brings every removed chat back: one
// button for "show me what is really there".
async function sync() {
  const res = await call('POST', 'api/history/sync', {});
  if (!res.ok) return failed(res);
  const n = res.data?.restored || 0;
  if (n) showToast('✓', n === 1 ? '1 removed chat is back in the list' : `${n} removed chats are back in the list`, 4000);
  refresh();
}

/* ---------- the list ---------- */

// refresh reloads the list; calls made while one is in flight share the next.
function refresh() {
  if (loading) {
    pending = true;
    return loading;
  }
  loading = (async () => {
    const ws = workspace();
    seenWs = selected();
    note = '';
    if (scope !== 'all' && !ws) {
      rows = [];
      note = 'Select a workspace to see its chats';
      return render();
    }
    const p = new URLSearchParams({ scope });
    if (scope !== 'all') p.set('ws', ws.id);
    if (query) p.set('q', query);
    if (showRemoved) p.set('removed', '1');
    const res = await call('GET', 'api/history?' + p);
    rows = res.ok ? res.data?.sessions || [] : [];
    note = res.ok ? '' : res.data?.error || 'Could not read the chat history';
    render();
  })().finally(() => {
    loading = null;
    if (pending) {
      pending = false;
      refresh();
    }
  });
  return loading;
}

function render() {
  const list = shown();
  // A row that is no longer listed takes its preview with it.
  if (openKey && !list.some(r => keyOf(r) === openKey)) closePreviewState();
  const mark = focusMark();
  drawAgents();
  if (!list.length) {
    listEl.innerHTML = `<p class="o-hist-none">${esc(note || (query ? 'No chat matches' : EMPTY[scope]))}</p>`;
    return;
  }
  listEl.innerHTML = scope === 'workspace' ? list.map(row).join('') : groups(list);
  refocus(mark);
}

const keyOf = r => `${r.agent}:${r.id}`;
const rowEl = key => listEl.querySelector(`[data-key="${CSS.escape(key)}"]`);

// picked is the agent the list is narrowed to: the stored one while the
// list holds chats of it, else none.
const picked = () => (agent && rows.some(r => r.agent === agent) ? agent : '');

// shown is the chats the list draws: the loaded ones, of the picked agent.
function shown() {
  const a = picked();
  return a ? rows.filter(r => r.agent === a) : rows;
}

// focusMark / refocus carry the focus across a re-render: the ten-second poll
// replaces the whole list, and an expanded row the user is reading with the
// keyboard must not lose it.
function focusMark() {
  const el = document.activeElement;
  if (!el || !listEl.contains(el)) return null;
  const fold = el.closest('[data-fold]');
  if (fold) return { fold: fold.dataset.fold };
  const r = el.closest('[data-key]');
  const act = el.closest('[data-act]')?.dataset.act;
  return r && act ? { key: r.dataset.key, act } : null;
}

function refocus(mark) {
  if (!mark) return;
  if (mark.fold) return listEl.querySelector(`[data-fold="${CSS.escape(mark.fold)}"]`)?.focus();
  rowEl(mark.key)?.querySelector(`[data-act="${mark.act}"]`)?.focus();
}

// drawAgents fills the dropdown and the filter menu with the agents that
// have chats in the list, in a fixed order; they are rebuilt only when that
// set changes. The filter button counts 1 while an agent is picked.
function drawAgents() {
  const have = new Set(rows.map(r => r.agent));
  const ids = [...Object.keys(AGENT), ...[...have].filter(a => !AGENT[a]).sort()].filter(a => have.has(a));
  const key = ids.join(' ');
  if (agentEl.dataset.agents !== key) {
    agentEl.dataset.agents = key;
    const all = [['', 'All agents'], ...ids.map(a => [a, AGENT[a] || a])];
    agentEl.innerHTML = all.map(([a, name]) => `<option value="${esc(a)}">${esc(name)}</option>`).join('');
    agentsEl.innerHTML = all.map(([a, name]) =>
      `<button type="button" role="menuitemradio" aria-checked="false" data-agent="${esc(a)}">${esc(name)}</button>`).join('');
  }
  const a = picked();
  agentEl.value = a;
  for (const b of agentsEl.querySelectorAll('[data-agent]')) b.setAttribute('aria-checked', String(b.dataset.agent === a));
  const count = filterEl.querySelector('.o-view-count');
  count.textContent = a ? '1' : '';
  count.hidden = !a;
  filterEl.classList.toggle('on', !!a);
  filterEl.title = a ? `Agent: ${AGENT[a] || a}` : 'Agent';
  filterEl.setAttribute('aria-label', filterEl.title);
}

// groupOf says which group a chat goes under: in Project scope the
// selected project's workspace it ran in, or the project itself for its
// other folders; in All scope its folder's project, or Other folders.
function groupOf(r) {
  if (scope === 'project') {
    const p = projectOf(state.ws);
    const w = r.workspace && p?.workspaces.find(x => x.id === r.workspace);
    if (w) return { key: 'ws:' + w.id, name: wsLabel(w) + (w.kind === 'worktree' ? ' (worktree)' : '') };
    return { key: 'project:' + (p?.id || ''), name: p?.name || 'This project' };
  }
  const p = r.workspace && projectOf(r.workspace);
  return p ? { key: 'project:' + p.id, name: p.name } : { key: OTHER, name: 'Other folders' };
}

// groups lists the chats under their group, in the order the server sent
// them: the group of the most recent chat comes first, Other folders last.
// A folded group is its header alone: chevron, name, count.
function groups(list) {
  const by = new Map();
  for (const r of list) {
    const g = groupOf(r);
    if (!by.has(g.key)) by.set(g.key, { ...g, chats: [] });
    by.get(g.key).chats.push(r);
  }
  const all = [...by.values()].sort((a, b) => (a.key === OTHER) - (b.key === OTHER));
  return all.map(g => {
    const open = isOpen(g.key);
    return `
    <div class="o-hist-group">
      <div class="o-hist-fold" data-fold="${esc(g.key)}" role="button" tabindex="0" aria-expanded="${open}">
        ${ICON_CHEV}<span class="o-hist-gname">${esc(g.name)}</span><span class="o-hist-gcount">${g.chats.length}</span>
      </div>
      ${open ? g.chats.map(row).join('') : ''}
    </div>`;
  }).join('');
}

// isOpen says whether a group shows its chats: all of them during a search
// unless folded in it, else as last folded by hand, else all but Other
// folders.
function isOpen(key) {
  if (query) return !searchFolded.has(key);
  if (folded.has(key)) return !folded.get(key);
  return key !== OTHER;
}

function toggleFold(key, want) {
  if (want !== undefined && want === isOpen(key)) return;
  if (query) {
    if (!searchFolded.delete(key)) searchFolded.add(key);
  } else {
    folded.set(key, isOpen(key));
    try { localStorage.setItem(FOLD_KEY, JSON.stringify(Object.fromEntries(folded))); } catch {}
  }
  render();
  listEl.querySelector(`[data-fold="${CSS.escape(key)}"]`)?.focus();
}

// loadFolded reads punk0.histFolded: {group key: true when folded}.
function loadFolded() {
  try {
    const o = JSON.parse(localStorage.getItem(FOLD_KEY) || '{}');
    return new Map(Object.entries(o && typeof o === 'object' ? o : {}).filter(([, v]) => typeof v === 'boolean'));
  } catch {
    return new Map();
  }
}

// row is one chat: a button that expands its preview (status dot, title,
// agent, age, chevron), the trash -- Restore for a removed chat -- over its
// right end, and the preview itself while this is the expanded row.
function row(r) {
  const key = keyOf(r);
  const name = AGENT[r.agent] || r.agent;
  const where = r.missing ? `${tildify(r.dir)} (gone)` : tildify(r.dir);
  const open = key === openKey;
  const cls = ['o-hist-row', r.open ? 'open' : '', r.removed ? 'removed' : '', r.missing ? 'missing' : '', open ? 'shown' : ''].filter(Boolean).join(' ');
  return `
    <div class="${cls}" data-key="${esc(key)}">
      <div class="o-hist-line">
        <button type="button" class="o-hist-open" data-act="row" aria-expanded="${open}" title="${esc(r.title)}\n${esc(where)}">
          <span class="o-hist-dot" aria-label="${r.open ? 'Open in a tab' : 'Not open'}"></span>
          <span class="o-hist-title">${esc(r.title)}</span>
          <span class="o-hist-agent">${esc(name)}${r.open ? ' · open' : ''}</span>
          <span class="o-hist-age">${esc(ago(r.updated))}</span>
          ${ICON_CHEV}
        </button>
        <span class="o-hist-acts">${r.removed
          ? `<button type="button" class="mini" data-act="restore" title="Restore" aria-label="Restore chat">${ICON_BACK}</button>`
          : `<button type="button" class="mini o-del" data-act="remove" title="Remove…" aria-label="Remove chat">${ICON_TRASH}</button>`}
        </span>
      </div>
      ${open ? previewHTML(r) : ''}
    </div>`;
}

// previewHTML is the expanded row: the last exchange and how it started, then
// what can be done with it. Everything read from a transcript is escaped --
// it is an agent's or the user's own text, never markup.
function previewHTML(r) {
  const p = preview?.key === keyOf(r) ? preview : null;
  let body = '<p class="o-hist-prev-note">Reading the conversation…</p>';
  if (p?.error) body = `<p class="o-hist-prev-note">${esc(p.error)}</p>`;
  else if (p?.data) {
    const d = p.data;
    // A detached head names no branch: punk0 never sends HEAD, and this keeps it so.
    const branch = d.branch && d.branch !== 'HEAD' ? d.branch : '';
    const facts = [d.messages ? `${d.messages} msg${d.messages === 1 ? '' : 's'}` : '', tildify(d.dir || r.dir), branch].filter(Boolean);
    const agent = AGENT[r.agent] || r.agent;
    body = `
      <p class="o-hist-facts">${facts.map(esc).join(' · ')}</p>
      ${d.lastPrompt ? `<h4 class="o-hist-prev-h">Last exchange</h4>
        <p class="o-hist-prompt"><span class="o-hist-role">You</span>${esc(d.lastPrompt)}</p>
        ${d.lastReply ? `<p class="o-hist-reply"><span class="o-hist-role">${esc(agent)}</span>${esc(d.lastReply)}</p>` : ''}` : ''}
      ${d.started ? `<h4 class="o-hist-prev-h">Started with</h4><p class="o-hist-prompt o-hist-started">${esc(d.started)}</p>` : ''}
      ${!d.lastPrompt ? '<p class="o-hist-prev-note">Nothing was said in this chat yet.</p>' : ''}`;
  }
  return `
      <div class="o-hist-prev">
        ${body}
        <div class="o-hist-prev-acts">
          <button type="button" class="wb-btn wb-primary" data-act="resume">Resume</button>
          <button type="button" class="wb-btn" data-act="copy">Copy id</button>
          ${r.removed
            ? '<button type="button" class="wb-btn" data-act="restore">Restore</button>'
            : '<button type="button" class="wb-btn" data-act="remove">Remove…</button>'}
        </div>
      </div>`;
}

// ago is how long since a chat was touched, as short as the row needs.
function ago(ms) {
  if (!ms) return '';
  const s = Math.max(0, (Date.now() - ms) / 1000);
  if (s < 60) return 'now';
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  const d = Math.floor(s / 86400);
  if (d < 7) return `${d}d`;
  if (d < 365) return `${Math.floor(d / 7)}w`;
  return `${Math.floor(d / 365)}y`;
}

function onClick(e) {
  const fold = e.target.closest('[data-fold]');
  if (fold) return toggleFold(fold.dataset.fold);
  const el = e.target.closest('[data-key]');
  const btn = e.target.closest('[data-act]');
  if (!el || !btn) return;
  const r = shown().find(x => keyOf(x) === el.dataset.key);
  if (!r) return;
  const act = btn.dataset.act;
  if (act === 'row') return togglePreview(r);
  if (act === 'resume') return openChat(r);
  if (act === 'copy') return copyID(r);
  if (act === 'remove') return removeChat(r, btn);
  if (act === 'restore') return setRemoved(r, false);
}

// onKey is the list's keyboard: → opens, ← closes, on a row its preview and
// on a group header the group. Enter and Space are the buttons' own.
function onKey(e) {
  if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft' && e.key !== 'Enter' && e.key !== ' ') return;
  const fold = e.target.closest?.('[data-fold]');
  if (fold) {
    e.preventDefault();
    const want = e.key === 'ArrowLeft' ? false : e.key === 'ArrowRight' ? true : undefined;
    return toggleFold(fold.dataset.fold, want);
  }
  if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return; // the row button takes Enter and Space
  const el = e.target.closest?.('[data-key]');
  if (!el || !e.target.closest('[data-act="row"]')) return;
  e.preventDefault();
  const r = shown().find(x => keyOf(x) === el.dataset.key);
  if (!r) return;
  const want = e.key === 'ArrowRight';
  if (want !== (keyOf(r) === openKey)) togglePreview(r);
}

/* ---------- the preview ---------- */

// togglePreview opens one preview at a time, read once as it opens: a poll
// re-renders the row but never asks again. Closing it drops what was read,
// so a chat opened again is read again -- the conversation may have gone on
// in the meantime.
function togglePreview(r) {
  const key = keyOf(r);
  if (openKey === key) return closePreview();
  openKey = key;
  preview = { key, loading: true };
  loadPreview(r);
  render();
  rowEl(key)?.querySelector('[data-act="row"]')?.focus();
}

async function loadPreview(r) {
  const key = keyOf(r);
  const res = await call('GET', `api/history/${encodeURIComponent(r.agent)}/${encodeURIComponent(r.id)}/preview`);
  if (preview?.key !== key) return; // another row was opened meanwhile
  preview = res.ok
    ? { key, data: res.data }
    : { key, error: res.data?.error || 'Could not read this conversation' };
  if (openKey === key) render();
}

function closePreview() {
  closePreviewState();
  render();
}

function closePreviewState() {
  openKey = null;
  preview = null;
}

async function copyID(r) {
  try {
    await navigator.clipboard.writeText(r.id);
    showToast('✓', 'Chat id copied', 2000);
  } catch {
    showToast('!', 'The browser would not let punk0 write to the clipboard', 4000);
  }
}

/* ---------- opening, removing, deleting ---------- */

// openChat resumes the chat in a tab. A folder punk0 does not work in cannot
// hold a tab; the server offers the one change that would give the chat a
// workspace in that very folder, and the page asks before making it. A
// folder that cannot become one -- inside a project, inside a repository,
// your home folder -- comes back as a plain error, so nothing loops.
async function openChat(r) {
  if (r.missing) return showToast('!', 'The folder this chat ran in is gone', 3000);
  let res = await call('POST', 'api/history/open', { agent: r.agent, id: r.id });
  if (res.status === 409 && (res.data?.adopt || res.data?.addProject)) {
    const dir = res.data.adopt || res.data.addProject;
    const what = res.data.adopt
      ? `This chat ran in ${tildify(dir)}, a worktree punk0 does not know. Add it as a workspace?`
      : `This chat ran in ${tildify(dir)}, which is not a punk0 project. Add it?`;
    if (!confirm(what)) return;
    const added = res.data.adopt
      ? await call('POST', `api/projects/${encodeURIComponent(res.data.project)}/workspaces`, { name: dir, worktree: true })
      : await call('POST', 'api/projects', { dir });
    if (!added.ok) return openFailed(added);
    await reload();
    res = await call('POST', 'api/history/open', { agent: r.agent, id: r.id });
  }
  if (!res.ok) return openFailed(res);
  await reload();
  selectWorkspace(res.data.workspace);
  selectTab(res.data.tab);
  refresh();
}

// openFailed shows why a chat would not open in full: these errors name a
// folder and say what to do, so the toast wraps them instead of cutting.
function openFailed(res) {
  failed(res);
  document.querySelector('#toast .toast-msg')?.classList.add('o-hist-err');
}

// removeChat is the one popup for both ways of letting a chat go: with the
// checkbox clear it only stops being listed, and Sync brings it back; ticked,
// the agent's own copy goes for good and the tabs on it close first.
async function removeChat(r, from) {
  const perm = (DELETES[r.agent] || (id => `${AGENT[r.agent] || r.agent} deletes ${id} for good. This cannot be undone.`))(r.id);
  const answers = await openRemoveDialog({
    heading: 'Remove chat',
    go: { off: 'Remove', on: 'Delete' },
    what: `Remove <b>${esc(r.title)}</b> from punk0? It stays on disk; Sync brings it back.`,
    options: [{
      id: 'perm',
      label: 'Delete permanently',
      note: perm,
      // What the tick itself does, so it is said only while it is ticked.
      noteOn: r.open ? 'Its tab closes and the agent in it stops.' : '',
      checked: false,
    }],
    confirm: '',
    danger: false,
    beside: true,
  }, from);
  if (!answers) return;
  if (!answers.perm) return setRemoved(r, true);
  const res = await call('DELETE', `api/history/${encodeURIComponent(r.agent)}/${encodeURIComponent(r.id)}`);
  if (!res.ok) return failed(res);
  showToast('✓', 'Chat deleted', 3000);
  closePreviewState();
  await reload(); // its tabs were closed on the server
  refresh();
}

async function setRemoved(r, removed) {
  const res = await call('POST', 'api/history/remove', { agent: r.agent, id: r.id, removed });
  if (!res.ok) return failed(res);
  if (removed) closePreviewState();
  refresh();
}
