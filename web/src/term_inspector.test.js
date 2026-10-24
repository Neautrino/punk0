import { test } from 'node:test';
import assert from 'node:assert';
import { getTabLabel } from './term_inspector.js';

test('getTabLabel maps default kind strings correctly', () => {
  assert.equal(getTabLabel({ kind: 'claude', name: 'claude' }), 'Claude Code');
  assert.equal(getTabLabel({ kind: 'shell', name: 'shell' }), 'Shell');
  assert.equal(getTabLabel({ kind: 'codex', name: 'codex' }), 'Codex');
  assert.equal(getTabLabel({ kind: 'opencode', name: 'opencode' }), 'OpenCode');
  assert.equal(getTabLabel({ kind: 'agy', name: 'agy' }), 'Antigravity');
  assert.equal(getTabLabel({ kind: 'cursor-agent', name: 'cursor-agent' }), 'Cursor Agent');
});

test('getTabLabel respects custom renamed tab names', () => {
  assert.equal(getTabLabel({ kind: 'claude', name: 'Bug Fixer' }), 'Bug Fixer');
  assert.equal(getTabLabel({ kind: 'shell', name: 'Build Runner' }), 'Build Runner');
});
