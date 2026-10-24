// web/src/linecomment.js
// The Comment button on a PR review's diff line numbers (difftable.js
// reviewButton): opens the selection actions menu aimed at that line, with
// Add Review Comment among them. Only rows GitHub knows about carry it.
import { doc_ } from './state.js';
import { openLineMenu, getSelectedRangeInfo } from './selbar.js';

// The selection as it stood when the button was pressed: a press can collapse it
// before the click lands, and the menu should act on what was selected.
let pressedSel = null;

export function initLineComment() {
  document.addEventListener('mousedown', e => {
    const btn = /** @type {HTMLElement|null} */ (e.target)?.closest?.('.line-btn');
    pressedSel = btn ? getSelectedRangeInfo() : null;
    if (btn) e.preventDefault(); // keep the selection alive
  }, true);
  document.addEventListener('click', e => {
    const target = /** @type {HTMLElement|null} */ (e.target);
    const btn = target?.closest('.line-btn');
    if (!btn) return;
    e.preventDefault();
    e.stopPropagation();
    const sel = pressedSel;
    pressedSel = null;
    handleLineBtnClick(btn, sel);
  });
}

function handleLineBtnClick(btn, sel) {
  const d = doc_();
  if (!d) return;

  const diffRow = btn.closest('.diff-row, .diff-side');
  const at = btn.getBoundingClientRect();

  // A selection covering the clicked line is what the menu acts on, so its
  // actions carry the whole start-end range rather than just this line.
  const clicked = +(btn.dataset.l || btn.closest('.row, .diff-row, .diff-side')?.dataset.l || 0);
  if (sel && sel.path === d.path && (!clicked || (clicked >= sel.l1 && clicked <= sel.l2))) {
    openLineMenu(sel, at.right + 4, at.top);
    return;
  }

  if (diffRow) openLineMenu(diffLineInfo(diffRow, d.path), at.right + 4, at.top);
}

// Mirrors selbar.js's diffSelection() for a single row instead of a range: a
// pure deletion has no line on disk, so it's anchored on the old side with
// both delL/l set (pr.js picks whichever its side needs); everything else --
// context or an addition -- lives on the new side.
function diffLineInfo(diffRow, path) {
  const text = diffRow.querySelector('.diff-code')?.textContent || '';
  const isOldOnly = diffRow.dataset.oldL !== undefined && diffRow.dataset.l === undefined;
  if (isOldOnly) {
    const old = +diffRow.dataset.oldL || 1;
    return { text, l1: old, l2: old, delL1: old, delL2: old, path, fromDiff: true, side: 'LEFT' };
  }
  const line = +(diffRow.dataset.l || diffRow.dataset.at || 1);
  return { text, l1: line, l2: line, path, fromDiff: true, side: 'RIGHT' };
}
