// web/src/tabs.js
// The editor's tabs: the documents it edits, and the two groups they share
// with the terminals and the read-only files of the right side
// (editorgroups.js). The center bar #tabs shows the center group's list,
// the right side's bar its own; a tab is dragged between them (tabdrag.js)
// or moved with its context menu.
import { $, esc, S, doc_, api, CHUNK, withKeys } from './state.js';
import { emit, on } from './bus.js';
import {
  CENTER, RIGHT, activate, activeEntry, beforeFor, canReorder, canonicalTerms, closeTargets,
  entries, entryOf, groupOf, move, next as nextEntry, other, persisted, place, reconcile,
  remove as dropEntries, seed, setActive,
} from './editorgroups.js';
import { createTabDrag } from './tabdrag.js';
import { createFileView } from './fileview.js';
import { getSetting } from './settings-form.js';
import { getTabLabel } from './term-util.js';
import { agentIcon } from './agenticons.js';
import { stopTab } from './tabstop.js';
import {
  attachTerm, detachTerm, isRunning, parentURL, refreshWorkspaceTabs, removeTab,
  shownTerm, showTermSide, termSideShown, termTab, termTabs, workspaceID,
} from './term_inspector.js';
import { vp, sizer, rowsEl, editor, setStatusNote, showToast } from './ui.js';
import { render, layout, refineChunk, getLH, lineTopY } from './renderer.js';
import { updateStatus, refreshResources } from './status.js';
import { pushHistory } from './history.js';
import { warmLSP } from './lsp.js';
import { loadOutline } from './outline.js';
import { showPanel } from './panels.js';
import { revealDir, treeEl } from './tree.js';
import { clearLink } from './hover.js';
import { clearFind } from './find.js';
import { clearSelectAll } from './selbar.js';
import { syncPreview, previewing, previewLine } from './markdown.js';
import { syncDiffView, layoutPref, diffScrollTop, setDiffMode, setSourceJumpHandler, setCommitStepHandler, scrollDiffToLine } from './diff.js';
import { syncImageView } from './imageview.js';
import { syncEditUI, editTextarea, updateGutter, centerEditLine } from './editview.js';
import { scrollTabsOnWheel } from './tabwheel.js';

// Recently closed files, newest last, for Alt+Shift+T.
const closedTabs = [];
const MAX_CLOSED = 20;
let tabMenu = null;
let tabMenuId = '';

/* The editor's two groups (editorgroups.js): the center, whose active entry
   is the document being edited or a terminal dragged in, and the right
   side, which holds terminals and the files dragged to it, read only.
   S.groups is the model; sessionStorage keeps the center's terminals, the
   documents being no more persisted than before. */
const GROUPS_KEY = 'punk0.editorGroups';
const STOPPED_NOTE = 'Tabs are stopped. Click one to start it, or use + for a new one.';
const NO_TABS_NOTE = 'No terminal tabs. Click + to start an agent or a shell.';
let drag = null;
let rightFile = null; // fileview.js pane of the right side
let stored = null; // what sessionStorage held at load
let adopted = false; // its center terminal has been taken back
let moving = 0; // moves the server has not answered yet
const drawn = {}; // group -> the markup its bar holds
const fronted = {}; // group -> the tab its bar last scrolled to

const barEl = g => $(g === CENTER ? '#tabs' : '#term-inspector-tabs');
const docOf = path => S.tabs.find(t => t.path === path);
const wsOf = id => termTab(id)?.workspace || '';
/* This editor's folder, the only one its file view shows. The same object
   every time: fileview.js tells a view switch from a stale load by it. */
const folderW = { id: '' };
const folder = () => {
  folderW.id = S.meta?.root || '';
  return folderW;
};
// A group never falls back to a stopped terminal: showing one starts it.
const showable = e => e.kind !== 'term' || isRunning(e.id);

function saveGroups() {
  try { sessionStorage.setItem(GROUPS_KEY, JSON.stringify(persisted(S.groups))); } catch {}
}

function loadGroups() {
  try { return JSON.parse(sessionStorage.getItem(GROUPS_KEY) || 'null'); } catch { return null; }
}

function closeTabMenu() {
  if (tabMenu) tabMenu.hidden = true;
  tabMenuId = '';
}

// tabActions is a tab's context menu: what closing it offers (documents
// only, so a bulk action never ends a shell), where it can move, and the
// one action its own kind has.
function tabActions(id) {
  const e = entryOf(S.groups, id);
  const g = groupOf(S.groups, id);
  const items = [{ action: 'close', label: 'Close' }];
  if (e.kind === 'view') items.push({ action: 'edit', label: 'Edit' });
  if (e.kind === 'term') items.push({ action: 'stop', label: isRunning(id) ? 'Stop' : 'Stopped', disabled: !isRunning(id) });
  if (g === CENTER) {
    items.push(
      { action: 'all', label: 'Close All', disabled: !closeTargets(S.groups, 'all', id).length },
      { action: 'others', label: 'Close Others', disabled: !closeTargets(S.groups, 'others', id).length },
      { action: 'right', label: 'Close to the Right', disabled: !closeTargets(S.groups, 'right', id).length },
      { action: 'left', label: 'Close to the Left', disabled: !closeTargets(S.groups, 'left', id).length },
    );
  }
  items.push(
    { action: 'other', label: 'Move to other side' },
    { action: 'move-left', label: 'Move left', disabled: !canStep(id, -1) },
    { action: 'move-right', label: 'Move right', disabled: !canStep(id, 1) },
  );
  return items;
}

