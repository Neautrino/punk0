// web/src/statusparts.js
// What the editor's and the agents page's status bars both draw, into the
// same markup (index.html, workbench.html): the agents needing you and
// working (#st-waiting, #st-working), the open file's details (#st-file:
// Ln / Col, lines, size, tab size, encoding, line endings) and the name its
// language gives the LSP item (#st-lsp).
import { $ } from './state.js';
import { fmtBytes } from './format.js';

// renderState shows n with its verb, which agrees with it: "1 needs you".
function renderState(el, n, verb) {
  const v = verb[n === 1 ? 0 : 1];
  el.hidden = !n;
  el.textContent = n ? `${n} ${v}` : '';
  el.title = n ? `${n} agent${n === 1 ? '' : 's'} ${v}: click for the next one` : '';
}

export function drawAgentStates(waiting, working) {
  renderState($('#st-waiting'), waiting, ['needs you', 'need you']);
  renderState($('#st-working'), working, ['working', 'working']);
}

// langName is how the LSP item names a highlighter's language. Some are
// lower case ("markdown"), and plain text is "plaintext".
export function langName(lang) {
  if (lang === 'plaintext') return 'Plain Text';
  return lang === lang.toLowerCase() ? lang.charAt(0).toUpperCase() + lang.slice(1) : lang;
}

// tabCol is the 1-based column after text, a tab counting to the next tab stop.
export function tabCol(text, tabSize) {
  let col = 1;
  for (const ch of text) col += ch === '\t' ? tabSize - (col - 1) % tabSize : 1;
  return col;
}

function put(sel, text) {
  const el = $(sel);
  el.textContent = text || '';
  el.hidden = !text;
}

// drawFileStatus fills #st-file, or hides it for f null. Each field left out
// hides its item: pos (the Ln / Col text), total (lines), size (bytes),
// tabSize, encoding, eol.
export function drawFileStatus(f) {
  $('#st-file').hidden = !f;
  if (!f) return;
  put('#st-pos', f.pos);
  put('#st-lines', f.total == null ? '' : `${f.total} line${f.total === 1 ? '' : 's'}`);
  put('#st-size', f.size == null ? '' : fmtBytes(f.size));
  put('#st-tabsize', f.tabSize ? `Tab Size: ${f.tabSize}` : '');
  put('#st-enc', f.encoding);
  put('#st-eol', f.eol);
}
