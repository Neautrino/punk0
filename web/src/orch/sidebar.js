// web/src/orch/sidebar.js
// Left pane, in two views behind a tab: Projects (projects, their
// workspaces, and the pull requests open for review) and Scratch (chats that
// belong to no project). Each workspace is one feature: local ones share the
// project folder, worktree ones own a folder and branch, scratch chats own a
// folder outside any project. A folder's editor (px0's single-folder view)
// runs in a child the parent starts on demand; its state is the dot. The
// header's filter narrows either view by name or branch, or to what runs.
import { $, esc } from '../state.js';
import { call, failed, openEditor } from './api.js';
import { state, subscribe, reload, selectWorkspace, workspace, tildify } from './store.js';
import { attachComplete, looksLikeURL } from './complete.js';

const ICON_PLUS = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>';
const ICON_X = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18"/></svg>';
const ICON_EDITOR = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3h7v7"/><path d="M10 14 21 3"/><path d="M21 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5"/></svg>';
const ICON_STOP = '<svg viewBox="0 0 24 24" width="11" height="11" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>';
const ICON_PR = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="18" r="2.5"/><path d="M6 8.5v7M18 15.5V9a3 3 0 0 0-3-3h-4"/><polyline points="13 4 11 6 13 8"/></svg>';
const ICON_FOLDER = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/></svg>';
const ICON_FOLDER_OPEN = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 18V7a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v1"/><path d="M3 18l2.6-6.5A2 2 0 0 1 7.5 10H21l-2.7 7.2a2 2 0 0 1-1.9 1.3H3Z"/></svg>';

// The tab status that most needs a look wins a workspace's badge.
const URGENCY = ['waiting', 'working', 'idle'];
const STATUS_LABEL = { waiting: 'needs you', working: 'working', idle: 'idle' };

const collapsed = new Set(JSON.parse(localStorage.getItem('punk0.collapsed') || '[]'));
let view = localStorage.getItem('punk0.sideView') === 'scratch' ? 'scratch' : 'projects';
const filter = { text: '', running: false };

let listEl, formEl, nameEl, wtRowEl, wtEl, baseEl, submitEl, msgEl, cloneEl, clonePathEl;
let formFor = null; // { mode: 'project' } or { mode: 'workspace', project, worktree }
let pendingPR = ''; // the PR URL waiting for its clone to be chosen

export function initSidebar() {
  listEl = $('#o-projects');
  formEl = $('#o-form');
  nameEl = $('#o-form-name');
  wtRowEl = $('#o-form-wt-row');
  wtEl = $('#o-form-wt');
  baseEl = $('#o-form-base');
  submitEl = $('#o-form-submit');
  msgEl = $('#o-form-msg');
  cloneEl = $('#o-clone');
  clonePathEl = $('#o-clone-path');

  attachComplete(nameEl, $('#o-suggest'));
  attachComplete(clonePathEl, $('#o-clone-suggest'));
  // + adds a project in the Projects view, a chat in the Scratch view.
  $('#o-add-project').addEventListener('click', () => {
    const scratch = state.projects.find(p => p.scratch);
    if (view === 'scratch') return scratch && openForm({ mode: 'workspace', project: scratch });
    openForm({ mode: 'project' });
  });
  $('#o-add-feature').addEventListener('click', () => {
    const project = featureProject();
    if (project) openForm({ mode: 'workspace', project, worktree: true });
  });
  $('.o-side-tabs').addEventListener('click', e => {
    const v = e.target.closest('[data-view]')?.dataset.view;
    if (v && v !== view) setView(v);
  });
  initFilter();
  $('#o-form-cancel').addEventListener('click', closeForm);
  wtEl.addEventListener('change', () => { baseEl.hidden = !wtEl.checked; });
  formEl.addEventListener('submit', e => {
    e.preventDefault();
    submitForm();
  });
  formEl.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeForm();
  });
  $('#o-clone-use').addEventListener('click', () => {
    const p = clonePathEl.value.trim();
    if (p && pendingPR) openTarget(pendingPR, p);
  });
  clonePathEl.addEventListener('keydown', e => {
    if (e.key !== 'Enter') return;
    e.preventDefault(); // not the form's submit: this field answers the clone question
    $('#o-clone-use').click();
  });
  $('#o-clone-managed').addEventListener('click', () => pendingPR && openTarget(pendingPR, 'managed'));
  listEl.addEventListener('click', onListClick);
  subscribe(renderSidebar);

  // ?open=<url> comes from a workspace's holding page when a PR has no known
  // clone: ask for it here.
  const target = new URLSearchParams(location.search).get('open');
  if (target) {
    history.replaceState(null, '', location.pathname);
    if (view !== 'projects') setView('projects');
    openForm({ mode: 'project' });
    nameEl.value = target;
    openTarget(target);
  }
}

