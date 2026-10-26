// web/src/orch/newworkspace.js
// New workspace, a panel beside the Projects sidebar: which project, its
// name, the branch to work on, whether it gets a worktree of its own
// (optionally on a new branch), and the agent its first tab runs.
//
// As it is filled in, GET api/projects/{id}/plan (newworkspace.go) says what
// creating it would do -- the branch, what it starts from, the folder, an
// existing worktree adopted or opened -- or why it cannot, before anything is
// made. Branch drops down the project folder's current branch, the project's
// worktrees and its other branches. Only the current branch can run in the
// project folder: git checks a branch out in one folder at a time, so any
// other branch, tag, commit or pull request gets a worktree. A pull request
// is looked up on its forge: it can be reviewed (the editor's PR view) or
// worked on in a worktree on its branch. Each project's last choices are
// offered again.
import { $, esc } from '../state.js';
import { showToast } from '../ui.js';
import { call, openEditor } from './api.js';
import { state, reload, selectWorkspace, tildify } from './store.js';
import { getSetting, visibleAgents, defaultAgent } from './settings.js';
import { placeFlyout } from './flyout.js';

const PREFS = 'punk0.newWorkspace';

let form, projectEl, nameEl, fromEl, fromRow, fromTag, wtEl, wtRow, wtHint, newRow, newEl;
let planEl, lspRow, lspEl, agentRow, agentEl, msgEl, createEl, reviewEl;
let fromList; // Branch's dropdown (pickList)
let done = null; // resolves the open panel's promise
let anchor = null;
let refs = null; // GET refs of a git project with commits: {current, head, refs, worktrees}; else null
let wantWorktree = true; // the checkbox as last chosen while it could be chosen
let plan = null; // the last plan answered for what the panel holds
let planSeq = 0;
let planTimer = 0;
let busy = false;

