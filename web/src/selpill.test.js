import { test } from 'node:test';
import assert from 'node:assert';
import { calculatePillPosition } from './selpill.js';

test('calculatePillPosition anchors above selection when space permits', () => {
  const selectionRect = { left: 250, top: 150, right: 400, bottom: 200, width: 150, height: 50 };
  const viewport = { width: 1000, height: 800 };
  const pillSize = { width: 280, height: 32 };

  const pos = calculatePillPosition(selectionRect, viewport, pillSize);
  assert.equal(pos.top, 150 - 32 - 8); // 110px
  assert.equal(pos.left, 250);
});

test('calculatePillPosition flips below selection near top boundary', () => {
  const selectionRect = { left: 250, top: 30, right: 400, bottom: 60, width: 150, height: 30 };
  const viewport = { width: 1000, height: 800 };
  const pillSize = { width: 280, height: 32 };

  const pos = calculatePillPosition(selectionRect, viewport, pillSize);
  assert.equal(pos.top, 60 + 8); // Flips below
});

test('calculatePillPosition clamps to viewport width', () => {
  const selectionRect = { left: 900, top: 200, right: 980, bottom: 230, width: 80, height: 30 };
  const viewport = { width: 1000, height: 800 };
  const pillSize = { width: 280, height: 32 };

  const pos = calculatePillPosition(selectionRect, viewport, pillSize);
  assert.equal(pos.left, 1000 - 280 - 10); // 710px clamped
});
