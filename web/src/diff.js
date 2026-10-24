// web/src/diff.js
// Git diff view for the active tab: renders the file's unified diff against
// HEAD in a dedicated overlay (like the Markdown preview), in either a
// side-by-side split layout (default) or a single-column unified layout.
// Unlike the code viewport this is not virtualized -- a file's own diff is
// bounded in size, so a plain DOM render is simple and fast enough.
import { $, S, doc_, esc, api, apiPostJson, MOD } from './state.js';
import { on } from './bus.js';
import { showToast } from './ui.js';
import { syncPreview } from './markdown.js';
import { setStatusNote, updateStatus } from './status.js';
import { wordAtPoint } from './cursor.js';
import { gotoDefinition } from './lsp.js';
import { hoverAt } from './hover.js';
import { pushHistory } from './history.js';
import { COMMIT_STATUS, loadCommitFiles, cachedCommitFiles } from './commitfiles.js';
import { hunkHeader, unifiedTable, splitTable, fileLayout, ICON_FOLD, ICON_UNFOLD } from './difftable.js';
import { handleGitStatus, triggerRefresh } from './gitstream.js';

export const diffview = $('#diffview');
const diffContent = $('#diffcontent');

let shown = null; // doc the diff view is currently showing, null while hidden

// d.diffMode is 'split' | 'unified' | null (off), per tab. The layout last
// picked (split vs unified) is remembered globally as the default for the
// next file entering diff view.
export function setLayoutPref(mode) {
  try { localStorage.setItem('px0.diffLayout', mode); } catch {}
}

export function layoutPref() {
  try { return localStorage.getItem('px0.diffLayout') || 'split'; } catch { return 'split'; }
}

function diffMode(d = doc_()) {
  return (d && d.diffMode) || null;
}

/* Show or hide the diff overlay to match the active tab, and re-render when
   the layout (split/unified) changes while already showing the same doc --
   switching layout doesn't change which doc is "shown", so that alone can't
   be the signal to redraw. Call whenever either might have changed. */
export function syncDiffView(force = false) {
  const d = doc_();
  const want = (d && d.diffMode) ? d : null;
  if (force && want) {
    want.diffText = undefined;
    want.diffHunks = undefined;
  }
  if (want !== shown || force) {
    shown = want;
    diffview.hidden = !want;
    if (want) drawDiff(want, force);
    else { diffContent.replaceChildren(); if (prSyncHandler) prSyncHandler(); }
  } else if (want) {
    /* Same doc still on screen. A caller that dropped the cached diff -- the
       tab being pointed at a different commit, say -- leaves the view showing
       the wrong revision, so that has to refetch rather than just repaint. */
    if (want.diffText === undefined) drawDiff(want);
    else if (want.diffHunks !== undefined) renderDiff(want);
  }
}

// Read by a reload, which swaps the doc and so redraws the diff from the top.
export function diffScrollTop() {
  return diffview.hidden ? 0 : diffview.scrollTop;
}

export async function toggleDiff() {
  if (!S.meta?.git) return;
  const d = doc_();
  if (!d) return;
  if (!d.diffMode && !d.diffAvailable) {
    setStatusNote(S.meta?.git ? 'No diff — clean file' : 'No diff — clean file or not a git repo', 4000);
    return;
  }
  setDiffMode(d.diffMode ? 'source' : (layoutPref() || 'split'));
}

export async function setDiffMode(mode) {
  const d = doc_();
  if (!d) return;
  if (mode !== 'source' && !d.diffAvailable) {
    setStatusNote(S.meta?.git ? 'No diff — clean file' : 'No diff — clean file or not a git repo', 4000);
    return;
  }
  if (mode === 'source') {
    d.diffMode = null;
    d.diffDismissed = true;
  } else {
    d.diffMode = mode;
    d.diffDismissed = false;
    d.openedInDiffView = true;
    setLayoutPref(mode);
  }
  syncPreview(); // markdown preview and diff view are mutually exclusive
  syncDiffView();
  updateStatus();
}

