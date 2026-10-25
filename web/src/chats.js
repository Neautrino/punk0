// web/src/chats.js
// The right side's Chats mode: the chats Claude Code, Codex and OpenCode
// kept for this editor's folder, read from the orchestrator that opened it
// (GET api/history, scope workspace, by the folder's backend id). Your
// groups come first, each folding on its header, the most recently used
// first; the chats in none follow under Today / Yesterday / Older. A row
// expands into its preview (chatlist.js, shared with the agents page);
// Resume opens the chat in a terminal tab (POST api/history/open, which
// reuses a tab already on it) and shows it. Right-click (or the context-menu
// key) a chat to put it in a group, a new one or none; a group's header to
// rename or delete it (POST api/history/group: a group is the name kept on
// its chats, so it goes with its last chat). Removing or deleting chats,
// and the chats of other folders, are the agents page's Chats mode. The list
// is read again every 10 s while it shows: the agents write these files from
// outside punk0.
import { $, S, esc } from './state.js';
import { on } from './bus.js';
import { showToast } from './ui.js';
import { sideShown, rightView } from './sideviews.js';
import { parentURL, openTab } from './term_inspector.js';
import { chatKey, chatRow, agentName, ICON_CHEV } from './chatlist.js';

const FOLD_KEY = 'punk0.chatsFolded';

let listEl, qEl, menuEl;
let rows = [];
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
      note = res.ok ? '' : res.data?.error || 'Could not read the chat history';
    } catch {
      rows = [];
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

// folded holds the sections folded by hand, by key ('g:' and a group's
// name, 'a:' and an agent). A search shows every section with matches open.
const folded = new Set((() => {
  try { return JSON.parse(localStorage.getItem(FOLD_KEY) || '[]'); } catch { return []; }
})());

function toggleFold(key) {
  if (!folded.delete(key)) folded.add(key);
  try { localStorage.setItem(FOLD_KEY, JSON.stringify([...folded])); } catch {}
  render();
}

// groupNames is this folder's groups, the one used last first (the rows come
// newest first).
const groupNames = () => [...new Set(rows.map(r => r.group).filter(Boolean))];

/* ---------- group by ---------- */

const GROUP_BY_KEY = 'punk0.chatsGroupBy';
// How the list is cut into sections: 'groups' your groups, then the rest by
// day; 'date' by day only; 'agent' by agent; 'none' one list, newest first.
const GROUP_BY = { groups: 'Your groups', date: 'Date', agent: 'Agent', none: 'None' };
let groupBy = GROUP_BY[localStorage.getItem(GROUP_BY_KEY)] ? localStorage.getItem(GROUP_BY_KEY) : 'groups';

function setGroupBy(by) {
  groupBy = by;
  try { localStorage.setItem(GROUP_BY_KEY, by); } catch {}
  render();
}

// sections cuts list into {key?, group?, name, chats}: key makes it fold,
// group makes it one of yours (renamed and deleted on its header).
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
  return [
    ...[...new Set(list.map(r => r.group).filter(Boolean))].map(g => ({ key: 'g:' + g, group: g, name: g, chats: list.filter(r => r.group === g) })),
    ...byDay(list.filter(r => !r.group)),
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
  if (!list.length) {
    const why = note || (query ? 'No chat matches' : rows.length ? 'No chat matches the filter' : 'No chats in this folder yet');
    listEl.innerHTML = `<p class="chat-none">${esc(why)}</p>`;
    return;
  }
  let html = '';
  for (const s of sections(list)) {
    const open = !s.key || !!query || !folded.has(s.key);
    if (s.key) {
      html += `<div class="chat-group" data-fold="${esc(s.key)}"${s.group ? ` data-group="${esc(s.group)}" title="Right-click to rename or delete"` : ''} role="button" tabindex="0" aria-expanded="${open}">` +
        `${ICON_CHEV}<span class="chat-gname">${esc(s.name)}</span><span class="chat-gcount">${s.chats.length}</span></div>`;
    } else if (s.name) {
      html += `<div class="chat-group"><span class="chat-gname">${s.name}</span></div>`;
    }
    if (open) html += s.chats.map(rowHTML).join('');
  }
  listEl.innerHTML = html;
}

function onClick(e) {
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
  if (fold && (e.key === 'Enter' || e.key === ' ')) {
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

// openMenuFor opens the menu of the chat or group header el is in, at x, y,
// and says whether there was one.
function openMenuFor(el, x, y) {
  const header = el.closest('.chat-group');
  if (header) {
    const name = header.dataset.group;
    if (!name) return false; // a day's or an agent's: nothing to rename
    const at = `.chat-group[data-group="${CSS.escape(name)}"]`;
    showMenu(x, y, [
      { label: 'Rename group…', run: () => askName('Rename group', name, at, to => to !== name && setGroup(rows.filter(r => r.group === name), to)) },
      { label: 'Delete group', note: 'its chats stay', run: () => setGroup(rows.filter(r => r.group === name), '') },
    ]);
    return true;
  }
  const row = el.closest('[data-key]');
  const r = row && rows.find(x => chatKey(x) === row.dataset.key);
  if (!r) return false;
  const at = `[data-key="${CSS.escape(chatKey(r))}"] .chat-line`;
  // Every group of this folder, the chat's own ticked: picking another moves
  // the chat there.
  showMenu(x, y, [
    { label: r.open ? 'Show' : 'Resume', run: () => resume(r) },
    { sep: true },
    { head: 'Add to group' },
    ...groupNames().map(n => ({ label: n, checked: r.group === n, run: () => r.group !== n && setGroup([r], n) })),
    { label: 'New group…', run: () => askName('New group', '', at, n => setGroup([r], n)) },
    ...(r.group ? [{ label: `Remove from “${r.group}”`, run: () => setGroup([r], '') }] : []),
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

// askName turns the menu into a name box under the row or header it names
// (anchor, a selector: the list may have been drawn again meanwhile), as
// wide as it: Enter takes the name, Esc drops it.
function askName(heading, value, anchor, done) {
  const label = document.createElement('div');
  label.className = 'chat-menu-head';
  label.textContent = heading;
  const input = document.createElement('input');
  input.className = 'chat-menu-input';
  input.type = 'text';
  input.maxLength = 60;
  input.value = value;
  input.placeholder = 'Group name';
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

// setGroup puts chats in group name, or out of theirs with ''.
async function setGroup(chats, name) {
  if (!chats.length) return;
  let res;
  try {
    res = await getJSON('api/history/group', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ chats: chats.map(r => ({ agent: r.agent, id: r.id })), group: name }),
    });
  } catch {
    res = { ok: false, data: { error: 'Could not reach punk0' } };
  }
  if (!res.ok) return showToast('!', res.data?.error || 'Could not change the group', 4000);
  if (name) folded.delete('g:' + name);
  refreshChats();
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
// and shows it in the terminal.
async function resume(r) {
  let res;
  try {
    res = await getJSON('api/history/open', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ agent: r.agent, id: r.id }),
    });
  } catch {
    res = { ok: false, data: { error: 'Could not reach punk0' } };
  }
  if (!res.ok || !res.data?.tab) return showToast('!', res.data?.error || 'Could not resume this chat', 5000);
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
