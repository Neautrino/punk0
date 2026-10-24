// web/src/quick_edit.js
import { $, doc_, api, esc, keyLabel } from './state.js';
import { showToast } from './ui.js';
import { openFile } from './tabs.js';
import { getSelectedRangeInfo } from './selbar.js';

let containerEl = null;
let currentTarget = null;
let isBusy = false;
let currentReplacement = null;

function calculateBoxPosition(selectionRect) {
  const top = Math.max(50, Math.min(window.innerHeight - 250, selectionRect.top - 12));
  const left = Math.max(20, Math.min(window.innerWidth - 420, selectionRect.left));
  return { top: Math.round(top), left: Math.round(left) };
}

export function openQuickEdit(info) {
  const sel = info || getSelectedRangeInfo();
  const d = doc_();
  if (!sel || !d || sel.isMarkdownPreview || sel.fromDiff) {
    showToast('!', 'Select lines in source code to quick edit');
    return;
  }

  // Expand to full line range
  const l1 = Math.max(1, sel.l1);
  const l2 = Math.min(d.lines ? d.lines.length : sel.l2, sel.l2);
  const originalLines = d.lines ? d.lines.slice(l1 - 1, l2).join('\n') : sel.text;

  currentTarget = {
    path: d.path,
    l1,
    l2,
    original: originalLines,
  };
  currentReplacement = null;
  isBusy = false;

  ensureDOM();

  const range = window.getSelection()?.rangeCount ? window.getSelection().getRangeAt(0) : null;
  const rect = range ? range.getBoundingClientRect() : { top: 120, left: 240 };
  const pos = calculateBoxPosition(rect);

  containerEl.style.top = `${pos.top}px`;
  containerEl.style.left = `${pos.left}px`;
  containerEl.hidden = false;

  const input = $('#quick-edit-input');
  if (input) {
    input.value = '';
    input.disabled = false;
    input.focus();
  }

  $('#quick-edit-diff').hidden = true;
  $('#quick-edit-err').hidden = true;
  $('#quick-edit-prompt-row').hidden = false;
  $('#quick-edit-submit').disabled = false;
  $('#quick-edit-submit').textContent = 'Edit ↵';
}

export function closeQuickEdit() {
  if (containerEl) {
    containerEl.hidden = true;
  }
  currentTarget = null;
  currentReplacement = null;
  isBusy = false;
}

async function submitEdit() {
  if (isBusy || !currentTarget) return;
  const input = /** @type {HTMLTextAreaElement|null} */ ($('#quick-edit-input'));
  const instruction = input?.value.trim();
  if (!instruction) {
    input?.focus();
    return;
  }

  isBusy = true;
  const submitBtn = $('#quick-edit-submit');
  const errEl = $('#quick-edit-err');
  if (errEl) errEl.hidden = true;
  if (submitBtn) {
    submitBtn.disabled = true;
    submitBtn.textContent = 'Editing…';
  }
  if (input) input.disabled = true;

  try {
    const res = await api('/api/quick-edit', {
      path: currentTarget.path,
      l1: currentTarget.l1,
      l2: currentTarget.l2,
      selection: currentTarget.original,
      instruction,
    });

    if (!res.ok || !res.replacement) {
      throw new Error(res.error || 'Failed to generate edit');
    }

    currentReplacement = res.replacement;
    renderDiff(currentTarget.original, currentReplacement);
  } catch (err) {
    if (errEl) {
      errEl.textContent = err.message || 'Quick Edit failed';
      errEl.hidden = false;
    }
  } finally {
    isBusy = false;
    if (submitBtn) {
      submitBtn.disabled = false;
      submitBtn.textContent = 'Edit ↵';
    }
    if (input) input.disabled = false;
  }
}

