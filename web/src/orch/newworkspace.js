// web/src/orch/newworkspace.js
// New workspace, a panel beside the Projects sidebar: which project, where the
// work runs (a new worktree with its own branch, or the project folder), what
// it starts from, and the agent its first tab runs.
//
// As it is filled in, GET api/projects/{id}/plan (newworkspace.go) says what
// creating it would do -- the branch, what it starts from, the folder -- or
// why it cannot, before anything is made. A pull request typed in is looked
// up on its forge: it can be reviewed (the editor's PR view) or worked on in
// a worktree on its branch. Each project's last choices are offered again.
import { $, esc } from '../state.js';
import { showToast } from '../ui.js';
import { call, openEditor } from './api.js';
import { state, reload, selectWorkspace, tildify } from './store.js';
import { getSetting, visibleAgents, defaultAgent } from './settings.js';
import { placeFlyout } from './flyout.js';

const PREFS = 'punk0.newWorkspace';

let form, projectEl, whereEls, nameEl, baseEl, baseRow, refsEl, planEl, agentEl, lspRow, lspEl;
let msgEl, createEl, reviewEl;
let done = null; // resolves the open panel's promise
let anchor = null;
let where = 'worktree';
let plan = null; // the last plan answered for what the panel holds
let planSeq = 0;
let planTimer = 0;
let defaultBase = '';
let busy = false;

export function initNewWorkspace() {
  form = $('#o-nw-form');
  projectEl = $('#o-nw-project');
  whereEls = [...form.querySelectorAll('[data-where]')];
  nameEl = $('#o-nw-name');
  baseEl = $('#o-nw-base');
  baseRow = $('#o-nw-base-row');
  refsEl = $('#o-nw-refs');
  planEl = $('#o-nw-plan');
  agentEl = $('#o-nw-agent');
  lspRow = $('#o-nw-lsp-row');
  lspEl = $('#o-nw-lsp');
  msgEl = $('#o-nw-msg');
  createEl = $('#o-nw-create');
  reviewEl = $('#o-nw-review');

  form.addEventListener('submit', e => {
    e.preventDefault();
    create();
  });
  form.addEventListener('keydown', e => {
    if (e.key === 'Escape') {
      e.preventDefault();
      close('');
    } else if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      create();
    }
  });
  $('#o-nw-close').addEventListener('click', () => close(''));
  $('#o-nw-cancel').addEventListener('click', () => close(''));
  reviewEl.addEventListener('click', review);
  // A click outside closes it, except on the buttons that open it, which
  // move it instead.
  document.addEventListener('mousedown', e => {
    if (done && !form.contains(e.target) && !e.target.closest?.('[data-act=add-ws], #o-add-feature')) close('');
  });
  addEventListener('resize', () => done && placeFlyout(form, anchor));
  new ResizeObserver(() => done && placeFlyout(form, anchor)).observe(form);

  projectEl.addEventListener('change', loadProject);
  for (const b of whereEls) b.addEventListener('click', () => setWhere(b.dataset.where));
  for (const el of [nameEl, baseEl]) el.addEventListener('input', schedulePlan);
}

// openNewWorkspace shows the panel for project, pointing at from. It
// resolves to the workspace made, or '' when none was. Opened again
// while open, it moves and switches project.
export function openNewWorkspace(project, from) {
  anchor = from.getBoundingClientRect();
  const projects = state.projects.filter(p => !p.scratch);
  projectEl.innerHTML = projects.map(p => `<option value="${esc(p.id)}" title="${esc(tildify(p.dir))}">${esc(p.name)}</option>`).join('');
  projectEl.value = project.id;
  if (!done) {
    nameEl.value = '';
    showMsg('');
    form.hidden = false;
    loadAgents();
  }
  loadProject();
  placeFlyout(form, anchor);
  nameEl.focus();
  if (done) return Promise.resolve('');
  return new Promise(resolve => { done = resolve; });
}

// closeNewWorkspace cancels an open panel, as switching sidebar views does.
export const closeNewWorkspace = () => close('');