async function drawDiff(d, force = false) {
  if (force || d.diffText === undefined) {
    diffContent.replaceChildren();
    /* Which revision this fetch is for. Clicking through a commit's files, or
       from a commit back to the tree, can leave an earlier request in flight;
       without this its answer would land in a doc that has since been pointed
       somewhere else and show the wrong commit's diff. */
    const ref = d.diffRef || '';
    const base = d.diffBase || '';
    try {
      d.diffReq = api('/api/diff', { path: d.path, ref, base });
      const j = await d.diffReq;
      if ((d.diffRef || '') !== ref || (d.diffBase || '') !== base) return;
      d.diffText = j.diff || '';
      // A fresh diff may follow an edit: expanded context must re-read the file.
      d.fullFileLines = d.fullFileRaw = undefined;
      d.diffHunks = j.hunks || parseDiff(d.diffText);
      // A diff that is one whole file (new or deleted: server wholeFile,
      // {kind, actions}) gets whole-file actions; large: an untracked file too
      // big to show as a diff.
      d.diffWholeFile = j.wholeFile || null;
      d.diffLarge = !!j.large;
      // In a PR review session the server also splits the diff at the PR's
      // checked-out head commit: prDiff is the PR's own change (frozen since
      // checkout/last Pull), yourDiff is whatever the reviewer has edited or
      // committed locally since then. Undefined outside PR mode.
      d.prDiffHunks = j.prHunks !== undefined ? j.prHunks : (j.prDiff !== undefined ? parseDiff(j.prDiff) : undefined);
      d.yourDiffHunks = j.yourHunks !== undefined ? j.yourHunks : (j.yourDiff !== undefined ? parseDiff(j.yourDiff) : undefined);
    } catch (e) {
      if ((d.diffRef || '') !== ref || (d.diffBase || '') !== base) return;
      d.diffText = '';
      d.diffHunks = [];
      d.prDiffHunks = undefined;
      d.yourDiffHunks = undefined;
      d.diffWholeFile = null;
      d.diffLarge = false;
      setStatusNote('No diff: ' + e.message, 4000);
    } finally {
      d.diffReq = null;
    }
    if (shown !== d) return;
  }
  renderDiff(d);
  if (d.diffScroll) {
    diffview.scrollTop = d.diffScroll;
    d.diffScroll = 0;
  } else if (d.cur) {
    scrollDiffToLine(d.cur);
  }
}

export function scrollDiffToLine(line) {
  if (!diffview || !line) return;
  const row = diffview.querySelector(`[data-l="${line}"]`) ||
              diffview.querySelector(`[data-at="${line}"]`) ||
              diffview.querySelector(`[data-old-l="${line}"]`);
  if (row) {
    row.scrollIntoView({ block: 'center', behavior: 'auto' });
    row.classList.add('diff-row-flash');
    setTimeout(() => row.classList.remove('diff-row-flash'), 1200);
  }
}

// actions: the hunk headers' button set (difftable.js ACTIONS), null for none.
function appendHunks(frag, hunks, mode, reviewable, actions = null) {
  const opts = {
    reviewable,
    lineActions: S.meta?.pr && reviewable ? 'Thread, review comment and line actions' : 'Thread and line actions',
    diagnostics: buildDiagMap(),
  };
  const layout = fileLayout(hunks, mode);
  for (let idx = 0; idx < hunks.length; idx++) {
    const hunk = hunks[idx];
    frag.append(hunkHeader(hunk, { index: idx, expand: expandable(hunk), actions }));
    frag.append(layout === 'unified' ? unifiedTable(hunk, opts) : splitTable(hunk, opts));
  }
}

function renderDiff(d) {
  diffContent.replaceChildren();
  const frag = document.createDocumentFragment();
  if (d.diffRef) {
    /* Pinned to one commit by the git panel's commit graph (a chat turn's
       snapshot or an agent's commit among them), or a thread turn
       snapshot range (diffBase..diffRef). Its line numbers are that commit's,
       not the working tree's, so nothing here is a review target
       (reviewable=false) -- see anchor() in difftable.js. */
    const hunks = d.diffHunks || [];
    const label = d.turnLabel || '';
    if (!hunks.length) {
      const p = document.createElement('div');
      p.className = 'diff-empty';
      p.textContent = (label || 'This commit') + ' made no change to ' + d.name + '.';
      diffContent.append(p);
      return;
    }
    frag.append(d.turnFiles ? reviewStrip(d) : commitStrip(d));
    const title = label || ('In commit ' + d.diffRef.slice(0, 7));
    const sub = d.turnLabel ? 'snapshot diff for this agent turn' : turn?.kind === 'turn' ? 'what this chat turn changed' : 'this commit only, not the working tree';
    frag.append(createDiffSection(d, 'commit', title, sub,
      (bodyEl) => appendHunks(bodyEl, hunks, d.diffMode, false)));
    diffContent.append(frag);
    syncDiffAgentTargets();
    if (prSyncHandler) prSyncHandler();
    return;
  }
  const strip = reviewStrip(d);
  if (strip) frag.append(strip);
  if (S.meta?.pr && d.prDiffHunks !== undefined) {
    const prHunks = d.prDiffHunks || [];
    const yourHunks = d.yourDiffHunks || [];
    // Opened from the sidebar's "Yours" or "PR changes" scope: open the
    // matching section and fold the other. Decided once per tab; after that
    // the section's own header toggle wins.
    const tree = $('#tree');
    if (d.prCollapsed === undefined) {
      d.prCollapsed = !!tree?.classList.contains('scope-yours') && yourHunks.length > 0;
    }
    if (d.yourCollapsed === undefined) {
      d.yourCollapsed = !!tree?.classList.contains('scope-pr') && prHunks.length > 0;
    }
    if (!prHunks.length && !yourHunks.length) {
      const p = document.createElement('div');
      p.className = 'diff-empty';
      p.textContent = 'No changes.';
      diffContent.append(p);
      return;
    }
    frag.append(createDiffSection(d, 'pr', 'PR changes', 'from ' + (S.meta.pr.base || 'base'), (body) => {
      if (!prHunks.length) body.append(sectionNote('The PR itself makes no change to this file.'));
      else appendHunks(body, prHunks, d.diffMode, true);
    }));

    const since = S.meta.pr.headSHA ? 'since ' + S.meta.pr.headSHA.slice(0, 7) : 'since checkout';
    frag.append(createDiffSection(d, 'you', 'Your changes', since, (body) => {
      if (!yourHunks.length) body.append(sectionNote('Nothing edited or committed yet — changes you make will show up here.'));
      else appendHunks(body, yourHunks, d.diffMode, false, d.diffWholeFile || 'hunk');
    }));
  } else {
    if (!d.diffHunks || !d.diffHunks.length) {
      const p = document.createElement('div');
      p.className = 'diff-empty';
      p.textContent = d.diffLarge ? 'New file over 256 KB: too large to show as a diff. Use Source to read it.' : 'No changes against HEAD.';
      frag.append(p);
      diffContent.append(frag); // the strip's ‹ › still step on from here
      return;
    }
    appendHunks(frag, d.diffHunks, d.diffMode, true, d.diffWholeFile || 'hunk');
  }
  diffContent.append(frag);
  syncDiffAgentTargets();
  if (prSyncHandler) prSyncHandler();
}

