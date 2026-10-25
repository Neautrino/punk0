// web/src/chats.js
// The right side's Chats mode: the chats Claude Code, Codex and OpenCode
// kept for this editor's folder, read from the orchestrator that opened it
// (GET api/history, scope workspace, by the folder's backend id). A
// workspace is the group its chats belong to: the workspaces at this folder
// come first, each folding on its header, with a + for a new chat in it and
// links to the worktrees made under it; then other workspaces this folder's
// chats belong to; the chats in none follow under Today / Yesterday / Older.
// A row expands into its preview (chatlist.js, shared with the agents page);
// Resume opens the chat in a terminal tab (POST api/history/open, which
// reuses a tab already on it) and shows it, or opens the editor of the
// workspace it answers with when that is at another folder. Right-click (or
// the context-menu key) a chat to move it to another workspace of this
// folder or a new one (POST api/history/workspace). Removing or deleting
// chats, and the chats of other folders, are the agents page's Chats mode.
// The list is read again every 10 s while it shows: the agents write these
// files from outside punk0.
import { $, S, esc } from './state.js';
import { on } from './bus.js';
import { showToast } from './ui.js';
import { sideShown, rightView } from './sideviews.js';
import { parentURL, openTab, agentChoices } from './term_inspector.js';
import { chatKey, chatRow, agentName, ICON_CHEV } from './chatlist.js';

const FOLD_KEY = 'punk0.chatsFolded';

let listEl, qEl, menuEl;
let rows = [];
// The workspaces the answer names ({id, name, kind, branch, parent, here,
// href}; here: at this folder) and the project they are in ({id, name}).
let workspaces = [];
let project = null;
let note = '';
let query = '';
let typing = 0;
let loading = null, pending = false;
// The one expanded row, by chatKey, and what was read for it: read once as
// it opens, never again by a poll.
let openKey = null;
let preview = null; // { key, loading } | { key, error } | { key, data }

const visible = () => sideShown('right') && rightView() === 'chats' && !document.hidden;

async function getJSON(path, init) {
  const res = await fetch(parentURL(path), init);
  let data = null;
  try { data = await res.json(); } catch {}
  return { ok: res.ok, data };
}

export function initChats() {
  listEl = $('#chats-list');
  qEl = $('#chats-q');
  qEl.addEventListener('input', () => {
    query = qEl.value.trim();
    clearTimeout(typing);
    typing = setTimeout(refreshChats, 150);
  });
  qEl.addEventListener('keydown', e => {
    if (e.key !== 'Escape' || !qEl.value) return;
    e.preventDefault();
    e.stopPropagation();
    qEl.value = query = '';
    refreshChats();
  });
  listEl.addEventListener('click', onClick);
  listEl.addEventListener('keydown', onKey);
  listEl.addEventListener('contextmenu', onContextMenu);
  menuEl = $('#chat-menu');
  const filterBtn = $('#chats-filter');
  document.addEventListener('mousedown', e => {
    if (!menuEl.contains(e.target) && !filterBtn.contains(e.target)) closeMenu();
  }, true);
  filterBtn.addEventListener('click', () => (menuOwner === 'filter' ? closeMenu() : openFilter()));
  drawFilter();
  // Esc closes the menu before the editor's own Esc handling sees the key.
  addEventListener('keydown', e => {
    if (e.key !== 'Escape' || menuEl.hidden) return;
    e.stopImmediatePropagation();
    closeMenu();
  }, true);
  for (const ev of ['resize', 'blur']) addEventListener(ev, closeMenu);
  listEl.addEventListener('scroll', closeMenu);
  on('rightview:changed', () => visible() && refreshChats());
  on('side:toggled', ({ side }) => side === 'right' && visible() && refreshChats());
  document.addEventListener('visibilitychange', () => visible() && refreshChats());
  setInterval(() => visible() && refreshChats(), 10000);
}

// startChats is called once the editor knows its orchestrator (/api/meta):
// an editor opened without one has no chats to list, so the mode goes.
export function startChats() {
  if (!S.meta?.workbench || !S.meta?.workspace) {
    $('#right-side .pane-tabs [data-view="chats"]').hidden = true;
    if (rightView() === 'chats') $('#right-side .pane-tabs [data-view="term"]').click();
    return;
  }
  if (visible()) refreshChats();
}