export function initNewWorkspace() {
  form = $('#o-nw-form');
  projectEl = $('#o-nw-project');
  nameEl = $('#o-nw-name');
  fromEl = $('#o-nw-from');
  fromRow = $('#o-nw-from-row');
  fromTag = $('#o-nw-from-tag');
  wtEl = $('#o-nw-wt');
  wtRow = $('#o-nw-wt-row');
  wtHint = $('#o-nw-wt-hint');
  newRow = $('#o-nw-newbranch-row');
  newEl = $('#o-nw-newbranch');
  planEl = $('#o-nw-plan');
  lspRow = $('#o-nw-lsp-row');
  lspEl = $('#o-nw-lsp');
  agentRow = $('#o-nw-agent-row');
  agentEl = $('#o-nw-agent');
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
  for (const el of [nameEl, fromEl, newEl]) el.addEventListener('input', sync);
  wtEl.addEventListener('change', () => {
    wantWorktree = wtEl.checked;
    sync();
  });
  planEl.addEventListener('click', e => e.target.closest('[data-act=prune]') && prune());
  fromList = pickList(fromEl, $('#o-nw-suggest'), fromRows);
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
    newEl.value = '';
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
  fromList.close();
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

// loadProject fills in what depends on the project: its branches and
// worktrees, and the choices last made in it. Before its refs arrive, and
// for a folder without git or commits, only a project-folder workspace can
// be made.
async function loadProject() {
  const p = project();
  if (!p) return;
  const prefs = loadPrefs()[p.id] || {};
  // where is what the panel remembered before the checkbox replaced it.
  wantWorktree = 'worktree' in prefs ? !!prefs.worktree : (prefs.where || 'worktree') === 'worktree';
  lspEl.checked = 'lsp' in prefs ? !!prefs.lsp : !!getSetting('workspace.lspDefault');
  chooseAgent(prefs);
  refs = null;
  fromEl.value = '';
  sync();
  if (p.git) await loadRefs(p);
}

// loadRefs asks for the project's current branch, branches and worktrees.
// Branch opens on the current branch unless something was typed already.
async function loadRefs(p) {
  const res = await call('GET', `api/projects/${encodeURIComponent(p.id)}/refs`);
  if (!res.ok || project() !== p) return;
  refs = res.data.empty ? null : res.data;
  if (refs && !fromEl.value) fromEl.value = refs.current;
  fromList.refresh();
  sync();
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

/* ---------- what Branch holds ---------- */

// linked is the project's worktrees the panel offers: not the repository's
// main checkout, which refs lists only so its branch is not offered.
const linked = () => (refs ? refs.worktrees.filter(w => !w.main) : []);

// fromKind classifies Branch: the project folder's own HEAD ('current': its
// branch, or empty), a pull request ('pr'), a linked worktree ('worktree': its
// branch or its folder), any other branch, tag or commit ('ref', with branch
// set for a known branch), or nothing to choose ('none': no git or commits).
function fromKind() {
  if (!refs) return { kind: 'none' };
  const v = fromEl.value.trim();
  if (!v || v === refs.current) return { kind: 'current' };
  if (/^#\d+$/.test(v) || /^[a-z]+:\/\//i.test(v)) return { kind: 'pr' };
  const wt = linked().find(w => (w.branch && w.branch === v) || w.path === v || tildify(w.path) === v);
  if (wt) return { kind: 'worktree', wt };
  return { kind: 'ref', branch: refs.refs.includes(v) };
}

// A worktree is made for anything but the current branch, and for it when
// the box is ticked.
const worktreeOn = k => (k.kind === 'current' ? wantWorktree : k.kind !== 'none');

const WORKTREE_TAG = {
  open: ['open', 'A workspace runs in it already: Open selects it'],
  adopt: ['adopt', 'Make it a workspace where it is: nothing is moved or checked out'],
  gone: ['deleted', 'Its folder was deleted: the plan line offers Prune, which forgets every deleted worktree of this repository'],
};
// A workspace on a deleted folder is opened, not pruned: its ✕ does both.
const worktreeTag = w => WORKTREE_TAG[w.workspace ? 'open' : w.prunable ? 'gone' : 'adopt'];

// branchSlug is newworkspace.go's: the branch a name becomes.
function branchSlug(name) {
  return name.trim().toLowerCase().replace(/[^a-z0-9._/-]+/g, '-').replace(/-{2,}/g, '-')
    .split('/').map(p => p.replace(/^[-.]+|[-.]+$/g, '')).filter(Boolean).join('/');
}

// sync lays the panel out for what Branch holds -- the checkbox editable,
// locked on or hidden, the new branch's field and its placeholder, the tag in
// Branch -- then replans.
function sync() {
  const k = fromKind();
  const on = worktreeOn(k);
  const v = fromEl.value.trim();
  fromRow.hidden = k.kind === 'none';
  wtRow.hidden = k.kind === 'none' || k.kind === 'worktree';
  wtEl.disabled = k.kind !== 'current';
  wtEl.checked = on;
  wtHint.textContent = k.kind === 'ref' || k.kind === 'pr' ? '· not checked out here' : '';
  newRow.hidden = !on || k.kind === 'worktree';
  if (k.kind === 'pr') newEl.placeholder = "empty: the pull request's branch";
  else if (k.kind === 'ref' && k.branch) newEl.placeholder = `empty: work on ${v} itself`;
  else {
    const slug = branchSlug(nameEl.value);
    newEl.placeholder = slug ? (getSetting('workspace.branchPrefix') || '') + slug : 'named after the Name';
  }
  fromEl.placeholder = refs ? (refs.current || `detached at ${refs.head}`) : '';
  let tag = '';
  let gone = false;
  if (k.kind === 'current') tag = 'current';
  else if (k.kind === 'pr') tag = 'pull request';
  else if (k.kind === 'worktree') {
    tag = worktreeTag(k.wt)[0];
    gone = tag === 'deleted';
  }
  fromTag.textContent = tag;
  fromTag.hidden = !tag;
  fromTag.classList.toggle('gone', gone);
  // With no branch to name it after, Name is what names the new branch.
  const nameNamesBranch = on && (k.kind === 'current' || (k.kind === 'ref' && !k.branch)) && !newEl.value.trim();
  nameEl.placeholder = !on ? 'Name of this workspace' : nameNamesBranch ? 'e.g. Fix login bug' : 'Named after the branch';
  schedulePlan();
}

/* ---------- the dropdown ---------- */

const LIST_MAX = 50; // branches the dropdown shows; typing narrows a long list

// pickList makes list the dropdown of input. rows(q) gives what it offers
// for the text typed, each {value, html}, with sec heading a run of rows. It
// opens as you type, filtered by what is typed, or on a click in the input or
// an arrow key, unfiltered; it closes on Escape, on a pick, or when the input
// loses focus. A click, or Enter on a row chosen with the arrows, puts its
// value in the input.
function pickList(input, list, rows) {
  let items = [];
  let sel = -1;
  let open = false;
  let filter = false;

  const render = () => {
    let sec = '';
    list.innerHTML = items.map((it, i) =>
      (it.sec && it.sec !== sec ? `<li class="o-nw-sec" role="presentation">${esc(sec = it.sec)}</li>` : '') +
      `<li role="option" id="${list.id}-${i}" data-i="${i}" aria-selected="${i === sel}"${it.title ? ` title="${esc(it.title)}"` : ''}>${it.html}</li>`).join('');
    list.hidden = !items.length;
    input.setAttribute('aria-expanded', String(!list.hidden));
    if (sel >= 0) input.setAttribute('aria-activedescendant', `${list.id}-${sel}`);
    else input.removeAttribute('aria-activedescendant');
    list.querySelector('[aria-selected=true]')?.scrollIntoView({ block: 'nearest' });
  };
  const refresh = () => {
    if (!open) return;
    items = rows(filter ? input.value.trim() : '');
    sel = Math.min(sel, items.length - 1);
    render();
  };
  const show = typed => {
    open = true;
    filter = typed;
    sel = -1;
    refresh();
  };
  const hide = () => {
    open = false;
    items = [];
    sel = -1;
    render();
  };
  const pick = it => {
    input.value = it.value;
    hide();
    sync();
  };

  input.addEventListener('input', () => show(true));
  input.addEventListener('click', () => open || show(false));
  input.addEventListener('blur', hide);
  input.addEventListener('keydown', e => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!open) return show(false);
      if (!items.length) return;
      // One step past either end is back to none chosen: Enter creates.
      const n = items.length + 1;
      sel = (sel + (e.key === 'ArrowDown' ? 1 : -1) + 1 + n) % n - 1;
      render();
    } else if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey && sel >= 0) {
      e.preventDefault();
      pick(items[sel]);
    } else if (e.key === 'Escape' && !list.hidden) {
      e.preventDefault();
      e.stopPropagation(); // the list closes, not the panel
      hide();
    }
  });
  list.addEventListener('mousedown', e => e.preventDefault()); // keep focus in the input
  // On click, not mousedown: the panel's outside-click check runs on
  // mousedown and would take a row the pick has already removed for outside.
  list.addEventListener('click', e => {
    const li = e.target.closest('li[data-i]');
    if (li) pick(items[Number(li.dataset.i)]);
  });
  return { refresh, close: hide };
}

