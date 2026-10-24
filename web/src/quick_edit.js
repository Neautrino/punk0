// web/src/quick_edit.js
// Quick Edit (Mod+K on a code selection): an instruction box above the
// selected lines, a one-shot harness run (POST /api/quick-edit) that returns
// their replacement, and an inline preview in the code (renderer.js): the
// lines struck out, the replacement under them, Accept / Reject below that.
// Nothing is written until Accept (POST /api/file/replace), which refuses
// lines that changed on disk since the preview was made from them.
import { $, doc_, apiPostJson, esc, keyLabel } from './state.js';
import { on } from './bus.js';
import { showToast, vp } from './ui.js';
import { reloadOpenTabs } from './tabs.js';
import { rowFor, setInlinePreview } from './renderer.js';

let containerEl = null;
let target = null;      // { path, l1, l2, original } as read from disk
let replacement = null; // the harness's lines, once it answered
let inflight = null;    // AbortController of the running harness request
let accepting = false;

/* The selected lines whole, as they are on disk now: the editor's own lines
   are highlighted HTML and only partly loaded, and Accept compares against
   exactly this text. */
async function readLines(path, l1, l2) {
  const u = new URL('api/raw?path=' + encodeURIComponent(path), document.baseURI);
  const r = await fetch(u, { cache: 'no-store' });
  if (!r.ok) throw new Error('could not read ' + path);
  const lines = (await r.text()).split(/\r?\n/);
  return lines.slice(l1 - 1, Math.min(l2, lines.length)).join('\n');
}

/* Above the selected lines, where it never covers them or the preview that
   grows below them; below the preview when the window has no room above.
   Placed after showing, to measure it. */
function place() {
  if (!target || !containerEl || containerEl.hidden) return;
  const sel = window.getSelection();
  const range = sel?.rangeCount && !sel.isCollapsed ? sel.getRangeAt(0).getBoundingClientRect() : null;
  const first = range || rowFor(target.l1)?.getBoundingClientRect();
  const last = rowFor(target.l2)?.getBoundingClientRect() || range || first;
  const h = containerEl.offsetHeight;
  let top = first ? first.top - h - 6 : 120;
  if (top < 40 && last) top = last.bottom + 6;
  containerEl.style.top = Math.round(Math.max(40, top)) + 'px';
  containerEl.style.left = Math.round(Math.max(20, Math.min(innerWidth - containerEl.offsetWidth - 20, first ? first.left : 240))) + 'px';
}

export const quickEditOpen = () => !!containerEl && !containerEl.hidden;

/** @param {{path: string, l1: number, l2: number, isEditor?: boolean}} info a code selection */
export async function openQuickEdit(info) {
  const d = doc_();
  if (!info?.isEditor || !d || d.path !== info.path) {
    showToast('!', 'Select code in the source view to quick edit');
    return;
  }
  if (/** @type {any} */ (d).isEditing) {
    showToast('!', 'Save or leave edit mode before a quick edit');
    return;
  }
  let original;
  try {
    original = await readLines(info.path, info.l1, info.l2);
  } catch (e) {
    showToast('!', e.message);
    return;
  }
  closeQuickEdit();
  target = { path: info.path, l1: info.l1, l2: info.l2, original };
  ensureDOM();
  showErr('');
  setBusy(false);
  setHint('');
  containerEl.hidden = false;
  place();
  const input = /** @type {HTMLTextAreaElement} */ ($('#quick-edit-input'));
  input.value = '';
  input.focus();
}

export function closeQuickEdit() {
  inflight?.abort(); // the server stops the harness with the request
  inflight = null;
  if (replacement !== null) setInlinePreview(null);
  target = null;
  replacement = null;
  if (containerEl) containerEl.hidden = true;
}

// readOnly, not disabled: a disabled field drops focus to the page, and then
// Esc no longer reaches the box.
function setBusy(busy) {
  const submit = /** @type {HTMLButtonElement} */ ($('#quick-edit-submit'));
  submit.disabled = busy;
  submit.textContent = busy ? 'Editing…' : 'Edit ↵';
  /** @type {HTMLTextAreaElement} */ ($('#quick-edit-input')).readOnly = busy;
}

function showErr(msg) {
  const el = $('#quick-edit-err');
  el.textContent = msg;
  el.hidden = !msg;
}

function setHint(msg) {
  const el = $('#quick-edit-hint');
  el.textContent = msg;
  el.hidden = !msg;
}

