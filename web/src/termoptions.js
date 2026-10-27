// web/src/termoptions.js
// termOptions maps the terminal.* settings onto xterm's options, for both
// terminals (orch/term.js, term_inspector.js); an empty font family is the
// page's monospace font.
import { getSetting } from './settings-form.js';

export function termOptions() {
  const mono = getComputedStyle(document.documentElement).getPropertyValue('--mono').trim() || 'monospace';
  return {
    fontSize: getSetting('terminal.fontSize') ?? 13,
    fontFamily: getSetting('terminal.fontFamily') || mono,
    lineHeight: getSetting('terminal.lineHeight') ?? 1,
    scrollback: getSetting('terminal.scrollback') ?? 10000,
    cursorStyle: getSetting('terminal.cursorStyle') || 'block',
    cursorBlink: getSetting('terminal.cursorBlink') ?? true,
  };
}
