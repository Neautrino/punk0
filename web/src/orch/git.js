// web/src/orch/git.js
// The branch bar under the files tree (#o-files-foot): the selected
// workspace's branch, how many of its changed files are staged, and how far
// it is ahead of or behind its upstream. A click opens the folder's editor
// on its git panel (?panel=git), where committing, pushing and the history
// are. Staging a single file is the tree's own tick (tree.js). The bar
// follows the git-status stream tree.js keeps open ('git:status').
import { $ } from '../state.js';
import { on } from '../bus.js';
import { openEditor } from './api.js';
import { subscribe, workspace, projectOf, hasGit } from './store.js';

let footEl, branchEl, sumEl, trackEl;
let w = null; // the selected workspace, when it is a git checkout
const latest = new Map(); // workspace id -> its last git-status payload

export function initGit() {
  footEl = $('#o-files-foot');
  branchEl = $('#o-git-branch');
  sumEl = $('#o-git-sum');
  trackEl = $('#o-git-track');
  on('git:status', ({ ws, data }) => {
    latest.set(ws, data);
    if (ws === w?.id) draw();
  });
  subscribe(() => {
    const cur = workspace();
    w = hasGit(cur) ? cur : null;
    draw();
  });
  $('#o-git-bar').addEventListener('click', () => w && openEditor(w.href, 'panel=git'));
}

function draw() {
  footEl.hidden = !w;
  if (!w) return;
  const s = latest.get(w.id);
  // The branch the records know, until the stream says.
  branchEl.textContent = s ? s.branch || 'detached HEAD' : (w.kind === 'worktree' ? w.branch : projectOf(w.id)?.branch) || '';
  const paths = Object.keys(s?.statuses || {});
  const staged = paths.filter(p => s.staged?.[p]).length;
  sumEl.textContent = !s ? '' : paths.length ? `${staged} / ${paths.length} staged` : 'no changes';
  // Even with its upstream the bar says nothing; ahead or behind, how far.
  trackEl.innerHTML = !s ? ''
    : s.upstream ? [s.ahead ? `<span class="o-git-ahead">↑${s.ahead}</span>` : '', s.behind ? `<span class="o-git-behind">↓${s.behind}</span>` : ''].filter(Boolean).join(' ')
    : s.branch ? 'unpublished' : '';
  trackEl.title = !s ? '' : s.upstream ? `${s.ahead} to push and ${s.behind} to pull (as of the last fetch), against ${s.upstream}` : s.branch ? 'The branch tracks no remote branch' : '';
}
