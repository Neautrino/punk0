// web/src/gitgraph.js
// The commit graph at the bottom of the sidebar git panel, VS Code style: one
// row per commit from GET /api/git/log, its lane drawn as inline SVG, ref
// badges, and the commits that are not pushed yet (outgoing) or not pulled yet
// (incoming) grouped under a pseudo-row each. Every row expands in place into
// the files it touched (commitfiles.js), each opening that commit's own diff,
// and resting on a row opens its hover card (#commitcard).
//
// It replaces both the Recent Commits list and the Unpushed section: the
// outgoing commits are counted into S.unpushedCount, which tree.js's
// hasGitView() reads so a clean tree with local commits keeps its Git view.
import { $, S, esc, api } from './state.js';
import { on, emit } from './bus.js';
import { openFile } from './tabs.js';
import { copyToClipboard } from './ui.js';
import { updateSidebarToggleState } from './tree.js';
import { makeCardKeeper, pointerPos } from './cardkeep.js';
import { commitFileRows, COMMIT_FILES_PAGE, loadCommitFiles, cachedCommitFiles } from './commitfiles.js';
import { forgeInfo } from './pr.js';
import { triggerRefresh } from './gitstream.js';

const listEl = () => $('#gg-list');
const cardEl = () => $('#commitcard');

const PAGE = 30;       // commits per fetch; "Load more" adds another page
const MAX_LIMIT = 200; // the server's cap
const ROW_H = 22;      // px, matches .gg-row's height
const LANE_W = 10;     // px per lane
const MAX_LANES = 8;   // lanes past this are clipped rather than squeezing the subject out
const COLORS = 6;      // .gg-c0 .. .gg-c5
const CARD_DELAY = 300; // rest time on a commit row before its card opens

let commits = [];     // GraphCommit[], topo order, newest first
let lanes = [];       // per commit, what layoutLanes() worked out
let width = 0;        // px of the lane column, the same on every row so subjects line up
let limit = PAGE;
let more = false;     // the log had at least `limit` rows
let upstream = '';    // "origin/main", "PR #9 head (feat)", or ''
let commitsUrl = '';
let headSha = '';
let failed = '';      // the last fetch's error, shown in place of the list

let ahead = 0, behind = 0; // from the git-status tick; the toolbar counts
let syncRunning = false;    // gitpanel.js's rebase/merge job, which owns push/pull meanwhile
let sig = null;             // last (head, ahead, behind, upstream) seen, to skip pointless refetches
let loading = null, again = false;

const expanded = new Set(); // full SHAs of the commits shown open
const details = new Map();  // full SHA -> CommitDetail, for the hover card
let cardTimer = 0, cardSeq = 0, cardSha = '';
let keeper = null;

/* ---------- loading ---------- */

/* Force a refetch and redraw. Calls while one is in flight share it, plus one
   more fetch after it: whatever changed since that fetch started (a commit,
   a push) must still show, so returning the stale one alone would be wrong. */
export function refreshGraph() {
  if (loading) { again = true; return loading; }
  loading = (async () => {
    try {
      do { again = false; await load(); } while (again);
    } finally {
      loading = null;
    }
  })();
  return loading;
}

/* A git-status tick. Its head SHA, ahead/behind counts and upstream together
   say whether the graph moved -- it is refetched only when they did, so the
   SSE stream doesn't shell out to git log on every heartbeat. */
export async function updateGraph(payload) {
  if (!payload) return;
  ahead = payload.ahead ?? 0;
  behind = payload.behind ?? 0;
  drawToolbar();
  const next = [payload.recentCommits?.[0]?.full || '', ahead, behind, payload.upstream || ''].join('\x1f');
  if (next === sig) return;
  sig = next;
  await refreshGraph();
}

async function load() {
  if (!S.meta?.git) { setCommits([], ''); return; }
  let j;
  try {
    j = await api('/api/git/log', { limit });
  } catch (e) {
    setCommits([], e.message || 'Could not read the history');
    return;
  }
  more = !!j.more;
  upstream = j.upstream || '';
  commitsUrl = j.commitsUrl || '';
  headSha = j.head || '';
  setCommits(j.commits || [], '');
}

