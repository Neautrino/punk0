// web/src/orch/groups.js
// The agents page's two tab groups, as plain data: which tabs each holds in
// what order, its active tab ('' while this page opened none of them), the
// focused group, and the group each kind of tab was last shown in. Pure:
// store.js persists it (punk0.groups) and renders from it.
//
// state = { groups: [{ ids, active }, { ids, active }?], focus: 0 | 1,
//           last: { term?: 0 | 1, file?: 0 | 1 } }
export const TERM = 'term';
export const FILE = 'file';

// idKind: the file group's own tabs (Changes, What's New, files) are
// 'file'; the server's shell and agent tabs 'term'.
export const idKind = id => (id === 'changes' || id === 'whatsnew' || id.startsWith('file:') ? FILE : TERM);

const fresh = () => ({ groups: [{ ids: [], active: '' }], focus: 0, last: {} });

function normalize(s) {
  if (!s || !Array.isArray(s.groups) || !s.groups.length) return fresh();
  const groups = s.groups.slice(0, 2).map(g => ({
    ids: Array.isArray(g?.ids) ? g.ids.filter(x => typeof x === 'string') : [],
    active: typeof g?.active === 'string' ? g.active : '',
  }));
  const focus = s.focus === 1 && groups[1] ? 1 : 0;
  const last = {};
  for (const k of [TERM, FILE]) if (s.last?.[k] === 0 || s.last?.[k] === 1) last[k] = s.last[k];
  return { groups, focus, last };
}

// placeFor is where a tab of kind opens when nothing chose a group: where
// that kind was last shown, else terminals left and files right (beside) or
// left (tabs).
function placeFor(s, kind, beside) {
  const l = s.last[kind];
  if (l === 0 || (l === 1 && s.groups[1])) return l;
  return kind === TERM || !beside ? 0 : 1;
}

const group = (s, g) => (s.groups[g] ||= { ids: [], active: '' });

// neighbour is the tab next to id in ids that may be shown: before it, else
// after; a stopped terminal is passed over, showing it would start it.
function neighbour(ids, id, ok) {
  const i = ids.indexOf(id);
  for (let d = 1; d < ids.length; d++) {
    if (i - d >= 0 && ok(ids[i - d])) return ids[i - d];
    if (i + d < ids.length && ok(ids[i + d])) return ids[i + d];
  }
  return '';
}

function closeEmpty(s) {
  if (s.groups[1] && !s.groups[1].ids.length) {
    s.groups.length = 1;
    s.focus = 0;
    for (const k of [TERM, FILE]) if (s.last[k] === 1) delete s.last[k];
  }
}

// reconcile brings a stored state up to date with what exists. live: terms
// (server tab ids in server order), files (file group ids open in this
// page), running (Set of running terminal ids), lastTab (the server's last
// used tab, given only when this page should adopt it). opts.beside:
// layout.fileView is beside.
export function reconcile(stored, live, { beside }) {
  const s = normalize(stored);
  const alive = new Set([...live.terms, ...live.files]);
  const showable = id => alive.has(id) && (idKind(id) === FILE || live.running.has(id));
  for (const g of s.groups) {
    if (g.active && !alive.has(g.active)) g.active = neighbour(g.ids, g.active, showable);
    g.ids = g.ids.filter(id => alive.has(id));
  }
  const order = new Map(live.terms.map((id, i) => [id, i]));
  for (const g of s.groups) {
    const slots = [];
    g.ids.forEach((id, i) => idKind(id) === TERM && slots.push(i));
    const terms = slots.map(i => g.ids[i]).sort((a, b) => order.get(a) - order.get(b));
    slots.forEach((i, k) => { g.ids[i] = terms[k]; });
  }
  const placed = new Set(s.groups.flatMap(g => g.ids));
  for (const id of [...live.terms, ...live.files]) {
    if (placed.has(id)) continue;
    group(s, placeFor(s, idKind(id), beside)).ids.push(id);
    placed.add(id);
  }
  if (live.lastTab && alive.has(live.lastTab)) {
    const g = s.groups.findIndex(x => x.ids.includes(live.lastTab));
    if (g >= 0 && !s.groups[g].active) {
      s.groups[g].active = live.lastTab;
      s.focus = g;
    }
  }
  // The second group has no welcome view: with nothing of its own in front
  // it would be an empty pane, so it shows the first tab it may show.
  if (s.groups[1] && !s.groups[1].active) s.groups[1].active = s.groups[1].ids.find(showable) || '';
  closeEmpty(s);
  return s;
}

// moveTab puts id in group to ahead of before ('' = last), active there,
// that group focused. The group it leaves falls back to the tab beside it
// that showable allows (a stopped terminal would start). Group 2 closes
// when its last tab leaves; group 1 stays (the welcome view).
export function moveTab(state, id, to, before, showable = () => true) {
  const s = normalize(structuredClone(state));
  const from = s.groups.findIndex(g => g.ids.includes(id));
  if (from < 0 || id === before) return s;
  const src = s.groups[from];
  if (src.active === id) src.active = neighbour(src.ids, id, x => x !== id && showable(x));
  src.ids = src.ids.filter(x => x !== id);
  const dst = group(s, to === 1 ? 1 : 0);
  const at = before ? dst.ids.indexOf(before) : -1;
  dst.ids.splice(at < 0 ? dst.ids.length : at, 0, id);
  dst.active = id;
  s.focus = to === 1 ? 1 : 0;
  s.last[idKind(id)] = s.focus;
  closeEmpty(s);
  if (s.groups.length === 1) s.last[idKind(id)] = 0;
  return s;
}

// canonicalTerms is the terminal order the server should hold: group 1's
// terminals in page order, then group 2's.
export const canonicalTerms = s => s.groups.flatMap(g => g.ids.filter(id => idKind(id) === TERM));

// movesToMatch is the sequence of /move calls that brings order (the
// server's tab order) to desired (canonicalTerms): walking desired from the
// end, a tab the wrong tab follows moves ahead of the one that should.
// Tabs the page does not know stay where they are.
export function movesToMatch(order, desired) {
  const want = new Set(desired);
  const seq = order.filter(id => want.has(id));
  const moves = [];
  for (let i = desired.length - 1; i >= 0; i--) {
    const id = desired[i], before = desired[i + 1] || '';
    const at = seq.indexOf(id);
    if (at < 0 || (seq[at + 1] || '') === before) continue;
    seq.splice(at, 1);
    const to = before ? seq.indexOf(before) : -1;
    seq.splice(to < 0 ? seq.length : to, 0, id);
    moves.push({ id, before });
  }
  return moves;
}