function openTabMenu(id, x, y) {
  if (!entryOf(S.groups, id)) return;
  tabMenu.replaceChildren();
  for (const { action, label, disabled } of tabActions(id)) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'sel-menu-item';
    button.dataset.tabAction = action;
    button.setAttribute('role', 'menuitem');
    button.textContent = label;
    button.disabled = !!disabled;
    tabMenu.append(button);
  }
  tabMenuId = id;
  tabMenu.hidden = false;
  const w = tabMenu.offsetWidth, h = tabMenu.offsetHeight;
  tabMenu.style.left = Math.max(4, Math.min(x, innerWidth - w - 4)) + 'px';
  tabMenu.style.top = Math.max(4, Math.min(y, innerHeight - h - 4)) + 'px';
}

// runTabAction does what the context menu asked of tab id.
function runTabAction(action, id) {
  if (!entryOf(S.groups, id)) return;
  if (action === 'edit') return void openFile(id);
  if (action === 'stop') return void stopTermTab(id);
  if (action === 'other') return moveEntry(id, other(groupOf(S.groups, id)), '');
  if (action === 'move-left') return stepTab(id, -1);
  if (action === 'move-right') return stepTab(id, 1);
  if (action === 'close') return closeTab(id);
  // Close Others brings the tab to the front, unless showing it would
  // start a stopped terminal.
  if (action === 'others' && showable(entryOf(S.groups, id))) switchTab(id);
  const docs = closeTargets(S.groups, action, id);
  if (docs.length) closeDocs(docs);
}

// canStep says whether Move left / Move right has anywhere to put the tab:
// a terminal only reorders among its own workspace's tabs.
function canStep(id, dir) {
  const g = groupOf(S.groups, id);
  const list = entries(S.groups, g);
  const i = list.findIndex(e => e.id === id);
  const at = i + (dir < 0 ? -1 : 2);
  if (i < 0 || at < 0 || at > list.length) return false;
  const before = list[at]?.id || '';
  return entryOf(S.groups, id).kind !== 'term' || canReorder(S.groups, id, g, before, wsOf);
}

// stepTab moves a tab one place along its own bar.
function stepTab(id, dir) {
  if (!canStep(id, dir)) return;
  const g = groupOf(S.groups, id);
  const list = entries(S.groups, g);
  const i = list.findIndex(e => e.id === id);
  moveEntry(id, g, list[i + (dir < 0 ? -1 : 2)]?.id || '');
}

async function stopTermTab(id) {
  const t = termTab(id);
  if (t?.running && await stopTab(parentURL(''), { id, name: getTabLabel(t) })) refreshWorkspaceTabs();
}

/* opts.ref pins the tab to one commit: every diff it shows is that commit's
   own change to the file, frozen no matter what happens in the working tree
   afterwards. The git panel's commit graph is what passes it. Opening the same
   path from anywhere else (tree, search, go-to-definition) passes no ref and so
   puts the tab back on the working tree, which is what "open this file" means
   everywhere else in px0. */
export async function openFile(path, opts = {}) {
  const { line, push = true, col, view, soft = false, ref = '', base = '' } = opts;
  const prev = doc_();
  const wantsDiff = view === 'diff';
  const wantsSource = view === 'source';

  let idx = S.tabs.findIndex(t => t.path === path);
  if (idx >= 0 && ((S.tabs[idx].diffRef || '') !== ref || (S.tabs[idx].diffBase || '') !== base)) {
    /* Same tab, different revision. Drop the cached diff and let loadGutter
       re-decide: the file may have a diff at the commit and none in the
       working tree, or the other way round, so neither diffAvailable nor
       diffMode survives the switch. */
    const t = S.tabs[idx];
    t.diffRef = ref;
    t.diffBase = base;
    t.diffText = undefined;
    t.diffHunks = undefined;
    t.diffDismissed = false;
    t.diffMode = null;
    t.openedInDiffView = true;
    await loadGutter(t);
    if (base) t.diffAvailable = true;
  }
  if (idx < 0) {
    let j;
    const start = line ? Math.max(0, Math.floor((line - 1) / CHUNK) * CHUNK) : 0;
    try {
      j = await api('/api/file', { path, start, count: CHUNK, ref });
    } catch (e) {
      setStatusNote(path + ': ' + e.message, 4000);
      return;
    }
    const isImg = !!j.image;
    const hasDiff = !isImg && (!!j.diffAvailable || !!base);

    let initialDiffMode = null;
    let initialOpenedInDiff = false;
    let initialDismissed = false;

    if (hasDiff) {
      if (wantsSource && !j.deleted) {
        initialDiffMode = null;
        initialDismissed = true;
        initialOpenedInDiff = false;
      } else {
        // Any file with a diff opens in diff mode unless source was asked for.
        initialDiffMode = layoutPref() || 'split';
        initialDismissed = false;
        initialOpenedInDiff = true;
      }
    }

    const d = {
      path, name: path.split('/').pop(), lang: isImg ? 'image' : j.lang,
      total: isImg ? 0 : j.total, maxCols: isImg ? 0 : j.maxCols,
      size: j.size, eol: j.eol || '', encoding: j.encoding || '', lines: isImg ? [] : new Array(j.total),
      chunks: new Set(isImg ? [] : [start / CHUNK]),
      pending: new Set(), refining: new Set(), scrollTop: 0, cur: line || 1,
      outline: null, gen: 0, markdown: !isImg && !!j.markdown, table: !isImg && !!j.table, isImage: isImg,
      deleted: !isImg && !!j.deleted,
      gutter: null,
      diagnostics: (!isImg && j.diagnostics) || [],
      diffMode: initialDiffMode,
      diffAvailable: hasDiff,
      diffDismissed: initialDismissed,
      openedInDiffView: initialOpenedInDiff,
      diffRef: ref,
      diffBase: base,
    };
    if (!isImg) {
      for (let i = 0; i < j.lines.length; i++) d.lines[j.start + i] = j.lines[i];
    }
    d.lsp = (!isImg && j.lsp) || { state: 'off', server: '' };
    S.tabs.push(d);
    idx = S.tabs.length - 1;
    if (!isImg && j.refine) refineChunk(d, start / CHUNK);
    if (!isImg) loadGutter(d);
  }
  if (prev && prev !== S.tabs[idx]) prev.scrollTop = vp.scrollTop;
  if (prev !== S.tabs[idx]) { clearSelectAll(); clearFind(); }
  S.active = idx;
  // The file takes the center, which a terminal may have been holding.
  placeDoc(path);
  const d = S.tabs[idx];
  if (d) {
    if (wantsSource) {
      d.diffMode = null;
      d.diffDismissed = true;
      d.openedInDiffView = false;
    } else if (wantsDiff) {
      if (d.diffAvailable) {
        d.diffMode = layoutPref() || 'split';
        d.diffDismissed = false;
        d.openedInDiffView = true;
      } else {
        d.diffMode = null;
        if (!soft) setStatusNote('No diff for ' + d.name + ' — showing source', 3000);
      }
    } else if (d.diffAvailable && !d.diffDismissed && d.diffMode === null) {
      d.diffMode = layoutPref() || 'split';
      d.openedInDiffView = true;
    }
  }

  // A diff was asked for on a file that is open for editing: show the diff.
  // The unsaved text stays in editContent for when editing resumes.
  if (d && d.isEditing && d.diffMode && wantsDiff) {
    d.isEditing = false;
    document.body.classList.remove('editing-file');
  }

  $('#empty').hidden = true;
  syncImageView();
  syncPreview();
  syncDiffView();
  if (!S.at || S.at.path !== d.path) S.at = null;
  S.lsp.state = (d.lsp && d.lsp.state) || 'off';
  S.lsp.server = (d.lsp && d.lsp.server) || '';
  S.lsp.missing = (d.lsp && d.lsp.missing) || '';
  warmLSP(d);
  drawTabs(); drawCrumbs(); layout();

  syncEditUI();
  if (line) {
    d.cur = line;
    if (d.diffMode) scrollDiffToLine(line);
    else centerLine(line);
  } else if (!d.diffMode && !d.isEditing) {
    vp.scrollTop = d.scrollTop;
  }
  render();
  updateStatus();
  if ($('#panel-outline')?.classList.contains('active')) loadOutline();
  if (push) pushHistory(path, line || d.cur);
  emit('tab:activated', { doc: d, prevDoc: prev });
}

