// web/src/termclip.js
// Copy and paste in a terminal tab, the same on the agents page
// (orch/term.js) and in the editor's Terminal panel (term_inspector.js):
//
//   - terminal.copyOnSelect: a selection goes to the clipboard once it
//     settles, as in gnome-terminal, Orca or Windows Terminal's option.
//   - Windows / Linux: Ctrl+C with a selection copies it (without one it is
//     still ^C), Ctrl+Insert copies, Ctrl+Shift+V and Shift+Insert paste.
//     Ctrl+V is left to the program: Claude Code pastes images with it.
//     Ctrl+Shift+C is the browser's element picker, so it is not used.
//   - Every copy, Cmd+C and the context menu's Copy included, drops the
//     indent all copied lines share: agent TUIs draw their output behind a
//     left margin that would otherwise come along.
//
// The clipboard needs a secure context (localhost or https).
import { isMac } from './state.js';
import { showToast } from './ui.js';
import { getSetting } from './settings-form.js';
import { trimGutter } from './term-util.js';

// selectSettle is how long a selection must stay put before copy on select
// takes it, so a drag is copied once rather than at every cell.
const selectSettle = 100;

async function copy(text, announce) {
  try {
    await navigator.clipboard.writeText(text);
    if (announce) {
      const n = text.split('\n').length;
      showToast('✓', n === 1 ? 'Copied' : `Copied ${n} lines`, 1500);
    }
  } catch {
    // A failed copy on select leaves the selection to copy another way.
    if (announce) showToast('!', 'The browser would not let punk0 write to the clipboard', 4000);
  }
}

// installClipboard adds copy on select and the trimmed native copy to term,
// shown in el (the element term.open was given).
export function installClipboard(term, el) {
  let settle = 0;
  term.onSelectionChange(() => {
    clearTimeout(settle);
    if (!getSetting('terminal.copyOnSelect') || !term.hasSelection()) return;
    settle = setTimeout(() => term.hasSelection() && copy(trimGutter(term.getSelection()), false), selectSettle);
  });
  // Captured above xterm's own copy handler, which would copy untrimmed.
  el.addEventListener('copy', e => {
    if (!term.hasSelection()) return;
    e.clipboardData.setData('text/plain', trimGutter(term.getSelection()));
    e.preventDefault();
    e.stopImmediatePropagation();
  }, true);
}

// clipboardKey handles the copy and paste keys of a terminal's key event and
// reports whether it did; the caller's attachCustomKeyEventHandler then
// returns false so xterm leaves the key alone. A paste key is not prevented:
// the browser pastes into xterm's textarea, which sends it to the program.
export function clipboardKey(term, e) {
  if (isMac || e.altKey || e.metaKey) return false;
  const key = e.key.toLowerCase();
  const copyKey = e.ctrlKey && !e.shiftKey && (key === 'insert' || (key === 'c' && term.hasSelection()));
  if (copyKey) {
    if (e.type === 'keydown') {
      if (term.hasSelection()) copy(trimGutter(term.getSelection()), true);
      term.clearSelection();
    }
    e.preventDefault();
    return true;
  }
  return (e.ctrlKey && e.shiftKey && key === 'v') || (e.shiftKey && !e.ctrlKey && key === 'insert');
}