// mark escapes s with the first match of q in bold.
function mark(s, q) {
  const i = q ? s.toLowerCase().indexOf(q) : -1;
  if (i < 0) return esc(s);
  return esc(s.slice(0, i)) + '<b>' + esc(s.slice(i, i + q.length)) + '</b>' + esc(s.slice(i + q.length));
}

// fromRows offers the project folder's current branch, then the project's
// worktrees, then up to LIST_MAX of its other branches, those matching what
// is typed. A branch checked out in a worktree is offered only as that
// worktree; a pull request offers nothing.
function fromRows(text) {
  if (!refs) return [];
  const q = text.toLowerCase();
  if (/^#|:\/\//.test(q)) return [];
  const hit = (...s) => !q || s.some(x => x && x.toLowerCase().includes(q));
  const cur = refs.current;
  const rows = [];
  if (hit(cur || 'detached')) {
    rows.push({
      value: cur,
      title: `The project folder, ${tildify(project()?.dir || '')}`,
      html: `<span class="o-nw-cur"></span>
        <span class="wb-sg-name">${cur ? mark(cur, q) : `detached at ${esc(refs.head)}`}</span>
        <span class="o-nw-tag">current</span>
        <span class="wb-sg-path">project folder</span>`,
    });
  }
  for (const w of linked().filter(w => hit(w.branch, w.path, tildify(w.path)))) {
    const [tag, title] = worktreeTag(w);
    const dir = tildify(w.path);
    rows.push({
      sec: 'Worktrees',
      value: w.branch || dir,
      title: `${dir}\n${title}`,
      html: `<span class="wb-ico">⑂</span>
        <span class="wb-sg-name">${w.branch ? mark(w.branch, q) : 'detached ' + esc(w.head.slice(0, 7))}</span>
        <span class="o-nw-tag${tag === 'deleted' ? ' gone' : ''}">${tag}</span>
        <span class="wb-sg-path">${esc(dir)}</span>`,
    });
  }
  const held = new Set([cur, ...refs.worktrees.map(w => w.branch)]);
  for (const b of refs.refs.filter(b => !held.has(b) && hit(b)).slice(0, LIST_MAX)) {
    rows.push({ sec: 'Branches', value: b, html: `<span class="wb-sg-name">${mark(b, q)}</span>` });
  }
  return rows;
}

/* ---------- the plan ---------- */

function body() {
  const k = fromKind();
  const on = worktreeOn(k);
  return {
    name: nameEl.value.trim(),
    from: k.kind === 'none' ? '' : fromEl.value.trim(),
    branch: on && k.kind !== 'worktree' ? newEl.value.trim() : '',
    worktree: on,
  };
}

// schedulePlan asks for the plan once typing pauses; an answer to an older
// question is dropped. On the current branch with nothing typed there is
// nothing to plan yet: the line says what is needed instead.
function schedulePlan() {
  clearTimeout(planTimer);
  const seq = ++planSeq;
  plan = null;
  const b = body();
  const k = fromKind();
  if (!b.name && !b.branch && (k.kind === 'current' || k.kind === 'none')) return renderPlan(null);
  planEl.classList.add('pending');
  planTimer = setTimeout(async () => {
    const p = project();
    if (!p) return;
    const b = body();
    const res = await call('GET', `api/projects/${encodeURIComponent(p.id)}/plan?` +
      new URLSearchParams({ name: b.name, from: b.from, branch: b.branch, worktree: b.worktree ? '1' : '' }));
    if (seq !== planSeq || !done) return;
    plan = res.ok ? res.data : { error: res.data.error };
    renderPlan(plan);
  }, 250);
}

const code = s => `<code>${esc(s)}</code>`;
const PRUNE = ' <button type="button" class="o-nw-prune" data-act="prune" title="Forget every deleted worktree of this repository, so their branches can be checked out again">Prune</button>';
const workspaceName = id => state.projects.flatMap(p => p.workspaces).find(w => w.id === id)?.name || '';

// renderPlan says what Create will do, names the button after it, and shows
// the agent and language-server choices only where they apply: Open makes
// nothing, and language servers are for a folder of the workspace's own.
function renderPlan(pl) {
  const k = fromKind();
  const on = worktreeOn(k);
  planEl.classList.remove('pending', 'err');
  reviewEl.hidden = !pl?.pr;
  createEl.textContent = { adopt: 'Adopt', open: 'Open' }[pl?.action] || (pl?.pr ? 'Work on it' : 'Create');
  reviewEl.textContent = 'Review';
  agentRow.hidden = pl?.action === 'open';
  lspRow.hidden = pl?.action === 'open' || (k.kind === 'worktree' ? pl?.action !== 'adopt' : !on);
  // The name an empty Name gets, once the plan knows it.
  if (on && !nameEl.value.trim() && pl?.name && !pl.error) nameEl.placeholder = pl.name;
  if (!pl) {
    const cur = refs?.current;
    planEl.innerHTML = on
      ? `Type a name or a new branch name: a new branch from ${code(cur || 'HEAD')} gets a worktree of its own.`
      : `Runs in the project folder${cur ? `, on ${code(cur)}` : ''}: ${code(tildify(project()?.dir || ''))}. Type a name for it.`;
    return;
  }
  if (pl.error && !pl.pr) {
    planEl.classList.add('err');
    planEl.innerHTML = esc(pl.error) + (pl.prune ? PRUNE : '');
    return;
  }
  const dir = `<span class="o-nw-dir">→ ${esc(pl.dir)}</span>`;
  const checkedOut = pl.branch ? code(pl.branch) : 'a detached commit';
  const start = code(fromEl.value.trim() || refs?.current || 'HEAD');
  let what = '';
  switch (pl.action) {
    case 'local': what = `Runs in the project folder${pl.branch ? `, on ${code(pl.branch)}` : ''}.`; break;
    case 'existing':
      what = pl.startIgnored
        ? `<span class="warn">${code(pl.branch)} already exists: checked out as it is, ${start} is not used.</span>`
        : `Checks out the existing branch ${code(pl.branch)}.`;
      break;
    case 'track':
      what = pl.startIgnored
        ? `<span class="warn">${code(pl.base)} exists: new branch ${code(pl.branch)} tracks it, ${start} is not used.</span>`
        : `New branch ${code(pl.branch)} tracking ${code(pl.base)}, fetched first.`;
      break;
    case 'new': what = `New branch ${code(pl.branch)} from ${code(pl.base)}${pl.fetch ? `, fetched from ${esc(pl.fetch)} first` : ''}.`; break;
    case 'adopt': what = `Adopts the worktree on ${checkedOut} where it is: nothing is moved or checked out.`; break;
    case 'open': what = `Already a workspace, <b>${esc(workspaceName(pl.workspace))}</b>, on ${checkedOut}: Open selects it.`; break;
    case 'pr': {
      const pr = pl.pr;
      what = `<b>#${pr.number} ${esc(pr.title)}</b><br>` +
        `Review opens it in the editor. Work on it checks out ${code(pl.branch || pr.head)}${pr.fork ? ' (from a fork)' : ''} in a worktree.`;
      if (pl.error) {
        planEl.innerHTML = `${what}<br><span class="o-err">${esc(pl.error)}</span>${pl.prune ? PRUNE : ''}`;
        return;
      }
      break;
    }
  }
  // A pull request whose branch is checked out already is adopted or opened.
  if (pl.pr && pl.action !== 'pr') what = `<b>#${pl.pr.number} ${esc(pl.pr.title)}</b><br>${what}`;
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
  if (!b.worktree && !b.name) {
    showMsg('Type a name for this workspace.');
    return nameEl.focus();
  }
  const agent = agentEl.value;
  setBusy(true, 'Creating…');
  const res = await call('POST', `api/projects/${encodeURIComponent(p.id)}/workspaces`, {
    ...b,
    agent,
    lsp: !lspRow.hidden && lspEl.checked,
  });
  setBusy(false);
  if (!res.ok) return showMsg(res.data.error);
  showMsg('');
  const all = loadPrefs();
  all[p.id] = { agent, worktree: wantWorktree, lsp: lspEl.checked };
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

// prune has git forget the project's deleted worktrees, then lists the
// worktrees again and replans: their branches are free to check out.
async function prune() {
  const p = project();
  if (!p || busy) return;
  setBusy(true, 'Pruning…');
  const res = await call('POST', `api/projects/${encodeURIComponent(p.id)}/prune`);
  setBusy(false);
  if (!res.ok) return showMsg(res.data.error);
  showMsg('');
  // The plan that offered Prune is answered: drop it rather than redraw it
  // with its button until the next one arrives.
  plan = null;
  renderPlan(null);
  await loadRefs(p);
}

function setBusy(on, label = '') {
  busy = on;
  createEl.disabled = reviewEl.disabled = on;
  if (on) (plan?.pr && label === 'Opening…' ? reviewEl : createEl).textContent = label;
  else renderPlan(plan);
}
