// web/src/outline.js
// The sidebar's Symbols view: the open file's outline (#symbols-list),
// narrowed by #symbols-filter; a click jumps to the symbol.
import { $, $$, esc, S, doc_, api } from './state.js';
import { centerLine } from './tabs.js';
import { render } from './renderer.js';
import { updateStatus, setLspState } from './status.js';
import { pushHistory } from './history.js';

const listEl = () => $('#symbols-list');

export async function loadOutline() {
  const d = doc_();
  if (!d) { listEl().innerHTML = '<div class="hint">No file open.</div>'; return; }
  if (!d.outline) {
    try { d.outline = (await api('/api/outline', { path: d.path })).symbols || []; }
    catch { d.outline = []; }
  }
  drawOutline();
  upgradeOutline(d);
}

/* A language server's document symbols beat regex on every axis, so swap them
   in whenever one answers. Panel only: this never moves the viewport. */
export async function upgradeOutline(d) {
  if (d.outlineLSP || S.lsp.state === 'off' || S.lsp.state === 'failed') return;
  d.outlineLSP = true;
  let j;
  try { j = await api('/api/lsp/symbols', { path: d.path, wait: 20000 }); }
  catch { d.outlineLSP = false; return; }
  setLspState(j);
  if (!j.symbols || !j.symbols.length) { d.outlineLSP = false; return; }
  d.outline = j.symbols;
  d.outlineSource = j.server;
  if (doc_() === d) drawOutline();
}

export function drawOutline() {
  const d = doc_();
  const el = listEl();
  if (!d || !d.outline) { el.innerHTML = '<div class="hint">No symbols found.</div>'; return; }
  const f = ($('#symbols-filter')?.value || '').toLowerCase();
  const items = f ? d.outline.filter(s => s.name.toLowerCase().includes(f)) : d.outline;
  if (!items.length) { el.innerHTML = '<div class="hint">No symbols found.</div>'; return; }
  let base = Infinity;
  for (const s of items) if (s.indent < base) base = s.indent;
  el.innerHTML = (d.outlineSource ? '<div class="hint"><span class="src">' + esc(d.outlineSource) + '</span> · ' + items.length + ' symbols</div>' : '') +
    items.map(s =>
      '<div class="sym" data-n="' + s.line + '" style="padding-left:' + (10 + Math.min(s.indent - base, 16) * 5) + 'px" title="Jump to ' + esc(s.name) + ' at line ' + s.line + '">' +
      '<span class="kd" data-k="' + esc(s.kind) + '">' + esc(kindLabel(s.kind)) + '</span>' +
      '<span class="sn">' + esc(s.name) + '</span><span class="sl">' + s.line + '</span></div>').join('');
}

export const KIND_LABEL = {
  func: 'fn', method: 'fn', fn: 'fn', def: 'fn', defp: 'fn', defmacro: 'mac',
  class: 'cls', struct: 'str', interface: 'int', trait: 'trt', impl: 'impl',
  type: 'typ', typealias: 'typ', enum: 'enm', record: 'rec', object: 'obj',
  const: 'cst', var: 'var', let: 'var', val: 'var',
  module: 'mod', mod: 'mod', namespace: 'ns', defmodule: 'mod', package: 'pkg',
  macro: 'mac', extension: 'ext', protocol: 'int', union: 'uni',
  heading: 'h', sym: '·',
};

export function kindLabel(k) { return KIND_LABEL[k] || k.slice(0, 3); }

export function initOutline() {
  listEl().addEventListener('click', e => {
    const s = e.target.closest('.sym');
    if (!s) return;
    $$('.sym.sel', listEl()).forEach(x => x.classList.remove('sel'));
    s.classList.add('sel');
    const d = doc_(); if (!d) return;
    d.cur = +s.dataset.n; centerLine(d.cur); render(); updateStatus();
    pushHistory(d.path, d.cur);
  });
  $('#symbols-filter').addEventListener('input', drawOutline);
}
