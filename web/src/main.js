// web/src/main.js
import { initSidebar } from './sidebar.js';
import { initTerm } from './term.js';
import { initViewer, setViewerSatellite } from './viewer.js';
import { initTree, setTreeSatellite } from './tree.js';

const term = initTerm(document.getElementById('term'));
document.getElementById('btn-claude').addEventListener('click', () => term.run('claude'));

initViewer(
  document.getElementById('code-vp'),
  document.getElementById('code-sizer'),
  document.getElementById('code-rows'),
  document.getElementById('file-title'),
  document.getElementById('file-meta')
);
initTree(document.getElementById('tree'));

initSidebar(document.getElementById('pane-satellites'), (id) => {
  term.attach(id);
  setViewerSatellite(id);
  setTreeSatellite(id);
});