function setView(v) {
  view = v;
  localStorage.setItem('punk0.sideView', v);
  if (formFor) closeForm(); // its + may belong to the other view
  renderSidebar();
}

function initFilter() {
  const box = $('#o-filter');
  const btn = $('#o-filter-btn');
  const input = $('#o-filter-input');
  const running = $('#o-filter-running');
  const sync = () => {
    filter.text = input.value.trim().toLowerCase();
    filter.running = running.checked;
    btn.classList.toggle('on', !!(filter.text || filter.running));
    renderSidebar();
  };
  btn.addEventListener('click', () => {
    box.hidden = !box.hidden;
    btn.setAttribute('aria-pressed', String(!box.hidden));
    if (!box.hidden) input.focus();
  });
  input.addEventListener('input', sync);
  running.addEventListener('change', sync);
  // Esc clears the filter and folds it away.
  box.addEventListener('keydown', e => {
    if (e.key !== 'Escape') return;
    input.value = '';
    running.checked = false;
    box.hidden = true;
    btn.setAttribute('aria-pressed', 'false');
    sync();
    btn.focus();
  });
}

// featureProject is where the header's new-worktree button adds one: the
// selected workspace's project, else the first project.
function featureProject() {
  const projects = state.projects.filter(p => !p.scratch);
  return projects.find(p => p.workspaces.some(w => w.id === state.ws)) || projects[0] || null;
}

const filtering = () => !!(filter.text || filter.running);
const isRunning = w => w.backend === 'running' || w.tabs.some(t => t.running);
const textMatch = (...s) => !filter.text || s.some(x => x && x.toLowerCase().includes(filter.text));

// shownWorkspaces is what the filter leaves of a project's workspaces: all
// of them when the project's own name matches, else those whose name or
// branch does.
function shownWorkspaces(p) {
  const own = !p.scratch && filter.text && textMatch(p.name);
  return p.workspaces.filter(w => (!filter.running || isRunning(w)) && (own || textMatch(w.name, w.branch)));
}

function renderSidebar() {
  for (const b of document.querySelectorAll('.o-side-tabs [data-view]')) b.setAttribute('aria-selected', String(b.dataset.view === view));
  const scratch = state.projects.find(p => p.scratch);
  $('#o-list-title').textContent = view === 'scratch' ? 'Chats' : 'Projects';
  $('#o-add-feature').hidden = view === 'scratch';
  $('#o-add-feature').disabled = !featureProject();
  const add = $('#o-add-project');
  add.title = view === 'scratch' ? 'New scratch chat' : 'Add a folder or a pull request';
  add.setAttribute('aria-label', add.title);
  listEl.innerHTML = view === 'scratch' ? renderScratch(scratch) : renderProjects();
}

function renderProjects() {
  const projects = state.projects.filter(p => !p.scratch);
  if (!projects.length) return `
    <div class="o-side-empty">
      <p>No projects yet.</p>
      <button type="button" class="wb-btn wb-primary" data-act="add-project">Add a folder</button>
    </div>`;
  const html = projects.map(renderProject).join('') + renderReviews();
  return html.trim() ? html : '<p class="o-note">Nothing matches the filter.</p>';
}

function renderProject(p) {
  const ws = shownWorkspaces(p);
  if (filtering() && !ws.length) return '';
  // A filter shows every match, folded projects included.
  const open = filtering() || !collapsed.has(p.id);
  return `
    <div class="o-proj" data-project="${esc(p.id)}">
      <div class="o-proj-row" data-act="toggle" title="${esc(tildify(p.dir))}" aria-expanded="${open}">
        <span class="o-proj-ico">${open ? ICON_FOLDER_OPEN : ICON_FOLDER}</span>
        <span class="o-proj-name">${esc(p.name)}</span>
        <span class="o-proj-branch">${esc(p.branch || '')}</span>
        <span class="o-acts">
          <button type="button" class="mini" data-act="add-ws" title="New workspace" aria-label="New workspace">${ICON_PLUS}</button>
          <button type="button" class="mini o-del" data-act="del-project" title="Remove project from punk0" aria-label="Remove project">${ICON_X}</button>
        </span>
      </div>
      ${open ? ws.map(renderWorkspace).join('') : ''}
    </div>`;
}

