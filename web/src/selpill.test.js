import { test } from 'node:test';
import assert from 'node:assert';
import { calculatePillPosition } from './selpill-util.js';

const pill = { width: 280, height: 32 };
const view = { width: 1000 };

test('sits above the selection when there is room', () => {
  assert.deepEqual(calculatePillPosition({ left: 250, top: 150, bottom: 200 }, view, pill), { left: 250, top: 110 });
});

test('flips below a selection near the top of the window', () => {
  assert.equal(calculatePillPosition({ left: 250, top: 30, bottom: 60 }, view, pill).top, 68);
});

test('stays inside the window on both sides', () => {
  assert.equal(calculatePillPosition({ left: 900, top: 200, bottom: 230 }, view, pill).left, 710);
  assert.equal(calculatePillPosition({ left: -40, top: 200, bottom: 230 }, view, pill).left, 10);
});
