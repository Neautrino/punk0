import { test } from 'node:test';
import assert from 'node:assert';
import { insertionAt } from './tabdrag.js';

const tabs = [{ left: 0, right: 100 }, { left: 100, right: 160 }, { left: 160, right: 300 }];

test('a drop lands before the first tab whose middle is right of the pointer', () => {
  assert.equal(insertionAt(tabs, -5), 0);
  assert.equal(insertionAt(tabs, 49), 0);
  assert.equal(insertionAt(tabs, 51), 1);
  assert.equal(insertionAt(tabs, 131), 2);
  assert.equal(insertionAt(tabs, 299), 3, 'past the last middle: the end');
  assert.equal(insertionAt([], 10), 0);
});
