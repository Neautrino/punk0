// web/src/gitpanel.js
// Sidebar git panel, laid out like VS Code's Source Control: a head row
// (branch, ahead/behind, upstream, staged/changed counts), a clean commit
// message box, and one contextual primary button whose ▾ menu holds the rest.
// The visual commit graph below it belongs to gitgraph.js.
// Shown whenever the workspace is a git repo. In a PR review session
// (S.meta.pr set), Push/Pull target the PR's actual head branch instead of
// the checkout's own remote.
import { $, esc, S, api, apiPostJson, debounce, keyLabel, MOD } from './state.js';
import { showToast } from './ui.js';
import { reindexWorkspace } from './panels.js';
import { refreshPRMeta } from './pr.js';
import { openFile, reloadOpenTabs } from './tabs.js';
import { layout, render } from './renderer.js';
import { setSidebarMode } from './tree.js';
import { on, emit } from './bus.js';
import { triggerRefresh } from './gitstream.js';
import { registerAgentPicker } from './agent.js';
import { refreshGraph } from './gitgraph.js';
import { loadCommitFiles } from './commitfiles.js';

const panel = () => $('#git-panel');
const msgBox = () => $('#git-commit-msg');

// The last status payload, reduced to what the controls are derived from.
const git = { staged: 0, changed: 0, ahead: 0, behind: 0, upstream: '', branch: '' };

let busy = ''; // label of a git action in flight ("Pushing…"); everything else waits

export function initGitPanel() {
  if (!panel()) return;

  $('#git-panel-collapse')?.addEventListener('click', () => {
    panel()?.classList.toggle('collapsed');
    layout(); render();
  });

  const rz = $('#git-panel-resizer');
  if (rz && panel()) {
    let dragging = false;
    rz.addEventListener('mousedown', e => {
      dragging = true;
      rz.classList.add('drag');
      panel().classList.remove('collapsed');
      e.preventDefault();
    });
    addEventListener('mousemove', e => {
      if (!dragging) return;
      const bottom = panel().getBoundingClientRect().bottom;
      const h = Math.max(60, Math.min(window.innerHeight * 0.8, bottom - e.clientY));
      panel().style.height = h + 'px';
      layout(); render();
    });
    addEventListener('mouseup', () => {
      if (!dragging) return;
      dragging = false;
      rz.classList.remove('drag');
      layout(); render();
    });
  }

  $('#git-counts')?.addEventListener('click', () => setSidebarMode('git'));
  msgBox()?.addEventListener('input', () => msgBox()?.classList.remove('warn-border'));
  msgBox()?.addEventListener('keydown', e => {
    if (e[MOD] && e.key === 'Enter') {
      e.preventDefault();
      runPrimary();
    }
  });
  $('#git-primary')?.addEventListener('click', runPrimary);
  initMenu();
  initNotice();
  on('agent:meta', syncControls);
  // The graph header's toolbar; btn is its clicked button, if any.
  on('git:pull', ev => doPull(ev?.btn));
  on('git:push', ev => (rewritten ? confirmForcePush() : doPush(false, ev?.btn)));

  syncGitPanelMeta();
}

// revealGitPanel unfolds the panel and brings it into view.
export function revealGitPanel() {
  const p = panel();
  if (!p || p.hidden) return;
  if (p.classList.contains('collapsed')) {
    p.classList.remove('collapsed');
    layout(); render();
  }
  p.scrollIntoView({ block: 'nearest' });
}

export function syncGitPanelMeta() {
  updateGitPanelVisibility();
  if (S.meta?.git) {
    refreshGraph();
    checkUnfinishedSync();
  }
}

function updateGitPanelVisibility() {
  const p = panel();
  if (!p) return;
  p.hidden = !S.meta?.git;
}

const hasUpstream = () => !!(S.meta?.pr || git.upstream);