/* Points a tab's diff at the section the sidebar scope is showing. A section
   with nothing in it is left open: folding away the only content leaves an
   empty pane. */
export function focusDiffScope(d) {
  const tree = $('#tree');
  if (!d || !S.meta?.pr || !tree) return;
  if (tree.classList.contains('scope-yours') && d.yourDiffHunks?.length !== 0) {
    d.prCollapsed = true; d.yourCollapsed = false;
  } else if (tree.classList.contains('scope-pr') && d.prDiffHunks?.length !== 0) {
    d.prCollapsed = false; d.yourCollapsed = true;
  }
}

function createDiffSection(d, kind, title, sub, populateBody) {
  const sec = document.createElement('div');
  sec.className = 'diff-section diff-section-' + kind;
  const collapsedKey = kind + 'Collapsed';
  if (d[collapsedKey]) {
    sec.classList.add('collapsed');
  }

  const head = document.createElement('div');
  head.className = 'diff-section-head diff-section-head-' + kind;
  head.title = 'Click to collapse/expand section';

  const t = document.createElement('span');
  t.className = 'diff-section-title';
  t.textContent = title;
  head.append(t);

  if (sub) {
    const s = document.createElement('span');
    s.className = 'diff-section-sub';
    s.textContent = sub;
    head.append(s);
  }

  const grow = document.createElement('span');
  grow.className = 'grow';
  head.append(grow);

  const btn = document.createElement('button');
  btn.className = 'pr-comments-collapse diff-section-collapse';
  btn.title = 'Collapse/Expand';
  btn.innerHTML = '&#9662;';
  head.append(btn);

  const body = document.createElement('div');
  body.className = 'diff-section-body';
  populateBody(body);

  head.addEventListener('click', () => {
    sec.classList.toggle('collapsed');
    d[collapsedKey] = sec.classList.contains('collapsed');
    if (prSyncHandler) prSyncHandler();
  });

  sec.append(head, body);
  return sec;
}

function sectionNote(text) {
  const el = document.createElement('div');
  el.className = 'diff-section-note';
  el.textContent = text;
  return el;
}

/* One-way registration for pr.js, mirroring agent.js's hook into selbar.js:
   diff.js never imports pr.js, it just calls this after every repaint when a
   PR review session has set it. */
let prSyncHandler = null;
export function setPRSyncHandler(fn) { prSyncHandler = fn; }

/* Same one-way registration for tabs.js: diff.js knows how to leave diff mode
   but not how to move the caret and scroll the (already open) source view to
   a given line, so it hands the line off to whatever tabs.js registered. */
let sourceJumpHandler = null;
export function setSourceJumpHandler(fn) { sourceJumpHandler = fn; }

/* And for the commit strip's ‹ › buttons: tabs.js opens the file stepped to. */
let commitStepHandler = null;
export function setCommitStepHandler(fn) { commitStepHandler = fn; }

// The nearest file from i in direction dir that can be opened: binary files
// have no text diff and /api/file refuses them, so stepping passes over them.
function commitStepTarget(files, i, dir) {
  for (let k = i + dir; k >= 0 && k < files.length; k += dir) if (!files[k].binary) return k;
  return -1;
}

/* Where a pinned tab's file sits in its commit, with ‹ › to step through the
   rest. The file list is fetched on first need and filled in when it lands.
   When the commit is a chat turn's snapshot or an agent's own commit (the
   turn ledger, turns.js), the strip leads with the turn and its prompt and
   carries the "Mark reviewed" toggle. */
