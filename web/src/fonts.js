// web/src/fonts.js
// Font names and font-family stacks, shared by the terminals (termoptions.js)
// and the settings modal's font pickers (settings-form.js). No imports: the
// settings modal must not pull anything else into either page's bundle.

// NERD_MONO is the family punk0 serves for terminals (@font-face in
// style.css): JetBrains Mono with Nerd Font icons that fit one cell.
export const NERD_MONO = 'JetBrainsMono Nerd Font Mono';

// SERVED_FONTS are the families punk0 itself serves, so they need not be
// installed; the pickers list them first.
export const SERVED_FONTS = [
  { name: NERD_MONO, mono: true },
  { name: 'JetBrains Mono', mono: true },
];

const GENERIC = new Set(['serif', 'sans-serif', 'monospace', 'cursive', 'fantasy', 'system-ui',
  'ui-serif', 'ui-sans-serif', 'ui-monospace', 'ui-rounded', 'emoji', 'math', 'fangsong',
  '-apple-system', 'blinkmacsystemfont']);

// families splits a CSS font-family value into names, quotes removed.
export function families(stack) {
  return String(stack || '').split(',').map(s => s.trim().replace(/^(["'])(.*)\1$/, '$2').trim()).filter(Boolean);
}

// quoteFamily writes one name for a font-family value.
export function quoteFamily(name) {
  return GENERIC.has(name.toLowerCase()) ? name : `"${name.replace(/["\\]/g, '')}"`;
}

// termFontFamily is the terminals' font-family: the chosen stack, or
// punk0's Nerd Font Mono then the code font when none is chosen. A chosen
// stack without a Nerd Font gets NERD_MONO after it, so agent and prompt
// icons still draw instead of boxes; its own fonts keep every other glyph.
export function termFontFamily(chosen, mono) {
  const own = String(chosen || '').trim();
  if (!own) return `${quoteFamily(NERD_MONO)}, ${mono}`;
  if (families(own).some(f => /nerd ?font|\bNF[MP]?\b/i.test(f))) return own;
  return `${own}, ${quoteFamily(NERD_MONO)}, monospace`;
}

// missingFont is the first name in stack when it is neither a generic
// family, one punk0 serves, nor in installed (lower-cased names); '' when it
// is known. The browser skips such a name silently and draws with the next,
// so the picker says so.
export function missingFont(stack, installed) {
  const [first] = families(stack);
  if (!first) return '';
  const k = first.toLowerCase();
  if (GENERIC.has(k) || SERVED_FONTS.some(s => s.name.toLowerCase() === k) || installed.has(k)) return '';
  return first;
}