function setCommits(next, error) {
  failed = error;
  if (!next.length) { more = false; upstream = ''; commitsUrl = ''; headSha = ''; }
  // A commit that vanished (amend, rebase) takes its open state and card with it.
  const live = new Set(next.map(c => c.full));
  for (const sha of [...expanded]) if (!live.has(sha)) expanded.delete(sha);
  for (const sha of [...details.keys()]) if (!live.has(sha)) details.delete(sha);

  commits = next;
  lanes = layoutLanes(commits);
  width = graphWidth();
  S.unpushedCount = commits.filter(c => c.outgoing).length;
  updateSidebarToggleState();
  draw();
}

/* ---------- lanes ----------
   One pass over the commits in topo order, carrying the lanes open at that
   point: each lane is the SHA it expects next (or null once free). A commit
   takes the first lane expecting it -- or a free one, starting a branch tip --
   and any other lane expecting it converges into it. Its first parent then
   continues that lane, and each further parent joins the lane already
   expecting it or opens one in the first free slot. Lanes never move sideways,
   so a lane that passes a row is a straight line through it. */

function layoutLanes(list) {
  const open = [];   // lane -> SHA it expects next, or null
  const colors = []; // lane -> colour index
  let nextColor = 0;
  const freeSlot = () => { const i = open.indexOf(null); return i < 0 ? open.length : i; };
  return list.map(c => {
    const before = open.slice(), beforeColors = colors.slice();
    let idx = open.indexOf(c.full);
    const fromAbove = idx >= 0;
    if (!fromAbove) { idx = freeSlot(); colors[idx] = nextColor++ % COLORS; }
    const color = colors[idx];

    const merges = []; // other lanes from above that end in this commit
    for (let j = 0; j < open.length; j++) {
      if (j !== idx && open[j] === c.full) { merges.push(j); open[j] = null; }
    }
    const [first, ...rest] = c.parents || [];
    open[idx] = first || null;
    const forks = []; // lanes this commit's further parents continue in
    for (const p of rest) {
      let j = open.indexOf(p);
      if (j < 0) { j = freeSlot(); open[j] = p; colors[j] = nextColor++ % COLORS; }
      forks.push(j);
    }
    while (open.length && open[open.length - 1] === null) open.pop();
    colors.length = Math.max(open.length, idx + 1);
    return { idx, color, fromAbove, merges, forks, before, beforeColors, after: open.slice(), afterColors: colors.slice() };
  });
}

function graphWidth() {
  let n = 1;
  for (const l of lanes) n = Math.max(n, l.before.length, l.after.length, l.idx + 1, ...l.forks.map(f => f + 1));
  return Math.min(n, MAX_LANES) * LANE_W + 2;
}

const laneX = i => i * LANE_W + LANE_W / 2 + 1;

function seg(d, color) {
  return '<path class="gg-c' + color + '" d="' + d + '"/>';
}

/* The row's own slice of the graph: lanes passing by, the commit's lane in and
   out, merges converging into it and forks leaving it, then its node. */