// VS Code-style diff gutter for the normal file view. Fetches once per opened
// doc and caches on it (each tab keeps its own; switching tabs needs no clear).
// Fetches on any open in a git repo rather than threading per-file status
// through every open path — the backend returns available:false for
// clean files, so the extra request is cheap and self-limiting.
export async function loadGutter(d) {
  if (!S.meta?.git) return;
  // Same stale-answer guard as drawDiff: the doc may be pointed at a different
  // commit (or back at the working tree) before this resolves.
  const ref = d.diffRef || '';
  try {
    const j = await api('/api/gutter', { path: d.path, ref });
    if ((d.diffRef || '') !== ref) return;
    d.diffAvailable = !!j.available;
    if (j.available && d.diffMode === null && !d.diffDismissed && d.openedInDiffView) {
      d.diffMode = layoutPref() || 'split';
      if (doc_() === d) {
        syncDiffView();
        syncPreview();
      }
    }
    if (!j.available) {
      d.gutter = null;
    } else {
      const marks = new Map();
      for (const n of j.modified) marks.set(n, 'mod');
      for (const n of j.added) marks.set(n, 'add');
      d.gutter = { marks, dels: new Set(j.deleted) };
    }
    if (doc_() === d) {
      updateStatus();
      render();
    }
    drawTabs();
  } catch {}
}