function commitStrip(d) {
  const el = document.createElement('div');
  el.className = 'diff-commit-strip';
  const fill = files => {
    const i = files.findIndex(f => f.path === d.path);
    if (i < 0) { el.remove(); return; }
    const f = files[i];
    const g = COMMIT_STATUS[f.status] || ['git-M', f.status];
    const path = f.from ? esc(f.from) + ' → ' + esc(f.path) : esc(f.path);
    el.innerHTML =
      '<span class="gs ' + g[0] + '" title="' + esc(g[1]) + '">' + esc(f.status) + '</span>' +
      '<span class="dcs-path" title="' + path + '">' + path + '</span>' +
      (f.binary ? '' : (f.add ? '<span class="up-file-add">+' + f.add + '</span>' : '') + (f.del ? '<span class="up-file-del">&minus;' + f.del + '</span>' : '')) +
      '<span class="grow"></span>' +
      hunkControls(d) +
      fileStepButtons('data-commit-step', 'file in this commit', i, files.length,
        commitStepTarget(files, i, -1) < 0, commitStepTarget(files, i, 1) < 0);
    el.dataset.i = i;
  };
  const have = cachedCommitFiles(d.diffRef);
  if (have) fill(have);
  else loadCommitFiles(d.diffRef).then(files => { if (el.isConnected || doc_() === d) fill(files); }, () => el.remove());
  return el;
}
// The working-tree line number of whichever diff row currently sits at the
// top of the scrolled viewport -- what "Source" should land on so switching
// out of diff view keeps you where you were reading, not wherever the
// doc's cursor last happened to be.
function currentDiffLine() {
  if (!diffview || diffview.hidden) return null;
  const top = diffview.getBoundingClientRect().top;
  for (const el of diffview.querySelectorAll('[data-l], [data-at]')) {
    if (el.getBoundingClientRect().bottom > top) {
      return el.dataset.l !== undefined ? +el.dataset.l : +el.dataset.at;
    }
  }
  return null;
}

export function syncDiffAgentTargets() {
  if (!diffview || diffview.hidden) return;
  const d = doc_();
  if (!d) return;
  const ranges = (S.agentTargets || []).filter(t => t.path === d.path);
  for (const el of diffview.querySelectorAll('[data-l]')) {
    const l = +el.dataset.l;
    const inAgent = ranges.some(r => l >= r.l1 && l <= r.l2);
    const isAnchor = ranges.some(r => l === r.l1);
    el.classList.toggle('agent-sel', inAgent);
    el.classList.toggle('agent-anchor', isAnchor);
  }
}

/* ---------- code review: hunk/file stepping, context expansion, hunk actions ---------- */

const svg = path => '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="' + path + '"/></svg>';
const ICON_HUNK_PREV = svg('m18 15-6-6-6 6');
const ICON_HUNK_NEXT = svg('m6 9 6 6 6-6');
const ICON_FILE_PREV = svg('m15 18-6-6 6-6');
const ICON_FILE_NEXT = svg('m9 18 6-6-6-6');

/* The files ‹ › and [f / ]f step through: the turn's or commit's files, else
   the working tree's changes in the order the file tree lists them. */
function currentChangedFiles(d = doc_()) {
  if (d?.turnFiles?.length) {
    return d.turnFiles.map(p => (typeof p === 'string' ? { path: p, status: 'M' } : p));
  }
  if (d?.diffRef) {
    return cachedCommitFiles(d.diffRef) || [];
  }
  const paths = [...(S.meta?.gitFiles || [])];
  if (d?.path && d.diffAvailable && !paths.includes(d.path)) paths.push(d.path);
  return paths.sort(treeOrder).map(path => ({ path }));
}

/* The file tree's order (index.go sortNodes): segment by segment, folders
   before files, then names case-insensitively. The server lists gitFiles
   tracked first, untracked after, which stepping must not follow: from an
   untracked file it would only ever reach other untracked ones. */
function treeOrder(a, b) {
  const pa = a.split('/'), pb = b.split('/');
  for (let i = 0; i < pa.length && i < pb.length; i++) {
    if (pa[i] === pb[i]) continue;
    const fileA = i === pa.length - 1, fileB = i === pb.length - 1;
    if (fileA !== fileB) return fileA ? 1 : -1;
    const x = pa[i].toLowerCase(), y = pb[i].toLowerCase();
    return x < y ? -1 : x > y ? 1 : 0;
  }
  return pa.length - pb.length;
}

/* The hunk lists renderDiff draws for d, each numbered against its own new
   side: the PR's change and your change since checkout in a PR review, else
   the one diff. */
function hunkLists(d) {
  if (!d.diffRef && S.meta?.pr && d.prDiffHunks !== undefined) {
    return [d.prDiffHunks || [], d.yourDiffHunks || []];
  }
  return [d.diffHunks || []];
}

/* ↑ ↓ through the hunks, and one toggle widening every hunk's context or,
   once any is widened, putting them all back. */
