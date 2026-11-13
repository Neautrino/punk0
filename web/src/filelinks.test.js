import { test } from 'node:test';
import assert from 'node:assert';
import { findPaths, resolvePath } from './filelinks.js';

const spans = text => findPaths(text).map(f => ({ text: text.slice(f.start, f.end), path: f.path, line: f.line, col: f.col }));

test('paths with a line and column, in the forms agents print them', () => {
  assert.deepEqual(spans('Update(src/main.go:42) and web/a.ts:7:3, x.py(12,5), docs/README.md#L9'), [
    { text: 'src/main.go:42', path: 'src/main.go', line: 42, col: 0 },
    { text: 'web/a.ts:7:3', path: 'web/a.ts', line: 7, col: 3 },
    { text: 'x.py(12,5)', path: 'x.py', line: 12, col: 5 },
    { text: 'docs/README.md#L9', path: 'docs/README.md', line: 9, col: 0 },
  ]);
});

test('a sentence\'s full stop and wrapping punctuation stay outside the link', () => {
  assert.deepEqual(spans('Edited ./server.go. See `go.mod`.').map(s => s.text), ['./server.go', 'go.mod']);
  assert.deepEqual(spans('[docs](docs/a.md).').map(s => s.text), ['docs/a.md']);
});

test('URLs and bare words are not paths', () => {
  assert.deepEqual(spans('https://example.com/a/b.go and word'), []);
});

test('file:// URLs give their path and line', () => {
  assert.deepEqual(spans('at file:///home/u/p/a%20b.go:3:2.'), [
    { text: 'file:///home/u/p/a%20b.go:3:2', path: '/home/u/p/a b.go', line: 3, col: 2 },
  ]);
  assert.equal(findPaths('file:///C:/w/x.go')[0].path, 'C:/w/x.go');
});

test('paths resolve inside the workspace folder only', () => {
  assert.equal(resolvePath('src/a.go', '/w/p'), 'src/a.go');
  assert.equal(resolvePath('./src/a.go', '/w/p/'), 'src/a.go');
  assert.equal(resolvePath('/w/p/src/a.go', '/w/p'), 'src/a.go');
  assert.equal(resolvePath('/w/project/a.go', '/w/p'), null, 'a folder sharing the prefix is outside');
  assert.equal(resolvePath('../x/a.go', '/w/p'), null);
  assert.equal(resolvePath('src/../../a.go', '/w/p'), null);
  assert.equal(resolvePath('~/a.go', '/w/p'), null);
  assert.equal(resolvePath('c:\\W\\P\\src\\a.go', 'C:\\w\\p'), 'src/a.go', 'Windows folders compare case-insensitively');
});
