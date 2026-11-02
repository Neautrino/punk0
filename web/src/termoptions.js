// web/src/termoptions.js
// termOptions maps the terminal.* settings onto xterm's options, for both
// terminals (orch/term.js, term_inspector.js). The font family is
// termFontFamily's: punk0's Nerd Font Mono when none is chosen.
import { getSetting } from './settings-form.js';
import { termFontFamily } from './fonts.js';

export function termOptions() {
  const mono = getComputedStyle(document.documentElement).getPropertyValue('--mono').trim() || 'monospace';
  return {
    fontSize: getSetting('terminal.fontSize') ?? 12,
    fontFamily: termFontFamily(getSetting('terminal.fontFamily'), mono),
    lineHeight: getSetting('terminal.lineHeight') ?? 1,
    scrollback: getSetting('terminal.scrollback') ?? 10000,
    cursorStyle: getSetting('terminal.cursorStyle') || 'block',
    cursorBlink: getSetting('terminal.cursorBlink') ?? true,
  };
}

// FONT_WAIT caps how long a terminal waits for its fonts: punk0 fetches its
// own on first use, and a slow or offline fetch must not hold the tab.
const FONT_WAIT = 3000;

// loadTermFonts loads the faces a terminal with opts draws with, so xterm
// measures its cells with them: it measures once and never notices a font
// arriving later. Sample text includes icons, as a face serving only them
// would otherwise not load. It resolves when they are in or after FONT_WAIT;
// faces still loading then call late once they are in.
export function loadTermFonts(opts, late) {
  const sample = 'W\u2500\ue0b6\uf07b';
  const all = Promise.all(['normal', 'bold'].map(w =>
    document.fonts.load(`${w} ${opts.fontSize}px ${opts.fontFamily}`, sample))).catch(() => {});
  let waited = false;
  all.then(() => { if (waited) late(); });
  return Promise.race([all, new Promise(r => setTimeout(() => { waited = true; r(undefined); }, FONT_WAIT))]);
}

// remeasure makes xterm measure its font again. Its width cache is only
// cleared when the family string changes, so it is changed and put back.
export function remeasure(term) {
  const family = term.options.fontFamily;
  term.options.fontFamily = 'monospace';
  term.options.fontFamily = family;
}