// Called from gitstream.js's handleGitStatus with each SSE/refresh payload,
// so the head row and the primary button stay live without a manual reload.
export function updateGitPanel(payload) {
  updateGitPanelVisibility();
  git.staged = payload?.staged ? Object.keys(payload.staged).length : 0;
  git.changed = payload?.gitChanges ?? (payload?.statuses ? Object.keys(payload.statuses).length : (S.meta?.gitChanges || 0));
  git.ahead = payload?.ahead ?? 0;
  git.behind = payload?.behind ?? 0;
  git.upstream = payload?.upstream || '';
  git.branch = payload?.branch || '';
  if (payload && !!payload.rewritten !== rewritten) {
    rewritten = !!payload.rewritten;
    emitSync();
  }
  renderHead();
  checkStagedSet(payload?.staged);
  syncControls();
}

function renderHead() {
  const pr = S.meta?.pr;
  const branch = pr ? pr.head : git.branch;
  const branchEl = $('#git-branch');
  if (branchEl) {
    branchEl.textContent = branch;
    branchEl.title = pr ? `${branch} (PR #${pr.number} review)` : branch;
  }
  const ab = $('#git-ahead-behind');
  if (ab) {
    const unpublished = !hasUpstream();
    const text = unpublished ? 'unpublished'
      : [git.ahead ? '↑' + git.ahead : '', git.behind ? '↓' + git.behind : ''].filter(Boolean).join(' ');
    ab.textContent = text;
    ab.hidden = !text;
    ab.classList.toggle('unpublished', unpublished);
    ab.title = unpublished
      ? 'This branch has no upstream yet; pushing publishes it'
      : `${plural(git.ahead, 'commit')} to push, ${git.behind} to pull`;
  }
  const up = $('#git-upstream');
  if (up) {
    up.textContent = pr ? `PR #${pr.number}` : git.upstream;
    up.title = pr ? `Pushes and pulls go to ${pr.head} of PR #${pr.number}` : `Tracking ${git.upstream}`;
    up.hidden = !up.textContent;
  }
  const counts = $('#git-counts');
  if (counts) {
    counts.textContent = git.staged + ' / ' + git.changed;
    counts.title = `${git.staged} staged / ${git.changed} changed: show the changed files`;
    counts.hidden = !git.changed && !git.staged;
  }
  const ta = msgBox();
  if (ta) ta.placeholder = `Message (${keyLabel('Mod+Enter')} to commit on "${branch}")`;
}

const idle = () => !busy && !syncJob;

// primaryAction is the one thing the panel suggests doing next.
function primaryAction() {
  if (busy) return { label: busy };
  if (syncJob) return { label: 'Rebase/merge running…', title: 'The harness is rewriting history; its card above says how it goes' };
  const { staged, changed, ahead, behind } = git;
  const pr = S.meta?.pr;

  if (staged > 0) {
    return {
      label: `Commit ${staged} staged`,
      title: `Commit the ${plural(staged, 'staged file')} with the message above (${keyLabel('Mod+Enter')})`,
      run: doCommit,
    };
  }
  if (changed > 0) {
    return {
      label: `Commit all (${changed})`,
      title: `Stage all changes and commit with the message above (${keyLabel('Mod+Enter')})`,
      run: doCommit,
    };
  }
  if (pr && rewritten) {
    return { label: 'Force-push', title: `This checkout was rebased: replace the commits on ${pr.head} with it (asks first)`, run: confirmForcePush };
  }
  if (ahead > 0 && behind > 0) {
    return { label: `Sync ↑${ahead} ↓${behind}`, title: 'Pull first (offers a rebase or merge when branches diverged), then push', run: () => doPull() };
  }
  if (ahead > 0) {
    if (pr) return { label: `Push to PR ↑${ahead}`, title: `Push ${plural(ahead, 'commit')} to PR #${pr.number} (${pr.head})`, run: () => doPush() };
    if (!git.upstream) return { label: 'Publish branch', title: `Push ${git.branch || 'this branch'} to origin and track it`, run: () => doPush() };
    return { label: `Push ↑${ahead}`, title: `Push ${plural(ahead, 'commit')} to ${git.upstream}`, run: () => doPush() };
  }
  if (behind > 0) {
    return { label: `Pull ↓${behind}`, title: `Pull ${plural(behind, 'incoming commit')} from ${git.upstream || 'upstream'}`, run: () => doPull() };
  }
  return { label: 'Up to date', title: hasUpstream() ? 'Nothing to commit, push or pull' : 'Nothing to commit' };
}