function hunkControls(d) {
  const lists = hunkLists(d);
  const expanded = lists.some(l => l.some(h => h.unexpanded));
  const none = !expanded && !lists.some(l => l.some(expandable));
  const expandTitle = none ? 'Whole file shown: no context to add'
    : expanded ? 'Collapse all hunks to the diff\'s own context' : 'Expand context around all hunks';
  return '<button type="button" class="dcs-step" data-hunk-step="-1" title="Previous hunk ([c)" aria-label="Previous hunk">' + ICON_HUNK_PREV + '</button>' +
    '<button type="button" class="dcs-step" data-hunk-step="1" title="Next hunk (]c)" aria-label="Next hunk">' + ICON_HUNK_NEXT + '</button>' +
    '<button type="button" class="dcs-step" data-hunk-expand-all="1" aria-pressed="' + expanded + '" title="' + expandTitle + '" aria-label="' + expandTitle + '"' + (none ? ' disabled' : '') + '>' + (expanded ? ICON_FOLD : ICON_UNFOLD) + '</button>' +
    '<span class="dcs-sep" aria-hidden="true"></span>';
}

/* ‹ › to the previous / next file; where the file sits among them is in the
   titles. attr is the data attribute the click handler keys on, keys the
   [prev, next] shortcuts if any. */
function fileStepButtons(attr, noun, i, n, prevOff, nextOff, keys = null) {
  const title = (dir, k) => dir + ' ' + noun + (keys ? ' (' + keys[k] + ')' : '') + ' · ' + (i + 1) + ' of ' + n;
  return '<button type="button" class="dcs-step" ' + attr + '="-1" title="' + title('Previous', 0) + '" aria-label="Previous ' + noun + '"' + (prevOff ? ' disabled' : '') + '>' + ICON_FILE_PREV + '</button>' +
    '<button type="button" class="dcs-step" ' + attr + '="1" title="' + title('Next', 1) + '" aria-label="Next ' + noun + '"' + (nextOff ? ' disabled' : '') + '>' + ICON_FILE_NEXT + '</button>';
}

function reviewStrip(d) {
  const files = currentChangedFiles(d);
  const i = files.findIndex(f => f.path === d.path);
  const el = document.createElement('div');
  el.className = 'diff-commit-strip diff-review-strip';
  el.innerHTML =
    (d.turnLabel ? '<span class="gs git-M">' + esc(d.turnLabel) + '</span>' : '') +
    '<span class="dcs-path" title="' + esc(d.path) + '">' + esc(d.path) + '</span>' +
    '<span class="grow"></span>' +
    hunkControls(d) +
    (i >= 0 ? fileStepButtons('data-review-step', 'changed file', i, files.length, i <= 0, i >= files.length - 1, ['[f', ']f']) : '');
  return el;
}

export function stepChangedFile(dir = 1) {
  const d = doc_();
  if (!d) return;
  if (d.diffRef && !d.turnFiles) {
    const files = cachedCommitFiles(d.diffRef);
    if (!files) return;
    const i = files.findIndex(f => f.path === d.path);
    const nextIdx = commitStepTarget(files, i, dir);
    if (nextIdx >= 0 && commitStepHandler) commitStepHandler(d, files[nextIdx]);
    return;
  }
  const files = currentChangedFiles(d);
  if (!files.length || !commitStepHandler) return;
  const i = files.findIndex(f => f.path === d.path);
  stepTo(d, files, i < 0 ? 0 : i + dir, dir);
}

/* Opens files[i], else the next file in direction dir that opens. Unlike a
   commit's list, the working tree's carries no binary flag, and /api/file
   refuses a binary file, so a failed open is the cue to pass it by. */
async function stepTo(d, files, i, dir) {
  for (; i >= 0 && i < files.length; i += dir) {
    if (await commitStepHandler(d, files[i])) return;
  }
}

/* Scrolls the next / previous hunk to just under the sticky strip. Passed hunk
   headers stay stuck at the top, so a hunk's place is read off its body (the
   header's next sibling), and diffview alone is scrolled: scrollIntoView
   would move the page's other scroll containers too. */
export function stepDiffHunk(dir = 1) {
  if (!diffview || diffview.hidden) return;
  // offsetParent is null inside a folded section.
  const heads = [...diffview.querySelectorAll('.diff-hunk-head')].filter(h => h.nextElementSibling && h.offsetParent);
  if (!heads.length) return;
  const strip = diffContent.querySelector(':scope > .diff-commit-strip');
  const top = diffview.getBoundingClientRect().top + (strip?.offsetHeight || 0);
  const pos = heads.map(h => h.nextElementSibling.getBoundingClientRect().top - h.offsetHeight - top);
  const slack = 4;
  let i = dir > 0 ? pos.findIndex(p => p > slack) : pos.findLastIndex(p => p < -slack);
  if (i < 0) i = dir > 0 ? 0 : heads.length - 1;
  diffview.scrollTop += pos[i];
}

