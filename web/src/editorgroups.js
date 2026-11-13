// web/src/editorgroups.js
// The editor's two tab groups as plain data. Each group -- the center
// (#tabs, where documents are edited) and the right side -- is an ordered
// list of entries {kind, id}: a document ('doc', id = its path, the tab in
// S.tabs), a terminal ('term', id = the agents page's tab id) or a
// read-only file in the right group ('view', id = its path). Each group
// also names its active entry, '' while it shows none, and one group has
// the focus. Pure: tabs.js keeps the model in S.groups, renders both bars
// from it and persists its terminal part (punk0.editorGroups).
//
// model = { center: [entry], right: [entry],
//           active: { center: id, right: id }, focus: 'center' | 'right' }
export const CENTER = 'center';
export const RIGHT = 'right';
export const GROUPS = [CENTER, RIGHT];

export const emptyModel = () => ({ center: [], right: [], active: { center: '', right: '' }, focus: CENTER });

const clone = m => ({
  center: m.center.map(e => ({ ...e })),
  right: m.right.map(e => ({ ...e })),
  active: { ...m.active },
  focus: m.focus === RIGHT ? RIGHT : CENTER,
});

export const entries = (m, g) => m[g];
export const groupOf = (m, id) =>
  (m.center.some(e => e.id === id) ? CENTER : m.right.some(e => e.id === id) ? RIGHT : '');
export const entryOf = (m, id) => m.center.find(e => e.id === id) || m.right.find(e => e.id === id) || null;
export const activeEntry = (m, g) => m[g].find(e => e.id === m.active[g]) || null;
export const other = g => (g === RIGHT ? CENTER : RIGHT);

// neighbour is the entry that takes id's place in list: the first one that
// may be shown after it, else before it. A stopped terminal is passed over
// where the caller says so (showing it would start it).
function neighbour(list, id, showable) {
  const i = list.findIndex(e => e.id === id);
  if (i < 0) return '';
  for (let j = i + 1; j < list.length; j++) if (showable(list[j])) return list[j].id;
  for (let j = i - 1; j >= 0; j--) if (showable(list[j])) return list[j].id;
  return '';
}

function removeFrom(m, id, showable) {
  const g = groupOf(m, id);
  if (!g) return;
  if (m.active[g] === id) m.active[g] = neighbour(m[g], id, showable);
  m[g] = m[g].filter(e => e.id !== id);
  if (g === RIGHT && !m.right.length) m.focus = CENTER;
}

// place puts a new entry in group g, ahead of before ('' = last). An id
// already held moves: one tab is never in two groups.
export function place(model, entry, g, before = '') {
  const m = clone(model);
  removeFrom(m, entry.id, () => true);
  const list = m[g];
  const at = before ? list.findIndex(e => e.id === before) : -1;
  list.splice(at < 0 ? list.length : at, 0, { ...entry });
  return m;
}

// setActive says which entry group g shows, '' for none, leaving the focus
// where it is: what a poll does when a group falls back to another tab.
export function setActive(model, g, id) {
  const m = clone(model);
  m.active[g] = id;
  return m;
}

// activate makes id its group's active entry and focuses that group.
export function activate(model, id) {
  const g = groupOf(model, id);
  if (!g) return model;
  const m = setActive(model, g, id);
  m.focus = g;
  return m;
}

// move puts id in group to, ahead of before ('' = last), active there, that
// group focused. showable says which entry may take its place in the group
// it leaves.
export function move(model, id, to, before = '', { showable = () => true } = {}) {
  const e = entryOf(model, id);
  if (!e || id === before) return model;
  const m = clone(model);
  removeFrom(m, id, showable);
  const list = m[to];
  const at = before ? list.findIndex(x => x.id === before) : -1;
  list.splice(at < 0 ? list.length : at, 0, { ...e });
  m.active[to] = id;
  m.focus = to;
  return m;
}

// remove drops the entries, each group's active falling to its neighbour.
export function remove(model, ids, { showable = () => true } = {}) {
  const m = clone(model);
  for (const id of ids) removeFrom(m, id, showable);
  return m;
}

// next is the entry after id in group g, dir -1 for the one before,
// wrapping: Ctrl+Tab and gt step through a bar, terminals included.
export function next(model, g, id, dir) {
  const list = model[g];
  if (!list.length) return '';
  const i = list.findIndex(e => e.id === id);
  if (i < 0) return list[0].id;
  return list[((i + dir) % list.length + list.length) % list.length].id;
}