function laneSvg(c, l) {
  const H = ROW_H, mid = H / 2, x = laneX(l.idx);
  let s = '';
  for (let i = 0; i < l.before.length; i++) {
    if (l.before[i] === null || i === l.idx || l.merges.includes(i)) continue;
    s += seg('M' + laneX(i) + ' 0V' + H, l.beforeColors[i]);
  }
  if (l.fromAbove) s += seg('M' + x + ' 0V' + mid, l.color);
  for (const j of l.merges) {
    const xj = laneX(j);
    s += seg('M' + xj + ' 0C' + xj + ' ' + mid + ' ' + xj + ' ' + mid + ' ' + x + ' ' + mid, l.beforeColors[j]);
  }
  if (l.after[l.idx]) s += seg('M' + x + ' ' + mid + 'V' + H, l.color);
  for (const j of l.forks) {
    const xj = laneX(j);
    s += seg('M' + x + ' ' + mid + 'C' + xj + ' ' + mid + ' ' + xj + ' ' + mid + ' ' + xj + ' ' + H, l.afterColors[j]);
  }
  // HEAD is a hollow ring, a merge a ring around a dot, anything else a dot.
  const cls = 'gg-c' + l.color;
  if (c.full === headSha) s += '<circle class="' + cls + ' gg-node-head" cx="' + x + '" cy="' + mid + '" r="4"/>';
  else if ((c.parents || []).length > 1) {
    s += '<circle class="' + cls + ' gg-node-merge" cx="' + x + '" cy="' + mid + '" r="4.5"/>' +
      '<circle class="' + cls + ' gg-node" cx="' + x + '" cy="' + mid + '" r="2"/>';
  } else s += '<circle class="' + cls + ' gg-node" cx="' + x + '" cy="' + mid + '" r="3.5"/>';
  return '<svg class="gg-lanes" width="' + width + '" height="' + H + '" aria-hidden="true">' + s + '</svg>';
}

/* Straight lines for the lanes open across something that is not a commit: a
   group pseudo-row, or a commit's expanded files (fill: as tall as its box). */
function passSvg(open, colors, fill) {
  let s = '';
  for (let i = 0; i < open.length; i++) {
    if (!open[i]) continue;
    const x = laneX(i);
    s += '<line class="gg-c' + colors[i] + '" x1="' + x + '" y1="0" x2="' + x + '" y2="100%"/>';
  }
  return '<svg class="gg-lanes' + (fill ? ' gg-pass' : '') + '" width="' + width + '" height="' +
    (fill ? '100%' : ROW_H) + '" aria-hidden="true">' + s + '</svg>';
}

/* ---------- drawing ---------- */

const ICON_UP = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="12" y1="19" x2="12" y2="5"/><polyline points="5 12 12 5 19 12"/></svg>';
const ICON_DOWN = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="12" y1="5" x2="12" y2="19"/><polyline points="19 12 12 19 5 12"/></svg>';

const plural = (n, word) => n + ' ' + word + (n === 1 ? '' : 's');

function drawToolbar() {
  const up = $('#gg-upstream');
  if (up) {
    up.textContent = upstream;
    up.title = upstream ? 'Outgoing and incoming commits are measured against ' + upstream : '';
  }
  const outgoing = S.unpushedCount || 0;
  const pushN = ahead || outgoing;
  const pullN = behind || commits.filter(c => c.incoming).length;

  const push = $('#gg-push');
  if (push) {
    push.disabled = pushN === 0 || syncRunning;
    push.querySelector('.gg-n').textContent = pushN ? String(pushN) : '';
    push.title = pushN === 0
      ? (S.meta?.pr ? 'Commit your changes to push them to this PR' : 'No outgoing commits to push')
      : S.meta?.pr
        ? 'Push ' + plural(pushN, 'commit') + ' to PR #' + S.meta.pr.number + ' (' + S.meta.pr.head + ')'
        : 'Push ' + plural(pushN, 'outgoing commit') + (upstream ? ' to ' + upstream : '');
  }
  const pull = $('#gg-pull');
  if (pull) {
    pull.disabled = syncRunning;
    pull.querySelector('.gg-n').textContent = pullN ? String(pullN) : '';
    pull.title = pullN
      ? 'Pull ' + plural(pullN, 'incoming commit') + (upstream ? ' from ' + upstream : '')
      : 'Pull (fast-forward only; if the branches diverged, offers a rebase or merge by your coding harness)';
  }
}