// Quietly re-fetches all open tabs on workspace reindex without tab-switching thrash.
// Preserves live scroll position, cursor column/line (clamped), diff settings, and markdown scroll.
// onlyIfChanged: leave a tab's doc untouched, and skip the repaint when no tab
// changed, if the file comes back with the same size, line count and diff
// availability. Keeps a background refresh from flickering an unchanged view.
export async function reloadOpenTabs({ onlyIfChanged = false } = {}) {
  if (S.tabs.length === 0) return;

  const activeDoc = doc_();
  if (activeDoc) {
    activeDoc.scrollTop = vp.scrollTop;
    if (previewing(activeDoc)) {
      const mv = $('#mdview');
      if (mv && !mv.hidden) activeDoc.mdScroll = mv.scrollTop;
    }
  }

  const targets = S.tabs.map(t => ({
    oldDoc: t,
    path: t.path,
    anchor: t.cur || 1,
    start: t.cur ? Math.max(0, Math.floor((t.cur - 1) / CHUNK) * CHUNK) : 0,
    ref: t.diffRef || '',
  }));

  let anyChanged = false;
  const results = await Promise.allSettled(
    targets.map(tgt => api('/api/file', { path: tgt.path, start: tgt.start, count: CHUNK, ref: tgt.ref }))
  );

  for (let i = 0; i < targets.length; i++) {
    const res = results[i];
    const tgt = targets[i];
    const idx = S.tabs.indexOf(tgt.oldDoc);
    if (idx < 0) continue; // tab closed while reloading

    if (res.status !== 'fulfilled') {
      if (idx === S.active) {
        setStatusNote(tgt.path + ': ' + (res.reason?.message || 'failed to load'), 4000);
      }
      continue;
    }

    const j = res.value;
    if (j.image) {
      tgt.oldDoc.size = j.size;
      continue;
    }

    const keep = tgt.oldDoc;
    const hasDiff = !!j.diffAvailable || !!keep.diffBase;
    let linesSame = true;
    if (keep.lines && j.lines) {
      for (let k = 0; k < j.lines.length; k++) {
        if (keep.lines[tgt.start + k] !== j.lines[k]) { linesSame = false; break; }
      }
    }
    const diagCount = (j.diagnostics || []).length;
    const keepDiagCount = (keep.diagnostics || []).length;
    if (onlyIfChanged && linesSame && diagCount === keepDiagCount &&
        keep.size === j.size && keep.total === j.total &&
        !!keep.diffAvailable === hasDiff && !!keep.deleted === !!j.deleted) continue;
    anyChanged = true;
    const newCur = Math.max(1, Math.min(keep.cur || 1, j.total || 1));

    /* A reload keeps each tab in the view it was in. The file changing under
       it, say from an agent edit, is no reason to swap source for a diff, so a
       tab in source is marked dismissed and loadGutter leaves it there too. */
    const diffMode = hasDiff ? (keep.diffMode || (j.deleted ? (layoutPref() || 'split') : null)) : null;

    const d = {
      path: tgt.path,
      name: tgt.path.split('/').pop(),
      lang: j.lang,
      total: j.total,
      maxCols: j.maxCols,
      size: j.size,
      eol: j.eol || '',
      encoding: j.encoding || '',
      lines: new Array(j.total),
      chunks: new Set([tgt.start / CHUNK]),
      pending: new Set(),
      refining: new Set(),
      scrollTop: keep.scrollTop || 0,
      cur: newCur,
      col: keep.col || 0,
      outline: null,
      gen: 0,
      markdown: !!j.markdown,
      table: !!j.table,
      deleted: !!j.deleted,
      mdScroll: keep.mdScroll || 0,
      mdLine: keep.mdLine || 0,
      gutter: null,
      diagnostics: j.diagnostics || [],
      diffMode,
      diffAvailable: hasDiff,
      diffDismissed: !!keep.diffDismissed || (!keep.diffMode && !j.deleted),
      openedInDiffView: !!keep.openedInDiffView || !!keep.diffMode || !!j.deleted,
      diffScroll: keep === activeDoc && keep.diffMode ? diffScrollTop() : 0,
      prCollapsed: keep.prCollapsed,
      youCollapsed: keep.youCollapsed,
      diffRef: tgt.ref,
      diffBase: keep.diffBase || '',
      isEditing: !!keep.isEditing,
      dirty: !!keep.dirty,
      editContent: keep.editContent,
    };

    for (let k = 0; k < j.lines.length; k++) {
      d.lines[j.start + k] = j.lines[k];
    }
    d.lsp = j.lsp || { state: 'off', server: '' };

    S.tabs[idx] = d;
    if (j.refine) refineChunk(d, tgt.start / CHUNK);
  }

  if (onlyIfChanged && !anyChanged) return;

  // Load all gutters concurrently before initial paint
  await Promise.allSettled(S.tabs.filter(t => !t.isImage).map(t => loadGutter(t)));

  const d = doc_();
  if (d) {
    S.lsp.state = (d.lsp && d.lsp.state) || 'off';
    S.lsp.server = (d.lsp && d.lsp.server) || '';
    S.lsp.missing = (d.lsp && d.lsp.missing) || '';
    warmLSP(d);
    syncImageView();
    syncPreview();
    syncDiffView(true);
    layout();
    vp.scrollTop = d.scrollTop;
    render();
    if ($('#panel-outline')?.classList.contains('active')) loadOutline();
  }

  drawTabs();
  drawCrumbs();
  updateStatus();
  if (d) emit('tab:activated', { doc: d });
}

export function centerLine(n) {
  const d = doc_();
  if (d?.isEditing) {
    centerEditLine(n);
    return;
  }
  if (previewing()) { previewLine(n); return; }
  const lh = getLH();
  const y = lineTopY(n) - Math.max(0, vp.clientHeight / 2 - lh * 2);
  vp.scrollTop = Math.max(0, y);
}

// Registered with diff.js (setSourceJumpHandler): leaves diff view for the
// plain source view of the doc already open in the active tab, caret and
// scroll landing on the given working-tree line -- what a click on a diff
// line number, or the Source toggle, hands over.
export function jumpToSourceLine(line) {
  const d = doc_();
  if (!d || !line) return;
  setDiffMode('source');
  d.cur = Math.max(1, Math.min(d.total || line, line));
  centerLine(d.cur);
  render();
  updateStatus();
  pushHistory(d.path, d.cur);
}

// closeTab closes a tab by what it is: a document, a read-only file of the
// right side, or a terminal, whose shell the agents page ends.
export function closeTab(id) {
  const e = entryOf(S.groups, id);
  if (!e) return;
  if (e.kind === 'doc') closeDocs([id]);
  else if (e.kind === 'view') closeView(id);
  else removeTab(id);
}

export function closeAllTabs() {
  const docs = closeTargets(S.groups, 'all', '');
  if (docs.length) closeDocs(docs);
}

// closeView drops a file the right side was showing read only. The file
// itself is untouched: only the tab goes, and the backend drops its copy
// as it does for a closed document.
function closeView(id) {
  closeTabMenu();
  S.groups = dropEntries(S.groups, [id], { showable });
  rightFile.forget(folder().id, entries(S.groups, RIGHT).filter(e => e.kind === 'view').map(e => e.id));
  saveGroups();
  api('/api/close', { path: id }).then(() => refreshResources(), () => {});
  syncRight();
  drawTabs();
}

