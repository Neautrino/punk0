// web/src/refs.js
// The References mode's references list (#refs-list): every use of the
// symbol at the caret, from the language server, else a whole-word text
// search. calls.js fills the same mode's call trail.
import { $, $$, esc, S, doc_, api } from './state.js';
import { updateStatus, setStatusNote } from './status.js';
import { openFile } from './tabs.js';
import { displayPath } from './search.js';
import { groupHits, flashFind, canAskServer, lspCall, positionNow } from './lsp.js';
import { showSideView, setRefsMode } from './sideviews.js';

const targetEl = () => $('#refs-target');
const badgeEl = () => $('#refs-badge');
const listEl = () => $('#refs-list');

function renderRefs(word, hits, server) {
  targetEl().textContent = word;
  badgeEl().textContent = hits.length;
  if (!hits.length) {
    listEl().innerHTML = '<div class="hint">No references found for "<b>' + esc(word) + '</b>".</div>';
    return;
  }
  const head = hits.length + ' reference' + (hits.length === 1 ? '' : 's') +
    (server ? ' · ' + esc(server) : ' · text search');
  let html = '<div class="hint">' + head + '</div>';
  for (const f of groupHits(hits)) {
    html += '<div class="rfile" data-toggle="r-' + esc(f.path) + '" title="' + esc(f.path) + '">' +
      '<span class="ar">&#9660;</span>' +
      '<span class="fp">' + esc(displayPath(f.path)) + '</span>' +
      '<span class="cnt">' + f.matches.length + '</span></div>' +
      '<div data-group="r-' + esc(f.path) + '">';
    for (const m of f.matches) {
      html += '<div class="rline" data-p="' + esc(f.path) + '" data-n="' + m.line + '" title="Jump to ' + esc(f.path) + ':' + m.line + '">' +
        '<span class="rn">' + m.line + '</span><span class="rt">' +
        esc(m.pre) + '<mark>' + esc(m.mid || word) + '</mark>' + esc(m.post) + '</span></div>';
    }
    html += '</div>';
  }
  listEl().innerHTML = html;
}

export async function inspectReferences(arg) {
  const d = doc_();
  const at = (arg && arg.word) ? arg : positionNow(typeof arg === 'string' ? arg : S.lastWord);
  if (!d || !at || !at.word) return;

  showSideView('refs');
  setRefsMode('refs');
  targetEl().textContent = at.word;
  badgeEl().textContent = '…';
  listEl().innerHTML = '<div class="hint">Finding references for "' + esc(at.word) + '"…</div>';

  if (canAskServer(at)) {
    setStatusNote('references to ' + at.word + '…', 8000);
    try {
      const j = await lspCall('refs', at, 30000);
      updateStatus();
      if (j && j.hits && j.hits.length) {
        setStatusNote('');
        renderRefs(at.word, j.hits, j.server);
        return;
      }
    } catch {
      updateStatus();
    }
  }

  // Fallback: search workspace text for whole word
  setStatusNote('searching references to ' + at.word + '…', 8000);
  try {
    const j = await api('/api/search', { q: at.word, word: true, case: true });
    updateStatus();
    setStatusNote('');
    const hits = [];
    for (const f of j.results || []) {
      for (const m of f.matches) hits.push({ path: f.path, line: m.line, pre: m.pre, mid: m.mid, post: m.post });
    }
    renderRefs(at.word, hits, '');
  } catch (err) {
    updateStatus();
    setStatusNote('');
    listEl().innerHTML = '<div class="hint">Search error: ' + esc(err.message) + '</div>';
  }
}

export function initRefs() {
  listEl().addEventListener('click', e => {
    const t = e.target.closest('[data-toggle]');
    if (t) {
      const g = listEl().querySelector('[data-group="' + CSS.escape(t.dataset.toggle) + '"]');
      if (!g) return;
      const hidden = g.style.display === 'none';
      g.style.display = hidden ? '' : 'none';
      const ar = $('.ar', t);
      if (ar) ar.innerHTML = hidden ? '&#9660;' : '&#9654;';
      return;
    }
    const r = e.target.closest('.rline');
    if (r) {
      $$('.rline.sel', listEl()).forEach(x => x.classList.remove('sel'));
      r.classList.add('sel');
      openFile(r.dataset.p, { line: +r.dataset.n });
      const word = targetEl().textContent;
      if (word) flashFind(word);
    }
  });
}
