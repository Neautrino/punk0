import test from 'node:test';
import assert from 'node:assert/strict';

// tips.js keeps its rotation in localStorage, and its imports look up
// elements as they load: give it a page with nothing on it.
const store = new Map();
globalThis.localStorage ??= {
  getItem: k => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => store.set(k, String(v)),
  removeItem: k => store.delete(k),
};
globalThis.document ??= { querySelector: () => null, querySelectorAll: () => [], baseURI: 'http://127.0.0.1/', addEventListener() {} };
globalThis.HTMLSelectElement ??= class {};

const { TIPS } = await import('./tipsdata.js');
const { tipsFor, pickTip, tipHTML } = await import('./tips.js');
const { getSetting, saveSetting } = await import('./settings-form.js');

const ids = page => tipsFor(page).map(t => t.id);
const resetSeen = () => localStorage.removeItem('punk0.tips.seen');

test('every tip is well formed and its `only` setting matches a default', () => {
  assert.equal(new Set(TIPS.map(t => t.id)).size, TIPS.length, 'ids are unique');
  for (const t of TIPS) {
    assert.ok(['agents', 'editor', 'both'].includes(t.page), `${t.id}: page`);
    assert.ok(t.text.length <= 160, `${t.id}: ${t.text.length} chars`);
    if (t.os) assert.ok(['mac', 'other'].includes(t.os), `${t.id}: os`);
    // A tip about turning something on is shown out of the box: the value it
    // waits for is the setting's default, and the setting exists.
    if (t.only) assert.equal(getSetting(t.only[0]), t.only[1], `${t.id}: ${t.only[0]}`);
    // Every {combo} and **label** closes: nothing of the markup is left over.
    const html = tipHTML(t.text);
    assert.ok(!/[{}]|\*\*/.test(html), `${t.id}: unclosed markup in ${html}`);
  }
});

test('each page shows its own tips and both pages\' ones, never the other page\'s', () => {
  const agents = ids('agents');
  const editor = ids('editor');
  assert.ok(agents.includes('a-enter') && agents.includes('b-settings'));
  assert.ok(editor.includes('e-diff') && editor.includes('b-settings'));
  assert.ok(!agents.some(id => id.startsWith('e-')));
  assert.ok(!editor.some(id => id.startsWith('a-')));
});

test('pickTip goes through a page\'s tips in order, one unseen tip at a time', () => {
  resetSeen();
  const list = ids('editor');
  const picked = list.map(() => pickTip('editor').id);
  assert.deepEqual(picked, list);
  // Every tip shown: the round starts over.
  assert.equal(pickTip('editor').id, list[0]);
});

test('a tip for both pages seen on one page is not shown again on the other', () => {
  resetSeen();
  const editorOnly = ids('editor').filter(id => !id.startsWith('b-'));
  for (const _ of ids('editor')) pickTip('editor');
  const agents = ids('agents');
  const shown = agents.map(() => pickTip('agents').id);
  // The agents page's round skips what the editor showed, then starts over.
  const own = agents.filter(id => !id.startsWith('b-'));
  assert.deepEqual(shown.slice(0, own.length), own);
  // Starting the agents page over leaves the editor's progress alone.
  const seen = JSON.parse(localStorage.getItem('punk0.tips.seen'));
  for (const id of editorOnly) assert.ok(seen.includes(id), `${id} still seen`);
});

test('a tip waiting for a setting goes quiet once the setting changes', async () => {
  const realFetch = globalThis.fetch;
  globalThis.fetch = async () => new Response(JSON.stringify({ settings: { 'editor.vimMode': true } }));
  try {
    assert.ok(ids('editor').includes('e-vim'));
    assert.equal(await saveSetting('editor.vimMode', true), true);
    assert.ok(!ids('editor').includes('e-vim'));
  } finally {
    globalThis.fetch = realFetch;
  }
});

test('tipHTML escapes the text and draws keys and labels', () => {
  const html = tipHTML('Press {Shift+Enter} on **<Stop>** & go');
  assert.match(html, /<kbd>[^<]*Enter<\/kbd>/);
  assert.match(html, /<b>&lt;Stop&gt;<\/b>/);
  assert.match(html, / &amp; go$/);
});