function draw() {
  drawToolbar();
  const list = listEl();
  if (!list) return;
  hideCommitCard();
  if (!commits.length) {
    list.innerHTML = '<div class="gg-empty">' + esc(failed || 'No commits yet') + '</div>';
    return;
  }
  const focused = list.contains(document.activeElement)
    ? document.activeElement.closest('.gg-commit')?.dataset.sha : '';

  // The groups count what is listed; a cut-off list says so rather than undercount.
  const count = flag => {
    const n = commits.filter(c => c[flag]).length;
    return n + (more && commits[commits.length - 1][flag] ? '+' : '');
  };
  let html = '', outShown = false, inShown = false;
  commits.forEach((c, i) => {
    const l = lanes[i];
    if (c.outgoing && !outShown) {
      outShown = true;
      html += groupRow('out', ICON_UP, 'Outgoing', count('outgoing') + ' to push',
        'Commits on your branch that ' + (upstream || 'the upstream') + ' does not have yet', l);
    }
    if (c.incoming && !inShown) {
      inShown = true;
      html += groupRow('in', ICON_DOWN, 'Incoming', count('incoming'),
        'Commits on ' + (upstream || 'the upstream') + ' that your branch does not have yet -- Pull brings them in', l);
    }
    html += commitRow(c, l);
  });
  html += footer();
  list.innerHTML = html;

  for (const sha of expanded) {
    const el = commitEl(sha);
    if (el) setExpanded(el, true);
  }
  if (focused) commitEl(focused)?.querySelector('.gg-row')?.focus();
}

function groupRow(kind, icon, label, count, title, l) {
  return '<div class="gg-group gg-group-' + kind + '" title="' + esc(title) + '">' +
    passSvg(l.before, l.beforeColors, false) +
    '<span class="gg-group-icon">' + icon + '</span>' +
    '<span class="gg-group-label">' + label + '</span>' +
    '<span class="gg-group-count">· ' + esc(count) + '</span>' +
  '</div>';
}

const REF_ORDER = { head: 0, branch: 1, tag: 2, remote: 3 };
const REF_TITLE = { head: 'Your checked-out branch', branch: 'Branch', tag: 'Tag', remote: 'Remote branch' };

function refBadges(refs) {
  if (!refs?.length) return '';
  return '<span class="gg-refs">' + [...refs]
    .sort((a, b) => (REF_ORDER[a.kind] ?? 9) - (REF_ORDER[b.kind] ?? 9))
    .map(r => '<span class="gg-ref gg-ref-' + esc(r.kind || 'branch') + '" title="' +
      esc((REF_TITLE[r.kind] || 'Ref') + ': ' + r.name) + '">' + esc(r.name) + '</span>')
    .join('') + '</span>';
}

function commitRow(c, l) {
  const where = c.outgoing ? '\nNot pushed yet' : c.incoming ? '\nIncoming: not on your branch yet' : '';
  const title = (c.subject || '(no message)') + '\n' + (c.author || '') + ' · ' + (c.date || '') +
    '\n' + c.hash + where;
  return '<div class="gg-commit' + (c.incoming ? ' gg-in' : '') + '" data-sha="' + esc(c.full) + '">' +
    '<div class="gg-row" tabindex="0" role="button" aria-expanded="false" title="' + esc(title) + '">' +
      laneSvg(c, l) +
      '<span class="gg-subj">' + esc(c.subject || '(no message)') + '</span>' +
      '<span class="gg-meta">' + esc(c.author || '') + ' · ' + esc(c.date || '') + '</span>' +
      refBadges(c.refs) +
      '<button type="button" class="gg-sha" tabindex="-1" title="Copy SHA">' + esc(c.hash) + '</button>' +
    '</div>' +
    '<div class="gg-files" hidden>' + passSvg(l.after, l.afterColors, true) +
      // Clear of the lanes: the row's 4px inset, the lane column, then the row's gap.
      '<div class="gg-files-list" style="padding-left:' + (width + 10) + 'px"></div>' +
    '</div>' +
  '</div>';
}

function footer() {
  const parts = [];
  if (more && limit < MAX_LIMIT) {
    parts.push('<button type="button" class="gg-load" title="Show ' + Math.min(PAGE, MAX_LIMIT - limit) + ' older commits">Load more</button>');
  }
  if (commitsUrl) {
    parts.push('<a class="gg-open" href="' + esc(commitsUrl) + '" target="_blank" rel="noopener" title="' +
      esc(commitsUrl) + '">Open on ' + esc(forgeInfo().label) + '</a>');
  }
  return parts.length ? '<div class="gg-foot">' + parts.join('') + '</div>' : '';
}

