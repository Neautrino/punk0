// web/src/picker.js
// Open file picker: fuzzy-find a file of the selected workspace by name
// (GET /api/workspaces/{id}/find) and open it in a tab.
import { state, openFile, esc } from './store.js';
import { fileKind } from './tree.js';

let rootEl, inputEl, listEl;
let results = [];
let sel = 0;
let seq = 0; // drops answers to queries typed over

export function initPicker(container) {
  rootEl = container;
  inputEl = container.querySelector('#picker-input');
  listEl = container.querySelector('#picker-list');
  inputEl.addEventListener('input', search);
  inputEl.addEventListener('keydown', onKey);
  inputEl.addEventListener('blur', () => setTimeout(closePicker, 0));
  // Keep focus in the input so blur means "clicked away".
  listEl.addEventListener('mousedown', (e) => e.preventDefault());
  listEl.addEventListener('click', (e) => {
    const i = e.target.closest('[data-i]')?.dataset.i;
    if (i !== undefined) choose(results[+i]);
  });
}

export function openPicker() {
  if (!state.ws) return;
  rootEl.classList.remove('hidden');
  inputEl.value = '';
  inputEl.focus();
  search();
}

function closePicker() {
  seq++;
  rootEl.classList.add('hidden');
}

async function search() {
  const n = ++seq;
  const res = await fetch(`/api/workspaces/${encodeURIComponent(state.ws)}/find?q=${encodeURIComponent(inputEl.value)}`);
  if (n !== seq || !res.ok) return;
  results = (await res.json()).results;
  sel = 0;
  render();
}

function render() {
  if (!results.length) {
    listEl.innerHTML = '<div class="picker-empty">No matching files</div>';
    return;
  }
  listEl.innerHTML = results.map((r, i) => {
    const dir = r.path.slice(0, r.path.length - r.name.length).replace(/\/$/, '');
    return `
      <div class="picker-row${i === sel ? ' sel' : ''}" data-i="${i}">
        <span class="ic" data-t="${fileKind(r.name)}"></span>
        <span class="picker-name">${marked(r.path, r.pos, r.path.length - r.name.length)}</span>
        <span class="picker-dir">${esc(dir)}</span>
      </div>`;
  }).join('');
  listEl.querySelector('.sel')?.scrollIntoView({ block: 'nearest' });
}

// marked renders path from UTF-16 offset `from`, wrapping matched characters
// in <b>. pos holds UTF-8 byte offsets, so the walk tracks both.
function marked(path, pos, from) {
  const hit = new Set(pos);
  const enc = new TextEncoder();
  let at = 0; // UTF-16 offset of ch
  let byte = 0; // UTF-8 offset of ch
  let out = '';
  for (const ch of path) {
    const s = esc(ch);
    if (at >= from) out += hit.has(byte) ? `<b>${s}</b>` : s;
    at += ch.length;
    byte += enc.encode(ch).length;
  }
  return out;
}

function onKey(e) {
  if (e.key === 'Escape') return inputEl.blur();
  if (e.key === 'Enter') return results[sel] && choose(results[sel]);
  const step = { ArrowDown: 1, ArrowUp: -1 }[e.key];
  if (!step || !results.length) return;
  e.preventDefault();
  sel = (sel + step + results.length) % results.length;
  render();
}

function choose(r) {
  inputEl.blur();
  openFile(r.path);
}