function runPrimary() {
  primaryAction().run?.();
}

function syncControls() {
  const a = primaryAction();
  const btn = $('#git-primary');
  if (btn) {
    btn.textContent = a.label;
    btn.title = a.title || '';
    btn.disabled = !a.run;
  }
  const more = $('#git-more');
  if (more) more.disabled = !idle();
  if (!idle()) toggleMenu(false);
  else if (!$('#git-more-menu')?.hidden) renderMenu();
}

function setBusy(label) {
  busy = label;
  syncControls();
}

/* The ▾ menu: actions the primary button isn't right now. */
function menuItems() {
  const { staged, changed, ahead } = git;
  const local = !(ahead === 0 && hasUpstream());
  const pushed = 'The last commit is already pushed';
  return [
    { label: 'Commit & Push', why: (staged || changed) ? '' : 'Nothing to commit', run: () => doCommit({ push: true }) },
    { label: 'Stage all', why: changed ? '' : 'Nothing to stage', run: stageAll },
    { label: 'Unstage all', why: staged ? '' : 'Nothing is staged', run: unstageAll },
    null,
    {
      label: 'Amend last commit', run: doAmend, why: local ? '' : pushed,
      title: 'Fold the staged files into the last commit; uses the message box if it has text, else keeps the old message',
    },
    {
      label: 'Undo last commit', run: doUndo, why: local ? '' : pushed,
      title: 'Undo the last commit, keeping its changes staged and its message in the box',
    },
    null,
    { label: 'Pull', why: hasUpstream() ? '' : 'This branch has no upstream to pull from', run: () => doPull() },
    {
      label: rewritten ? 'Force-push…' : 'Push', why: ahead ? '' : 'No commits to push',
      run: () => (rewritten ? confirmForcePush() : doPush()),
    },
  ];
}

function renderMenu() {
  const menu = $('#git-more-menu');
  if (!menu) return;
  const items = menuItems();
  menu.replaceChildren(...items.map(it => {
    if (!it) return document.createElement('hr');
    const b = document.createElement('button');
    b.type = 'button';
    b.setAttribute('role', 'menuitem');
    b.textContent = it.label;
    b.disabled = !!it.why;
    b.title = it.why || it.title || '';
    b.addEventListener('click', () => {
      toggleMenu(false);
      it.run();
    });
    return b;
  }));
}

function toggleMenu(force) {
  const menu = $('#git-more-menu');
  if (!menu) return;
  const open = force === undefined ? menu.hidden : force;
  if (open) renderMenu();
  menu.hidden = !open;
  $('#git-more')?.setAttribute('aria-expanded', String(open));
}

function initMenu() {
  $('#git-more')?.addEventListener('click', e => {
    e.stopPropagation();
    toggleMenu();
  });
  document.addEventListener('click', e => {
    if (!e.target?.closest?.('#git-actions')) toggleMenu(false);
  });
  document.addEventListener('keydown', e => {
    if (e.key === 'Escape' && !$('#git-more-menu')?.hidden) toggleMenu(false);
  });
}

async function doCommit(opts = {}) {
  const ta = msgBox();
  const message = ta ? ta.value.trim() : '';
  if (!message) {
    if (ta) shakeBox(ta, 'Write a commit message first');
    return;
  }
  setBusy('Committing…');
  try {
    // If nothing is explicitly staged, stage all changes first (clean convenience).
    if (git.staged === 0 && git.changed > 0) {
      await apiPostJson('/api/git/stage', { path: '.' });
    }
    await apiPostJson('/api/git/commit', { message });
    if (ta) {
      ta.value = '';
      ta.classList.remove('warn-border');
    }
    clearGitNotice();
    showToast('✓', 'Committed');
    await afterCommit();
    if (opts.push) await doPush();
  } catch (e) {
    showGitError('Commit failed', e.message);
  } finally {
    setBusy('');
  }
}

