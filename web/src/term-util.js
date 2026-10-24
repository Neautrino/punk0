// web/src/term-util.js
// Pure helpers for the editor's terminal pane (term_inspector.js), kept apart
// from the DOM so they can be tested.

const KIND_LABELS = {
  claude: 'Claude Code',
  shell: 'Shell',
  codex: 'Codex',
  opencode: 'OpenCode',
  agy: 'Antigravity',
  'cursor-agent': 'Cursor Agent',
};

/** A tab's chip label: its own name once renamed, else its kind's. */
export function getTabLabel(t) {
  if (t.name && t.name !== t.kind) return t.name;
  return KIND_LABELS[t.kind] || t.name || t.kind;
}
