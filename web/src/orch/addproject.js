// web/src/orch/addproject.js
// Add project, a flyout beside the Projects sidebar. Its main step is a menu,
// as Orca's is: a folder path typed in (with completion), Browse folder, Clone
// from URL, Create new project, and the projects removed lately, whose
// worktrees come back with them. A pull request URL typed in opens it for
// review, as the editor's Open Pull Request does (asking for a local clone
// when none is known).
//
// Browse, Clone and Create are steps with the folder browser, which starts
// where the last project went (else where most projects are). Browsing, the
// git repositories inside a folder can be ticked to add each as a project of
// its own. A clone runs on the server (addproject.go) and is polled for its
// progress; closing the flyout cancels it.
import { $, esc } from '../state.js';
import { call } from './api.js';
import { state, reload, selectWorkspace, tildify } from './store.js';
import { attachComplete, looksLikeURL } from './complete.js';
import { placeFlyout } from './flyout.js';
import { folderBrowser, parentOf, joinPath } from './browser.js';

const PREFS = 'punk0.addProject';
const HEADINGS = { main: 'Add project', browse: 'Browse folder', clone: 'Clone from URL', create: 'Create new project' };
const SUBMIT = { main: 'Add', browse: 'Add', clone: 'Clone', create: 'Create' };

let form, pathEl, urlEl, nameEl, noteEl, msgEl, submitEl, cancelEl, browseLabel;
let cloneEl, clonePathEl, progressEl, removedEl, removedList;
let fb; // the folder browser
let done = null; // resolves the open flyout's promise
let anchor = null;
let step = 'main';
let pendingPR = ''; // the PR URL waiting for its clone to be chosen
let folder = null; // what GET api/wb/folder said about the folder field
let folderSeq = 0;
let folderTimer = 0;
let entries = []; // the folders the browser shows
let job = null; // the clone under way: { id, timer }
let busy = false;
let quiet = false; // the next folder shown was not opened by the user

export function initAddProject() {
  form = $('#o-ap-form');
  pathEl = $('#o-ap-path');
  urlEl = $('#o-ap-url');
  nameEl = $('#o-ap-name');
  noteEl = $('#o-ap-note');
  msgEl = $('#o-ap-msg');
  submitEl = $('#o-ap-submit');
  cancelEl = $('#o-ap-cancel');
  browseLabel = $('#o-ap-browse-label');
  cloneEl = $('#o-clone');
  clonePathEl = $('#o-clone-path');
  progressEl = $('#o-ap-progress');
  removedEl = $('#o-ap-removed');
  removedList = $('#o-ap-removed-list');

  fb = folderBrowser($('#o-ap-browser'), {
    onOpen: onBrowse,
    badge: path => (projectAt(path) ? 'added' : ''),
    checkable: (path, e) => step === 'browse' && e.git && !projectAt(path),
    onCheck: render,
  });
  attachComplete(pathEl, $('#o-ap-suggest'));
  attachComplete(clonePathEl, $('#o-clone-suggest'));

  form.addEventListener('submit', e => {
    e.preventDefault();
    submit();
  });
  form.addEventListener('keydown', e => {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    if (step !== 'main' && !job) return setStep('main');
    close();
  });
  $('#o-ap-close').addEventListener('click', close);
  cancelEl.addEventListener('click', () => (job ? cancelClone() : close()));
  $('#o-ap-back').addEventListener('click', () => !job && setStep('main'));
  for (const b of form.querySelectorAll('[data-step-to]')) b.addEventListener('click', () => setStep(b.dataset.stepTo));
  // A click outside closes it, except on the button that opens it, and
  // except while a clone runs: that is cancelled only on purpose.
  document.addEventListener('mousedown', e => {
    if (done && !job && !form.contains(e.target) && !e.target.closest?.('#o-add-project, [data-act=add-project]')) close();
  });
  addEventListener('resize', () => done && placeFlyout(form, anchor));
  new ResizeObserver(() => done && placeFlyout(form, anchor)).observe(form);

  pathEl.addEventListener('input', () => {
    hideClone();
    describeSoon();
  });
  urlEl.addEventListener('input', render);
  nameEl.addEventListener('input', render);
  removedList.addEventListener('click', e => {
    const b = e.target.closest('[data-dir]');
    if (b) addFolders([b.dataset.dir]);
  });

  $('#o-clone-use').addEventListener('click', () => {
    const p = clonePathEl.value.trim();
    if (p && pendingPR) openPR(pendingPR, p);
  });
  clonePathEl.addEventListener('keydown', e => {
    if (e.key !== 'Enter') return;
    e.preventDefault(); // not the form's submit: this field answers the clone question
    $('#o-clone-use').click();
  });
  $('#o-clone-managed').addEventListener('click', () => pendingPR && openPR(pendingPR, 'managed'));
}