function serializeHunk(hunk) {
  let oldCount = 0, newCount = 0;
  let oldStart = hunk.oldStart || 1;
  let newStart = hunk.newStart || 1;
  let foundOld = false, foundNew = false;
  const body = [];
  for (const r of hunk.rows) {
    if (!foundOld && r.oldLine !== undefined) { oldStart = r.oldLine; foundOld = true; }
    if (!foundNew && r.newLine !== undefined) { newStart = r.newLine; foundNew = true; }
    const text = r.text ?? '';
    if (r.type === 'ctx') {
      oldCount++; newCount++;
      body.push(' ' + text);
    } else if (r.type === 'del') {
      oldCount++;
      body.push('-' + text);
    } else if (r.type === 'add') {
      newCount++;
      body.push('+' + text);
    }
  }
  return `@@ -${oldStart},${oldCount} +${newStart},${newCount} @@\n` + body.join('\n') + '\n';
}

async function applyHunkAction(d, hunk, action) {
  if (!d || !hunk) return;
  try {
    await apiPostJson('/api/git/hunk', {
      path: d.path,
      hunk: serializeHunk(hunk),
      action,
    });
    const labels = { stage: 'Staged hunk', unstage: 'Unstaged hunk', discard: 'Discarded hunk' };
    showToast(labels[action] || 'Applied hunk', 'success', 2000);
    d.diffScroll = diffview.scrollTop;
    syncDiffView(true);
  } catch (e) {
    showToast('Hunk ' + action + ' failed: ' + e.message, 'error', 4000);
  }
}

/* A whole-file diff's buttons (difftable.js FILE_ACTION_TITLES): the same
   stage / unstage the tree's tick does, and discard, which deletes a new file
   -- git holds no copy, hence the confirm -- or restores a deleted one. */
const FILE_ACTIONS = {
  stage: ['/api/git/stage', 'Staged'],
  unstage: ['/api/git/unstage', 'Unstaged'],
  discard: ['/api/git/discard-file', 'Discarded'],
};

async function applyFileAction(d, action) {
  const [url, done] = FILE_ACTIONS[action] || [];
  if (!d || !url) return;
  const restoring = d.diffWholeFile?.kind === 'deleted';
  if (action === 'discard' && !restoring && !confirm(`Delete "${d.path}"? It is a new file, so git has no copy to bring back.`)) return;
  try {
    const res = await apiPostJson(url, { path: d.path });
    showToast(action === 'discard' ? (restoring ? 'Restored ' : 'Deleted ') + d.name : done + ' ' + d.name, 'success', 2000);
    if (res && res.git !== undefined) await handleGitStatus(res);
    else await triggerRefresh();
    // A file left changed shows its new state; a clean one's tab has closed.
    if (doc_() === d) syncDiffView(true);
  } catch (e) {
    showToast(action[0].toUpperCase() + action.slice(1) + ' failed: ' + e.message, 'error', 4000);
  }
}

// A hunk at old line 0 adds a whole file, one at new line 0 removes one
// (with git's default 3 lines of context, nothing else lands there): either
// way the hunk is the whole file, with no context to add.
function expandable(hunk) {
  return hunk.oldStart !== 0 && hunk.newStart !== 0;
}

const CONTEXT_STEP = 15;

// The new-side line a hunk starts at / ends on. A leading deletion has no
// new line; at is the line it sat before.
function hunkFirstNew(hunk) {
  const first = hunk.rows[0];
  return first.newLine ?? first.at ?? hunk.newStart ?? 1;
}

function hunkLastNew(hunk) {
  let last = hunkFirstNew(hunk);
  for (const r of hunk.rows) if (r.newLine !== undefined) last = r.newLine;
  return last;
}

async function loadFullFile(d) {
  if (d.fullFileLines && d.fullFileRaw) return;
  const j = await api('/api/file', { path: d.path, raw: '1', ref: d.diffRef || '' });
  d.fullFileLines = j.lines || [];
  d.fullFileRaw = j.raw || [];
}

/* Adds up to CONTEXT_STEP file lines of context on each side of hunks[idx],
   stopping short of its neighbours so no line shows twice, and keeps the git
   rows in hunk.unexpanded for collapseHunk. False when there was nothing
   more to show. Needs loadFullFile first. */
