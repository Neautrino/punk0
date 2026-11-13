import { test } from 'node:test';
import assert from 'node:assert';
import { reconcile, moveTab, canonicalTerms, movesToMatch, idKind } from './groups.js';

const live = (terms, files = [], running = terms) => ({ terms, files, running: new Set(running) });
const ids = s => s.groups.map(g => g.ids);

test('a first render puts terminals left and files right (beside), or all left (tabs)', () => {
  const s = reconcile(null, live(['a', 'b'], ['file:x', 'changes']), { beside: true });
  assert.deepEqual(ids(s), [['a', 'b'], ['file:x', 'changes']]);
  // The first group has the welcome view while this page opened none of
  // its tabs; the second would be an empty pane, so it shows its first.
  assert.deepEqual(s.groups.map(g => g.active), ['', 'file:x']);
  assert.deepEqual(ids(reconcile(null, live(['a'], ['file:x']), { beside: false })), [['a', 'file:x']]);
});

test('the second group never stands empty, and never starts a stopped tab', () => {
  const s = reconcile({ groups: [{ ids: [], active: '' }, { ids: ['b', 'file:x'], active: '' }], focus: 0, last: {} },
    live(['b'], ['file:x'], []), { beside: true });
  assert.equal(s.groups[1].active, 'file:x', 'b is stopped: passed over');
  assert.equal(reconcile({ groups: [{ ids: ['a'], active: 'a' }, { ids: ['b'], active: '' }], focus: 0, last: {} },
    live(['a', 'b'], [], []), { beside: true }).groups[1].active, '', 'nothing of its own may be shown');
});

test('terminal slots follow the server order; files keep their places', () => {
  const stored = { groups: [{ ids: ['a', 'file:x', 'b'], active: 'b' }, { ids: ['c'], active: 'c' }], focus: 0, last: { term: 0, file: 0 } };
  const s = reconcile(stored, live(['c', 'b', 'a'], ['file:x']), { beside: true });
  assert.deepEqual(ids(s), [['b', 'file:x', 'a'], ['c']]);
});

test('gone tabs leave; an active one gone gives way to a running neighbour', () => {
  const stored = { groups: [{ ids: ['a', 'b', 'c'], active: 'b' }], focus: 0, last: {} };
  const s = reconcile(stored, live(['a', 'c'], [], ['c']), { beside: true });
  assert.deepEqual(ids(s), [['a', 'c']]);
  assert.equal(s.groups[0].active, 'c', 'a is stopped: skipped');
});

test('new tabs go where their kind was last shown, else the defaults; group 2 closes when empty', () => {
  const stored = { groups: [{ ids: ['a'], active: 'a' }, { ids: ['file:x'], active: 'file:x' }], focus: 1, last: { term: 1, file: 1 } };
  let s = reconcile(stored, live(['a', 'n'], ['file:x', 'file:y']), { beside: true });
  assert.deepEqual(ids(s), [['a'], ['file:x', 'n', 'file:y']]);
  s = reconcile(s, live(['a'], []), { beside: true });
  assert.deepEqual(ids(s), [['a']]);
  assert.equal(s.focus, 0);
});

test('group 1 stays, empty, for the welcome view', () => {
  const stored = { groups: [{ ids: ['a'], active: 'a' }, { ids: ['file:x'], active: 'file:x' }], focus: 0, last: {} };
  const s = reconcile(stored, live([], ['file:x']), { beside: true });
  assert.deepEqual(ids(s), [[], ['file:x']]);
  assert.equal(s.groups[0].active, '');
});

test('the last used tab opens on a fresh page, stopped or not', () => {
  const s = reconcile(null, { ...live(['a', 'b'], [], []), lastTab: 'b' }, { beside: true });
  assert.equal(s.groups[0].active, 'b');
  assert.equal(s.focus, 0);
});

test('moving a tab: inserted ahead of before, active and focused there, last updated', () => {
  const s0 = { groups: [{ ids: ['a', 'b'], active: 'a' }, { ids: ['file:x'], active: 'file:x' }], focus: 0, last: { term: 0, file: 1 } };
  const s = moveTab(s0, 'b', 1, 'file:x');
  assert.deepEqual(ids(s), [['a'], ['b', 'file:x']]);
  assert.equal(s.groups[1].active, 'b');
  assert.equal(s.focus, 1);
  assert.equal(s.last.term, 1);
  assert.notEqual(s, s0, 'pure: a new state');
  const t = moveTab(moveTab(s0, 'file:x', 0, ''), 'a', 0, '');
  assert.deepEqual(ids(t), [['b', 'file:x', 'a']], 'group 2 closes when its last tab leaves');
});

test('the group a shown tab leaves falls back to a tab that may be shown', () => {
  const s0 = { groups: [{ ids: ['a', 'b', 'c'], active: 'b' }], focus: 0, last: {} };
  const s = moveTab(s0, 'b', 1, '', id => id === 'c');
  assert.deepEqual(ids(s), [['a', 'c'], ['b']]);
  assert.equal(s.groups[0].active, 'c', 'a is stopped: moving b must not start it');
});

test('the server order is group 1 terminals then group 2 terminals', () => {
  const s = { groups: [{ ids: ['a', 'file:x', 'b'], active: '' }, { ids: ['c', 'file:y'], active: '' }], focus: 0, last: {} };
  assert.deepEqual(canonicalTerms(s), ['a', 'b', 'c']);
  assert.equal(idKind('changes'), 'file');
  assert.equal(idKind('whatsnew'), 'file');
  assert.equal(idKind('file:x'), 'file');
  assert.equal(idKind('t-123'), 'term');
});

test('the moves that bring the server order to the groups\' own', () => {
  const order = ['a', 'c', 'b', 'd'];
  // c dragged to the end of group 1: [a, d, c] | [b].
  assert.deepEqual(movesToMatch(order, ['a', 'd', 'c', 'b']), [{ id: 'b', before: '' }, { id: 'c', before: 'b' }]);
  // d dragged into group 2 ahead of b: [a, c] | [d, b]; the page's own
  // sequence did not change, but the server's still has to.
  assert.deepEqual(movesToMatch(order, ['a', 'c', 'd', 'b']), [{ id: 'b', before: '' }]);
  assert.deepEqual(movesToMatch(order, ['a', 'c', 'b', 'd']), [], 'already in order: nothing to send');
  assert.deepEqual(movesToMatch(['a', 'b'], ['x', 'b', 'a']), [{ id: 'a', before: '' }], 'tabs this page does not hold are left out');
});
