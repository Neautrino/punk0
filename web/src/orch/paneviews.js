// web/src/orch/paneviews.js
// The right side's two modes, Files and Chats, picked with the icon switch
// at the left of its top row (.pane-tabs in #o-file-head; ../paneviews.js
// draws it). Each mode is a <section data-pane-view> in #o-files holding
// its head (the right side's second row) and body; only the active one
// shows. #wb-app carries the mode as data-pane-view, so CSS can give Chats
// the file group's columns too: the modes never share the right side.
// The choice is remembered in this browser (punk0.paneView: 'files' or
// 'chats'; anything else is Files) and announced as 'paneview:changed'
// {view}.
import { $, $$ } from '../state.js';
import { emit } from '../bus.js';
import { paneViews } from '../paneviews.js';

export const paneView = () => $('#wb-app').dataset.paneView || '';

export function initPaneViews() {
  paneViews({
    host: $('#o-files'),
    bars: $$('#o-file-head .pane-tabs'),
    key: 'punk0.paneView',
    fallback: 'files',
    onChange: view => {
      $('#wb-app').dataset.paneView = view;
      emit('paneview:changed', { view });
    },
  });
}
