// web/src/selpill.js
// The floating pill over a code selection: Quick Edit, Add to Chat, Copy Ref.
// selbar.js shows and hides it with the selection and runs the action picked.
import { keyLabel } from './state.js';
import { vp } from './ui.js';
import { calculatePillPosition } from './selpill-util.js';

const ACTIONS = [
  { act: 'quick-edit', label: '✦ Quick Edit', keys: 'Mod+K', primary: true },
  { act: 'add-to-chat', label: 'Add to Chat', keys: 'Alt+L' },
  { act: 'copy-ref', label: 'Copy Ref', keys: 'Alt+C' },
];

let pillEl = null;
let runAction = null;

function ensurePill() {
  if (pillEl) return;
  pillEl = document.createElement('div');
  pillEl.id = 'floating-sel-pill';
  pillEl.className = 'floating-sel-pill';
  pillEl.hidden = true;
  for (const a of ACTIONS) {
    const btn = document.createElement('button');
    btn.className = 'sel-pill-btn' + (a.primary ? ' pill-btn-primary' : '');
    btn.dataset.act = a.act;
    btn.title = `${a.label.replace('✦ ', '')} (${keyLabel(a.keys)})`;
    btn.append(a.label + ' ');
    const kbd = document.createElement('kbd');
    kbd.textContent = keyLabel(a.keys);
    btn.append(kbd);
    pillEl.append(btn);
  }
  // A press anywhere else collapses the selection; the click must still see it.
  pillEl.addEventListener('mousedown', e => e.preventDefault());
  pillEl.addEventListener('click', e => {
    const btn = /** @type {HTMLElement|null} */ (/** @type {Element} */ (e.target)?.closest('[data-act]'));
    if (!btn || !runAction) return;
    const run = runAction;
    hideSelPill();
    run(btn.dataset.act);
  });
  document.body.appendChild(pillEl);
  // The rows move under a fixed pill; it comes back with the next selection.
  vp?.addEventListener('scroll', hideSelPill, { passive: true });
}

/**
 * @param {{isEditor?: boolean}} info the selection
 * @param {(act: string) => void} onAction runs an action on it
 */
export function showSelPill(info, onAction) {
  const sel = window.getSelection();
  if (!info?.isEditor || !sel || sel.isCollapsed || !sel.rangeCount) return hideSelPill();
  const rect = sel.getRangeAt(0).getBoundingClientRect();
  if (!rect.width && !rect.height) return hideSelPill();
  ensurePill();
  runAction = onAction;
  pillEl.hidden = false; // measured while shown
  const pos = calculatePillPosition(rect, { width: innerWidth }, { width: pillEl.offsetWidth, height: pillEl.offsetHeight });
  pillEl.style.left = pos.left + 'px';
  pillEl.style.top = pos.top + 'px';
}

export function hideSelPill() {
  runAction = null;
  if (pillEl) pillEl.hidden = true;
}
