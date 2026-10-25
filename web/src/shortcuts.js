// web/src/shortcuts.js
import { $, $$, esc, S, doc_, isMac, MOD, LH, keyCaps } from './state.js';
import { vp, sizer } from './ui.js';
import { render, paint, toggleWordWrap } from './renderer.js';
import { updateStatus } from './status.js';
import { closeTab, switchTab, reopenClosedTab } from './tabs.js';
import { go } from './history.js';
import { clearLink, hovercard } from './hover.js';
import { openFind, clearFind, findbar, findNextMatch } from './find.js';
import { gotoDefinition, findReferences } from './lsp.js';
import { showSideView, toggleSide } from './sideviews.js';
import { showTerminal } from './term_inspector.js';
import { paneViews } from './paneviews.js';
import { emit } from './bus.js';
import { overlay, openPalette, closePalette } from './palette.js';
import { moveCursor, moveCol, moveWord, caretToEdge } from './cursor.js';
import { showCalls } from './calls.js';
import { SEL_KEYS, runSelectionAction, selectAll, clearSelectAll, copySelectAll, getSelectedRangeInfo } from './selbar.js';
import { openQuickEdit, quickEditOpen, closeQuickEdit } from './quick_edit.js';

import { previewing, togglePreview, previewKey, selectPreview } from './markdown.js';
import { exitMermaidFullscreen } from './mermaid.js';
import { toggleDiff, stepDiffHunk, stepChangedFile } from './diff.js';
import { openSettings, closeSettings, isSettingsOpen } from './settings-form.js';
import { handleVimKeyDown, isVimEnabled, getVimMode, renderVimHelp } from './vim.js';
import { handleImageKey } from './imageview.js';
import { reindexWorkspace } from './panels.js';
import { enterEditMode, exitEditMode, saveActiveFile, editTextarea } from './editview.js';
import { navigateToTips } from './tips.js';

/* Each entry lists alternative combos, written as for keyLabel in state.js so
   they show as ⌘/⌥/⇧ on a Mac and Ctrl/Alt/Shift elsewhere. Browsers keep
   Ctrl+W and Cmd+W for themselves, so Alt+W is the close shortcut shown. */
export const SHORTCUTS = [
  [['Mod+,'], 'Open settings'],
  [['Mod+K'], 'Quick search / palette'], [['Mod+P'], 'Go to file'],
  [['Mod+Shift+P'], 'Command palette'], [['Mod+Shift+O'], 'Symbols in this file'],
  [['Mod+Shift+F'], 'Search in files'], [['Mod+Shift+R'], 'Refresh workspace'], [['Mod+F'], 'Find in file'],
  [['Mod+E'], 'Toggle edit mode'], [['Mod+S'], 'Save file (in edit mode)'],
  [['Mod+G'], 'Go to line'], [['Mod+D'], 'Toggle diff view (git)'], [['Alt+Z'], 'Toggle word wrap'],
  [['[c', ']c'], 'Previous / next diff hunk'], [['[f', ']f'], 'Previous / next changed file'],
  [['Alt+M'], 'Toggle Markdown / table preview'],
  [['Enter', 'Shift+Enter'], 'Next / previous match'],
  [['F12', 'Mod+Click'], 'Go to definition'], [['Shift+F12'], 'Find all references'],
  [['Alt+Shift+H'], 'Call trail (callers / callees)'],
  [['Mod+J'], 'Toggle terminal'],
  [['Alt+Left', 'Alt+Right'], 'Navigate back / forward'], [['Mod+B'], 'Toggle sidebar'],
  [['Alt+W'], 'Close tab'], [['Alt+Shift+T'], 'Reopen closed tab'], [['Ctrl+Tab'], 'Next tab'],
  [['Alt+1…9'], 'Select tab'], [['Double click'], 'Highlight all occurrences'],
  [['Mod+A'], 'Select whole file'],
  [['Mod+C'], 'Copy selection (source / formatted text)'],
  [['Alt+C', 'Alt+A'], 'Copy selection ref / with context'], [['Alt+U'], 'Find usages of selection'],
  [['Mod+K'], 'Quick Edit (with selection) / Quick Open'],
  [['Alt+L'], 'Add selection to Agent Terminal'],
  [['Alt+T'], 'Focus Agent Terminal'],
  [['Right click'], 'Selection actions at the pointer'],
  [['Mod+Home|Mod+Up', 'Mod+End|Mod+Down'], 'Top / bottom of file'],
  [['Home|Mod+Left', 'End|Mod+Right'], 'Start / end of line'],
  [['Left', 'Right'], 'Move caret along the line'],
  [['Esc'], 'Dismiss'],
];