/* ---------- expanding a commit ---------- */

function commitEl(sha) {
  return listEl()?.querySelector('.gg-commit[data-sha="' + CSS.escape(sha) + '"]');
}

async function setExpanded(el, open) {
  const sha = el.dataset.sha;
  const row = el.querySelector('.gg-row');
  const box = el.querySelector('.gg-files');
  const body = el.querySelector('.gg-files-list');
  el.classList.toggle('expanded', open);
  row.setAttribute('aria-expanded', String(open));
  box.hidden = !open;
  if (!open) {
    expanded.delete(sha);
    body.replaceChildren(); // hiding would keep every row in memory
    return;
  }
  expanded.add(sha);
  const cached = cachedCommitFiles(sha);
  if (cached) { drawCommitFiles(body, sha, cached); return; }
  body.innerHTML = '<div class="gg-note">Loading…</div>';
  try {
    const files = await loadCommitFiles(sha);
    if (el.isConnected && expanded.has(sha)) drawCommitFiles(body, sha, files);
  } catch (e) {
    if (el.isConnected) body.innerHTML = '<div class="gg-note">' + esc(e.message || 'Could not load commit') + '</div>';
  }
}

function drawCommitFiles(body, sha, files) {
  const c = commits.find(x => x.full === sha);
  const meta = c ? '<div class="gg-note">' + esc(c.hash) + ' · ' + esc(c.author || '') + ' · ' + esc(c.date || '') + '</div>' : '';
  if (!files.length) {
    body.innerHTML = meta + '<div class="gg-note">No file changes</div>';
    return;
  }
  body.innerHTML = meta;
  appendCommitFiles(body, sha, files);
}

// Builds the next page of rows; returns the index of the first one added.
function appendCommitFiles(body, sha, files) {
  const from = body.querySelectorAll('.up-file').length;
  body.querySelector('.up-more')?.remove();
  body.insertAdjacentHTML('beforeend', commitFileRows(files, from, from + COMMIT_FILES_PAGE, activeCommitPath(sha)));
  return from;
}

function showMoreCommitFiles(el) {
  const files = cachedCommitFiles(el.dataset.sha);
  if (!files) return -1;
  return appendCommitFiles(el.querySelector('.gg-files-list'), el.dataset.sha, files);
}

function openCommitPath(el, path) {
  // px0 has no diff for a binary file, so say so rather than doing nothing.
  if (cachedCommitFiles(el.dataset.sha)?.find(f => f.path === path)?.binary) {
    emit('git:error', { title: 'Binary file, no diff to show', message: '' });
    return;
  }
  openFile(path, { ref: el.dataset.sha, view: 'diff' });
}

// Double-click or Enter on a commit: expand it and open its first file.
async function openFirstCommitFile(el) {
  if (!el.classList.contains('expanded')) setExpanded(el, true);
  try {
    const files = await loadCommitFiles(el.dataset.sha);
    if (files.length) openCommitPath(el, files[0].path);
    else emit('git:error', { title: 'This commit changes no files', message: '' });
  } catch (e) {
    emit('git:error', { title: 'Could not load commit', message: e.message || '' });
  }
}

// The path the active tab shows if it is pinned to sha, else ''.
function activeCommitPath(sha) {
  const d = S.tabs[S.active];
  return d && d.diffRef === sha ? d.path : '';
}

function markActiveCommitFile() {
  const list = listEl();
  if (!list) return;
  for (const x of list.querySelectorAll('.up-file.sel')) x.classList.remove('sel');
  for (const el of list.querySelectorAll('.gg-commit.expanded')) {
    const path = activeCommitPath(el.dataset.sha);
    if (path) el.querySelector('.up-file[data-path="' + CSS.escape(path) + '"]')?.classList.add('sel');
  }
}

/* ---------- commit hover card ---------- */

function commitKeeper() {
  const el = cardEl();
  if (el && !keeper) keeper = makeCardKeeper(el, { hide: hideCommitCard });
  return keeper;
}