async function stageAll() {
  setBusy('Staging…');
  try {
    await apiPostJson('/api/git/stage', { path: '.' });
    clearGitNotice();
    await triggerRefresh();
  } catch (e) {
    showGitError('Could not stage', e.message);
  } finally {
    setBusy('');
  }
}

async function unstageAll() {
  setBusy('Unstaging…');
  try {
    await apiPostJson('/api/git/unstage', { path: '.' });
    clearGitNotice();
    await triggerRefresh();
  } catch (e) {
    showGitError('Could not unstage', e.message);
  } finally {
    setBusy('');
  }
}

async function doAmend() {
  const ta = msgBox();
  const message = ta ? ta.value.trim() : '';
  setBusy('Amending…');
  try {
    await apiPostJson('/api/git/commit', { message, amend: true });
    if (ta) {
      ta.value = '';
      ta.classList.remove('warn-border');
    }
    clearGitNotice();
    showToast('✓', 'Amended');
    await afterCommit();
  } catch (e) {
    showGitError('Amend failed', e.message);
  } finally {
    setBusy('');
  }
}

async function doUndo() {
  setBusy('Undoing commit…');
  try {
    const res = await apiPostJson('/api/git/undo-commit', {});
    const ta = msgBox();
    if (ta && res.message) {
      ta.value = res.message;
      ta.focus();
    }
    clearGitNotice();
    showToast('✓', 'Commit undone — changes are staged');
    await afterCommit();
  } catch (e) {
    showGitError('Undo failed', e.message);
  } finally {
    setBusy('');
  }
}

async function afterCommit() {
  await refreshGraph();
  await triggerRefresh();
}

/* Review entry points: ?commit= (main.js) */
export async function reviewCommit(sha) {
  let files;
  try {
    files = await loadCommitFiles(sha);
  } catch (e) {
    showGitError('Could not open the commit', e.message);
    return;
  }
  const f = files.find(x => !x.binary);
  if (f) await openFile(f.path, { ref: sha, view: 'diff' });
  else showGitError(files.length ? 'Binary files only, no diff to show' : 'This commit changes no files');
}

/* ---------- staged-set warnings ----------
   A secret or a huge file is easiest to catch before the commit, so the
   staged set is checked whenever it changes (not on every status tick). */

let stagedKey = '';
let preflightSeq = 0;

function checkStagedSet(staged) {
  const key = Object.keys(staged || {}).sort().join('\n');
  if (key === stagedKey) return;
  stagedKey = key;
  if (!key) {
    preflightSeq++;
    renderWarnings([]);
    return;
  }
  const seq = ++preflightSeq;
  debouncedPreflight(seq);
}

const debouncedPreflight = debounce(async seq => {
  try {
    const res = await api('/api/git/preflight?scope=staged');
    if (seq === preflightSeq) renderWarnings(res?.warnings || []);
  } catch {
    if (seq === preflightSeq) renderWarnings([]);
  }
}, 350);
function renderWarnings(warnings) {
  const el = $('#git-warnings');
  if (!el) return;
  if (!warnings || !warnings.length) {
    el.hidden = true;
    el.replaceChildren();
    return;
  }
  el.innerHTML = warnings.map(w => {
    const base = w.path.split('/').pop() || w.path;
    const why = w.reason === 'secret' ? 'secret' : 'large';
    return `<span class="git-warning-chip" title="${esc(w.path)}: flagged as ${why}">⚠ ${esc(base)} (${why})</span>`;
  }).join('');
  el.hidden = false;
}

function shakeBox(el, placeholder) {
  if (!el) return;
  el.classList.remove('shake', 'warn-border');
  void el.offsetWidth;
  el.classList.add('shake', 'warn-border');
  if (placeholder) el.placeholder = placeholder;
  setTimeout(() => el.classList.remove('shake'), 400);
  el.focus();
}