function closeDocs(paths) {
  const docs = paths.map(docOf).filter(Boolean);
  if (!docs.length) return;
  const dirtyTabs = docs.filter(t => t.dirty);
  if (dirtyTabs.length > 0) {
    const names = dirtyTabs.map(t => t.name).join(', ');
    if (!confirm(`You have unsaved changes in ${names}. Close anyway and discard changes?`)) {
      return;
    }
  }
  closeTabMenu();
  clearSelectAll();
  const activeDoc = doc_();
  if (activeDoc) activeDoc.scrollTop = vp.scrollTop;
  const closedEvents = [];
  const evictions = [];
  for (const closed of docs) {
    const i = S.tabs.indexOf(closed);
    S.tabs.splice(i, 1);
    closedTabs.push({ path: closed.path, cur: closed.cur, scrollTop: closed.scrollTop });
    if (closedTabs.length > MAX_CLOSED) closedTabs.shift();
    evictions.push(api('/api/close', { path: closed.path }));
    // Release large arrays to assist garbage collection
    closed.lines = null;
    closed._wrap = null;
    closed._lineCols = null;
    closed._colsChunks?.clear?.();
    closed.chunks?.clear?.();
    closed.pending?.clear?.();
    closed.refining?.clear?.();
    closed.outline = null;
    closedEvents.push({ doc: closed, index: i });
  }
  S.groups = dropEntries(S.groups, docs.map(t => t.path), { showable });
  saveGroups();
  Promise.allSettled(evictions).then(results => {
    if (results.some(result => result.status === 'fulfilled')) refreshResources();
  });
  showCenterActive();
  drawTabs();
  for (const event of closedEvents) emit('tab:closed', event);
  if (!S.tabs.length) emit('tabs:cleared');
  const d = doc_();
  if (d) emit('tab:activated', { doc: d });
}

/* showCenterActive paints what the center's active entry is: its document,
   its terminal, or the welcome view when it holds none. A stopped terminal
   is never attached by itself, so the center falls to the nearest tab it
   may show and waits for a click on the stopped one. */
function showCenterActive() {
  const a = activeEntry(S.groups, CENTER);
  if (a && !showable(a)) {
    const list = entries(S.groups, CENTER);
    const i = list.indexOf(a);
    const near = list.slice(i + 1).find(showable) || list.slice(0, i).reverse().find(showable);
    S.groups = setActive(S.groups, CENTER, near?.id || '');
  }
  const front = activeEntry(S.groups, CENTER);
  if (front?.kind === 'term') {
    // Already in front: a tab closing behind it must not reset its screen.
    if (S.center !== 'term' || shownTerm(CENTER) !== front.id) showCenterTerm(front.id, { pick: false, focus: false });
    return;
  }
  if (S.center === 'term') detachTerm(CENTER);
  S.center = 'doc';
  S.centerTerm = '';
  S.active = front?.kind === 'doc' ? S.tabs.findIndex(t => t.path === front.id) : -1;
  paintCenterDoc();
}

// paintCenterDoc draws the document in front, or the welcome view with no
// document, the way closing the last tab always has.
function paintCenterDoc() {
  syncCenterPanes();
  const d = doc_();
  syncImageView();
  syncPreview();
  syncDiffView();
  syncEditUI();
  if (!d) {
    rowsEl.innerHTML = ''; sizer.style.height = '0px';
    $('#empty').hidden = false; drawCrumbs(); updateStatus();
    return;
  }
  $('#empty').hidden = true;
  if (d.isEditing && editTextarea) {
    editTextarea.value = d.editContent || '';
    updateGutter();
  }
  drawCrumbs(); layout();
  vp.scrollTop = d.scrollTop; render(); updateStatus();
}

// syncCenterPanes swaps the document view for the center's terminal pane.
function syncCenterPanes() {
  const term = S.center === 'term';
  editor.hidden = term;
  $('#center-term').hidden = !term;
}

// Reopens the most recently closed file that is not open already, where it was left.
export async function reopenClosedTab() {
  while (closedTabs.length) {
    const t = closedTabs.pop();
    if (S.tabs.some(d => d.path === t.path)) continue;
    await openFile(t.path, { line: t.cur });
    if (doc_()?.path !== t.path) return;
    vp.scrollTop = t.scrollTop;
    render(); updateStatus();
    return;
  }
}

// Registered with diff.js: the commit strip's ‹ › open the next file of the
// commit and close the one stepped away from, so stepping swaps a tab in place
// rather than leaving one open per file. True when the file opened.
async function stepCommitFile(from, file) {
  const ref = from.diffRef || '';
  const base = from.diffBase || '';
  await openFile(file.path, { ref, base, view: 'diff' });
  if (doc_()?.path !== file.path) return false;
  if (from !== doc_() && (from.diffRef || '') === ref) closeTab(from.path);
  return true;
}