function close(id) {
  if (!done) return;
  form.hidden = true;
  clearTimeout(planTimer);
  planSeq++;
  done(id);
  done = null;
}

const project = () => state.projects.find(p => p.id === projectEl.value);

function loadPrefs() {
  try {
    return JSON.parse(localStorage.getItem(PREFS)) || {};
  } catch {
    return {};
  }
}

// loadProject fills in what depends on the project: whether it can have
// worktrees, its branches as start points, and the choices last made in it.
async function loadProject() {
  const p = project();
  if (!p) return;
  const prefs = loadPrefs()[p.id] || {};
  where = p.git ? (prefs.where || 'worktree') : 'local';
  baseEl.value = prefs.base || '';
  lspEl.checked = 'lsp' in prefs ? !!prefs.lsp : !!getSetting('workspace.lspDefault');
  chooseAgent(prefs);
  whereEls.find(b => b.dataset.where === 'worktree').disabled = !p.git;
  setWhere(where);
  refsEl.innerHTML = '';
  defaultBase = '';
  baseEl.placeholder = 'default branch';
  if (!p.git) return;
  const res = await call('GET', `api/projects/${encodeURIComponent(p.id)}/refs`);
  if (!res.ok || project() !== p) return;
  // A repository with no commits has nothing for a worktree to start from.
  const wt = whereEls.find(b => b.dataset.where === 'worktree');
  wt.disabled = res.data.empty;
  wt.title = res.data.empty ? 'No commits yet: make a first commit to create worktrees' : 'Its own folder and branch, beside the project folder';
  if (res.data.empty) setWhere('local');
  defaultBase = res.data.default;
  refsEl.innerHTML = res.data.refs.map(r => `<option value="${esc(r)}"></option>`).join('');
  baseEl.placeholder = defaultBase || 'default branch';
}

function setWhere(w) {
  where = w;
  for (const b of whereEls) b.setAttribute('aria-checked', String(b.dataset.where === w));
  const wt = w === 'worktree';
  baseRow.hidden = lspRow.hidden = !wt;
  nameEl.placeholder = wt ? 'Name, branch, #123 or pull request URL' : 'Name of this workspace';
  form.querySelector('label[for=o-nw-name]').textContent = wt ? 'Name or start from' : 'Name';
  schedulePlan();
}

// loadAgents fills the agent list: none (the welcome view), installed agents,
// a plain shell, then those not installed, shown but disabled; agents in
// agents.hidden are left out.
async function loadAgents() {
  agentEl.innerHTML = '<option value="">None (welcome screen)</option>';
  const res = await call('GET', 'api/agents');
  if (!done) return;
  const agents = res.ok ? res.data : [];
  const hidden = new Set(getSetting('agents.hidden') || []);
  agentEl.innerHTML = '<option value="">None (welcome screen)</option>' +
    visibleAgents(agents).map(a => `<option value="${esc(a.id)}">${esc(a.label)}</option>`).join('') +
    agents.filter(a => !a.installed && !hidden.has(a.id)).map(a => `<option value="${esc(a.id)}" disabled>${esc(a.label)} (not installed)</option>`).join('');
  chooseAgent(loadPrefs()[projectEl.value] || {});
}

// chooseAgent selects the project's last choice (None included), else
// agents.default.
function chooseAgent(prefs) {
  const usable = id => [...agentEl.options].some(o => o.value === id && !o.disabled);
  if ('agent' in prefs && usable(prefs.agent)) agentEl.value = prefs.agent;
  else agentEl.value = defaultAgent([...agentEl.options].filter(o => o.value && !o.disabled).map(o => ({ id: o.value })));
}

/* ---------- the plan ---------- */

function body() {
  return {
    name: nameEl.value.trim(),
    worktree: where === 'worktree',
    base: where === 'worktree' ? baseEl.value.trim() : '',
  };
}

