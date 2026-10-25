// web/src/orch/flyout.js
// Panels that open beside the sidebar (New chat, New workspace), pointing at
// the button that opened them, and panels that open beside the button itself
// (the Chats rows' Remove), which is anywhere on the page.
import { $ } from '../state.js';

// anchorRect is where a flyout's opening button is on screen, or the nearest
// ancestor that is laid out: a Chats row's trash is `display: none` until its
// row is hovered or focused, and a click arriving without either -- a
// viewport change drops the hover without a mouse move -- measures an empty
// rect, which would put the flyout in the window's corner. The row around it
// is what the user means anyway.
export function anchorRect(el) {
  for (let e = el; e; e = e.parentElement) {
    const r = e.getBoundingClientRect();
    if (r.width || r.height) return r;
  }
  return new DOMRect(innerWidth / 2, innerHeight / 2, 0, 0);
}

// placeFlyout puts el just right of the sidebar, level with anchor (the
// opening button's rect, kept because the list re-renders under the panel)
// and inside the window; the notch on its left edge points at the button.
export function placeFlyout(el, anchor) {
  const side = $('#o-side').getBoundingClientRect();
  const h = el.offsetHeight;
  const top = Math.max(8, Math.min(anchor.top - 12, innerHeight - h - 8));
  el.style.left = `${side.right + 10}px`;
  el.style.top = `${top}px`;
  el.style.setProperty('--notch', `${Math.max(12, Math.min(h - 12, anchor.top + anchor.height / 2 - top))}px`);
  el.classList.remove('o-flip');
}

// placeFlyoutAt puts el beside anchor (a rect kept from the opening button),
// flipping to its left when the right side would leave the window, and level
// with it; the notch points back at the button. Used where the button is not
// in the sidebar, so the sidebar's edge says nothing about where it is.
export function placeFlyoutAt(el, anchor) {
  const w = el.offsetWidth;
  const h = el.offsetHeight;
  const right = anchor.right + 10;
  const left = right + w + 8 > innerWidth ? Math.max(8, anchor.left - 10 - w) : right;
  const top = Math.max(8, Math.min(anchor.top - 12, innerHeight - h - 8));
  el.style.left = `${left}px`;
  el.style.top = `${top}px`;
  el.style.setProperty('--notch', `${Math.max(12, Math.min(h - 12, anchor.top + anchor.height / 2 - top))}px`);
  el.classList.toggle('o-flip', left < anchor.left);
}