/* ---------- push, pull ---------- */

async function doPush(force = false, btn = null) {
  if (!idle()) return;
  setBusy(force ? 'Force-pushing…' : 'Pushing…');
  try {
    await apiPostJson('/api/git/push', force ? { force: true } : {});
    rewritten = false;
    emitSync();
    clearGitNotice();
    showToast('✓', S.meta?.pr ? `${force ? 'Force-pushed' : 'Pushed'} to PR #${S.meta.pr.number}` : 'Pushed');
    await afterHistoryChange();
  } catch (e) {
    showGitError('Push failed', e.message);
  } finally {
    setBusy('');
    triggerRefresh();
  }
}

async function doPull(btn = null) {
  if (!idle()) return;
  setBusy('Pulling…');
  try {
    const j = await apiPostJson('/api/git/pull', {});
    clearGitNotice();
    showToast('✓', j.message || 'Pulled');
    await afterHistoryChange();
  } catch (e) {
    const b = e.body || {};
    if (b.code === 'diverged') offerSync(b);
    else if (b.code === 'in-progress') offerUnfinished(b);
    else showGitError('Pull failed', e.message);
  } finally {
    setBusy('');
    triggerRefresh();
  }
}

async function afterHistoryChange() {
  await reindexWorkspace();
  if (S.meta?.pr) await refreshPRMeta();
  await reloadOpenTabs();
  await refreshGraph();
  await triggerRefresh();
}

/* ---------- PR branch: rebase onto the base, force-push ---------- */

let rewritten = false;

function emitSync() {
  emit('git:sync', { running: !!syncJob, inProgress: syncInProgress, rewritten });
}

let syncInProgress = '';

function confirmForcePush() {
  const pr = S.meta?.pr || {};
  notice({
    tone: 'warn', title: `Force-push to PR #${pr.number}?`,
    html: `This checkout was rebased, so its commits replace the ones on ${code(pr.head)} rather than adding to them.` +
      `<span class="hint">Pushed with a lease: if anyone else pushed to ${esc(pr.head || 'the branch')} since this checkout, the push is refused and nothing of theirs is lost.</span>`,
    actions: [
      { label: 'Force-push', primary: true, title: `git push --force-with-lease to ${pr.head}`, run: () => doPush(true) },
      { label: 'Not now', run: clearGitNotice },
    ],
  });
}

/* ---------- notice: git outcomes, inline ---------- */

let syncJob = 0;

function initNotice() {
  $('#git-notice-close')?.addEventListener('click', clearGitNotice);
  const n = $('#git-notice');
  const harnessSelect = $('#git-sync-harness');
  const modelSelect = $('#git-sync-model');
  if (n && harnessSelect && modelSelect) registerAgentPicker({ el: n, harnessSelect, modelSelect });
  on('git:error', ({ title, message }) => showGitError(title, message));
  on('pr:force-push', confirmForcePush);
  on('pr:pull', () => doPull());
  on('forge:connected', updateGitPanelVisibility);
}

function notice({ tone = 'err', title, html = '', picker = false, actions = [], log = '', closable = true }) {
  const n = $('#git-notice');
  if (!n) return;
  n.dataset.tone = tone;
  $('#git-notice-title').textContent = title;
  $('#git-notice-text').innerHTML = html;
  $('#git-notice-agent').hidden = !picker;
  $('#git-notice-close').hidden = !closable;
  $('#git-notice-actions').replaceChildren(...actions.map(a => {
    const b = document.createElement('button');
    b.type = 'button';
    b.textContent = a.label;
    if (a.primary) b.className = 'primary';
    if (a.title) b.title = a.title;
    b.addEventListener('click', () => {
      for (const x of $('#git-notice-actions').children) x.disabled = true;
      a.run();
    });
    return b;
  }));
  const details = $('#git-notice-log');
  if (details) details.open = false;
  setNoticeLog(log);
  n.hidden = false;
  revealGitPanel();
  n.scrollIntoView({ block: 'nearest' });
}

