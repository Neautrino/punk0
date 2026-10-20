// web/src/main.js
import { subscribe, reload, workspace, activeTab } from './store.js';
import { initSidebar } from './sidebar.js';
import { initTabs } from './tabs.js';
import { initTerm } from './term.js';
import { initViewer, setViewerWorkspace } from './viewer.js';
import { initTree, setTreeWorkspace } from './tree.js';

const term = initTerm(document.getElementById('term'), reload);

initViewer(
  document.getElementById('code-vp'),
  document.getElementById('code-sizer'),
  document.getElementById('code-rows'),
  document.getElementById('file-title'),
  document.getElementById('file-meta')
);
initTree(document.getElementById('tree'));
initSidebar(document.getElementById('pane-projects'));
initTabs(document.getElementById('pane-term'));

// Reloads re-render everything; the panes only switch when the selection
// actually changes, so a refresh never drops a terminal or an open file.
let shownWs, shownTab;
subscribe(() => {
  const ws = workspace()?.id || '';
  if (ws !== shownWs) {
    shownWs = ws;
    setTreeWorkspace(ws);
    setViewerWorkspace(ws);
  }
  const tab = activeTab()?.id || null;
  if (tab !== shownTab) {
    shownTab = tab;
    term.attach(tab);
  }
});
reload();
