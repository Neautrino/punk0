// web/src/orch/flyout.js
// Panels that open beside the sidebar (New chat, New workspace), pointing at
// the button that opened them.
import { $ } from '../state.js';

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
}
