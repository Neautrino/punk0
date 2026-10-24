// web/src/tabwheel.js
// A tab bar with more tabs than fit scrolls sideways under a plain mouse
// wheel. Horizontal deltas (trackpads, Shift+wheel) already scroll it
// natively. Shared by the editor's #tabs (tabs.js) and the agents page's tab
// bars (orch/tabs.js).
/** @param {HTMLElement} el */
export function scrollTabsOnWheel(el) {
  el.addEventListener('wheel', onWheel, { passive: false });
}

/** @param {WheelEvent} e */
function onWheel(e) {
  const el = /** @type {HTMLElement} */ (e.currentTarget);
  if (e.ctrlKey || Math.abs(e.deltaY) <= Math.abs(e.deltaX) || el.scrollWidth <= el.clientWidth) return;
  e.preventDefault();
  const step = e.deltaMode === WheelEvent.DOM_DELTA_LINE ? 40 : e.deltaMode === WheelEvent.DOM_DELTA_PAGE ? el.clientWidth : 1;
  el.scrollLeft += e.deltaY * step;
}