function setNoticeLog(text) {
  const details = $('#git-notice-log');
  if (!details) return;
  const pre = details.querySelector('pre');
  const atEnd = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 4;
  details.hidden = !text;
  pre.textContent = text || '';
  if (atEnd) pre.scrollTop = pre.scrollHeight;
}

export function clearGitNotice() {
  if (syncJob) return;
  const n = $('#git-notice');
  if (n) n.hidden = true;
}

export function showGitError(title, message = '') {
  if (syncJob) return;
  notice({ tone: 'err', title, html: esc(message) });
}

const plural = (n, word) => n + ' ' + word + (n === 1 ? '' : 's');
const code = s => '<code>' + esc(s || '') + '</code>';
const hasHarness = () => (S.meta?.agents || []).some(h => h.installed);

function offerSync(st, err = '') {
  const up = st.upstream || 'upstream';
  const pr = st.pr > 0;
  const title = pr ? `PR #${st.pr} is behind ${up}` : "Can't fast-forward";
  let html = pr
    ? `${code(st.branch)} is missing ${plural(st.behind || 0, 'commit')} from ${code(up)}; the PR has ${plural(st.ahead || 0, 'commit')} of its own.`
    : `${code(st.branch)} and ${code(up)} have diverged: ${plural(st.ahead || 0, 'local commit')} not upstream, ${st.behind || 0} incoming.`;
  if (!hasHarness()) {
    notice({
      tone: 'warn', title,
      html: html + `<span class="hint">Install a coding harness to rebase or merge from here, or run ${code(pr ? 'git rebase ' + up : 'git pull --rebase')} in a terminal.</span>`,
    });
    return;
  }
  html += pr
    ? `<span class="hint">Rebase replays the PR's commits on top of ${esc(up)} for a straight history, then needs a force-push to update the PR. Merge brings ${esc(up)} in with a merge commit and a normal push. The harness resolves any conflicts; nothing is pushed until you say so.</span>`
    : `<span class="hint">Rebase replays your commits on top of ${esc(up)} for a straight history; merge joins the two with a merge commit. The harness resolves any conflicts. Nothing is pushed.</span>`;
  if (err) html += `<span class="hint" style="color:var(--err)">${esc(err)}</span>`;
  notice({
    tone: 'warn', title, html, picker: true,
    actions: [
      { label: 'Rebase with AI', primary: true, title: `Have the harness run git rebase ${up} and resolve conflicts`, run: () => startSync('rebase', st) },
      { label: 'Merge with AI', title: `Have the harness run git merge ${up} and resolve conflicts`, run: () => startSync('merge', st) },
    ],
  });
}

function offerUnfinished(st, log = '', err = '') {
  const op = st.inProgress;
  let html = `${code(st.branch)} is in the middle of a ${op}${st.upstream ? ' onto ' + code(st.upstream) : ''}, most likely stopped on a conflict.` +
    `<span class="hint">Abort to put the branch back exactly as it was before the ${op}, or have the harness resolve the conflicts and finish it.</span>`;
  if (err) html += `<span class="hint" style="color:var(--err)">${esc(err)}</span>`;
  const actions = [{ label: `Abort ${op}`, run: abortSync }];
  if (hasHarness() && st.upstream) actions.unshift({ label: 'Finish with AI', primary: true, run: () => startSync(op, st) });
  notice({ tone: 'warn', title: `${op === 'rebase' ? 'Rebase' : 'Merge'} not finished`, html, picker: hasHarness(), actions, log, closable: true });
}

