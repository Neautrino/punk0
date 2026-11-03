import test from 'node:test';
import assert from 'node:assert/strict';

// store.js reads sessionStorage and the page URL when it loads, and its
// imports look up elements: give it a page with nothing on it.
const page = new Map();
globalThis.sessionStorage ??= {
  getItem: k => (page.has(k) ? page.get(k) : null),
  setItem: (k, v) => page.set(k, String(v)),
  removeItem: k => page.delete(k),
};
globalThis.document ??= { querySelector: () => null, querySelectorAll: () => [], baseURI: 'http://127.0.0.1/' };
globalThis.location ??= { search: '', pathname: '/', hash: '' };
globalThis.history ??= { state: null, replaceState() {} };

const s = await import('./store.js');

const ws = { id: 'w1', tabs: [{ id: 't1', kind: 'shell', running: true }] };
s.state.projects = [{ id: 'p1', workspaces: [ws] }];

test('the What\'s New tab is the page\'s: there with and without a workspace', () => {
  s.state.ws = '';
  assert.deepEqual(s.fileTabsOf(null), []);
  s.openWhatsNew('0.2.0');
  assert.deepEqual(s.tabsOf(null).map(t => t.id), [s.WHATSNEW_TAB]);
  assert.equal(s.activeTab()?.whatsNew, '0.2.0');
  assert.equal(sessionStorage.getItem('punk0.whatsNew'), '0.2.0');
  // Nothing is remembered for a workspace that is not there.
  assert.equal(s.state.tabs[''], undefined);

  s.state.changes.w1 = true;
  s.state.files.w1 = ['src/a.go'];
  assert.deepEqual(s.tabsOf(ws).map(t => t.id), ['t1', s.WHATSNEW_TAB, s.CHANGES_TAB, 'file:src/a.go']);
  delete s.state.changes.w1;
  delete s.state.files.w1;
  s.closeWhatsNew();
});

test('selecting it in a workspace puts the file group in front', () => {
  s.state.ws = 'w1';
  s.openWhatsNew('0.2.0');
  assert.equal(s.state.tabs.w1.file, s.WHATSNEW_TAB);
  assert.equal(s.state.tabs.w1.front, 'file');
  assert.equal(s.activeTab()?.id, s.WHATSNEW_TAB);
  s.closeWhatsNew();
  s.state.ws = '';
});

test('closing it, the last file tab, ends a collapsed split', () => {
  s.openWhatsNew('0.2.0');
  s.setCollapsed('term');
  s.closeWhatsNew();
  assert.equal(s.state.whatsNew, '');
  assert.equal(sessionStorage.getItem('punk0.whatsNew'), '');
  assert.equal(s.state.collapsed, '');
  assert.deepEqual(s.fileTabsOf(null), []);
});