// refreshChats reloads the list; calls made while one is in flight share
// the next.
export function refreshChats() {
  if (!S.meta?.workspace) return;
  if (loading) {
    pending = true;
    return loading;
  }
  loading = (async () => {
    const p = new URLSearchParams({ scope: 'workspace', ws: S.meta.workspace });
    if (query) p.set('q', query);
    try {
      const res = await getJSON('api/history?' + p);
      rows = res.ok ? res.data?.sessions || [] : [];
      workspaces = res.ok ? res.data?.workspaces || [] : [];
      project = res.ok ? res.data?.project || null : null;
      note = res.ok ? '' : res.data?.error || 'Could not read the chat history';
    } catch {
      rows = workspaces = [];
      project = null;
      note = 'Could not reach punk0';
    }
    render();
  })().finally(() => {
    loading = null;
    if (pending) {
      pending = false;
      refreshChats();
    }
  });
  return loading;
}

// dayGroup names a chat's group by the day it was last touched.
function dayGroup(ms) {
  const start = new Date();
  start.setHours(0, 0, 0, 0);
  if (ms >= start.getTime()) return 'Today';
  if (ms >= start.getTime() - 86400000) return 'Yesterday';
  return 'Older';
}

// folded holds the sections folded by hand, by key ('w:' and a workspace's
// id, 'a:' and an agent). A search shows every section with matches open.
const folded = new Set((() => {
  try { return JSON.parse(localStorage.getItem(FOLD_KEY) || '[]'); } catch { return []; }
})());

function toggleFold(key) {
  if (!folded.delete(key)) folded.add(key);
  try { localStorage.setItem(FOLD_KEY, JSON.stringify([...folded])); } catch {}
  render();
}

const hereWorkspaces = () => workspaces.filter(w => w.here);

// descendants is every workspace made under workspace id, at any depth
// (worktrees agents made, then theirs), parents before children.
function descendants(id) {
  const out = [];
  const walk = pid => {
    for (const w of workspaces) {
      if (w.parent === pid && w.id !== id && !out.includes(w)) {
        out.push(w);
        walk(w.id);
      }
    }
  };
  walk(id);
  return out;
}

/* ---------- group by ---------- */

const GROUP_BY_KEY = 'punk0.chatsGroupBy';
// How the list is cut into sections: 'workspace' the workspace each chat
// belongs to, then the rest by day; 'date' by day only; 'agent' by agent;
// 'none' one list, newest first. (A stored 'groups', the name groups this
// replaced, is not a key here, so it falls back to workspace.)
const GROUP_BY = { workspace: 'Workspace', date: 'Date', agent: 'Agent', none: 'None' };
let groupBy = GROUP_BY[localStorage.getItem(GROUP_BY_KEY)] ? localStorage.getItem(GROUP_BY_KEY) : 'workspace';

function setGroupBy(by) {
  groupBy = by;
  try { localStorage.setItem(GROUP_BY_KEY, by); } catch {}
  render();
}

// sections cuts list into {key?, ws?, name, chats}: key makes it fold, ws
// makes it a workspace's (a + and its worktrees' links on the header).
function sections(list) {
  const byDay = chats => {
    const out = [];
    for (const r of chats) {
      const name = dayGroup(r.updated || 0);
      if (out.at(-1)?.name !== name) out.push({ name, chats: [] });
      out.at(-1).chats.push(r);
    }
    return out;
  };
  if (groupBy === 'none') return [{ name: '', chats: list }];
  if (groupBy === 'date') return byDay(list);
  if (groupBy === 'agent') {
    return [...new Set(list.map(r => r.agent))].map(a => ({ key: 'a:' + a, name: agentName(a), chats: list.filter(r => r.agent === a) }));
  }
  // This folder's workspaces show even with no chats, so their + is there.
  const known = new Map(workspaces.map(w => [w.id, w]));
  const here = hereWorkspaces();
  const others = [...new Set(list.map(r => r.workspace))].filter(id => known.has(id) && !known.get(id).here).map(id => known.get(id));
  return [
    ...[...here, ...others].map(w => ({ key: 'w:' + w.id, ws: w, name: w.name, chats: list.filter(r => r.workspace === w.id) })),
    ...byDay(list.filter(r => !known.has(r.workspace))),
  ];
}

/* ---------- the filter (the funnel beside the search) ---------- */

const FILTER_KEY = 'punk0.chatsFilter';
// agent: '' for every agent; open: only chats a tab has open; untitled:
// hide chats with no title (no prompt was ever sent).
const filter = { agent: '', open: false, untitled: false };
try { Object.assign(filter, JSON.parse(localStorage.getItem(FILTER_KEY) || '{}')); } catch {}