async function startSync(strategy, st) {
  let job;
  try {
    job = await apiPostJson('/api/git/sync-agent', { strategy });
  } catch (e) {
    if (st.inProgress) offerUnfinished(st, '', e.message);
    else offerSync(st, e.message);
    return;
  }
  syncJob = job.id;
  syncControls();
  emitSync();
  const verb = strategy === 'rebase' ? 'Rebasing' : 'Merging';
  const up = st.upstream || 'upstream';
  notice({
    tone: 'busy', title: `${verb} with ${job.harness}...`, closable: false,
    html: `${code(st.branch)} onto ${code(up)}. The harness resolves conflicts as it goes; nothing is pushed.`,
    actions: [{ label: 'Cancel', title: 'Stop the harness; anything half-done can be aborted next', run: () => apiPostJson('/api/agent/cancel', { id: job.id }).catch(() => {}) }],
  });
  const poll = async () => {
    let j;
    try {
      j = await api('/api/agent/job?id=' + job.id);
    } catch (e) {
      j = { running: false, error: e.message };
    }
    if (j.running) {
      $('#git-notice-title').textContent = `${verb} with ${job.harness}... (${Math.round((j.ms || 0) / 1000)}s)`;
      setNoticeLog(j.stdout || j.log || '');
      setTimeout(poll, 800);
      return;
    }
    await finishSync(strategy, j);
  };
  setTimeout(poll, 500);
}

async function finishSync(strategy, j) {
  syncJob = 0;
  syncControls();
  const log = (j.stdout || j.log || '').trim();
  let st = {};
  try {
    st = await api('/api/git/sync-state');
  } catch {}
  syncInProgress = st.inProgress || '';
  rewritten = !!st.rewritten;
  emitSync();
  await afterHistoryChange();
  const done = strategy === 'rebase' ? 'Rebased' : 'Merged';
  if (st.inProgress) {
    offerUnfinished(st, log);
  } else if (st.behind > 0) {
    const why = j.error === 'cancelled' ? 'Cancelled.' : j.error ? (j.harness || 'harness') + ': ' + j.error : `The harness didn't finish the ${strategy}; its output below says why.`;
    notice({
      tone: 'err', title: `${strategy === 'rebase' ? 'Rebase' : 'Merge'} didn't complete`, log,
      html: `${esc(why)} ${code(st.branch)} is unchanged: still ${plural(st.ahead || 0, 'local commit')} and ${st.behind} incoming.`,
      actions: [{ label: 'Try again', run: () => offerSync(st) }],
    });
  } else if (st.pr > 0) {
    const pr = S.meta?.pr || {};
    notice({
      tone: 'ok', title: `${done} onto ${st.upstream}`, log,
      html: st.rewritten
        ? `The PR's commits now sit on top of ${code(st.upstream)}. Review the result; force-pushing replaces the commits on ${code(pr.head)}.` +
          `<span class="hint">Approve and Merge wait until it's pushed, so they never act on the old commits.</span>`
        : `${code(pr.head)} now contains ${code(st.upstream)}. Review the result, then push to update the PR.`,
      actions: st.rewritten
        ? [{ label: `Force-push to PR #${st.pr}`, primary: true, title: `git push --force-with-lease to ${pr.head}`, run: () => doPush(true) }]
        : [{ label: 'Push to PR', primary: true, run: () => doPush() }],
    });
  } else {
    const ahead = st.ahead || 0;
    notice({
      tone: 'ok', title: `${done} onto ${st.upstream || 'upstream'}`, log,
      html: ahead
        ? `${code(st.branch)} is ${plural(ahead, 'commit')} ahead and up to date. Review the result, then push when ready.`
        : `${code(st.branch)} is up to date.`,
      actions: ahead ? [{ label: `Push (${ahead})`, primary: true, run: doPush }] : [],
    });
  }
}

async function abortSync() {
  try {
    await apiPostJson('/api/git/abort', {});
  } catch (e) {
    showGitError('Abort failed', e.message);
    return;
  }
  syncInProgress = '';
  emitSync();
  await afterHistoryChange();
  notice({
    tone: 'ok', title: 'Aborted',
    html: S.meta?.pr ? 'The checkout is back where it was.' : 'The branch is back where it was. Pull again to choose rebase or merge.',
  });
}

async function checkUnfinishedSync() {
  try {
    const st = await api('/api/git/sync-state');
    syncInProgress = st.inProgress || '';
    rewritten = !!st.rewritten;
    emitSync();
    if (st.inProgress) offerUnfinished(st);
  } catch {}
}
