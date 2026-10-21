// web/src/workbench.js
// The orchestrator page (web/workbench.html), what a bare `punk0` opens.
// Bundled on its own into web/workbench.js by scripts/build-web.js; the
// modules it is built from live in web/src/orch/ and share only the theme,
// toast and status-bar metrics modules with the editor app. The server side
// is workbench.go.
//
// Three panes: projects and workspaces on the left, the selected workspace's
// tabs in the middle (terminals from the parent, files from the folder's
// editor backend), its files on the right.
import { $, esc } from './state.js';
import { state, subscribe, reload, workspace, activeTab, projectOf, tildify } from './orch/store.js';
import { initStatusBar } from './orch/statusbar.js';
import { initSidebar } from './orch/sidebar.js';
import { initTabs } from './orch/tabs.js';
import { initTree } from './orch/tree.js';
import { initPicker } from './orch/picker.js';
import { initViewer, showFile, forgetClosed } from './orch/viewer.js';
import { initTerm, attachTerm, focusTerm } from './orch/term.js';
import { initResize } from './orch/resize.js';

// Editor windows link back here by this name (main.js), so going back
// reuses this window instead of opening another orchestrator.
window.name = 'punk0-agents';

const termPane = $('#o-term');
const viewerPane = $('#o-viewer');
const emptyPane = $('#o-empty');

initResize(); // first: the panes take their widths before anything measures them
const pollResources = initStatusBar();
initSidebar();
initTabs();
initTree();
initPicker();
initViewer();
initTerm(termPane, reload);

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
    if (t?.kind === 'file' && shownTerm) attachTerm((shownTerm = null)); // not this workspace's terminal
  }
  emptyPane.dataset.ws = !w ? 'none' : w.missing ? 'missing' : 'yes';
  const p = w && projectOf(w.id);
  $('#o-empty-ws').innerHTML = w ? `<b>${esc(p && !p.scratch ? p.name + ' / ' + w.name : w.name)}</b> <span class="wb-faint">${esc(tildify(w.dir))}</span>` : '';
  if ((t?.id || null) === shown) return;
  shown = t?.id || null;

  const file = t?.kind === 'file';
  termPane.hidden = !t || file;
  viewerPane.hidden = !file;
  emptyPane.hidden = !!t;
  if (file) return showFile(w, t.path);
  if (!t) return shownTerm && attachTerm((shownTerm = null));
  if (t.id === shownTerm) return focusTerm();
  attachTerm((shownTerm = t.id));
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