async function submitEdit() {
  if (!target || inflight) return;
  const input = /** @type {HTMLTextAreaElement} */ ($('#quick-edit-input'));
  const instruction = input.value.trim();
  if (!instruction) { input.focus(); return; }
  const mine = target;
  const ctl = new AbortController();
  inflight = ctl;
  showErr('');
  setBusy(true);
  try {
    const res = await apiPostJson('/api/quick-edit', {
      path: mine.path, l1: mine.l1, l2: mine.l2, selection: mine.original, instruction,
    }, { signal: ctl.signal });
    if (target !== mine) return; // closed or reopened meanwhile
    replacement = typeof res.replacement === 'string' ? res.replacement : '';
    showPreview(mine, replacement);
  } catch (e) {
    if (ctl.signal.aborted || target !== mine) return;
    showErr(e.message || 'Quick Edit failed');
  } finally {
    if (inflight === ctl) inflight = null;
    if (target === mine) setBusy(false);
  }
}

function showPreview(t, rep) {
  const lines = rep === ''
    ? '<div class="qe-line qe-note">the selected lines are deleted</div>'
    : rep.split('\n').map(l => `<div class="qe-line qe-add"><span class="qe-sign">+</span><span class="qe-code">${esc(l)}</span></div>`).join('');
  setInlinePreview({
    path: t.path, l1: t.l1, l2: t.l2,
    html: `<div class="qe-inline">${lines}<div class="qe-inline-bar">` +
      `<button class="qe-btn-accept" data-qe="accept">✓ Accept <kbd>${esc(keyLabel('Mod+Enter'))}</kbd></button>` +
      `<button class="qe-btn-reject" data-qe="reject">✕ Reject <kbd>Esc</kbd></button></div></div>`,
  });
  setHint(`${keyLabel('Mod+Enter')} to accept, Esc to reject, ${keyLabel('Enter')} to try again`);
  // Once painted: bring the preview into view, then move the box clear of it.
  requestAnimationFrame(() => requestAnimationFrame(() => {
    rowFor(t.l2)?.querySelector('.qe-inline')?.scrollIntoView({ block: 'nearest' });
    place();
  }));
}

async function acceptEdit() {
  if (!target || replacement === null || accepting) return;
  const mine = target;
  accepting = true;
  showErr('');
  try {
    await apiPostJson('/api/file/replace', { ...mine, replacement });
  } catch (e) {
    // 409: the lines changed on disk since the preview; nothing was written.
    if (target === mine) showErr(e.message);
    return;
  } finally {
    accepting = false;
  }
  closeQuickEdit();
  await reloadOpenTabs();
  showToast('✓', 'Edit applied to ' + mine.path);
}

function ensureDOM() {
  if (containerEl) return;
  containerEl = document.createElement('div');
  containerEl.id = 'quick-edit-container';
  containerEl.className = 'quick-edit-container';
  containerEl.hidden = true;
  containerEl.innerHTML = `
    <div class="quick-edit-box">
      <div class="quick-edit-prompt-row">
        <span class="qe-prompt-icon">✦</span>
        <textarea id="quick-edit-input" class="qe-input" rows="1" spellcheck="false" placeholder="Describe the change (${esc(keyLabel('Enter'))} to run, Esc to cancel)"></textarea>
        <button id="quick-edit-submit" class="qe-btn-submit" title="Run (${esc(keyLabel('Enter'))})">Edit ↵</button>
        <button id="quick-edit-close" class="qe-btn-close" title="Cancel (Esc)">✕</button>
      </div>
      <div id="quick-edit-hint" class="qe-hint" hidden></div>
      <div id="quick-edit-err" class="qe-err" hidden></div>
    </div>`;
  document.body.appendChild(containerEl);

  $('#quick-edit-submit').addEventListener('click', submitEdit);
  $('#quick-edit-close').addEventListener('click', closeQuickEdit);
  containerEl.addEventListener('keydown', e => {
    if (e.key === 'Escape') {
      e.preventDefault();
      closeQuickEdit();
    } else if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      if (replacement !== null) acceptEdit(); else submitEdit();
    } else if (e.key === 'Enter' && !e.shiftKey && e.target === $('#quick-edit-input')) {
      e.preventDefault();
      submitEdit();
    }
  });

  // The preview's buttons live in the code rows, which are redrawn on every
  // paint: listen on the document. A press keeps focus in the box.
  document.addEventListener('mousedown', e => {
    if (/** @type {Element} */ (e.target)?.closest?.('.qe-inline')) e.preventDefault();
  });
  document.addEventListener('click', e => {
    const btn = /** @type {HTMLElement|null} */ (/** @type {Element} */ (e.target)?.closest?.('.qe-inline [data-qe]'));
    if (!btn) return;
    if (btn.dataset.qe === 'accept') acceptEdit(); else closeQuickEdit();
  });
  // A preview belongs to its file: switching away ends it.
  on('tab:activated', ({ doc }) => { if (target && doc?.path !== target.path) closeQuickEdit(); });
  vp?.addEventListener('scroll', place, { passive: true });
}
