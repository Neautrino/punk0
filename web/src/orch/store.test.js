import test from 'node:test';
import assert from 'node:assert/strict';

// store.js reads sessionStorage and the page URL when it loads, and its
// imports look up elements and extend the DOM classes: give it a page with
// nothing on it.
const page = new Map();
globalThis.sessionStorage ??= {
  getItem: k => (page.has(k) ? page.get(k) : null),
  setItem: (k, v) => page.set(k, String(v)),
  removeItem: k => page.delete(k),
};
globalThis.localStorage ??= globalThis.sessionStorage;
globalThis.document ??= { querySelector: () => null, querySelectorAll: () => [], baseURI: 'http://127.0.0.1/' };
globalThis.location ??= { search: '', pathname: '/', hash: '' };
globalThis.history ??= { state: null, replaceState() {} };
globalThis.HTMLSelectElement ??= { prototype: {} };
globalThis.addEventListener ??= () => {};

const s = await import('./store.js');

const ws = { id: 'w1', tabs: [{ id: 't1', kind: 'shell', running: true }] };
s.state.projects = [{ id: 'p1', workspaces: [ws] }];

test('the What\'s New tab is the page\'s: there with and without a workspace', () => {
  s.state.ws = '';
  assert.deepEqual(s.fileTabsOf(null), []);
  s.openWhatsNew('0.2.0');
  assert.deepEqual(s.tabsOf(null).map(t => t.id), [s.WHATSNEW_TAB]);
  assert.equal(s.frontTab()?.whatsNew, '0.2.0');
  assert.equal(sessionStorage.getItem('punk0.whatsNew'), '0.2.0');

  s.state.changes.w1 = true;
  s.state.files.w1 = ['src/a.go'];
  assert.deepEqual(s.tabsOf(ws).map(t => t.id), ['t1', s.WHATSNEW_TAB, s.CHANGES_TAB, 'file:src/a.go']);
  delete s.state.changes.w1;
  delete s.state.files.w1;
  s.closeWhatsNew();
});

test('a workspace opens its terminals left and its files right', () => {
  s.state.ws = 'w1';
  s.openWhatsNew('0.2.0');
  assert.deepEqual(s.groupsOf(ws).groups.map(g => g.ids), [['t1'], [s.WHATSNEW_TAB]]);
  assert.equal(s.groupOf(s.WHATSNEW_TAB, ws), 1);
  assert.equal(s.focusedGroup(ws), 1, 'selecting it focused the file group');
  assert.equal(s.tabIn(1, ws)?.id, s.WHATSNEW_TAB);
  assert.equal(s.frontTab()?.id, s.WHATSNEW_TAB);
  s.closeWhatsNew();
  s.state.ws = '';
});

test('closing the second group\'s last tab ends a collapsed split', () => {
  s.state.ws = 'w1';
  s.openWhatsNew('0.2.0');
  s.setCollapsed(1);
  assert.equal(s.collapsedGroup(), 1);
  s.closeWhatsNew();
  assert.equal(s.state.whatsNew, '');
  assert.equal(sessionStorage.getItem('punk0.whatsNew'), '');
  assert.deepEqual(s.groupsOf(ws).groups.map(g => g.ids), [['t1']]);
  assert.equal(s.collapsedGroup(), -1);
  s.state.ws = '';
});

test('a maximized second group leaves no collapse behind when it closes', () => {
  s.state.ws = 'w1';
  s.openWhatsNew('0.2.0');
  s.setCollapsed(0); // Maximize: the first group is hidden
  s.closeWhatsNew();
  assert.deepEqual(s.groupsOf(ws).groups.map(g => g.ids), [['t1']]);
  assert.equal(s.collapsedGroup(), -1, 'the next tab opened there must not be hidden');
  s.state.ws = '';
});

test('a tab selected before it is listed is shown once it appears', () => {
  s.state.ws = 'w1';
  s.selectTab('file:src/b.go');
  assert.notEqual(s.groupsOf(ws).groups[0].active, 'file:src/b.go', 'nothing to show yet');
  s.state.files.w1 = ['src/b.go'];
  const g = s.groupsOf(ws);
  assert.equal(g.groups[g.focus].active, 'file:src/b.go');
  delete s.state.files.w1;
  s.state.ws = '';
});

test('a group the user opens a kind in takes the next new tab of it', () => {
  s.state.ws = 'w1';
  s.openIn(0, 'file');
  s.state.files.w1 = ['src/a.go'];
  assert.deepEqual(s.groupsOf(ws).groups.map(g => g.ids), [['t1', 'file:src/a.go']]);
  delete s.state.files.w1;
  s.state.ws = '';
});
