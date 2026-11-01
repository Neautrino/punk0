// web/src/term-util.js
// Pure helpers for the terminals (term_inspector.js, termclip.js), kept apart
// from the DOM so they can be tested.

const KIND_LABELS = {
  claude: 'Claude Code',
  shell: 'Shell',
  codex: 'Codex',
  opencode: 'OpenCode',
  agy: 'Antigravity',
  'cursor-agent': 'Cursor Agent',
};

/** A tab's label: the user's name for it, else its chat's title, else what
 *  runs in it (records.go tabLabel does the same). */
export function getTabLabel(t) {
  return t.name || t.title || KIND_LABELS[t.kind] || t.kind;
}

/** text without the leading spaces every non-blank line shares; lines keep
 *  their indent relative to each other. */
export function trimGutter(text) {
  const nl = text.includes('\r\n') ? '\r\n' : '\n';
  const lines = text.split(nl);
  const indent = l => l.length - l.replace(/^ +/, '').length;
  let min = Infinity;
  for (const l of lines) if (l.trim()) min = Math.min(min, indent(l));
  if (!min || min === Infinity) return text;
  return lines.map(l => l.slice(Math.min(min, indent(l)))).join(nl);
}
