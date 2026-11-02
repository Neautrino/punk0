import { test } from 'node:test';
import assert from 'node:assert';
import { getTabLabel, trimGutter } from './term-util.js';

test('getTabLabel names an unnamed tab after what runs in it', () => {
  assert.equal(getTabLabel({ kind: 'claude', name: '' }), 'Claude Code');
  assert.equal(getTabLabel({ kind: 'shell', name: '' }), 'Shell');
  assert.equal(getTabLabel({ kind: 'cursor-agent' }), 'Cursor Agent');
  assert.equal(getTabLabel({ kind: 'omp' }), 'Oh My Pi');
});

test('getTabLabel prefers the user name, then the chat title', () => {
  assert.equal(getTabLabel({ kind: 'claude', name: 'Bug Fixer', title: 'Fix login' }), 'Bug Fixer');
  assert.equal(getTabLabel({ kind: 'claude', name: '', title: 'Fix login' }), 'Fix login');
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
