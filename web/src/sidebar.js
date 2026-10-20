// web/src/sidebar.js
// Projects and their workspaces: selecting, adding and removing them. Each
// workspace is one feature; local ones share the project folder, worktree
// ones own a folder and branch.
import { state, subscribe, reload, selectWorkspace, api, esc } from './store.js';

let listEl, formEl, nameEl, wtRowEl, wtEl, baseEl, submitEl;
let formFor = null; // { mode: 'project' } or { mode: 'workspace', project }
const collapsed = new Set(JSON.parse(localStorage.getItem('punk0.collapsed') || '[]'));

export function initSidebar(container) {
  listEl = container.querySelector('#projects-list');
  formEl = container.querySelector('#side-form');
  nameEl = container.querySelector('#form-name');
  wtRowEl = container.querySelector('#form-wt-row');
  wtEl = container.querySelector('#form-wt');
  baseEl = container.querySelector('#form-base');
  submitEl = container.querySelector('#form-submit');

  container.querySelector('#btn-add-project').addEventListener('click', () => openForm({ mode: 'project' }));
  container.querySelector('#form-cancel').addEventListener('click', closeForm);
  submitEl.addEventListener('click', submit);
  wtEl.addEventListener('change', () => baseEl.classList.toggle('hidden', !wtEl.checked));
  formEl.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') submit();
    if (e.key === 'Escape') closeForm();
  });
  listEl.addEventListener('click', onListClick);
  subscribe(render);
}

function render() {
  const projects = state.projects;
  if (!projects.length) {
    listEl.innerHTML = `
      <div class="side-empty">
        <p>No projects yet.</p>
        <button type="button" class="btn-accent" data-act="add-project">Add project</button>
      </div>`;
    return;
  }
  listEl.innerHTML = projects.map(renderProject).join('');
}

function renderProject(p) {
  const open = !collapsed.has(p.id);
  return `
    <div class="proj" data-project="${esc(p.id)}">
      <div class="proj-row" data-act="toggle" title="${esc(p.dir)}">
        <span class="proj-arrow">${open ? '▾' : '▸'}</span>
        <span class="proj-name">${esc(p.name)}</span>
        ${p.branch ? `<span class="proj-branch">${esc(p.branch)}</span>` : ''}
        <button type="button" class="icon-btn" data-act="add-ws" title="New workspace">+</button>
        <button type="button" class="icon-btn del" data-act="del-project" title="Remove project from punk0">×</button>
      </div>
      ${open ? p.workspaces.map(renderWorkspace).join('') : ''}
    </div>`;
}

function renderWorkspace(w) {
  const running = w.tabs.some((t) => t.running);
  let chip = '<span class="chip">local</span>';
  if (w.missing) chip = '<span class="chip missing" title="Folder deleted outside punk0">missing</span>';
  else if (w.kind === 'worktree') chip = `<span class="chip wt" title="Own worktree">⑂ ${esc(w.branch)}</span>`;
  return `
    <div class="ws-row${w.id === state.ws ? ' active' : ''}${w.missing ? ' missing' : ''}" data-ws="${esc(w.id)}" title="${esc(w.dir)}">
      <span class="dot${running ? ' on' : ''}"></span>
      <span class="ws-name">${esc(w.name)}</span>
      ${chip}
      <button type="button" class="icon-btn del" data-act="del-ws" title="Delete workspace">×</button>
    </div>`;
}

async function onListClick(e) {
  const act = e.target.closest('[data-act]')?.dataset.act;
  const projectId = e.target.closest('[data-project]')?.dataset.project;
  const project = state.projects.find((p) => p.id === projectId);
  const wsId = e.target.closest('[data-ws]')?.dataset.ws;

  if (act === 'add-project') return openForm({ mode: 'project' });
  if (act === 'add-ws') return openForm({ mode: 'workspace', project });
  if (act === 'del-project') return removeProject(project);
  if (act === 'del-ws') return removeWorkspace(project.workspaces.find((w) => w.id === wsId));
  if (wsId) return selectWorkspace(wsId);
  if (act === 'toggle') {
    collapsed.has(projectId) ? collapsed.delete(projectId) : collapsed.add(projectId);
    localStorage.setItem('punk0.collapsed', JSON.stringify([...collapsed]));
    render();
  }
}

async function removeProject(p) {
  if (!confirm(`Remove "${p.name}" from punk0? Its tabs stop. No files are deleted, and its worktrees come back if you add it again.`)) return;
  const res = await api('DELETE', `/api/projects/${encodeURIComponent(p.id)}`);
  if (!res.ok) return alert(res.data.error);
  reload();
}

async function removeWorkspace(w) {
  const what = w.kind === 'worktree'
    ? `Delete "${w.name}"? Its tabs stop and its worktree folder is removed. Branch ${w.branch} is kept.`
    : `Delete "${w.name}"? Its tabs stop. The project folder is not touched.`;
  if (!confirm(what)) return;
  const path = `/api/workspaces/${encodeURIComponent(w.id)}`;
  let res = await api('DELETE', path);
  if (res.status === 409) {
    if (!confirm(`"${w.name}" has uncommitted changes. Delete them with the worktree?`)) return;
    res = await api('DELETE', `${path}?force=1`);
  }
  if (!res.ok) return alert(res.data.error);
  reload();
}

function openForm(f) {
  formFor = f;
  const ws = f.mode === 'workspace';
  nameEl.value = '';
  nameEl.placeholder = ws ? `new workspace in ${f.project.name}` : 'folder path, e.g. ~/Code/app';
  wtEl.checked = false;
  wtRowEl.classList.toggle('hidden', !ws || !f.project.git);
  baseEl.value = '';
  baseEl.classList.add('hidden');
  submitEl.textContent = ws ? 'Create' : 'Add';
  formEl.classList.remove('hidden');
  nameEl.focus();
}

function closeForm() {
  formFor = null;
  formEl.classList.add('hidden');
}

async function submit() {
  const value = nameEl.value.trim();
  if (!value || !formFor) return;
  let res;
  if (formFor.mode === 'project') {
    res = await api('POST', '/api/projects', { dir: value });
  } else {
    res = await api('POST', `/api/projects/${encodeURIComponent(formFor.project.id)}/workspaces`, {
      name: value,
      worktree: wtEl.checked,
      base: baseEl.value.trim(),
    });
  }
  if (!res.ok) return alert(res.data.error);
  closeForm();
  await reload();
  // A new project opens on its first workspace.
  selectWorkspace(res.data.workspaces ? res.data.workspaces[0].id : res.data.id);
}
