// web/src/main.js
import { initTerm } from './term.js';
import { initViewer } from './viewer.js';
import { initTree } from './tree.js';

// 1. Initialize Terminal in left pane
const term = initTerm(document.getElementById('term'));
document.getElementById('btn-claude').addEventListener('click', () => term.run('claude'));

// 2. Initialize Code Viewer in right pane
initViewer(
  document.getElementById('code-vp'),
  document.getElementById('code-sizer'),
  document.getElementById('code-rows'),
  document.getElementById('file-title'),
  document.getElementById('file-meta')
);

// 3. Initialize File Tree sidebar
initTree(document.getElementById('tree'));