// openAddProject shows the flyout beside the sidebar, pointing at from, on its
// main step; target (a folder or a pull request URL) is put in the folder
// field and opened at once. It resolves when the flyout closes.
export function openAddProject(from, target = '') {
  anchor = from.getBoundingClientRect();
  if (!done) {
    form.hidden = false;
    pathEl.value = '';
    showMsg('');
    hideClone();
    setStep('main');
    loadRemoved();
  }
  placeFlyout(form, anchor);
  if (target) {
    pathEl.value = target;
    submit();
  }
  if (done) return Promise.resolve();
  return new Promise(resolve => { done = resolve; });
}

// closeAddProject closes it, as switching sidebar views does; a clone under
// way is cancelled.
export const closeAddProject = () => close();

function close() {
  if (!done) return;
  if (job) cancelClone();
  form.hidden = true;
  clearTimeout(folderTimer);
  folderSeq++;
  done();
  done = null;
}

function setStep(s) {
  step = s;
  form.dataset.step = s;
  $('#o-ap-heading').textContent = HEADINGS[s];
  browseLabel.textContent = s === 'browse' ? 'Folders' : 'In folder';
  showMsg('');
  progressEl.hidden = true;
  // The browser is the other steps'; its checkboxes are Browse's.
  if (s !== 'main') {
    quiet = true;
    fb.browse(fb.dir() || startDir(), '~');
  }
  render();
  stepInput().focus();
}

/* ---------- where to start ---------- */

function loadPrefs() {
  try {
    return JSON.parse(localStorage.getItem(PREFS)) || {};
  } catch {
    return {};
  }
}

// startDir is the folder the last project went in; the first time, the one
// most projects are in.
function startDir() {
  const last = loadPrefs().parent;
  if (last) return last;
  const count = new Map();
  for (const p of state.projects) {
    if (p.scratch) continue;
    const parent = parentOf(p.dir);
    count.set(parent, (count.get(parent) || 0) + 1);
  }
  let best = '~';
  let most = 0;
  for (const [dir, n] of count) {
    if (n > most) [best, most] = [dir, n];
  }
  return best;
}

const remember = dir => localStorage.setItem(PREFS, JSON.stringify({ ...loadPrefs(), parent: parentOf(dir) }));

const projectAt = dir => state.projects.find(p => !p.scratch && p.dir === dir.replace(/[/\\]+$/, ''));

/* ---------- a folder ---------- */

// onBrowse follows the browser with the folder field, as a file dialog does,
// once the user opens a folder.
function onBrowse(dir, list) {
  entries = list;
  if (step === 'browse' && !job && !quiet) {
    pathEl.value = tildify(dir);
    hideClone();
    describeSoon(0);
  } else {
    render();
  }
  quiet = false;
}

// describeSoon asks what the typed folder is once typing pauses.
function describeSoon(delay = 200) {
  clearTimeout(folderTimer);
  const seq = ++folderSeq;
  folder = null;
  render();
  const v = pathEl.value.trim();
  if (!v || looksLikeURL(v)) return;
  folderTimer = setTimeout(async () => {
    const res = await call('GET', 'api/wb/folder?' + new URLSearchParams({ dir: v }));
    if (seq !== folderSeq || !done) return;
    folder = res.ok ? res.data : null;
    render();
  }, delay);
}

const code = s => `<code class="o-ap-note-dest">${esc(s)}</code>`;