// tabHTML draws one entry of a bar: a document as it always looked, a
// terminal with its status dot and agent icon, or a file the right side
// shows read only.
function tabHTML(e, active, several) {
  const cls = extra => 'class="tab' + (active ? ' active' : '') + extra + '"';
  const close = '<span class="x" title="' + withKeys('Close tab ({Alt+W})') +
    '"><svg viewBox="0 0 10 10" aria-hidden="true"><path d="M2 2l6 6M8 2l-6 6"/></svg></span>';
  if (e.kind === 'doc') {
    const t = docOf(e.id);
    if (!t) return '';
    return '<div ' + cls((t.isImage ? ' tab-image' : '') + (t.deleted ? ' tab-deleted' : '') + (t.dirty ? ' tab-dirty' : '')) +
      ' data-id="' + esc(e.id) + '" title="' + esc(t.path) + (t.diffRef ? ' — in commit ' + esc(t.diffRef.slice(0, 7)) : '') + '">' +
      (t.isImage ? '<svg class="tab-icon" viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.4"><rect x="2" y="2" width="12" height="12" rx="2"/><circle cx="5.5" cy="5.5" r="1.5"/><path d="M14 10l-3.5-3.5L3 14"/></svg>' : '') +
      '<span class="tn">' + esc(t.name) + '</span>' +
      // Pinned to a commit: the tab is not the working tree, and it should say so.
      (t.diffRef ? '<span class="tab-ref">@' + esc(t.diffRef.slice(0, 7)) + '</span>' : '') +
      (t.dirty ? '<span class="tab-dirty-dot" title="Unsaved changes">●</span>' : '') +
      close + '</div>';
  }
  if (e.kind === 'term') {
    const t = termTab(e.id);
    if (!t) return '';
    const busy = t.status === 'working' || t.status === 'waiting';
    return '<div ' + cls(' tab-term') + ' data-id="' + esc(e.id) + '" title="' +
      esc(getTabLabel(t)) + (t.running ? '' : ' (stopped: click to start it)') + '">' +
      '<span class="term-status-dot ' + (!t.running ? 'off' : busy ? 'busy' : 'idle') + '"></span>' +
      agentIcon(t.kind, 'term-tab-kind') +
      // A folder's tabs may belong to several workspaces at it: then each
      // tab says whose it is.
      '<span class="tn">' + esc((several && t.workspaceName ? t.workspaceName + ' · ' : '') + getTabLabel(t)) + '</span>' +
      close + '</div>';
  }
  return '<div ' + cls(' tab-view') + ' data-id="' + esc(e.id) + '" title="' + esc(e.id) + ' — read only, Edit opens it in the editor">' +
    '<span class="tn">' + esc(e.id.split('/').pop()) + '</span>' +
    '<span class="tab-ref">read only</span>' + close + '</div>';
}

/* drawTabs redraws a bar only when its markup changed: the 3 s poll calls
   it, and rebuilding a bar on every tick would snap its sideways scroll
   back, drop a click mid-rebuild and take the ghost's source away from a
   drag. The tab in front is scrolled into view only when it changes. */
export function drawTabs() {
  if (drag?.active) return;
  const several = new Set(termTabs().map(t => t.workspace)).size > 1;
  for (const g of [CENTER, RIGHT]) {
    const el = barEl(g);
    if (!el) continue;
    const html = entries(S.groups, g).map(e => tabHTML(e, e.id === S.groups.active[g], several)).join('');
    if (html !== drawn[g]) {
      drawn[g] = html;
      el.innerHTML = html;
    }
    if (S.groups.active[g] !== fronted[g]) {
      fronted[g] = S.groups.active[g];
      el.querySelector('.tab.active')?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    }
  }
}

// switchTab shows the tab id in the group holding it and focuses that
// group: a document or a terminal in the center, a terminal or a read-only
// file on the right side.
export function switchTab(id) {
  const e = entryOf(S.groups, id);
  if (!e) return;
  S.groups = activate(S.groups, id);
  saveGroups();
  if (e.kind === 'doc') selectDoc(id);
  else if (e.kind === 'term') selectTerm(id);
  else selectView();
  drawTabs();
}

function selectDoc(path) {
  const i = S.tabs.findIndex(t => t.path === path);
  if (i < 0) return;
  const prev = doc_();
  if (prev === S.tabs[i]) return;
  clearLink();
  if (prev) {
    prev.scrollTop = vp.scrollTop;
    if (prev.isEditing && editTextarea) {
      prev.editContent = editTextarea.value;
    }
  }
  if (S.center === 'term') detachTerm(CENTER);
  S.center = 'doc';
  S.centerTerm = '';
  S.active = i;
  const curDoc = S.tabs[i];
  syncCenterPanes();
  syncImageView();
  syncPreview();
  syncDiffView();
  syncEditUI();
  if (curDoc.isEditing && editTextarea) {
    editTextarea.value = curDoc.editContent || '';
    updateGutter();
  }
  clearFind();
  clearSelectAll();
  S.at = null;
  S.lsp.state = (curDoc.lsp && curDoc.lsp.state) || 'off';
  S.lsp.server = (curDoc.lsp && curDoc.lsp.server) || '';
  S.lsp.missing = (curDoc.lsp && curDoc.lsp.missing) || '';
  warmLSP(curDoc);
  $('#empty').hidden = true;
  drawCrumbs(); layout();
  vp.scrollTop = curDoc.scrollTop;
  render(); updateStatus();
  if ($('#panel-outline')?.classList.contains('active')) loadOutline();
  pushHistory(curDoc.path, curDoc.cur);
  emit('tab:activated', { doc: curDoc, prevDoc: prev });
}

// selectTerm shows a terminal in the group holding it. A click on a
// stopped tab starts it, which is the user's choice; nothing else attaches
// one (pick tells punk0 this is the tab to reopen).
function selectTerm(id, { pick = true, focus = true } = {}) {
  if (groupOf(S.groups, id) === CENTER) {
    showCenterTerm(id, { pick, focus });
    return;
  }
  showTermSide();
  showRightPane('term');
  attachTerm(RIGHT, id, { pick, focus });
}

// showCenterTerm puts a terminal in the document view's place: there is no
// document in front then (doc_() is null), so the overlays, the status bar
// and every module reading the open file take their no-document path.
function showCenterTerm(id, { pick = false, focus = false } = {}) {
  const prev = doc_();
  if (prev) {
    prev.scrollTop = vp.scrollTop;
    if (prev.isEditing && editTextarea) prev.editContent = editTextarea.value;
  }
  clearLink();
  clearFind();
  clearSelectAll();
  S.at = null;
  S.center = 'term';
  S.centerTerm = id;
  syncImageView();
  syncPreview();
  syncDiffView();
  syncEditUI();
  syncCenterPanes();
  updateStatus();
  attachTerm(CENTER, id, { pick, focus });
}

// selectView shows a file the right side holds, read only. Its Edit button
// (and the menu's) opens it in the center.
function selectView() {
  showTermSide();
  syncRight();
}

// showRightPane puts the right side on its terminal or on its read-only
// file view, whichever its active tab is.
function showRightPane(which) {
  $('#editor-term-container').hidden = which !== 'term';
  $('#right-file').hidden = which !== 'file';
}