function renderScratch(p) {
  if (!p) return '';
  if (!p.workspaces.length) return `
    <div class="o-side-empty">
      <p>No scratch chats yet.</p>
      <p class="wb-faint">A chat with Claude in a folder of its own, outside any project.</p>
      <button type="button" class="wb-btn wb-primary" data-act="add-ws" data-project="${esc(p.id)}">New chat</button>
    </div>`;
  const ws = shownWorkspaces(p);
  if (!ws.length) return '<p class="o-note">Nothing matches the filter.</p>';
  return `<div class="o-proj o-scratch" data-project="${esc(p.id)}" title="${esc(tildify(p.dir))}">${ws.map(renderWorkspace).join('')}</div>`;
}

// statusOf is the most urgent status of a workspace's running tabs.
function statusOf(w) {
  const statuses = w.tabs.filter(t => t.running).map(t => t.status);
  return URGENCY.find(s => statuses.includes(s)) || '';
}

const BACKEND_TITLE = {
  stopped: 'Editor stopped: starts when opened',
  starting: 'Editor starting',
  running: 'Editor running',
  failed: 'Editor failed to start',
};

function renderWorkspace(w) {
  let chip = '';
  if (w.missing) chip = '<span class="o-chip o-missing" title="Folder deleted outside punk0">missing</span>';
  else if (w.kind === 'worktree') chip = `<span class="o-chip o-wt" title="Own worktree on branch ${esc(w.branch)}">⑂ ${esc(w.branch)}</span>`;
  const st = statusOf(w);
  const running = w.tabs.filter(t => t.running).length;
  const badge = running ? `<span class="o-badge${st ? ' ' + st : ''}" title="${running} running${st ? ', ' + STATUS_LABEL[st] : ''}">${st ? esc(STATUS_LABEL[st]) : running}</span>` : '';
  const lspTitle = w.lsp
    ? 'Language servers on for this folder (click to turn off; restarts its editor)'
    : 'Language servers off for this folder (click to turn on; restarts its editor)';
  return `
    <div class="o-ws-row${w.id === state.ws ? ' active' : ''}${w.missing ? ' missing' : ''}" data-ws="${esc(w.id)}" title="${esc(tildify(w.dir))}">
      <span class="o-backend ${esc(w.backend || 'stopped')}" title="${esc(BACKEND_TITLE[w.backend] || w.backend)}"></span>
      <span class="o-ws-name">${esc(w.name)}</span>
      ${chip}${badge}
      <span class="o-acts">
        <button type="button" class="mini o-lsp${w.lsp ? ' on' : ''}" data-act="lsp" title="${lspTitle}" aria-pressed="${!!w.lsp}">LSP</button>
        ${w.missing ? '' : `<button type="button" class="mini" data-act="editor" title="Open in editor" aria-label="Open in editor">${ICON_EDITOR}</button>`}
        <button type="button" class="mini o-del" data-act="del-ws" title="Delete workspace" aria-label="Delete workspace">${ICON_X}</button>
      </span>
    </div>`;
}

function renderReviews() {
  const items = state.reviews.filter(it => (!filter.running || it.state === 'running') && textMatch(it.title, it.repo, String(it.number)));
  if (!items.length) return '';
  return `
    <div class="o-proj o-reviews">
      <div class="o-section"><span>Reviews</span></div>
      ${items.map(renderReview).join('')}
    </div>`;
}

function renderReview(it) {
  const title = it.title || `#${it.number}`;
  const sub = it.state === 'starting' && it.progress ? it.progress : `${it.repo}#${it.number}`;
  const st = it.state === 'stopped' && !it.error ? '' :
    `<span class="wb-state ${esc(it.state)}" title="${esc(it.error || it.state)}"></span>`;
  return `
    <div class="o-pr-row" data-pr="${esc(it.id)}" title="${esc(it.url || '')}">
      <span class="o-pr-ico">${ICON_PR}</span>
      <span class="o-pr-text"><span class="o-pr-title">${esc(title)}</span><span class="o-pr-sub">${esc(sub)}</span></span>
      ${st}
      <span class="o-acts">
        ${it.state === 'running' ? `<button type="button" class="mini" data-act="pr-stop" title="Stop its editor" aria-label="Stop">${ICON_STOP}</button>` : ''}
        <button type="button" class="mini o-del" data-act="pr-remove" title="Remove from the list" aria-label="Remove">${ICON_X}</button>
      </span>
    </div>`;
}