// render updates the note under the browser and the submit button.
function render() {
  submitEl.textContent = SUBMIT[step];
  if (step === 'clone') {
    const name = repoFolderName(urlEl.value);
    noteEl.innerHTML = name && fb.dir() ? `Clones into ${code(tildify(joinPath(fb.dir(), name)))}` : 'Paste the URL you would give git clone.';
    return;
  }
  if (step === 'create') {
    const name = nameEl.value.trim();
    noteEl.innerHTML = fb.dir()
      ? `Makes ${code(tildify(joinPath(fb.dir(), name || 'my-project')))} and runs git init in it.`
      : '';
    return;
  }
  const ticked = step === 'browse' ? fb.checked() : [];
  if (ticked.length) {
    submitEl.textContent = ticked.length === 1 ? 'Add 1 project' : `Add ${ticked.length} projects`;
    noteEl.textContent = 'Each ticked repository becomes a project of its own.';
    return;
  }
  const v = pathEl.value.trim();
  if (!v) {
    noteEl.textContent = step === 'main' ? 'Type a folder path or a pull request URL, or choose a way below.'
      : entries.some(e => e.git) ? 'Tick git repositories to add each as a project, or open a folder and Add it.'
      : 'Open a folder and Add it, or type its path.';
    return;
  }
  if (looksLikeURL(v)) {
    noteEl.textContent = 'A pull request: Add opens it for review.';
    return;
  }
  const f = folder;
  if (!f) {
    noteEl.textContent = '';
    return;
  }
  if (!f.exists) {
    noteEl.textContent = 'No such folder.';
  } else if (f.project) {
    submitEl.textContent = 'Open';
    noteEl.textContent = 'Already a project: Open selects it.';
  } else if (f.git && f.top !== f.dir) {
    noteEl.innerHTML = `Inside the git repository ${code(tildify(f.top))}: Add adds that.`;
  } else if (f.git) {
    noteEl.textContent = f.empty ? 'Git repository with no commits yet: worktrees need a first commit.'
      : f.branch && f.branch !== 'HEAD' ? `Git repository, on ${f.branch}.` : 'Git repository, detached HEAD.';
  } else {
    const repos = step === 'browse' && f.dir === fb.dir().replace(/[/\\]+$/, '') ? entries.filter(e => e.git).length : 0;
    noteEl.textContent = repos
      ? `${repos === 1 ? 'A git repository is' : `${repos} git repositories are`} inside: tick ${repos === 1 ? 'it' : 'them'} to add each as a project, or Add adds this folder.`
      : 'Not a git repository yet: worktrees appear once you git init and commit.';
  }
}

async function loadRemoved() {
  removedEl.hidden = true;
  const res = await call('GET', 'api/projects/removed');
  if (!res.ok || !done) return;
  const list = res.data.filter(r => !projectAt(r.dir));
  removedEl.hidden = !list.length;
  removedList.innerHTML = list.map(r => `<li><button type="button" data-dir="${esc(r.dir)}" title="Add it again">
      <span><b>${esc(r.name)}</b><small>${esc(tildify(r.dir))}</small></span>
      ${r.worktrees ? `<span class="o-ap-count">${r.worktrees} worktree${r.worktrees === 1 ? '' : 's'}</span>` : ''}</button></li>`).join('');
}

/* ---------- doing it ---------- */

const showMsg = text => {
  msgEl.textContent = text || '';
  msgEl.hidden = !text;
};

function setBusy(on, label = '') {
  busy = on;
  submitEl.disabled = on;
  if (on) {
    submitEl.textContent = label;
    return;
  }
  render();
  // The disabled button dropped focus; Esc and Enter should still reach the form.
  if (done && !form.contains(document.activeElement)) stepInput().focus();
}

const stepInput = () => (step === 'clone' ? urlEl : step === 'create' ? nameEl : pathEl);

function submit() {
  if (busy) return;
  if (step === 'clone') return startClone();
  if (step === 'create') return create();
  const ticked = step === 'browse' ? fb.checked() : [];
  if (ticked.length) return addFolders(ticked);
  const v = pathEl.value.trim();
  if (!v) return showMsg('Pick a folder, or type its path.');
  if (looksLikeURL(v)) return openPR(v);
  addFolders([v]);
}

