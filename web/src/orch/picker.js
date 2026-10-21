// web/src/orch/picker.js
// Open file picker: fuzzy-find a file of the selected workspace by name
// (w/<id>/api/find, the app's own finder) and open it in a tab.
import { $, esc } from '../state.js';
import { call } from './api.js';
import { workspace, openFile } from './store.js';
import { fileKind } from './tree.js';

let pickerEl, pickInputEl, pickListEl;
let results = [];
let pickSel = 0;
let pickSeq = 0; // drops answers to queries typed over

export function initPicker() {
  pickerEl = $('#o-picker');
  pickInputEl = $('#o-picker-input');
  pickListEl = $('#o-picker-list');
  pickInputEl.addEventListener('input', searchFiles);
  pickInputEl.addEventListener('keydown', onPickerKey);
  pickInputEl.addEventListener('blur', () => setTimeout(closePicker, 0));
  // Keep focus in the input so blur means "clicked away".
  pickListEl.addEventListener('mousedown', e => e.preventDefault());
  pickListEl.addEventListener('click', e => {
    const i = e.target.closest('[data-i]')?.dataset.i;
    if (i !== undefined) choose(results[+i]);
  });
  pickerEl.addEventListener('mousedown', e => {
    if (e.target === pickerEl) pickInputEl.blur();
  });
}

export function openPicker() {
  const w = workspace();
  if (!w || w.missing) return;
  pickerEl.hidden = false;
  pickInputEl.value = '';
  results = [];
  pickListEl.innerHTML = '';
  pickInputEl.focus();
  searchFiles();
}

function closePicker() {
  pickSeq++;
  pickerEl.hidden = true;
}

async function searchFiles() {
  const w = workspace();
  if (!w) return;
  const n = ++pickSeq;
  const res = await call('GET', `${w.href}api/find?` + new URLSearchParams({ q: pickInputEl.value, limit: '50' }));
  if (n !== pickSeq) return;
  if (!res.ok) {
    results = [];
    pickListEl.innerHTML = `<div class="o-note o-err">${esc(res.data.error)}</div>`;
    return;
  }
  results = res.data.results || [];
  pickSel = 0;
  renderPicker();
}

function renderPicker() {
  if (!results.length) {
    pickListEl.innerHTML = '<div class="o-note">No matching files</div>';
    return;
  }
  pickListEl.innerHTML = results.map((r, i) => {
    const dir = r.path.slice(0, r.path.length - r.name.length).replace(/\/$/, '');
    return `<div class="pi${i === pickSel ? ' sel' : ''}" data-i="${i}">
      <span class="o-tab-ic" data-t="${fileKind(r.name)}"></span>
      <span class="pn">${marked(r.path, r.pos || [], r.path.length - r.name.length)}</span>
      <span class="pp">${esc(dir)}</span>
    </div>`;
  }).join('');
  pickListEl.querySelector('.sel')?.scrollIntoView({ block: 'nearest' });
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

function onPickerKey(e) {
  if (e.key === 'Escape') return pickInputEl.blur();
  if (e.key === 'Enter') return results[pickSel] && choose(results[pickSel]);
  const step = { ArrowDown: 1, ArrowUp: -1 }[e.key];
  if (!step || !results.length) return;
  e.preventDefault();
  pickSel = (pickSel + step + results.length) % results.length;
  renderPicker();
}

function choose(r) {
  pickInputEl.blur();
  openFile(r.path);
}