/* syncRight shows what the right side's active entry is. A file already in
   front is left alone: redrawing it would wipe a selection and snap its
   scroll. Attaching a terminal starts a stopped tab (handleTerm calls
   Shell()) and a hidden pane shows nothing, so the side only ever attaches
   one while its terminal is in front; a load or a poll never picks a tab
   the user has not. */
function syncRight() {
  const a = activeEntry(S.groups, RIGHT);
  if (a?.kind === 'view') {
    showRightPane('file');
    detachTerm(RIGHT);
    if (rightFile.path() !== a.id) rightFile.show(folder(), a.id);
    return;
  }
  showRightPane('term');
  if (!termSideShown()) return;
  if (a?.kind === 'term' && (shownTerm(RIGHT) === a.id || isRunning(a.id))) {
    if (shownTerm(RIGHT) !== a.id) attachTerm(RIGHT, a.id, { pick: false, focus: false });
    return;
  }
  const run = entries(S.groups, RIGHT).find(e => e.kind === 'term' && isRunning(e.id));
  if (run) {
    S.groups = setActive(S.groups, RIGHT, run.id);
    attachTerm(RIGHT, run.id, { pick: false, focus: false });
    return;
  }
  S.groups = setActive(S.groups, RIGHT, '');
  // Until the first poll answers, which tabs exist is unknown: saying
  // there are none would only be taken back a moment later.
  const note = entries(S.groups, RIGHT).some(e => e.kind === 'term') ? STOPPED_NOTE : NO_TABS_NOTE;
  detachTerm(RIGHT, adopted ? note : '');
}

// placeDoc makes path the center's document tab, taking it back from the
// right side if it was dragged there, and leaves it in front.
function placeDoc(path) {
  const was = entryOf(S.groups, path);
  if (was?.kind !== 'doc' || groupOf(S.groups, path) !== CENTER) {
    S.groups = place(S.groups, { kind: 'doc', id: path }, CENTER);
  }
  if (S.center === 'term') detachTerm(CENTER);
  S.center = 'doc';
  S.centerTerm = '';
  S.groups = activate(S.groups, path);
  saveGroups();
  syncCenterPanes();
  if (was?.kind === 'view') {
    rightFile.forget(folder().id, entries(S.groups, RIGHT).filter(e => e.kind === 'view').map(e => e.id));
    syncRight();
  }
}

/* moveEntry is what a drop or a Move menu item does: the tab lands in
   group to, ahead of before ('' = last), and is shown there. The server
   holds the terminal order (the center's terminals, then the right
   side's), and only a user move posts it: the poll's reconcile never
   does, so two pages cannot push an order back and forth. */
function moveEntry(id, to, before = '') {
  const e = entryOf(S.groups, id);
  if (!e || id === before) return;
  if (e.kind === 'doc' && to === RIGHT) return sendDocToSide(id, before);
  if (e.kind === 'view' && to === CENTER) return void editView(id, before);
  const from = groupOf(S.groups, id);
  const was = canonicalTerms(S.groups).join();
  S.groups = move(S.groups, id, to, before, { showable });
  if (e.kind === 'term' && from !== to && shownTerm(from) === id) detachTerm(from);
  saveGroups();
  if (from === CENTER && to !== CENTER) showCenterActive();
  switchTab(id);
  if (from !== to) syncRight();
  if (e.kind === 'term' && canonicalTerms(S.groups).join() !== was) postMove(id);
  drawTabs();
}

// editView opens a read-only file of the right side in the center, where
// it was dropped: openFile appends it, so the drop's place comes after.
async function editView(id, before) {
  await openFile(id);
  if (!before || groupOf(S.groups, id) !== CENTER || !entryOf(S.groups, before)) return;
  S.groups = activate(place(S.groups, { kind: 'doc', id }, CENTER, before), id);
  saveGroups();
  drawTabs();
}

// A document dragged to the right side becomes a read-only view of the
// same file: one path is never both.
function sendDocToSide(id, before) {
  const d = docOf(id);
  closeDocs([id]);
  if (S.tabs.includes(d)) return; // the unsaved-changes question was refused
  S.groups = place(S.groups, { kind: 'view', id }, RIGHT, before);
  switchTab(id);
}

/* postMove tells the agents page the new order: the moved terminal goes
   ahead of the next one of its own workspace, or last. A poll answered
   from before the move would put the bar back for a moment, so the lists
   keep the user's order until the server has taken it. */
async function postMove(id) {
  moving++;
  try {
    const res = await fetch(parentURL(`api/tabs/${encodeURIComponent(id)}/move`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ before: beforeFor(S.groups, id, wsOf) }),
    });
    if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || 'Could not move the tab');
  } catch (err) {
    showToast('!', err.message);
  } finally {
    moving--;
    refreshWorkspaceTabs(); // taken or refused, the bars show the server's order
  }
}

