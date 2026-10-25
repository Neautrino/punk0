// web/src/orch/paneviews.js
// The right side's two modes, Files and Chats, picked with the icon switch
// at the left of its top row (.o-pane-tabs in #o-file-head, a button per
// data-view). Each mode is a <section data-pane-view> in #o-files holding
// its head (the right side's second row) and body; only the active one
// shows. #wb-app carries the mode as data-pane-view, so CSS can give Chats
// the file group's columns too: the modes never share the right side.
// The choice is remembered in this browser (punk0.paneView: 'files' or
// 'chats'; anything else is Files) and announced as 'paneview:changed'
// {view}.
import { $, $$ } from '../state.js';
import { emit } from '../bus.js';

const KEY = 'punk0.paneView';
let current = '';

export const paneView = () => current;

// setPaneView shows mode id; one the page does not have shows Files.
export function setPaneView(id) {
  if (!$(`#o-files > [data-pane-view="${CSS.escape(id)}"]`)) id = 'files';
  if (id === current) return;
  current = id;
  $('#wb-app').dataset.paneView = id;
  for (const s of $$('#o-files > [data-pane-view]')) s.hidden = s.dataset.paneView !== id;
  for (const b of $$('.o-pane-tabs [data-view]')) {
    const on = b.dataset.view === id;
    b.setAttribute('aria-selected', String(on));
    b.tabIndex = on ? 0 : -1;
  }
  try { localStorage.setItem(KEY, id); } catch {}
  emit('paneview:changed', { view: id });
}

// The arrow keys, Home and End move along the switch, as in any tablist.
function key(e) {
  const btns = $$('[data-view]', e.currentTarget);
  const i = btns.indexOf(document.activeElement);
  if (i < 0) return;
  const n = btns.length;
  const to = { ArrowRight: (i + 1) % n, ArrowLeft: (i + n - 1) % n, Home: 0, End: n - 1 }[e.key];
  if (to === undefined) return;
  e.preventDefault();
  const next = btns[to];
  setPaneView(next.dataset.view);
  next.focus();
}

export function initPaneViews() {
  let saved = '';
  try { saved = localStorage.getItem(KEY) || ''; } catch {}
  setPaneView(saved === 'chats' ? 'chats' : 'files');
  for (const bar of $$('.o-pane-tabs')) {
    bar.addEventListener('click', e => {
      const b = e.target.closest('[data-view]');
      if (b) setPaneView(b.dataset.view);
    });
    bar.addEventListener('keydown', key);
  }
}
