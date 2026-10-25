// web/src/sideviews.js
// The editor's two sides. The sidebar (#side) has four modes, picked with
// the icon switch in its top row (paneviews.js, remembered as
// punk0.sideView): Files, Search, Symbols and References. The right side
// (#right-side) has two, the same way (punk0.rightView): Terminal and Chats
// ('rightview:changed' {view} follows a change), and can take the whole
// editor's width (zoom; body.right-zoom). A side hides with the button at
// the end of its top row and comes back with the one the tab bar then
// shows at that edge (any [data-toggle-side]), or Mod+B / Mod+J; body
// carries side-hidden / right-hidden meanwhile. 'side:toggled' {side,
// shown} follows each change.
import { $, $$ } from './state.js';
import { emit } from './bus.js';
import { layout, render } from './renderer.js';
import { paneViews } from './paneviews.js';
import { cancelSearch } from './search.js';
import { loadOutline } from './outline.js';

const HIDDEN = { left: 'side-hidden', right: 'right-hidden' };
let views = null, rightViews = null;

export const sideShown = side => !document.body.classList.contains(HIDDEN[side]);

const zoomed = () => document.body.classList.contains('right-zoom');

// setRightZoom gives the right side the editor's whole width, or gives the
// sidebar and the code their places back.
export function setRightZoom(on) {
  if (zoomed() === on) return;
  document.body.classList.toggle('right-zoom', on);
  const b = $('#btn-right-zoom');
  b.setAttribute('aria-pressed', String(on));
  b.title = on ? 'Restore' : 'Maximize';
  b.setAttribute('aria-label', b.title);
  layout();
  render();
}

// setSideShown shows or hides a side. Either ends a zoom first: the sidebar
// cannot show beside a zoomed right side, and a hidden one has nothing to
// zoom.
export function setSideShown(side, shown) {
  if (zoomed() && (side === 'left' || !shown)) setRightZoom(false);
  if (sideShown(side) === shown) return;
  document.body.classList.toggle(HIDDEN[side], !shown);
  layout();
  render();
  emit('side:toggled', { side, shown });
}

// toggleSide is the hide/show buttons and keys. While the right side is
// zoomed, the sidebar's only restores the layout it zoomed out of.
export function toggleSide(side) {
  if (side === 'left' && zoomed()) return setRightZoom(false);
  setSideShown(side, !sideShown(side));
}

export const rightView = () => rightViews.current();

// showRightView shows the right side on mode id.
export function showRightView(id) {
  setSideShown('right', true);
  rightViews.set(id);
}

// showSideView shows the sidebar on mode id and puts the caret in its
// input, as its shortcut asks.
export function showSideView(id) {
  setSideShown('left', true);
  views.set(id);
  if (id === 'search') $('#q').select();
  if (id === 'symbols') $('#symbols-filter').focus();
}

// The References mode lists a symbol's references (mode 'refs') or traces
// its calls ('in' callers, 'out' callees); the switch in its head shows
// which, and only that one's body.
export function setRefsMode(mode) {
  for (const b of $$('#refs-mode [data-refs]')) {
    const on = b.dataset.refs === mode;
    b.classList.toggle('active', on);
    b.setAttribute('aria-pressed', String(on));
  }
  $('[data-refs-body="refs"]').hidden = mode !== 'refs';
  $('[data-refs-body="calls"]').hidden = mode === 'refs';
}

export function initSideViews() {
  views = paneViews({
    host: $('#side'),
    bars: $$('#side > .side-top .pane-tabs'),
    key: 'punk0.sideView',
    fallback: 'files',
    onChange: id => {
      if (id !== 'search') cancelSearch();
      if (id === 'symbols') loadOutline();
    },
  });
  rightViews = paneViews({
    host: $('#right-side'),
    bars: $$('#right-side > .side-top .pane-tabs'),
    key: 'punk0.rightView',
    fallback: 'term',
    onChange: view => emit('rightview:changed', { view }),
  });
  for (const b of $$('[data-toggle-side]')) b.addEventListener('click', () => toggleSide(b.dataset.toggleSide));
  $('#btn-right-zoom').addEventListener('click', () => setRightZoom(!zoomed()));

  // The terminal's width: its left border drags.
  const rz = $('#right-resizer');
  let dragging = false;
  rz.addEventListener('mousedown', e => { dragging = true; rz.classList.add('drag'); e.preventDefault(); });
  addEventListener('mousemove', e => {
    if (dragging) $('#right-side').style.width = Math.max(200, Math.min(700, innerWidth - e.clientX)) + 'px';
  });
  addEventListener('mouseup', () => {
    if (!dragging) return;
    dragging = false;
    rz.classList.remove('drag');
    layout();
    render();
  });
}
