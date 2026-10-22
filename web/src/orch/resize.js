// web/src/orch/resize.js
// The side panes: their widths, dragged on the borders between panes, and
// whether each is shown. Widths live in two CSS variables on #wb-app
// (--wb-left, --wb-right); the left one also sizes the status bar's left
// section, so the bar follows the sidebar. A hidden pane is a class on
// #wb-app (hide-left, hide-right). The center's head at the left edge
// brings a hidden projects pane back (.o-show); the right side's toggle,
// which closes the files pane with the open files, is split.js's
// (isHidden, setHidden, 'pane:toggled'). All of it is remembered in this
// browser.
//
// Smoothness: a drag only records the pointer; one requestAnimationFrame per
// frame writes the variables, and body.wb-resizing turns off text selection
// and lets the terminal postpone refitting its PTY until the drag rests
// (term.js).
import { $, $$ } from '../state.js';
import { emit } from '../bus.js';

// Sizes, in CSS pixels. The left pane holds names, so it stays narrow; the
// right one may grow until the center reaches CENTER_MIN, for long paths.
const LIMITS = {
  left: { def: 260, min: 200, max: 400 },
  right: { def: 260, min: 200, max: 1200 },
};
const CENTER_MIN = 360;
const STEP = 16; // arrow keys; Shift moves four steps
const KEY = 'punk0.layout';

const app = $('#wb-app');
const other = side => (side === 'left' ? 'right' : 'left');
const layout = load(); // { left, right, hidden: { left, right } }
let frame = 0;

function load() {
  const l = { left: LIMITS.left.def, right: LIMITS.right.def, hidden: { left: false, right: false } };
  try {
    const saved = JSON.parse(localStorage.getItem(KEY));
    for (const side of ['left', 'right']) {
      if (Number.isFinite(saved?.[side])) l[side] = saved[side];
      l.hidden[side] = saved?.hidden?.[side] === true;
    }
  } catch {}
  return l;
}

function save() {
  try {
    localStorage.setItem(KEY, JSON.stringify(layout));
  } catch {}
}

// shownWidth is what a pane takes from the window: nothing while hidden.
const shownWidth = side => (layout.hidden[side] ? 0 : layout[side]);

// maxOf is how wide side may be with the other pane as it is: its own cap,
// or less, so the center keeps CENTER_MIN.
const maxOf = side => Math.max(LIMITS[side].min, Math.min(LIMITS[side].max, innerWidth - shownWidth(other(side)) - CENTER_MIN));
const clamp = (side, px) => Math.round(Math.max(LIMITS[side].min, Math.min(px, maxOf(side))));

function apply() {
  frame = 0;
  app.style.setProperty('--wb-left', layout.left + 'px');
  app.style.setProperty('--wb-right', layout.right + 'px');
  app.classList.toggle('hide-left', layout.hidden.left);
  app.classList.toggle('hide-right', layout.hidden.right);
  for (const h of $$('.o-resizer[data-side]')) {
    const side = h.dataset.side;
    h.setAttribute('aria-valuenow', layout[side]);
    h.setAttribute('aria-valuemin', LIMITS[side].min);
    h.setAttribute('aria-valuemax', maxOf(side));
  }
}

const schedule = () => { frame ||= requestAnimationFrame(apply); };

function set(side, px) {
  layout[side] = clamp(side, px);
  schedule();
}

// fit keeps the shown panes inside a narrower window: the right pane gives
// way first, then the left.
function fit() {
  if (!layout.hidden.right) layout.right = clamp('right', layout.right);
  if (!layout.hidden.left) layout.left = clamp('left', layout.left);
}

// toggleSide hides or shows a side pane. Applied at once rather than on the
// next frame, so the button that brings it back can take focus.
function toggleSide(side) {
  layout.hidden[side] = !layout.hidden[side];
  fit();
  apply();
  save();
  emit('pane:toggled', side);
  // Keep focus on the pane's buttons when one of them was used; a toggle
  // stays in place, so it keeps focus by itself.
  if (!document.activeElement?.closest?.(`#o-hide-${side}, .o-show[data-show="${side}"]`)) return;
  const btn = layout.hidden[side]
    ? [...$$(`.o-show[data-show="${side}"], .o-side-toggle[data-side="${side}"]`)].find(b => b.offsetParent)
    : $(`#o-hide-${side}`);
  btn?.focus();
}

export const isHidden = side => layout.hidden[side];

export function setHidden(side, hidden) {
  if (layout.hidden[side] !== hidden) toggleSide(side);
}

function drag(handle, e) {
  if (e.button !== 0) return;
  e.preventDefault();
  const side = handle.dataset.side;
  const startX = e.clientX;
  const startW = layout[side];
  handle.setPointerCapture(e.pointerId);
  handle.classList.add('active');
  document.body.classList.add('wb-resizing');
  const move = ev => {
    const dx = ev.clientX - startX;
    set(side, side === 'left' ? startW + dx : startW - dx);
  };
  const end = () => {
    handle.removeEventListener('pointermove', move);
    handle.removeEventListener('pointerup', end);
    handle.removeEventListener('pointercancel', end);
    handle.classList.remove('active');
    document.body.classList.remove('wb-resizing');
    save();
  };
  handle.addEventListener('pointermove', move);
  handle.addEventListener('pointerup', end);
  handle.addEventListener('pointercancel', end);
}

function key(handle, e) {
  const side = handle.dataset.side;
  // Arrows move the border itself, so on the right pane Left widens it.
  const grow = side === 'left' ? 1 : -1;
  const step = e.shiftKey ? STEP * 4 : STEP;
  if (e.key === 'ArrowLeft') set(side, layout[side] - grow * step);
  else if (e.key === 'ArrowRight') set(side, layout[side] + grow * step);
  else if (e.key === 'Home') set(side, LIMITS[side].min);
  else if (e.key === 'End') set(side, maxOf(side));
  else if (e.key === 'Enter') set(side, LIMITS[side].def);
  else return;
  e.preventDefault();
  save();
}

// Ctrl+B (Cmd+B on macOS) toggles the projects pane, as in VS Code. Not
// inside a terminal: there Ctrl+B is the shell's (readline's back a
// character, tmux's prefix). With Alt it is split.js's, for the whole right
// side.
function shortcut(e) {
  if (e.key.toLowerCase() !== 'b' && e.code !== 'KeyB') return;
  if (!(e.ctrlKey || e.metaKey) || e.shiftKey || e.altKey || e.target.closest?.('#o-term')) return;
  e.preventDefault();
  toggleSide('left');
}

export function initResize() {
  for (const h of $$('.o-resizer[data-side]')) {
    h.addEventListener('pointerdown', e => drag(h, e));
    h.addEventListener('keydown', e => key(h, e));
    h.addEventListener('dblclick', () => {
      set(h.dataset.side, LIMITS[h.dataset.side].def);
      save();
    });
  }
  for (const side of ['left', 'right']) {
    $(`#o-hide-${side}`).addEventListener('click', () => toggleSide(side));
    for (const b of $$(`.o-show[data-show="${side}"]`)) b.addEventListener('click', () => toggleSide(side));
  }
  addEventListener('keydown', shortcut);
  addEventListener('resize', () => {
    fit();
    schedule();
  });
  fit();
  apply(); // at once: the page starts at the CSS defaults
}
