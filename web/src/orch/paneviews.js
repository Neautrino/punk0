// web/src/orch/paneviews.js
// The right side's two modes, Files and Chats, picked with the icon switch
// at the left of the files pane's top row (.o-pane-tabs in #o-files
// .o-pane-head), or, while the pane does not show, at the left of the
// right-end buttons of whichever tab group is rightmost (one per group
// head; ../paneviews.js draws them all). Each
// mode is a <section data-pane-view> in #o-files holding its head and body;
// only the active one shows. #wb-app carries the mode as data-pane-view, so
// CSS can give Chats the second group's column too: the modes never share
// the right side. The choice is remembered in this browser (punk0.paneView:
// 'files' or 'chats'; anything else is Files) and announced as
// 'paneview:changed' {view}.
import { $, $$ } from '../state.js';
import { emit } from '../bus.js';
import { paneViews } from '../paneviews.js';
export const paneView = () => $('#wb-app').dataset.paneView || '';

let views = null;

// setPaneView switches the right side, as its switch does: a tab sent to
// the second group while the chats cover it brings Files back.
export const setPaneView = id => views?.set(id);

export function initPaneViews() {
  views = paneViews({
    host: $('#o-files'),
    bars: $$('.o-pane-tabs'),
    key: 'punk0.paneView',
    fallback: 'files',
    onChange: view => {
      $('#wb-app').dataset.paneView = view;
      emit('paneview:changed', { view });
    },
  });
}