async function onListClick(e) {
  const act = e.target.closest('[data-act]')?.dataset.act;
  const projectId = e.target.closest('[data-project]')?.dataset.project;
  const project = state.projects.find(p => p.id === projectId);
  const w = workspace(e.target.closest('[data-ws]')?.dataset.ws);
  const prId = e.target.closest('[data-pr]')?.dataset.pr;

  if (prId) return reviewAction(state.reviews.find(r => r.id === prId), act);
  if (act === 'add-project') return openForm({ mode: 'project' });
  if (act === 'add-ws') return openForm({ mode: 'workspace', project });
  if (act === 'del-project') return removeProject(project);
  if (w && act === 'del-ws') return removeWorkspace(w);
  if (w && act === 'editor') return openEditor(w.href);
  if (w && act === 'lsp') return toggleLsp(w);
  if (w) return w.id !== state.ws && selectWorkspace(w.id);
  if (act === 'toggle') {
    if (collapsed.has(projectId)) collapsed.delete(projectId);
    else collapsed.add(projectId);
    localStorage.setItem('punk0.collapsed', JSON.stringify([...collapsed]));
    renderSidebar();
  }
}

async function reviewAction(it, act) {
  if (!it) return;
  if (!act) return openEditor(it.href);
  const res = await call('POST', act === 'pr-stop' ? 'api/wb/stop' : 'api/wb/remove', { id: it.id });
  if (!res.ok) failed(res);
  reload();
}

async function toggleLsp(w) {
  const res = await call('POST', `api/workspaces/${encodeURIComponent(w.id)}/lsp`, { on: !w.lsp });
  if (!res.ok) failed(res);
  reload();
}

async function removeProject(p) {
  if (!confirm(`Remove "${p.name}" from punk0? Its tabs stop. No files are deleted, and its worktrees come back if you add it again.`)) return;
  const res = await call('DELETE', `api/projects/${encodeURIComponent(p.id)}`);
  if (!res.ok) failed(res);
  reload();
}

async function removeWorkspace(w) {
  const what = {
    worktree: `Delete "${w.name}"? Its tabs stop and its worktree folder is removed. Branch ${w.branch} is kept.`,
    local: `Delete "${w.name}"? Its tabs stop. The project folder is not touched.`,
    folder: `Delete chat "${w.name}"? Its tabs stop. Its folder ${w.dir} is kept.`,
  }[w.kind];
  if (!confirm(what)) return;
  const path = `api/workspaces/${encodeURIComponent(w.id)}`;
  let res = await call('DELETE', path);
  if (res.status === 409 && res.data?.dirty) {
    if (!confirm(`"${w.name}" has uncommitted changes. Delete them with the worktree?`)) return;
    res = await call('DELETE', `${path}?force=1`);
  }
  if (!res.ok) failed(res);
  reload();
}

function openForm(f) {
  formFor = f;
  const ws = f.mode === 'workspace';
  hideClone();
  showMsg('');
  nameEl.value = '';
  // The header's feature button asks for a worktree: its own folder and
  // branch. A folder that is not a git repository can only have local ones.
  const wt = ws && !!f.worktree && f.project.git;
  nameEl.placeholder = !ws ? '~/path/to/folder or pull request URL'
    : f.project.scratch ? 'new scratch chat, e.g. research-auth'
    : wt ? `new feature branch in ${f.project.name}` : `new workspace in ${f.project.name}`;
  nameEl.setAttribute('aria-label', !ws ? 'Folder path or pull request URL' : 'Workspace name');
  wtEl.checked = wt;
  wtRowEl.hidden = !ws || !f.project.git;
  baseEl.value = '';
  baseEl.hidden = !wt;
  submitEl.textContent = ws ? 'Create' : 'Add';
  formEl.hidden = false;
  nameEl.focus();
}

function closeForm() {
  formFor = null;
  hideClone();
  formEl.hidden = true;
}

const showMsg = text => {
  msgEl.textContent = text || '';
  msgEl.hidden = !text;
};

async function submitForm() {
  const value = nameEl.value.trim();
  if (!value || !formFor) return;
  if (formFor.mode === 'project') return openTarget(value);
  const res = await call('POST', `api/projects/${encodeURIComponent(formFor.project.id)}/workspaces`, {
    name: value,
    worktree: wtEl.checked,
    base: baseEl.value.trim(),
  });
  if (!res.ok) return showMsg(res.data.error);
  closeForm();
  await reload();
  selectWorkspace(res.data.id);
}

// openTarget adds a folder as a project, or opens a pull request for review,
// through the same /api/wb/open the editor's Open Pull Request uses. A PR
// whose repository has no known clone first asks for one.
async function openTarget(target, clone) {
  showMsg('');
  const res = await call('POST', 'api/wb/open', clone ? { target, clone } : { target });
  if (!res.ok) return showMsg(res.data.error);
  if (res.data.needClone) return askForClone(target, res.data.repo, res.data.managed);
  closeForm();
  await reload();
  if (!looksLikeURL(target) && res.data.workspace) selectWorkspace(res.data.workspace);
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