// addFolders adds each folder as a project and selects the last one added.
async function addFolders(dirs) {
  showMsg('');
  setBusy(true, 'Adding…');
  let last = null;
  const failed = [];
  for (const dir of dirs) {
    const res = await call('POST', 'api/projects', { dir });
    if (res.ok) last = res.data;
    else failed.push(`${tildify(dir)}: ${res.data.error}`);
  }
  setBusy(false);
  if (last) await finish(last.workspace);
  if (failed.length) {
    showMsg(failed.join('\n'));
    fb.browse(fb.dir());
    return;
  }
  close();
}

// finish selects the new project's workspace and remembers where it went.
async function finish(workspace) {
  await reload();
  const p = state.projects.find(x => x.workspaces.some(w => w.id === workspace));
  if (p) remember(p.dir);
  selectWorkspace(workspace);
}

async function create() {
  showMsg('');
  setBusy(true, 'Creating…');
  const res = await call('POST', 'api/projects/new', { name: nameEl.value.trim(), parent: fb.dir() });
  setBusy(false);
  if (!res.ok) return showMsg(res.data.error);
  await finish(res.data.workspace);
  close();
}

/* ---------- cloning ---------- */

// repoFolderName is the folder git clone would make for url (addproject.go).
function repoFolderName(url) {
  let u = url.trim().replace(/[/\\]+$/, '').replace(/\.git$/, '').replace(/[/\\]+$/, ''); // repo/.git is repo
  u = u.slice(Math.max(u.lastIndexOf('/'), u.lastIndexOf('\\'), u.lastIndexOf(':')) + 1);
  return u === '.' || u === '..' || u.startsWith('-') ? '' : u;
}

async function startClone() {
  showMsg('');
  setBusy(true, 'Cloning…');
  const res = await call('POST', 'api/clones', { url: urlEl.value.trim(), parent: fb.dir() });
  if (!res.ok) {
    setBusy(false);
    return showMsg(res.data.error);
  }
  job = { id: res.data.id };
  urlEl.disabled = true;
  cancelEl.textContent = 'Cancel clone';
  showProgress(res.data);
  poll();
}

async function poll() {
  if (!job) return;
  const res = await call('GET', `api/clones/${encodeURIComponent(job.id)}`);
  if (!job) return;
  if (!res.ok) return endClone(res.data.error);
  showProgress(res.data);
  if (!res.data.done) {
    job.timer = setTimeout(poll, 400);
    return;
  }
  if (res.data.error) return endClone(res.data.error === 'cancelled' ? '' : res.data.error);
  endClone('');
  await finish(res.data.workspace);
  close();
}

function showProgress(j) {
  progressEl.hidden = false;
  $('#o-ap-phase').textContent = j.phase || 'Starting git clone…';
  $('#o-ap-percent').textContent = j.phase ? `${j.percent}%` : '';
  $('#o-ap-bar').style.width = `${j.phase ? j.percent : 0}%`;
}

function endClone(error) {
  if (job) clearTimeout(job.timer);
  job = null;
  urlEl.disabled = false;
  cancelEl.textContent = 'Cancel';
  progressEl.hidden = true;
  setBusy(false);
  showMsg(error);
}

// cancelClone stops the clone; the server deletes what it had cloned.
function cancelClone() {
  if (!job) return;
  call('DELETE', `api/clones/${encodeURIComponent(job.id)}`);
  endClone('');
}

/* ---------- pull requests ---------- */

// openPR opens a pull request for review through the same /api/wb/open the
// editor's Open Pull Request uses. A PR whose repository has no known clone
// first asks for one.
async function openPR(target, clone) {
  showMsg('');
  setBusy(true, 'Opening…');
  const res = await call('POST', 'api/wb/open', clone ? { target, clone } : { target });
  setBusy(false);
  if (!res.ok) return showMsg(res.data.error);
  if (res.data.needClone) return askForClone(target, res.data.repo, res.data.managed);
  await reload();
  close();
}

function askForClone(prURL, repo, managed) {
  pendingPR = prURL;
  $('#o-clone-repo').textContent = repo;
  $('#o-clone-dest').textContent = managed;
  $('#o-clone-dest').title = managed;
  cloneEl.hidden = false;
  submitEl.hidden = true;
  clonePathEl.value = '';
  clonePathEl.focus();
}

function hideClone() {
  pendingPR = '';
  cloneEl.hidden = true;
  submitEl.hidden = false;
}
