// web/src/selpill-util.js
// Pure placement for the selection pill (selpill.js), kept apart from the DOM
// so it can be tested.

/**
 * Above the selection's start, or below its end when that would run under the
 * tab bar; kept inside the window horizontally.
 * @param {{left: number, top: number, bottom: number}} sel selection bounds
 * @param {{width: number}} view the window
 * @param {{width: number, height: number}} pill
 */
export function calculatePillPosition(sel, view, pill) {
  const left = Math.max(10, Math.min(sel.left, view.width - pill.width - 10));
  let top = sel.top - pill.height - 8;
  if (top < 45) top = sel.bottom + 8;
  return { left: Math.round(left), top: Math.round(top) };
}