// filtersOn counts the filter's choices that narrow the list.
const filtersOn = () => [filter.agent, filter.open, filter.untitled].filter(Boolean).length;

// shown is the loaded chats the filter lets through.
const shown = () => rows.filter(r =>
  (!filter.agent || r.agent === filter.agent) && (!filter.open || r.open) && (!filter.untitled || r.title !== '(untitled)'));

function setFilter(change) {
  Object.assign(filter, change);
  try { localStorage.setItem(FILTER_KEY, JSON.stringify(filter)); } catch {}
  drawFilter();
  render();
}

function drawFilter() {
  const btn = $('#chats-filter'), n = filtersOn();
  btn.classList.toggle('on', n > 0);
  btn.querySelector('.chat-filter-n').textContent = n || '';
  btn.title = n ? `Group and filter: ${n} filter${n === 1 ? '' : 's'} on` : 'Group and filter';
}

// openFilter shows the list's choices under the funnel: how it is grouped,
// then what it shows. A choice applies at once and the menu stays, so
// several can be set in a row.
function openFilter() {
  const at = $('#chats-filter').getBoundingClientRect();
  const agents = [...new Set([...rows.map(r => r.agent), filter.agent].filter(Boolean))].sort();
  const again = change => () => { setFilter(change); openFilter(); };
  showMenu(at.right, at.bottom + 4, [
    { head: 'Group by' },
    ...Object.entries(GROUP_BY).map(([by, label]) => ({ label, checked: groupBy === by, run: () => { setGroupBy(by); openFilter(); } })),
    { sep: true },
    { head: 'Agent' },
    { label: 'All agents', checked: !filter.agent, run: again({ agent: '' }) },
    ...agents.map(a => ({ label: agentName(a), checked: filter.agent === a, run: again({ agent: a }) })),
    { sep: true },
    { head: 'Show' },
    { label: 'Only chats open in a tab', checked: filter.open, check: true, run: again({ open: !filter.open }) },
    { label: 'Hide untitled chats', checked: filter.untitled, check: true, run: again({ untitled: !filter.untitled }) },
  ], 'filter');
  $('#chats-filter').setAttribute('aria-expanded', 'true');
}

function rowHTML(r) {
  const key = chatKey(r);
  return chatRow(r, {
    shown: key === openKey,
    preview: preview?.key === key ? preview : null,
    prevActs: `
          <button type="button" class="chat-btn primary" data-act="resume">${r.open ? 'Show' : 'Resume'}</button>
          <button type="button" class="chat-btn" data-act="copy">Copy id</button>`,
  });
}

function render() {
  if (openKey && !rows.some(r => chatKey(r) === openKey)) openKey = preview = null;
  const list = shown();
  // Narrowed (a search, a filter), a workspace with no match is left out;
  // else this folder's workspaces show empty too, so their + is there.
  const narrowed = !!query || filtersOn() > 0;
  const secs = sections(list).filter(s => !narrowed || s.chats.length);
  if (!secs.some(s => s.chats.length || s.ws)) {
    const why = note || (query ? 'No chat matches' : rows.length ? 'No chat matches the filter' : 'No chats in this folder yet');
    listEl.innerHTML = `<p class="chat-none">${esc(why)}</p>`;
    return;
  }
  let html = '';
  for (const s of secs) {
    const open = !s.key || !!query || !folded.has(s.key);
    if (s.key) {
      const w = s.ws;
      const branch = w?.kind === 'worktree' && w.branch ? `<span class="chat-gbranch">⑂ ${esc(w.branch)}</span>` : '';
      const add = w ? `<button type="button" class="chat-gadd" data-act="ws-new" data-ws="${esc(w.id)}" title="New chat in ${esc(w.name)}" aria-label="New chat in ${esc(w.name)}" aria-haspopup="menu">+</button>` : '';
      html += `<div class="chat-group" data-fold="${esc(s.key)}" role="button" tabindex="0" aria-expanded="${open}">` +
        `${ICON_CHEV}<span class="chat-gname">${esc(s.name)}</span>${branch}<span class="chat-gcount">${s.chats.length}</span>${add}</div>`;
      if (open && w) {
        html += descendants(w.id).map(d =>
          `<button type="button" class="chat-wslink" data-wslink="${esc(d.id)}" title="Open the editor of ${esc(d.name)}">⑂ ${esc(d.name)}</button>`).join('');
      }
    } else if (s.name) {
      html += `<div class="chat-group"><span class="chat-gname">${esc(s.name)}</span></div>`;
    }
    if (open) html += s.chats.map(rowHTML).join('');
  }
  listEl.innerHTML = html;
}