function renderDiff(orig, rep) {
  const diffBox = $('#quick-edit-diff');
  const diffLinesEl = $('#quick-edit-diff-lines');
  if (!diffBox || !diffLinesEl) return;

  const origLines = orig.split('\n');
  const repLines = rep.split('\n');

  let html = '';
  for (const l of origLines) {
    html += `<div class="qe-line qe-del"><span class="qe-sign">-</span><span class="qe-code">${esc(l)}</span></div>`;
  }
  for (const l of repLines) {
    html += `<div class="qe-line qe-add"><span class="qe-sign">+</span><span class="qe-code">${esc(l)}</span></div>`;
  }

  diffLinesEl.innerHTML = html;
  diffBox.hidden = false;
  $('#btn-qe-accept')?.focus();
}

async function acceptEdit() {
  if (!currentTarget || currentReplacement === null) return;
  const acceptBtn = $('#btn-qe-accept');
  const errEl = $('#quick-edit-err');
  if (errEl) errEl.hidden = true;
  if (acceptBtn) acceptBtn.disabled = true;

  try {
    const res = await api('/api/file/replace', {
      path: currentTarget.path,
      l1: currentTarget.l1,
      l2: currentTarget.l2,
      original: currentTarget.original,
      replacement: currentReplacement,
    });

    if (!res.ok) {
      throw new Error(res.error || 'Conflict: file was modified on disk');
    }

    const savedTarget = currentTarget;
    closeQuickEdit();
    await openFile(savedTarget.path, { line: savedTarget.l1 });
    showToast('✓', 'Edit applied');
  } catch (err) {
    if (errEl) {
      errEl.textContent = err.message;
      errEl.hidden = false;
    }
  } finally {
    if (acceptBtn) acceptBtn.disabled = false;
  }
}

function ensureDOM() {
  if (containerEl) return;
  containerEl = document.createElement('div');
  containerEl.id = 'quick-edit-container';
  containerEl.className = 'quick-edit-container';
  containerEl.hidden = true;

  containerEl.innerHTML = `
    <div class="quick-edit-box" id="quick-edit-box">
      <div class="quick-edit-prompt-row" id="quick-edit-prompt-row">
        <span class="qe-prompt-icon">✦</span>
        <textarea id="quick-edit-input" class="qe-input" rows="1" placeholder="Describe change... (${keyLabel('Enter')} to submit, ${keyLabel('Esc')} to cancel)"></textarea>
        <button id="quick-edit-submit" class="qe-btn-submit" title="Generate Edit (${keyLabel('Enter')})">Edit ↵</button>
        <button id="quick-edit-close" class="qe-btn-close" title="Cancel (${keyLabel('Esc')})">✕</button>
      </div>
      <div id="quick-edit-err" class="qe-err" hidden></div>
      <div id="quick-edit-diff" class="quick-edit-diff" hidden>
        <div id="quick-edit-diff-lines" class="qe-diff-lines"></div>
        <div class="qe-review-bar">
          <button id="btn-qe-accept" class="qe-btn-accept" title="Accept (${keyLabel('Mod+Enter')})">✓ Accept (${keyLabel('Mod+Enter')})</button>
          <button id="btn-qe-reject" class="qe-btn-reject" title="Reject (${keyLabel('Esc')})">✕ Reject (${keyLabel('Esc')})</button>
        </div>
      </div>
    </div>
  `;

  document.body.appendChild(containerEl);

  const input = $('#quick-edit-input');
  input?.addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submitEdit();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      closeQuickEdit();
    }
  });

  $('#quick-edit-submit')?.addEventListener('click', () => submitEdit());
  $('#quick-edit-close')?.addEventListener('click', () => closeQuickEdit());
  $('#btn-qe-accept')?.addEventListener('click', () => acceptEdit());
  $('#btn-qe-reject')?.addEventListener('click', () => closeQuickEdit());

  // Global Esc or Mod+Enter for diff review
  containerEl.addEventListener('keydown', e => {
    if (e.key === 'Escape') {
      e.preventDefault();
      closeQuickEdit();
    } else if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault();
      if (!currentReplacement) submitEdit();
      else acceptEdit();
    }
  });
}