function widenHunk(d, hunks, idx) {
  const hunk = hunks[idx];
  const raw = d.fullFileRaw;
  const html = d.fullFileLines;
  if (!hunk?.rows?.length || (!raw.length && !html.length)) return false;

  const first = hunk.rows[0];
  const firstOld = first.oldLine ?? hunk.oldStart ?? 1;
  const firstNew = hunkFirstNew(hunk);
  let lastOld = firstOld;
  for (const r of hunk.rows) if (r.oldLine !== undefined) lastOld = r.oldLine;
  const lastNew = hunkLastNew(hunk);
  const floor = idx > 0 ? hunkLastNew(hunks[idx - 1]) : 0;
  const ceil = idx < hunks.length - 1 ? hunkFirstNew(hunks[idx + 1]) - 1 : Math.max(raw.length, html.length);

  const row = (ol, nl) => ({ type: 'ctx', oldLine: ol, newLine: nl, text: raw[nl - 1] ?? '', html: html[nl - 1] ?? '' });
  const preCount = Math.min(CONTEXT_STEP, Math.max(0, Math.min(firstOld - 1, firstNew - 1 - floor)));
  const preRows = [];
  for (let k = preCount; k >= 1; k--) preRows.push(row(firstOld - k, firstNew - k));
  const postCount = Math.min(CONTEXT_STEP, Math.max(0, ceil - lastNew));
  const postRows = [];
  for (let k = 1; k <= postCount; k++) postRows.push(row(lastOld + k, lastNew + k));
  if (!preRows.length && !postRows.length) return false;

  hunk.unexpanded ??= { rows: hunk.rows, oldStart: hunk.oldStart, newStart: hunk.newStart };
  hunk.rows = [...preRows, ...hunk.rows, ...postRows];
  hunk.oldStart = Math.max(1, firstOld - preCount);
  hunk.newStart = Math.max(1, firstNew - preCount);
  return true;
}

function collapseHunk(hunk) {
  if (!hunk.unexpanded) return;
  Object.assign(hunk, hunk.unexpanded);
  delete hunk.unexpanded;
}

function redrawKeepingScroll(d) {
  const savedScroll = diffview.scrollTop;
  renderDiff(d);
  diffview.scrollTop = savedScroll;
}

// One hunk's Expand / Collapse button.
async function toggleHunkContext(d, hunk) {
  if (!d || !hunk) return;
  if (hunk.unexpanded) {
    collapseHunk(hunk);
    redrawKeepingScroll(d);
    return;
  }
  const hunks = hunkLists(d).find(l => l.includes(hunk));
  if (!hunks) return;
  try {
    await loadFullFile(d);
  } catch (e) {
    setStatusNote('Cannot expand context: ' + e.message, 3000);
    return;
  }
  if (!widenHunk(d, hunks, hunks.indexOf(hunk))) {
    setStatusNote('Full file context already visible for this hunk', 2500);
    return;
  }
  redrawKeepingScroll(d);
}

// The strip's toggle: collapse every widened hunk, or widen them all.
async function toggleAllHunkContext(d) {
  if (!d) return;
  const lists = hunkLists(d);
  if (lists.some(l => l.some(h => h.unexpanded))) {
    for (const l of lists) l.forEach(collapseHunk);
    redrawKeepingScroll(d);
    return;
  }
  if (!lists.some(l => l.length)) return;
  try {
    await loadFullFile(d);
  } catch (e) {
    setStatusNote('Cannot expand context: ' + e.message, 3000);
    return;
  }
  let widened = false;
  for (const l of lists) for (let i = 0; i < l.length; i++) widened = widenHunk(d, l, i) || widened;
  if (!widened) {
    setStatusNote('Full file context already visible', 2500);
    return;
  }
  redrawKeepingScroll(d);
}

/* ---------- unified diff parsing ---------- */

const HUNK_RE = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@[ \t]?(.*)$/;

// Parses a unified diff (as returned by `git diff`) into hunks, each a flat
// list of rows tagged ctx/add/del carrying old- and/or new-file line numbers.
// File headers (diff --git, index, ---, +++) are skipped: nothing before the
// first @@ is kept.
function parseDiff(text) {
  if (!text) return [];
  const hunks = [];
  let cur = null, oldLine = 0, newLine = 0;
  for (const line of text.split('\n')) {
    const m = HUNK_RE.exec(line);
    if (m) {
      oldLine = +m[1];
      newLine = +m[3];
      cur = { oldStart: oldLine, newStart: newLine, section: m[5] || '', rows: [] };
      hunks.push(cur);
      continue;
    }
    if (!cur || line === '' || line.startsWith('\\')) continue; // trailing split artifact, pre-hunk header, or "\ No newline..."
    const c = line[0], body = line.slice(1);
    if (c === '+') cur.rows.push({ type: 'add', newLine: newLine++, text: body });
    // A deletion has no line on disk; at is the working-tree line it sat before.
    else if (c === '-') cur.rows.push({ type: 'del', oldLine: oldLine++, at: newLine, text: body });
    else cur.rows.push({ type: 'ctx', oldLine: oldLine++, newLine: newLine++, text: body });
  }
  return hunks;
}

/* Diagnostics by line for the diff tables' code cells (data-diag), the worst
   per line winning: errors (severity 1) over warnings. */