function hideCommitCard() {
  clearTimeout(cardTimer);
  cardTimer = 0;
  cardSeq++;
  cardSha = '';
  keeper?.close();
  const el = cardEl();
  if (el && !el.hidden) { el.hidden = true; el.replaceChildren(); }
}

async function showCommitCard(sha, x, y) {
  const el = cardEl();
  if (!el) return;
  const keep = commitKeeper();
  const seq = ++cardSeq;

  let d = details.get(sha);
  if (!d) {
    try { d = await api('/api/commitdetail', { sha }); } catch { return; }
    details.set(sha, d);
  }
  if (seq !== cardSeq) return;

  // Anchor where the pointer is now: the fetch above may have taken a while,
  // and a card anchored behind the pointer closes the moment it moves again.
  const live = pointerPos() || { x, y };
  cardSha = sha;
  el.innerHTML = commitCardMarkup(d);
  el.hidden = false;
  placeCommitCard(el, live.x, live.y);
  keep.open(live);

  el.querySelector('.cc-copy')?.addEventListener('click', e => {
    e.stopPropagation();
    copyToClipboard(d.hash, 'Copied ' + d.short, e.currentTarget);
  });
}

function commitCardMarkup(d) {
  const stat = [];
  if (d.files) stat.push('<span class="cc-files">' + d.files + (d.files === 1 ? ' file' : ' files') + '</span>');
  if (d.insertions) stat.push('<span class="cc-add">+' + d.insertions + '</span>');
  if (d.deletions) stat.push('<span class="cc-del">&minus;' + d.deletions + '</span>');

  const name = esc(d.author || 'unknown');
  /* Best effort: a users.noreply.github.com address carries the login outright,
     any other address only supports "this repo's commits by this author". A
     non-GitHub remote gets no link and the name stays plain text. */
  const who = d.authorUrl
    ? '<a class="cc-who" href="' + esc(d.authorUrl) + '" target="_blank" rel="noopener" title="' +
      esc(d.email || '') + ' — open on GitHub">' + name + '</a>'
    : '<span class="cc-who" title="' + esc(d.email || '') + '">' + name + '</span>';

  return '<div class="cc-head">' +
      '<span class="cc-sha" title="' + esc(d.hash) + '">' + esc(d.short) + '</span>' +
      '<span class="grow"></span>' +
      '<button class="cc-copy" type="button" title="Copy the full SHA">Copy SHA</button>' +
    '</div>' +
    '<div class="cc-meta">' + who +
      '<span class="cc-dot">·</span>' +
      '<span class="cc-date" title="' + esc(d.dateIso || '') + '">' + esc(d.date || '') + '</span>' +
    '</div>' +
    '<div class="cc-msg">' + esc(d.message || d.subject || '') + '</div>' +
    (stat.length ? '<div class="cc-stat">' + stat.join('') + '</div>' : '');
}

/* Just clear of the sidebar rather than under the pointer: the card is wider
   than the sidebar, and anchoring it to the pointer would bury the very list
   it describes. Vertically it follows the pointer, clamped to the window. */
function placeCommitCard(el, x, y) {
  const r = el.getBoundingClientRect();
  const side = $('#side')?.getBoundingClientRect();
  let left = (side ? side.right : x) + 8;
  if (left + r.width > innerWidth - 8) left = Math.max(8, innerWidth - r.width - 8);
  let top = y - 12;
  if (top + r.height > innerHeight - 8) top = innerHeight - r.height - 8;
  el.style.left = left + 'px';
  el.style.top = Math.max(8, top) + 'px';
}

/* ---------- wiring ---------- */

