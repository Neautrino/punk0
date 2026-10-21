// web/src/main.js
import { state, subscribe, reload, workspace, activeTab } from './store.js';
import { initSidebar } from './sidebar.js';
import { initTabs } from './tabs.js';
import { initTerm } from './term.js';
import { initViewer, showFile, forgetClosed } from './viewer.js';
import { initTree } from './tree.js';
import { initPicker } from './picker.js';

const termEl = document.getElementById('term');
const viewerEl = document.getElementById('viewer');
const emptyEl = document.getElementById('center-empty');
const term = initTerm(termEl, reload);

initViewer(
  document.getElementById('code-vp'),
  document.getElementById('code-sizer'),
  document.getElementById('code-rows'),
  document.getElementById('file-title'),
  document.getElementById('file-meta')
);
initTree(document.getElementById('pane-files'));
initSidebar(document.getElementById('pane-projects'));
initTabs(document.getElementById('pane-center'));
initPicker(document.getElementById('picker'));

// Reloads re-render everything; the center only switches when the active
// tab actually changes, so a refresh never drops a terminal or an open file.
// A terminal stays attached while a file tab is in front of it.
let shown = null; // id of the tab in the center
let shownTerm = null; // id of the terminal tab xterm is attached to
let shownWs = null;
subscribe(() => {
  const w = workspace();
  forgetClosed(w?.id || '', state.files[w?.id] || []);
  const t = activeTab();
  if ((w?.id || '') !== shownWs) {
    shownWs = w?.id || '';
    if (t?.kind === 'file' && shownTerm) term.attach((shownTerm = null)); // not this workspace's shell
  }
  if ((t?.id || null) === shown) return;
  shown = t?.id || null;

  const file = t?.kind === 'file';
  termEl.classList.toggle('hidden', !t || file);
  viewerEl.classList.toggle('hidden', !file);
  emptyEl.classList.toggle('hidden', !!t);
  if (file) return showFile(w.id, t.path);
  if (!t) return shownTerm && term.attach((shownTerm = null));
  if (t.id === shownTerm) return term.focus();
  term.attach((shownTerm = t.id));
});
reload();