// closeTargets are the documents a Close action closes, in center order.
// Terminals and read-only files are left alone: closing a terminal ends its
// shell, which a bulk file action must not do.
export function closeTargets(model, action, id) {
  const list = model.center;
  const docs = keep => list.filter((e, i) => e.kind === 'doc' && keep(e, i)).map(e => e.id);
  if (action === 'all') return docs(() => true);
  const i = list.findIndex(e => e.id === id);
  if (i < 0) return [];
  if (action === 'close') return list[i].kind === 'doc' ? [id] : [];
  if (action === 'others') return docs(e => e.id !== id);
  if (action === 'right') return docs((_, k) => k > i);
  if (action === 'left') return docs((_, k) => k < i);
  return [];
}

// canonicalTerms is the terminal order the server should hold: the center's
// terminals in bar order, then the right side's.
export const canonicalTerms = m => [...m.center, ...m.right].filter(e => e.kind === 'term').map(e => e.id);

/* The editor's bar can list the tabs of several workspaces sharing this
   folder, and the server orders each workspace's tabs on its own. wsOf(id)
   gives a terminal's workspace. */

// beforeFor is the /move before for terminal id: the next terminal of its
// own workspace in canonical order, '' when it is the last of them.
export function beforeFor(model, id, wsOf = () => '') {
  const ws = wsOf(id);
  const seq = canonicalTerms(model).filter(t => wsOf(t) === ws);
  const i = seq.indexOf(id);
  return i < 0 ? '' : seq[i + 1] || '';
}

// canReorder says whether terminal id may land ahead of before in group g:
// only next to its own workspace's terminals, so a drop never asks the
// server to order one workspace's tab against another's. A bar with no
// other terminal takes it anywhere.
export function canReorder(model, id, g, before, wsOf = () => '') {
  const ws = wsOf(id);
  const list = model[g].filter(e => e.id !== id);
  const at = before ? list.findIndex(e => e.id === before) : -1;
  const i = at < 0 ? list.length : at;
  const prev = list.slice(0, i).reverse().find(e => e.kind === 'term');
  const after = list.slice(i).find(e => e.kind === 'term');
  const same = e => wsOf(e.id) === ws;
  return (!prev && !after) || (!!prev && same(prev)) || (!!after && same(after));
}

// seed is the model a page load starts from, before any tab is known: the
// center holds the terminals it held when the page was left (punk0.editorGroups),
// the rest arriving with the first poll. Which of them the center shows
// again is reconcile's and the page's business: attaching starts a stopped
// tab, so a reload never shows one that is not running. Documents are not
// stored, as they never were.
export function seed(stored) {
  const m = emptyModel();
  const ids = Array.isArray(stored?.center) ? stored.center.filter(id => typeof id === 'string') : [];
  m.center = ids.map(id => ({ kind: 'term', id }));
  m.focus = stored?.focus === RIGHT ? RIGHT : CENTER;
  return m;
}

// reconcile brings the model up to date with the terminals the poll found,
// in the server's order: gone ones drop out (each group falling back to a
// tab it may show), each group's terminal slots take its own terminals
// back in server order (documents and read-only files never move), and
// terminals opened elsewhere land on the right.
export function reconcile(model, terms, { showable = () => true } = {}) {
  const live = new Set(terms);
  const gone = [...model.center, ...model.right].filter(e => e.kind === 'term' && !live.has(e.id)).map(e => e.id);
  const m = remove(model, gone, { showable });
  const order = new Map(terms.map((id, i) => [id, i]));
  for (const g of GROUPS) {
    const list = m[g];
    const slots = [];
    list.forEach((e, i) => { if (e.kind === 'term') slots.push(i); });
    const sorted = slots.map(i => list[i]).sort((a, b) => order.get(a.id) - order.get(b.id));
    slots.forEach((i, k) => { list[i] = sorted[k]; });
  }
  const placed = new Set([...m.center, ...m.right].map(e => e.id));
  for (const id of terms) if (!placed.has(id)) m.right.push({ kind: 'term', id });
  return m;
}

// persisted is what sessionStorage keeps (punk0.editorGroups): which
// terminals the center holds, in its order, the one it shows, and the
// focused group.
export const persisted = m => ({
  center: m.center.filter(e => e.kind === 'term').map(e => e.id),
  active: activeEntry(m, CENTER)?.kind === 'term' ? m.active.center : '',
  focus: m.focus,
});
