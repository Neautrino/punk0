// web/src/workbench.js
// The orchestrator page (web/workbench.html), what a bare `punk0` opens.
// Bundled on its own into web/workbench.js by scripts/build-web.js; the
// modules it is built from live in web/src/orch/ and share only the theme,
// toast, status-bar metrics and settings modal modules with the editor app.
// The server side is workbench.go.
//
// Three panes: projects and workspaces on the left, the selected workspace's
// tabs in the middle (terminals from the parent, files from the folder's
// editor backend, side by side or as tabs: split.js), its files on the right.
import { $, esc } from './state.js';
import { state, subscribe, reload, workspace, activeTab, termTab, projectOf, tildify, wsLabel } from './orch/store.js';
import { initStatusBar } from './orch/statusbar.js';
import { initSidebar } from './orch/sidebar.js';
import { initNewChat } from './orch/newchat.js';
import { initNewWorkspace } from './orch/newworkspace.js';
import { initAddProject } from './orch/addproject.js';
import { initLsp } from './orch/lsp.js';
import { initTabs } from './orch/tabs.js';
import { initTree } from './orch/tree.js';
import { initPicker } from './orch/picker.js';
import { initViewer, showFile, hideFile, forgetClosed } from './orch/viewer.js';
import { initChanges, showChanges, hideChanges } from './orch/changes.js';
import { initTerm, attachTerm, focusTerm } from './orch/term.js';
import { initResize } from './orch/resize.js';
import { initSplit, layoutCenter } from './orch/split.js';
import { initSettings } from './orch/settings.js';
import { initNotify } from './orch/notify.js';

// Editor windows link back here by this name (main.js), so going back
// reuses this window instead of opening another orchestrator.
window.name = 'punk0-agents';

const termPane = $('#o-term');
const viewerPane = $('#o-viewer');
const codeVp = $('#o-code-vp');
const changesVp = $('#o-diff-vp');
const emptyPane = $('#o-empty');

initResize(); // first: the panes take their widths before anything measures them
initSplit();
const pollResources = initStatusBar();
initSettings();
initAddProject(); // before the sidebar, which may open it for ?open=<url>
initSidebar();
initNewChat();
initNewWorkspace();
initLsp();
initTabs();
initTree();
initPicker();
initViewer();
initChanges();
initTerm(termPane, reload);
initNotify();

// Reloads re-render everything; the center only switches what it shows when
// that changes, so a refresh never drops a terminal or an open file. A
// terminal stays attached while it is out of view (a file in front of it, or
// its group collapsed); attaching one that is not in view would start it.
let shownFile = null; // "<workspace id>\n<tab id>" in the viewer
let shownTerm = null; // id of the terminal tab xterm is attached to
let shownWs = null;
let front = null; // id of the tab in front
subscribe(() => {
  const w = workspace();
  forgetClosed(w?.id || '', state.files[w?.id] || []);
  const v = layoutCenter();
  if ((w?.id || '') !== shownWs) {
    shownWs = w?.id || '';
    if (!v.term && shownTerm) attachTerm((shownTerm = null)); // not this workspace's terminal
  }
  emptyPane.dataset.ws = !w ? 'none' : w.missing ? 'missing' : 'yes';
  const p = w && projectOf(w.id);
  $('#o-empty-ws').innerHTML = w ? `<b>${esc(p && !p.scratch ? p.name + ' / ' + w.name : wsLabel(w))}</b> <span class="wb-faint">${esc(tildify(w.dir))}</span>` : '';
  termPane.hidden = !v.term;
  viewerPane.hidden = !v.file;
  emptyPane.hidden = !v.welcome;

  // A file tab shows its text, or its diff as the Changes tab shows all of
  // them; switching between the two is a different key.
  const diff = !!(v.file?.changes || v.file?.diff);
  const fileKey = v.file ? `${w.id}\n${v.file.id}\n${diff}` : null;
  if (fileKey !== shownFile) {
    shownFile = fileKey;
    codeVp.hidden = diff;
    changesVp.hidden = !diff;
    if (v.file && !diff) showFile(w, v.file.path);
    else hideFile();
    if (!diff) hideChanges();
  }
  if (diff) showChanges(w, v.collapsed === 'term', v.file.changes ? '' : v.file.path);

  const was = front;
  front = activeTab()?.id || null;
  if (!termTab()) {
    if (shownTerm) attachTerm((shownTerm = null));
  } else if (v.term && v.term.id !== shownTerm) {
    attachTerm((shownTerm = v.term.id));
  } else if (v.term && front === v.term.id && was !== front) {
    focusTerm();
  }
});

// Keep tab states, editor states and the status bar current while the page
// is in view.
const tick = () => {
  if (document.hidden) return;
  reload();
  pollResources();
};
setInterval(tick, 3000);
document.addEventListener('visibilitychange', tick);
tick();