// schedulePlan asks for the plan once typing pauses; an answer to an older
// question is dropped.
function schedulePlan() {
  clearTimeout(planTimer);
  const seq = ++planSeq;
  plan = null;
  if (!body().name) return renderPlan(null);
  planEl.classList.add('pending');
  planTimer = setTimeout(async () => {
    const p = project();
    if (!p) return;
    const res = await call('GET', `api/projects/${encodeURIComponent(p.id)}/plan?` + new URLSearchParams({ ...body(), worktree: where === 'worktree' ? '1' : '' }));
    if (seq !== planSeq || !done) return;
    plan = res.ok ? res.data : { error: res.data.error };
    renderPlan(plan);
  }, 250);
}

const code = s => `<code>${esc(s)}</code>`;

function renderPlan(pl) {
  planEl.classList.remove('pending', 'err');
  reviewEl.hidden = !pl?.pr;
  createEl.textContent = pl?.pr ? 'Work on it' : 'Create';
  reviewEl.textContent = 'Review';
  if (!pl) {
    planEl.innerHTML = where === 'worktree'
      ? 'A new branch and folder are named after what you type.'
      : `Runs in ${code(tildify(project()?.dir || ''))}, beside the project's other local workspaces.`;
    return;
  }
  if (pl.error && !pl.pr) {
    planEl.classList.add('err');
    planEl.textContent = pl.error;
    return;
  }
  const dir = `<span class="o-nw-dir">→ ${esc(pl.dir)}</span>`;
  let what = '';
  switch (pl.action) {
    case 'local': what = `Runs in the project folder${pl.branch ? `, on ${code(pl.branch)}` : ''}.`; break;
    case 'existing': what = `Checks out the existing branch ${code(pl.branch)}.`; break;
    case 'track': what = `New branch ${code(pl.branch)} tracking ${code(pl.base)}, fetched first.`; break;
    case 'new': what = `New branch ${code(pl.branch)} from ${code(pl.base)}${pl.fetch ? `, fetched from ${esc(pl.fetch)} first` : ''}.`; break;
    case 'pr': {
      const pr = pl.pr;
      what = `<b>#${pr.number} ${esc(pr.title)}</b><br>` +
        `Review opens it in the editor. Work on it checks out ${code(pl.branch || pr.head)}${pr.fork ? ' (from a fork)' : ''} in a worktree.`;
      if (pl.error) {
        planEl.innerHTML = `${what}<br><span class="o-err">${esc(pl.error)}</span>`;
        return;
      }
      break;
    }
  }
  planEl.innerHTML = what + (pl.action === 'local' ? '' : `<br>${dir}`);
}

/* ---------- making it ---------- */

const showMsg = text => {
  msgEl.textContent = text || '';
  msgEl.hidden = !text;
};

async function create() {
  const p = project();
  const b = body();
  if (!p || busy) return;
  if (!b.name) {
    showMsg('Type a name, a branch or a pull request.');
    return nameEl.focus();
  }
  const agent = agentEl.value;
  setBusy(true, 'Creating…');
  const res = await call('POST', `api/projects/${encodeURIComponent(p.id)}/workspaces`, {
    ...b,
    agent,
    lsp: b.worktree && lspEl.checked,
  });
  setBusy(false);
  if (!res.ok) return showMsg(res.data.error);
  showMsg('');
  const all = loadPrefs();
  all[p.id] = { where, agent, base: baseEl.value.trim(), lsp: lspEl.checked };
  localStorage.setItem(PREFS, JSON.stringify(all));
  if (res.data.warning) showToast('!', res.data.warning, 6000);
  await reload();
  selectWorkspace(res.data.id);
  close(res.data.id);
}

// review opens the pull request in the editor's review view, with the
// project as its clone.
async function review() {
  const p = project();
  if (!p || !plan?.pr || busy) return;
  setBusy(true, 'Opening…');
  const res = await call('POST', 'api/wb/open', { target: plan.pr.url, clone: p.dir });
  setBusy(false);
  if (!res.ok) return showMsg(res.data.error);
  await reload();
  openEditor(res.data.href);
  close('');
}

function setBusy(on, label = '') {
  busy = on;
  createEl.disabled = reviewEl.disabled = on;
  if (on) (plan?.pr && label === 'Opening…' ? reviewEl : createEl).textContent = label;
  else renderPlan(plan);
}
