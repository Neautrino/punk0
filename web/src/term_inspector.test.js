import { test } from 'node:test';
import assert from 'node:assert';
import { getTabLabel, trimGutter } from './term-util.js';

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

test('trimGutter drops only the indent every non-blank line shares', () => {
  // an agent's output drawn behind a two-space margin, code indented inside it
  assert.equal(trimGutter('  func f() {\n\n      return 1\n  }'), 'func f() {\n\n    return 1\n}');
  assert.equal(trimGutter('    a\r\n    b'), 'a\r\nb');
  // a line without indent (a selection starting mid-line) keeps everything as it is
  assert.equal(trimGutter('x = 1\n    y = 2'), 'x = 1\n    y = 2');
  assert.equal(trimGutter('   '), '   ');
});