// openEditorOf opens workspace w's editor in its own window, the one the
// orchestrator's sidebar opens it in too.
function openEditorOf(w) {
  if (w?.href) window.open(parentURL(w.href), 'punk0-w-' + w.id);
}

function onClick(e) {
  const add = e.target.closest('[data-act="ws-new"]');
  if (add) {
    // A new chat in that workspace: the terminal's agent choices, here.
    const at = add.getBoundingClientRect();
    return showMenu(at.right, at.bottom + 4, agentChoices(add.dataset.ws).map(c => ({ label: c.label, run: c.run })));
  }
  const link = e.target.closest('[data-wslink]');
  if (link) return openEditorOf(workspaces.find(w => w.id === link.dataset.wslink));
  const fold = e.target.closest('[data-fold]');
  if (fold) return toggleFold(fold.dataset.fold);
  const el = e.target.closest('[data-key]');
  const act = e.target.closest('[data-act]')?.dataset.act;
  const r = el && rows.find(x => chatKey(x) === el.dataset.key);
  if (!r || !act) return;
  if (act === 'row') return togglePreview(r);
  if (act === 'resume') return resume(r);
  if (act === 'copy') return copyID(r);
}

function onKey(e) {
  const fold = e.target.closest?.('[data-fold]');
  if (fold && e.target === fold && (e.key === 'Enter' || e.key === ' ')) {
    e.preventDefault();
    return toggleFold(fold.dataset.fold);
  }
  // The context-menu key and Shift+F10 open the menu the right click does.
  if (e.key === 'ContextMenu' || (e.shiftKey && e.key === 'F10')) {
    const at = e.target.getBoundingClientRect?.();
    if (!at || !openMenuFor(e.target, at.left + 12, at.bottom)) return;
    e.preventDefault();
  }
}

/* ---------- the right-click menu ---------- */

function onContextMenu(e) {
  if (openMenuFor(e.target, e.clientX, e.clientY)) e.preventDefault();
}

// openMenuFor opens the menu of the chat el is in, at x, y, and says
// whether there was one.
function openMenuFor(el, x, y) {
  const row = el.closest('[data-key]');
  const r = row && rows.find(x => chatKey(x) === row.dataset.key);
  if (!r) return false;
  const at = `[data-key="${CSS.escape(chatKey(r))}"] .chat-line`;
  // Every workspace at this folder, the chat's own ticked: picking another
  // moves the chat there.
  showMenu(x, y, [
    { label: r.open ? 'Show' : 'Resume', run: () => resume(r) },
    { sep: true },
    { head: 'Move to workspace' },
    ...hereWorkspaces().map(w => ({ label: w.name, checked: r.workspace === w.id, run: () => r.workspace !== w.id && moveChat(r, w.id) })),
    ...(project ? [{ label: 'New workspace…', run: () => askName('New workspace', '', 'Workspace name', at, n => newWorkspaceFor(r, n)) }] : []),
    { sep: true },
    { label: 'Copy id', run: () => copyID(r) },
  ]);
  return true;
}

// showMenu fills the menu with items -- {label, run, note?, checked?, check?}
// a choice ({checked} a radio, with {check} a checkbox), {head} a heading,
// {sep} a line -- and opens it at x, y. owner names what opened it, so its
// button can close it again.
let menuOwner = '';
function showMenu(x, y, items, owner = '') {
  menuOwner = owner;
  menuEl.replaceChildren(...items.map(it => {
    if (it.sep || it.head) {
      const el = document.createElement('div');
      el.className = it.sep ? 'chat-menu-sep' : 'chat-menu-head';
      el.textContent = it.head || '';
      return el;
    }
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'sel-menu-item';
    const mark = 'checked' in it;
    b.setAttribute('role', !mark ? 'menuitem' : it.check ? 'menuitemcheckbox' : 'menuitemradio');
    if (mark) b.setAttribute('aria-checked', String(!!it.checked));
    b.innerHTML = `<span>${mark ? `<span class="chat-menu-check">${it.checked ? '✓' : ''}</span>` : ''}${esc(it.label)}</span>` +
      (it.note ? `<span class="chat-menu-note">${esc(it.note)}</span>` : '');
    b.addEventListener('click', () => { closeMenu(); it.run(); });
    return b;
  }));
  menuEl.style.width = '';
  place(x, y);
  menuEl.querySelector('button')?.focus();
}