function buildDiagMap(d = doc_()) {
  if (!d?.diagnostics?.length) return null;
  const map = new Map();
  for (const diag of d.diagnostics) {
    if (!diag.line) continue;
    const prefix = diag.severity === 1 ? '■ ' : '▲ ';
    const prev = map.get(diag.line);
    if (!prev || diag.severity === 1) {
      map.set(diag.line, prefix + diag.message);
    }
  }
  return map;
}
export function initDiff() {
  const sw = $('#diff-switch');
  if (!sw) return;
  sw.addEventListener('mousedown', e => {
    if (!e.target.closest('button')) e.preventDefault();
  });
  // Each half of the switch names a view, so a click shows that view rather than toggling.
  $('#diff-source')?.addEventListener('click', e => {
    e.stopPropagation();
    const line = currentDiffLine();
    if (line && sourceJumpHandler) sourceJumpHandler(line);
    else setDiffMode('source');
  });
  diffContent.addEventListener('mousedown', e => {
    if (e.button !== 0) return;
    if (e.target.closest('.line-btn') || e.target.closest('.diff-ln-nav')) return;
    const diffCode = e.target.closest('.diff-code');
    if (!diffCode) return;
    const w = wordAtPoint(e.clientX, e.clientY);
    if (!w) return;
    S.at = w;
    S.lastWord = w.word;
    const d = doc_();
    if (e[MOD]) {
      e.preventDefault();
      e.stopPropagation();
      if (d) pushHistory(d.path, w.line);
      // Plain click keeps the current mode; Alt flips it.
      const inDiff = !!d.diffMode;
      gotoDefinition(w, { view: (e.altKey ? !inDiff : inDiff) ? 'diff' : 'source' });
      return;
    }
  });

  // Clicking a line number jumps straight to the full file at that line --
  // the diff shows what changed, but reading it usually means seeing it in
  // context, not just the hunk. Clicking a symbol highlights and opens hover actions.
  diffContent.addEventListener('click', e => {
    const fileActBtn = e.target.closest('[data-file-act]');
    if (fileActBtn) {
      e.stopPropagation();
      applyFileAction(doc_(), fileActBtn.dataset.fileAct);
      return;
    }
    const hunkActBtn = e.target.closest('[data-hunk-act]');
    if (hunkActBtn) {
      e.stopPropagation();
      const head = hunkActBtn.closest('.diff-hunk-head');
      const d = doc_();
      if (head?._hunk && d) applyHunkAction(d, head._hunk, hunkActBtn.dataset.hunkAct);
      return;
    }
    const expandBtn = e.target.closest('[data-hunk-expand]');
    if (expandBtn) {
      e.stopPropagation();
      const head = expandBtn.closest('.diff-hunk-head');
      const d = doc_();
      if (head?._hunk && d) toggleHunkContext(d, head._hunk);
      return;
    }
    const hunkStepBtn = e.target.closest('[data-hunk-step]');
    if (hunkStepBtn) {
      e.stopPropagation();
      stepDiffHunk(+hunkStepBtn.dataset.hunkStep);
      return;
    }
    if (e.target.closest('[data-hunk-expand-all]')) {
      e.stopPropagation();
      toggleAllHunkContext(doc_());
      return;
    }
    const reviewStep = e.target.closest('[data-review-step]');
    if (reviewStep) {
      e.stopPropagation();
      if (!reviewStep.disabled) stepChangedFile(+reviewStep.dataset.reviewStep);
      return;
    }
    const review = e.target.closest('[data-ledger-review]');
    if (review) {
      e.stopPropagation();
      if (!review.disabled) toggleReviewed(review);
      return;
    }
    const step = e.target.closest('[data-commit-step]');
    if (step) {
      const d = doc_();
      const files = d && cachedCommitFiles(d.diffRef);
      const next = files?.[commitStepTarget(files, +step.closest('.diff-commit-strip').dataset.i, +step.dataset.commitStep)];
      if (!step.disabled && next && commitStepHandler) commitStepHandler(d, next);
      return;
    }
    if (e.target.closest('.line-btn')) return;
    const cell = e.target.closest('.diff-ln-nav');
    if (cell) {
      const rowEl = cell.closest('[data-l], [data-at]');
      if (!rowEl || !sourceJumpHandler) return;
      e.stopPropagation();
      const line = rowEl.dataset.l !== undefined ? +rowEl.dataset.l : +rowEl.dataset.at;
      sourceJumpHandler(line);
      return;
    }
    const diffCode = e.target.closest('.diff-code');
    if (diffCode && !e[MOD]) {
      const w = wordAtPoint(e.clientX, e.clientY);
      if (w) {
        S.at = w;
        S.lastWord = w.word;
        hoverAt(e.clientX, e.clientY);
      }
    }
  });

  diffContent.addEventListener('dblclick', e => {
    const w = wordAtPoint(e.clientX, e.clientY);
    if (w) { S.at = w; S.lastWord = w.word; }
  });
  $('#diff-btn')?.addEventListener('click', e => {
    e.stopPropagation();
    toggleDiff();
  });
  const menu = $('#diff-menu');
  if (menu) {
    menu.addEventListener('click', e => {
      const item = e.target.closest('[data-diff-opt]');
      if (!item) return;
      e.stopPropagation();
      setDiffMode(item.dataset.diffOpt);
      item.blur();
    });
  }
  on('tab:activated', () => syncDiffView());
  on('tabs:cleared', () => syncDiffView());
}