export function initGitGraph() {
  const list = listEl();
  if (!list) return;

  $('#gg-pull')?.addEventListener('click', e => emit('git:pull', { btn: e.currentTarget }));
  $('#gg-push')?.addEventListener('click', e => emit('git:push', { btn: e.currentTarget }));
  $('#gg-refresh')?.addEventListener('click', async e => {
    const svg = e.currentTarget.querySelector('svg');
    svg?.classList.add('spin');
    try {
      await Promise.all([refreshGraph(), triggerRefresh()]);
    } finally {
      svg?.classList.remove('spin');
    }
  });

  list.addEventListener('click', e => {
    if (e.target.closest('.gg-load')) {
      limit = Math.min(MAX_LIMIT, limit + PAGE);
      refreshGraph();
      return;
    }
    const sha = e.target.closest('.gg-sha');
    if (sha) {
      e.stopPropagation();
      copyToClipboard(sha.closest('.gg-commit').dataset.sha, 'Copied ' + sha.textContent, sha);
      return;
    }
    const more = e.target.closest('.up-more');
    if (more) { showMoreCommitFiles(more.closest('.gg-commit')); return; }
    const file = e.target.closest('.up-file');
    if (file) { openCommitPath(file.closest('.gg-commit'), file.dataset.path); return; }
    const row = e.target.closest('.gg-row');
    // The second click of a double-click opens instead (dblclick below).
    if (row && e.detail < 2) {
      hideCommitCard();
      const el = row.closest('.gg-commit');
      setExpanded(el, !el.classList.contains('expanded'));
    }
  });
  list.addEventListener('dblclick', e => {
    const row = e.target.closest('.gg-row');
    if (!row || e.target.closest('.gg-sha')) return;
    openFirstCommitFile(row.closest('.gg-commit'));
  });
  list.addEventListener('keydown', e => {
    const item = e.target.closest('.gg-row, .up-file, .up-more');
    if (!item) return;
    const el = item.closest('.gg-commit');
    const isRow = item.classList.contains('gg-row');
    if (item.classList.contains('up-more') && (e.key === 'Enter' || e.key === ' ')) {
      e.preventDefault();
      const first = showMoreCommitFiles(el);
      el.querySelectorAll('.up-file')[first]?.focus();
      return;
    }
    const items = [...list.querySelectorAll('.gg-row, .gg-commit.expanded .up-file, .gg-commit.expanded .up-more')];
    const at = items.indexOf(item);
    switch (e.key) {
      case 'ArrowDown': items[at + 1]?.focus(); break;
      case 'ArrowUp': items[at - 1]?.focus(); break;
      case 'ArrowRight':
        if (isRow && !el.classList.contains('expanded')) setExpanded(el, true);
        else if (isRow) items[at + 1]?.focus();
        break;
      case 'ArrowLeft':
        if (isRow) setExpanded(el, false);
        else el.querySelector('.gg-row').focus();
        break;
      case 'Enter':
        if (isRow) openFirstCommitFile(el);
        else openCommitPath(el, item.dataset.path);
        break;
      case ' ':
        if (isRow) setExpanded(el, !el.classList.contains('expanded'));
        else openCommitPath(el, item.dataset.path);
        break;
      default: return;
    }
    e.preventDefault();
  });

  /* The card follows the commit row under the pointer; once it is open its
     keeper watches the pointer the rest of the way in, across the gap between
     the sidebar and the card (see cardkeep.js). */
  list.addEventListener('mousemove', e => {
    const row = e.target.closest('.gg-row');
    if (!row) { clearTimeout(cardTimer); cardTimer = 0; return; }
    const sha = row.closest('.gg-commit')?.dataset.sha;
    if (!sha || sha === cardSha) return;
    clearTimeout(cardTimer);
    const { clientX, clientY } = e;
    cardTimer = setTimeout(() => showCommitCard(sha, clientX, clientY), CARD_DELAY);
  });
  list.addEventListener('mouseleave', () => { clearTimeout(cardTimer); cardTimer = 0; });
  list.addEventListener('scroll', hideCommitCard, { passive: true });
  addEventListener('keydown', e => { if (e.key === 'Escape') hideCommitCard(); });

  on('tab:activated', markActiveCommitFile);
  // While gitpanel.js runs a rebase/merge job, push and pull wait for it.
  on('git:sync', st => {
    syncRunning = !!st?.running;
    drawToolbar();
  });
}