// initGroups builds the two groups, their bars and the drag between them.
function initGroups() {
  stored = loadGroups();
  S.groups = seed(stored);
  rightFile = createFileView({
    vp: $('#right-file-vp'), sizer: $('#right-file-sizer'), rows: $('#right-file-rows'),
    title: $('#right-file-title'), meta: $('#right-file-meta'),
  }, {
    load: (w, path, start, count) => api('/api/file', { path, start, count })
      .then(data => ({ ok: true, data }), e => ({ ok: false, data: { error: e.message } })),
    rawURL: (w, path) => new URL('api/raw?path=' + encodeURIComponent(path), document.baseURI).href,
    getSetting,
    // The editor's status bar and Go to line stay the center document's.
    ownsStatus: () => false,
  });
  $('#right-file-edit').addEventListener('click', () => {
    const a = activeEntry(S.groups, RIGHT);
    if (a?.kind === 'view') openFile(a.id);
  });
  drag = createTabDrag({
    tabSel: '[data-id]',
    idOf: el => el.dataset.id,
    canDrop: ({ id, to, before, kind }) => {
      const e = entryOf(S.groups, id);
      if (!e) return false;
      if (e.kind !== 'term') return true;
      if (!workspaceID()) return false;
      // Moving to the other group is always allowed; a reorder is only
      // possible among the tabs of the terminal's own workspace, since the
      // server orders each workspace's tabs on its own.
      return to !== groupOf(S.groups, id) || canReorder(S.groups, id, to, kind === 'bar' ? before : '', wsOf);
    },
    onDrop: ({ id, to, before, kind }) => moveEntry(id, to, kind === 'bar' ? before : ''),
  });
  for (const g of [CENTER, RIGHT]) {
    const el = barEl(g);
    scrollTabsOnWheel(el);
    drag.bar(el, g);
    bindBar(el);
  }
  drag.body(editor, CENTER);
  drag.body($('#center-term'), CENTER);
  drag.body($('#right-side'), RIGHT);
  drag.side($('#center-side-zone'), RIGHT);
  on('termtabs:changed', onTermTabs);
}

function bindBar(el) {
  el.addEventListener('click', e => {
    closeTabMenu();
    const t = e.target.closest('[data-id]');
    if (!t) return;
    if (e.target.closest('.x')) closeTab(t.dataset.id);
    else switchTab(t.dataset.id);
  });
  el.addEventListener('auxclick', e => {
    const t = e.target.closest('[data-id]');
    if (t && e.button === 1) { e.preventDefault(); closeTab(t.dataset.id); }
  });
  el.addEventListener('contextmenu', e => {
    const t = e.target.closest('[data-id]');
    if (!t) { closeTabMenu(); return; }
    e.preventDefault();
    openTabMenu(t.dataset.id, e.clientX, e.clientY);
  });
}

// onTermTabs follows the 3 s poll (term_inspector.js): the groups take the
// terminals that exist, in the server's order, and each shows what its
// active entry says.
function onTermTabs() {
  if (!moving) S.groups = reconcile(S.groups, termTabs().map(t => t.id), { showable });
  if (!adopted) {
    adopted = true;
    adoptStored();
  }
  if (S.center === 'term' && groupOf(S.groups, S.centerTerm) !== CENTER) showCenterActive();
  syncRight();
  saveGroups();
  drawTabs();
}

// adoptStored shows the terminal the center held when the page was left,
// once the first poll says which tabs exist: only while it runs, and only
// while the center has nothing else in front.
function adoptStored() {
  const id = stored?.active;
  if (!id || groupOf(S.groups, id) !== CENTER || !isRunning(id) || S.groups.active.center) return;
  S.groups = setActive(S.groups, CENTER, id);
  showCenterTerm(id, { pick: false, focus: false });
}

export const centerTabCount = () => entries(S.groups, CENTER).length;

/* stepCenterTab shows the next tab of the center bar, terminals included,
   dir -1 for the one before: Ctrl+Tab and vim's gt. It skips what the
   center cannot show: a stopped terminal starts when it is shown, which
   only a click asks for. */
export function stepCenterTab(dir) {
  const from = S.groups.active.center;
  let id = from;
  for (let n = entries(S.groups, CENTER).length; n > 0; n--) {
    id = nextEntry(S.groups, CENTER, id, dir);
    if (!id || id === from) return;
    if (showable(entryOf(S.groups, id))) return switchTab(id);
  }
}

// centerTabAt shows the nth tab of the center bar (Alt+1…9, vim's {n}gt).
export function centerTabAt(n) {
  const e = entries(S.groups, CENTER)[n];
  if (e && showable(e)) switchTab(e.id);
}

// closeCenterTab closes the document in front, as Alt+W and the palette
// ask; with a terminal there it does nothing, as with no document.
export function closeCenterTab() {
  const e = activeEntry(S.groups, CENTER);
  if (e?.kind === 'doc') closeDocs([e.id]);
}

export function drawCrumbs() {
  const el = $('#crumbs');
  if (el) el.innerHTML = '';
}

export function showImage(path) {
  openFile(path);
}

export function hideImage() {
  const b = $('#imgview');
  if (b) b.hidden = true;
}

export function initTabs() {
  setSourceJumpHandler(jumpToSourceLine);
  setCommitStepHandler(stepCommitFile);
  tabMenu = document.createElement('div');
  tabMenu.id = 'tab-menu';
  tabMenu.setAttribute('role', 'menu');
  tabMenu.hidden = true;
  document.body.append(tabMenu);
  tabMenu.addEventListener('click', e => {
    const button = e.target.closest('[data-tab-action]');
    if (!button || button.disabled) return;
    const id = tabMenuId;
    closeTabMenu();
    runTabAction(button.dataset.tabAction, id);
  });
  initGroups();
  document.addEventListener('mousedown', e => {
    if (tabMenu && !tabMenu.hidden && !tabMenu.contains(e.target)) closeTabMenu();
  }, true);
  addEventListener('keydown', e => { if (e.key === 'Escape') closeTabMenu(); });
  addEventListener('resize', closeTabMenu);
  addEventListener('blur', closeTabMenu);
  document.addEventListener('scroll', closeTabMenu, true);
  const crumbsEl = $('#crumbs');
  if (crumbsEl) {
    crumbsEl.addEventListener('click', e => {
      const c = e.target.closest('[data-dir]');
      if (c) { showPanel('files'); revealDir(c.dataset.dir); }
    });
  }
  window.addEventListener('beforeunload', e => {
    if (S.tabs.some(t => t.dirty)) {
      e.preventDefault();
      e.returnValue = '';
    }
  });
}
