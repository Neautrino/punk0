import { test } from 'node:test';
import assert from 'node:assert';
import {
  CENTER, RIGHT, emptyModel, place, activate, move, remove, next, closeTargets,
  canonicalTerms, beforeFor, canReorder, seed, reconcile, persisted, groupOf,
} from './editorgroups.js';

const doc = id => ({ kind: 'doc', id });
const term = id => ({ kind: 'term', id });
const view = id => ({ kind: 'view', id });
const ids = (m, g) => m[g].map(e => e.id);

// a center of [a.js, t1, b.js] and a right of [t2, read.md]
function sample() {
  let m = emptyModel();
  for (const e of [doc('a.js'), term('t1'), doc('b.js')]) m = place(m, e, CENTER);
  for (const e of [term('t2'), view('read.md')]) m = place(m, e, RIGHT);
  return activate(activate(m, 't2'), 'a.js');
}

test('a tab sits in one group; placing it again moves it', () => {
  const m = place(sample(), term('t2'), CENTER, 't1');
  assert.deepEqual(ids(m, CENTER), ['a.js', 't2', 't1', 'b.js']);
  assert.deepEqual(ids(m, RIGHT), ['read.md']);
  assert.equal(m.active.right, 'read.md', 'the right side falls to its neighbour');
});

test('moving a tab: inserted ahead of before, active there, that group focused', () => {
  const s = sample();
  const m = move(s, 't1', RIGHT, 't2');
  assert.deepEqual(ids(m, CENTER), ['a.js', 'b.js']);
  assert.deepEqual(ids(m, RIGHT), ['t1', 't2', 'read.md']);
  assert.equal(m.active.right, 't1');
  assert.equal(m.focus, RIGHT);
  assert.notEqual(m, s, 'pure: a new model');
  assert.deepEqual(ids(s, CENTER), ['a.js', 't1', 'b.js'], 'the old model stands');
  assert.deepEqual(ids(move(s, 'b.js', CENTER, 'a.js'), CENTER), ['b.js', 'a.js', 't1'], 'a move inside a bar reorders');
  assert.equal(move(s, 'nope', CENTER, ''), s, 'an unknown tab moves nowhere');
});

test('what a group shows next when its active tab leaves, stopped terminals passed over', () => {
  const m = activate(sample(), 't1');
  assert.equal(remove(m, ['t1']).active.center, 'b.js', 'the one after it');
  const stopped = remove(m, ['b.js']);
  assert.equal(remove(stopped, ['t1'], { showable: e => e.kind !== 'term' }).active.center, 'a.js');
  const last = remove(remove(sample(), ['t2']), ['read.md']);
  assert.deepEqual(ids(last, RIGHT), []);
  assert.equal(last.active.right, '');
  assert.equal(last.focus, CENTER, 'an emptied right side hands the focus back');
});

test('Ctrl+Tab and gt step through a bar, terminals included', () => {
  const m = sample();
  assert.equal(next(m, CENTER, 'a.js', 1), 't1');
  assert.equal(next(m, CENTER, 'b.js', 1), 'a.js', 'wraps');
  assert.equal(next(m, CENTER, 'a.js', -1), 'b.js');
  assert.equal(next(emptyModel(), CENTER, '', 1), '');
});

test('Close Others / Left / Right close documents only, in center order', () => {
  const m = sample();
  assert.deepEqual(closeTargets(m, 'close', 'a.js'), ['a.js']);
  assert.deepEqual(closeTargets(m, 'close', 't1'), [], 'a terminal is not closed by a file action');
  assert.deepEqual(closeTargets(m, 'others', 'a.js'), ['b.js']);
  assert.deepEqual(closeTargets(m, 'right', 'a.js'), ['b.js']);
  assert.deepEqual(closeTargets(m, 'left', 'b.js'), ['a.js']);
  assert.deepEqual(closeTargets(m, 'all', ''), ['a.js', 'b.js']);
});

test('the server order is the center terminals then the right ones, per workspace', () => {
  const m = sample();
  assert.deepEqual(canonicalTerms(m), ['t1', 't2']);
  const ws = id => (id === 't2' ? 'w2' : 'w1');
  assert.equal(beforeFor(m, 't1'), 't2', 'one workspace: the next terminal');
  assert.equal(beforeFor(m, 't2'), '');
  assert.equal(beforeFor(m, 't1', ws), '', 'the next terminal of another workspace is not a before');
  assert.equal(canReorder(m, 't1', RIGHT, 't2', ws), false, 'no place next to another workspace\'s tab alone');
  assert.equal(canReorder(m, 't1', RIGHT, 't2'), true, 'one workspace: anywhere');
  assert.equal(canReorder(m, 't1', CENTER, 'b.js', ws), true, 'documents are no barrier');
  // [a1, a2, b1]: a1 may go after a2, which is its own workspace's
  let bar = emptyModel();
  for (const e of [term('a1'), term('a2'), term('b1')]) bar = place(bar, e, CENTER);
  const two = id => (id.startsWith('a') ? 'wa' : 'wb');
  assert.equal(canReorder(bar, 'a1', CENTER, 'b1', two), true, 'after its own workspace\'s last tab');
  assert.equal(canReorder(bar, 'a1', CENTER, '', two), false, 'past another workspace\'s tab');
  assert.equal(canReorder(emptyModel(), 'a1', RIGHT, '', two), true, 'an empty bar takes it');
});

test('a reload keeps the center terminals and the rest go right', () => {
  const stored = { center: ['t1', 'gone'], active: 't1', focus: CENTER };
  const m = reconcile(seed(stored), ['t2', 't1', 't3']);
  assert.deepEqual(ids(m, CENTER), ['t1'], 'a terminal that no longer exists drops out');
  assert.deepEqual(ids(m, RIGHT), ['t2', 't3']);
  assert.equal(m.active.center, '', 'what the center shows again is the page\'s call: a stored tab may be stopped');
  assert.deepEqual(ids(reconcile(seed(null), ['t1']), RIGHT), ['t1'], 'no stored state: everything on the right');
});

test('the poll drops gone terminals, re-sorts each bar by server order and adds new ones', () => {
  let m = sample();
  m = reconcile(m, ['t2', 't1']);
  assert.deepEqual(ids(m, CENTER), ['a.js', 't1', 'b.js'], 'one terminal per group stays put');
  m = place(m, term('t3'), CENTER, 'b.js');
  m = reconcile(m, ['t3', 't1', 't2', 't4']);
  assert.deepEqual(ids(m, CENTER), ['a.js', 't3', 't1', 'b.js'], 'the terminal slots follow the server order');
  assert.deepEqual(ids(m, RIGHT), ['t2', 'read.md', 't4'], 'a terminal opened elsewhere lands on the right');
  m = reconcile(m, ['t3']);
  assert.deepEqual(ids(m, RIGHT), ['read.md']);
  assert.equal(groupOf(m, 't1'), '');
});

test('a terminal closed elsewhere hands its group to a tab it may show', () => {
  const stopped = { showable: e => e.kind !== 'term' || e.id === 't1' };
  const m = reconcile(activate(sample(), 't2'), ['t1'], stopped);
  assert.equal(m.active.right, 'read.md', 'a group never falls back to a stopped terminal');
});

test('only the center terminals are persisted', () => {
  const m = activate(sample(), 't1');
  assert.deepEqual(persisted(m), { center: ['t1'], active: 't1', focus: CENTER });
  assert.deepEqual(persisted(sample()).active, '', 'a document in front is not a stored terminal');
});