let helpViews = null;

// fillHelp draws the help card's tab view as it comes to the front: tips.js
// lists the tips, vim.js draws its keys and on/off switch.
function fillHelp(view) {
  if (view === 'tips') emit('tips:shown');
  if (view === 'vim') renderVimHelp($('#helpsheet [data-pane-view="vim"]'));
}

// showHelp opens the help card on view: 'keys' (the shortcuts), 'tips' or
// 'vim'.
export function showHelp(view = 'keys') {
  const ver = $('#help-ver');
  if (S.meta?.version) {
    ver.textContent = 'v' + S.meta.version;
    ver.hidden = false;
  }
  $('#helpsheet').hidden = false;
  if (helpViews.current() === view) fillHelp(view);
  else helpViews.set(view);
}

export function closeHelp() {
  $('#helpsheet').hidden = true;
}

export const inField = el => el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA');

export function initShortcuts() {
  // Ln / Col, as on the agents page, opens Go to; an image's zoom there does not.
  $('#st-pos')?.addEventListener('click', () => { if (!doc_()?.isImage) openPalette('line'); });
  $('#btn-settings')?.addEventListener('click', () => openSettings('ui'));
  $('#btn-help')?.addEventListener('click', () => navigateToTips());
  $('#st-ver')?.addEventListener('click', () => showHelp());
  $('#help-keys').innerHTML = SHORTCUTS.map(([combos, v]) =>
    '<dt>' + combos.map(keyCaps).filter(Boolean).join('<span class="key-or">/</span>') + '</dt><dd>' + esc(v) + '</dd>').join('');
  helpViews = paneViews({
    host: $('#helpsheet .help-body'),
    bars: [$('#helpsheet .help-tabs')],
    fallback: 'keys',
    onChange: view => { if (!$('#helpsheet').hidden) fillHelp(view); },
  });
  $('#help-close').addEventListener('click', closeHelp);
  $('#helpsheet').addEventListener('click', e => { if (e.target === e.currentTarget) closeHelp(); });

  // Footer quick action buttons
  $('#footer-actions')?.addEventListener('click', e => {
    const btn = e.target.closest('.footer-btn');
    if (!btn) return;
    const act = btn.dataset.action;
    if (act === 'goto') openPalette('line');
    else if (act === 'wrap') toggleWordWrap();
  });

  addEventListener('keydown', e => {
    // The terminal and the Quick Edit box own their keys: a shell needs Ctrl+W/D/K/E,
    // and an agent's TUI reads Esc; none of them should reach the viewer.
    if (/** @type {Element|null} */ (e.target)?.closest?.('#editor-term-container, #quick-edit-container')) return;
    const mod = e[MOD];

    if (e.key === 'Escape') {
      if (exitMermaidFullscreen()) return;
      if (quickEditOpen()) { closeQuickEdit(); return; }
      const lb = $('#img-lightbox');
      if (lb && !lb.hidden) { lb.hidden = true; return; }
      // The help card can open over settings (its Vim action), so it closes first.
      if (!$('#helpsheet').hidden) { closeHelp(); return; }
      if (isVimEnabled() && handleVimKeyDown(e)) return;
      if (isSettingsOpen()) { closeSettings(); return; }
      if (!overlay.hidden) { closePalette(); return; }
      if (!hovercard.hidden) { clearLink(); return; }
      if (!findbar.hidden) {
        clearFind();
        const d = doc_();
        if (d?.isEditing && editTextarea) editTextarea.focus();
        return;
      }
      if (S.selAll) { clearSelectAll(); return; }
      if (S.occ) { S.occ = null; paint(); return; }
      if (inField(document.activeElement)) /** @type {HTMLElement} */ (document.activeElement).blur();
      return;
    }

    if (mod && (e.key === ',' || e.key === '<')) {
      e.preventDefault();
      openSettings('ui');
      return;
    }

    // Quick Edit on a code selection, else Quick Open.
    if (mod && (e.key === 'k' || e.key === 'K')) {
      e.preventDefault();
      const selInfo = getSelectedRangeInfo();
      if (selInfo?.isEditor && !e.shiftKey) {
        openQuickEdit(selInfo);
        return;
      }
      openPalette(e.shiftKey ? 'command' : 'file');
      return;
    }
    if (e.altKey && !mod && !e.shiftKey && e.code === 'KeyT') {
      e.preventDefault();
      showTerminal();
      return;
    }

    if (mod && (e.key === 'j' || e.key === 'J')) {
      e.preventDefault();
      toggleSide('right');
      return;
    }

    if (mod && e.shiftKey && (e.key === 'P' || e.key === 'p')) { e.preventDefault(); openPalette('command'); return; }
    if (mod && e.shiftKey && (e.key === 'O' || e.key === 'o')) { e.preventDefault(); showSideView('symbols'); return; }
    if (mod && e.shiftKey && (e.key === 'F' || e.key === 'f')) { e.preventDefault(); showSideView('search'); return; }
    if (mod && e.shiftKey && (e.key === 'R' || e.key === 'r')) { e.preventDefault(); reindexWorkspace(); return; }
    if (mod && !e.shiftKey && (e.key === 'p' || e.key === 'P')) { e.preventDefault(); openPalette('file'); return; }
    if (mod && (e.key === 'g' || e.key === 'G')) { e.preventDefault(); openPalette('line'); return; }
    if (mod && (e.key === 'f' || e.key === 'F')) { e.preventDefault(); openFind(S.lastWord); return; }
    if (mod && (e.key === 's' || e.key === 'S')) {
      e.preventDefault();
      saveActiveFile();
      return;
    }
    if (mod && !e.shiftKey && (e.key === 'e' || e.key === 'E')) {
      const d = doc_();
      if (d && !d.isImage && !d.deleted && !d.diffRef) {
        e.preventDefault();
        if (d.isEditing) exitEditMode();
        else enterEditMode();
        return;
      }
    }
    if (mod && (e.key === 'b' || e.key === 'B')) { e.preventDefault(); toggleSide('left'); return; }
    // Diff view of the open file (git only; fails quiet when git is off).
    if (mod && !e.shiftKey && (e.key === 'd' || e.key === 'D')) { if (S.meta?.git) { e.preventDefault(); toggleDiff(); } return; }
    // Alt shortcuts match e.code: on a Mac, Option+letter types a symbol, so e.key is not the letter.
    if ((mod && (e.key === 'w' || e.key === 'W')) || (e.altKey && e.code === 'KeyW')) {
      e.preventDefault();
      e.stopPropagation();
      if (S.active >= 0) closeTab(S.active);
      return;
    }
    if (e.altKey && e.shiftKey && !mod && e.code === 'KeyT') { e.preventDefault(); reopenClosedTab(); return; }
    if (e.key === 'F12') {
      e.preventDefault();
      if (e.shiftKey) findReferences(); else gotoDefinition();
      return;
    }
    if (e.altKey && e.key === 'ArrowLeft') { e.preventDefault(); go(-1); return; }
    if (e.altKey && e.key === 'ArrowRight') { e.preventDefault(); go(1); return; }
    if (e.ctrlKey && e.key === 'Tab') {
      e.preventDefault();
      if (S.tabs.length > 1) switchTab((S.active + (e.shiftKey ? -1 : 1) + S.tabs.length) % S.tabs.length);
      return;
    }
    if (e.altKey && e.shiftKey && e.code === 'KeyH') { e.preventDefault(); showCalls(); return; }
    if (e.altKey && !mod && !e.shiftKey && /^Digit[1-9]$/.test(e.code)) { e.preventDefault(); switchTab(+e.code.slice(5) - 1); return; }
    // Selection actions, live only while the status bar is showing them.
    if (e.altKey && !mod && !e.shiftKey && SEL_KEYS[e.code] && runSelectionAction(SEL_KEYS[e.code])) { e.preventDefault(); return; }
    if (e.altKey && e.code === 'KeyZ') {
      e.preventDefault();
      toggleWordWrap();
      return;
    }

    if (e.altKey && !mod && !e.shiftKey && e.code === 'KeyM') {
      e.preventDefault();
      togglePreview();
      return;
    }

    if (!mod && e.key === 'Enter' && !findbar.hidden) {
      e.preventDefault();
      findNextMatch(e.shiftKey ? -1 : 1);
      return;
    }

    if (inField(document.activeElement)) return;

    // [c / ]c (prev/next diff hunk) and [f / ]f (prev/next changed file) chords
    if (!mod && !e.altKey && !e.shiftKey) {
      if (e.key === '[' || e.key === ']') {
        S._bracketPrefix = e.key;
        S._bracketTime = performance.now();
        return;
      }
      if (S._bracketPrefix && performance.now() - (S._bracketTime || 0) < 900) {
        const dir = S._bracketPrefix === ']' ? 1 : -1;
        S._bracketPrefix = '';
        if (e.key === 'c') {
          e.preventDefault();
          stepDiffHunk(dir);
          return;
        }
        if (e.key === 'f') {
          e.preventDefault();
          stepChangedFile(dir);
          return;
        }
      } else {
        S._bracketPrefix = '';
      }
    }

    // Select all takes the open file only, never the sidebar or status bar around it.
    const plainMod = mod && !e.shiftKey && !e.altKey;
    if (plainMod && (e.key === 'a' || e.key === 'A')) { e.preventDefault(); if (previewing()) selectPreview(); else selectAll(); return; }
    if (plainMod && (e.key === 'c' || e.key === 'C')) {
      if (copySelectAll()) { e.preventDefault(); return; }
      const info = getSelectedRangeInfo();
      if (info) {
        e.preventDefault();
        runSelectionAction(info.isMarkdownPreview ? 'copy-preview-text' : 'copy-source');
        return;
      }
    }

    if (handleVimKeyDown(e)) return;

    if (e.key === '?') { e.preventDefault(); showHelp(); return; }
    const d = doc_();
    if (!d) return;
    if (d.isImage) { if (handleImageKey(e)) e.preventDefault(); return; }
    if (previewing(d)) { if (previewKey(e)) e.preventDefault(); return; }
    const toTop = () => { vp.scrollTop = 0; d.cur = 1; render(); updateStatus(); };
    const toBottom = () => { vp.scrollTop = sizer.offsetHeight; d.cur = d.total; render(); updateStatus(); };
    const shift = e.shiftKey;
    if (mod && e.key === 'Home') { e.preventDefault(); if (shift) caretToEdge(false, true); else toTop(); return; }
    if (mod && e.key === 'End') { e.preventDefault(); if (shift) caretToEdge(true, true); else toBottom(); return; }
    // A Mac keyboard has no Home or End: Cmd with the arrows does their job there.
    if (isMac && mod && e.key === 'ArrowUp') { e.preventDefault(); toTop(); return; }
    if (isMac && mod && e.key === 'ArrowDown') { e.preventDefault(); toBottom(); return; }
    if (isMac && mod && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) { e.preventDefault(); caretToEdge(e.key === 'ArrowRight', shift); return; }
    if (mod && !isMac && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) { e.preventDefault(); moveWord(e.key === 'ArrowRight' ? 1 : -1, shift); return; }
    if (isMac && e.altKey && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) { e.preventDefault(); moveWord(e.key === 'ArrowRight' ? 1 : -1, shift); return; }
    if (!mod && (e.key === 'ArrowDown' || e.key === 'j')) { e.preventDefault(); moveCursor(1, shift); return; }
    if (!mod && (e.key === 'ArrowUp' || e.key === 'k')) { e.preventDefault(); moveCursor(-1, shift); return; }
    if (!mod && !e.altKey && e.key === 'ArrowLeft') { e.preventDefault(); moveCol(-1, shift); return; }
    if (!mod && !e.altKey && e.key === 'ArrowRight') { e.preventDefault(); moveCol(1, shift); return; }
    if (!mod && (e.key === 'Home' || e.key === 'End')) { e.preventDefault(); caretToEdge(e.key === 'End', shift); return; }
    if (e.key === 'PageDown') { e.preventDefault(); moveCursor(Math.floor(vp.clientHeight / LH) - 2, shift); return; }
    if (e.key === 'PageUp') { e.preventDefault(); moveCursor(-(Math.floor(vp.clientHeight / LH) - 2), shift); return; }
  }, { capture: true });


}