// askName turns the menu into a name box under the row it names (anchor, a
// selector: the list may have been drawn again meanwhile), as wide as it:
// Enter takes the name, Esc drops it.
function askName(heading, value, placeholder, anchor, done) {
  const label = document.createElement('div');
  label.className = 'chat-menu-head';
  label.textContent = heading;
  const input = document.createElement('input');
  input.className = 'chat-menu-input';
  input.type = 'text';
  input.maxLength = 60;
  input.value = value;
  input.placeholder = placeholder;
  input.spellcheck = false;
  input.setAttribute('aria-label', heading);
  input.addEventListener('keydown', e => {
    e.stopPropagation();
    if (e.key === 'Escape') return closeMenu();
    if (e.key !== 'Enter') return;
    const name = input.value.trim();
    if (!name) return;
    closeMenu();
    done(name);
  });
  menuEl.replaceChildren(label, input);
  const at = (listEl.querySelector(anchor) || listEl).getBoundingClientRect();
  menuEl.style.width = Math.max(190, at.width) + 'px';
  place(at.left, at.bottom + 2);
  input.focus();
  input.select();
}

function place(x, y) {
  menuEl.hidden = false;
  const w = menuEl.offsetWidth, h = menuEl.offsetHeight;
  menuEl.style.left = Math.max(4, x + w > innerWidth - 4 ? x - w : x) + 'px';
  menuEl.style.top = Math.max(4, y + h > innerHeight - 4 ? y - h : y) + 'px';
}

function closeMenu() {
  if (menuEl.hidden) return;
  menuEl.hidden = true;
  menuEl.replaceChildren();
  menuOwner = '';
  $('#chats-filter').setAttribute('aria-expanded', 'false');
}

// post sends body to path as JSON; an unreachable punk0 answers like a
// refusal.
async function post(path, body) {
  try {
    return await getJSON(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
  } catch {
    return { ok: false, data: { error: 'Could not reach punk0' } };
  }
}

// moveChat makes chat r belong to workspace wsId, and opens its section.
async function moveChat(r, wsId) {
  const res = await post('api/history/workspace', { chats: [{ agent: r.agent, id: r.id }], workspace: wsId });
  if (!res.ok) return showToast('!', res.data?.error || 'Could not move the chat', 4000);
  folded.delete('w:' + wsId);
  refreshChats();
}

// newWorkspaceFor makes a local workspace named name in this project (it
// shares this folder) and moves chat r into it.
async function newWorkspaceFor(r, name) {
  const res = await post(`api/projects/${encodeURIComponent(project.id)}/workspaces`, { name, worktree: false });
  if (!res.ok || !res.data?.id) return showToast('!', res.data?.error || 'Could not make the workspace', 4000);
  moveChat(r, res.data.id);
}

// togglePreview opens one preview at a time; closing it drops what was read,
// so a chat opened again is read again -- it may have gone on meanwhile.
async function togglePreview(r) {
  const key = chatKey(r);
  if (openKey === key) {
    openKey = preview = null;
    return render();
  }
  openKey = key;
  preview = { key, loading: true };
  render();
  listEl.querySelector(`[data-key="${CSS.escape(key)}"] [data-act="row"]`)?.focus();
  let res;
  try {
    res = await getJSON(`api/history/${encodeURIComponent(r.agent)}/${encodeURIComponent(r.id)}/preview`);
  } catch {
    res = { ok: false, data: null };
  }
  if (preview?.key !== key) return; // another row was opened meanwhile
  preview = res.ok ? { key, data: res.data } : { key, error: res.data?.error || 'Could not read this conversation' };
  render();
}

// resume opens the chat in a tab of this folder (or the tab already on it)
// and shows it in the terminal. When its workspace is at another folder,
// the tab is that folder's editor's to show, so that editor opens instead.
async function resume(r) {
  const res = await post('api/history/open', { agent: r.agent, id: r.id });
  if (!res.ok || !res.data?.tab) return showToast('!', res.data?.error || 'Could not resume this chat', 5000);
  const w = workspaces.find(x => x.id === res.data.workspace);
  if (w && !w.here && w.href) return openEditorOf(w);
  openTab(res.data.tab);
}

async function copyID(r) {
  try {
    await navigator.clipboard.writeText(r.id);
    showToast('✓', 'Chat id copied', 2000);
  } catch {
    showToast('!', 'The browser would not let punk0 write to the clipboard', 4000);
  }
}
