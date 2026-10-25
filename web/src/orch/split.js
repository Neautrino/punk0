// web/src/orch/split.js
// The center's layout. With layout.fileView "beside" (the default) files
// open in a group of their own right of the terminals, each group with its
// own tab strip, as in VS Code's Agents window; "tabs" puts them among the
// terminal tabs. The split needs GROUP_MIN for each group beside the files
// pane: with less room it uses tabs until there is enough again, without
// touching the setting.
//
// The border between the groups keeps a fraction of the center, so resizing
// the window or a side pane keeps the proportion; CSS clamps each group to
// GROUP_MIN. Dragging the border past half of GROUP_MIN into a group
// collapses it (VS Code's snap) and dragging back brings it out; the
// collapsed group is store.js's state.collapsed, which Maximize and Hide
// panel set too. Like the side panes (resize.js), a drag writes once per
// frame and body.wb-resizing lets the terminal refit once the pointer rests.
//
// The ◫ toggle (.o-side-toggle) and Ctrl+Alt+B close everything right of
// the terminals, the open files and the files pane, and open both again.
// In Chats mode (paneviews.js) the chats take the file group's columns and
// the files pane's, and the right side shows while either has room. There
// Maximize hides the terminals and the chats take the whole center; in
// Files mode it is for the file group alone, so only while it has files
// (a maximized tree would be a narrow list stretched over empty width).
import { $, $$ } from '../state.js';
import { on } from '../bus.js';
import { state, rerender, setCollapsed, workspace, activeTab, termTab, fileTab, fileTabsOf } from './store.js';
import { paneView } from './paneviews.js';
import { getSetting } from './settings.js';
import { isHidden, setHidden } from './resize.js';

const GROUP_MIN = 320; // also in workbench.css (#o-center.files-open)
const STEP = 16; // arrow keys; Shift moves four steps
const KEY = 'punk0.split';

let center, handle, files;
let ratio = 0.5; // the terminal group's share of the two groups
let wide = true; // the center fits both groups beside the files pane
let frame = 0;

export function isSplit() {
  return wide && getSetting('layout.fileView') !== 'tabs';
}

// view is what the center shows for the selected workspace: the terminal
// tab, the file tab and whether the welcome view is up (each null or false
// when not shown), plus which split group is collapsed.
export function view() {
  const w = workspace();
  const front = activeTab();
  const split = isSplit();
  const files = split && fileTabsOf(w).length > 0;
  // Files mode collapses a group only while the file group has files;
  // Chats mode maximizes the chats over the terminals while they show: in
  // the file group's columns, or in the pane's while it is not hidden.
  const chatsMax = state.collapsed === 'term' && paneView() === 'chats' && (files || !isHidden('right'));
  const collapsed = chatsMax ? 'term' : files ? state.collapsed : '';
  if (!split) {
    const file = !collapsed && front?.kind === 'file' ? front : null;
    return { split: false, files: false, term: collapsed || file ? null : front, file, welcome: !collapsed && !front, collapsed };
  }
  const term = collapsed === 'term' ? null : termTab(w);
  return {
    split: true, files, collapsed, term,
    file: files && collapsed !== 'file' ? fileTab(w) : null,
    welcome: collapsed !== 'term' && !term,
  };
}

// layoutCenter puts view() into the center's classes and returns it.
export function layoutCenter() {
  const v = view();
  center.classList.toggle('split', v.split);
  center.classList.toggle('files-open', v.files);
  center.classList.toggle('term-off', v.collapsed === 'term');
  center.classList.toggle('file-off', v.collapsed === 'file');
  center.classList.toggle('file-shown', !!v.file);
  syncToggle(v);
  const max = $('#o-file-max');
  max.title = v.collapsed === 'term' ? 'Restore terminals' : paneView() === 'chats' ? 'Maximize chats' : 'Maximize files';
  max.setAttribute('aria-label', max.title);
  return v;
}

// rightOpen says whether everything right of the terminals shows: the open
// files (when there are any) and the files pane; in Chats mode, the chats
// in whatever of their columns has room.
const groupShown = v => v.files && v.collapsed !== 'file';
const rightOpen = v => (paneView() === 'chats' && groupShown(v)) || (!isHidden('right') && !(v.files && v.collapsed === 'file'));

function syncToggle(v) {
  for (const b of $$('.o-side-toggle')) b.setAttribute('aria-pressed', String(rightOpen(v)));
}

// toggleRight closes the right side as a whole, the open files and the
// files pane, or opens whatever of it is closed: the ◫ button and
// Ctrl+Alt+B. It is also how open files collapsed by a drag come back.
function toggleRight() {
  const open = !rightOpen(view());
  setHidden('right', !open);
  const collapsed = open ? '' : fileTabsOf(workspace()).length ? 'file' : state.collapsed;
  if (collapsed !== state.collapsed) setCollapsed(collapsed);
  else rerender();
}

// Ctrl+Alt+B (Cmd+Alt+B), as VS Code's side panel; not inside a terminal,
// which owns the key (resize.js has Ctrl+B).
function shortcut(e) {
  if (e.key.toLowerCase() !== 'b' && e.code !== 'KeyB') return;
  if (!(e.ctrlKey || e.metaKey) || !e.altKey || e.shiftKey || e.target.closest?.('#o-term')) return;
  e.preventDefault();
  toggleRight();
}

function load() {
  const r = parseFloat(localStorage.getItem(KEY));
  return r > 0 && r < 1 ? r : 0.5;
}

function save() {
  try {
    localStorage.setItem(KEY, String(ratio));
  } catch {}
}

function apply() {
  frame = 0;
  center.style.setProperty('--o-split', ratio.toFixed(5));
  handle.setAttribute('aria-valuenow', String(Math.round(ratio * 100)));
}

const schedule = () => { frame ||= requestAnimationFrame(apply); };

// groupsWidth is what the terminal and file groups share: the center less
// the files pane's column at its right (nothing while hidden). Not the
// pane's own width: in Chats mode it spans the file group's columns too.
const groupsWidth = () => center.clientWidth - (parseFloat(getComputedStyle(center).getPropertyValue('--o-files-w')) || 0);

// setPx puts the border x pixels from the center's left edge, keeping both
// groups at least GROUP_MIN wide.
function setPx(x) {
  const w = groupsWidth();
  ratio = Math.max(GROUP_MIN, Math.min(x, w - GROUP_MIN)) / w;
  schedule();
}

function drag(e) {
  if (e.button !== 0) return;
  e.preventDefault();
  handle.setPointerCapture(e.pointerId);
  handle.classList.add('active');
  center.classList.add('dragging'); // keeps the border while a group is snapped away
  document.body.classList.add('wb-resizing');
  // A snapped group comes back at the size it had before the drag, as in
  // VS Code.
  const start = ratio;
  const move = ev => {
    const w = groupsWidth();
    const x = ev.clientX - center.getBoundingClientRect().left;
    const snap = x < GROUP_MIN / 2 ? 'term' : w - x < GROUP_MIN / 2 ? 'file' : '';
    if (!snap) setPx(x);
    else if (ratio !== start) {
      ratio = start;
      schedule();
    }
    if (snap !== state.collapsed) setCollapsed(snap);
  };
  const end = () => {
    handle.removeEventListener('pointermove', move);
    handle.removeEventListener('pointerup', end);
    handle.removeEventListener('pointercancel', end);
    handle.classList.remove('active');
    center.classList.remove('dragging');
    document.body.classList.remove('wb-resizing');
    save();
  };
  handle.addEventListener('pointermove', move);
  handle.addEventListener('pointerup', end);
  handle.addEventListener('pointercancel', end);
}

function key(e) {
  const x = ratio * groupsWidth();
  const step = e.shiftKey ? STEP * 4 : STEP;
  if (e.key === 'ArrowLeft') setPx(x - step);
  else if (e.key === 'ArrowRight') setPx(x + step);
  else if (e.key === 'Home') setPx(0);
  else if (e.key === 'End') setPx(Infinity);
  else if (e.key === 'Enter') setPx(groupsWidth() / 2);
  else return;
  e.preventDefault();
  save();
}

export function initSplit() {
  center = $('#o-center');
  handle = $('#o-split-resizer');
  files = $('#o-files');
  ratio = load();
  apply();
  handle.addEventListener('pointerdown', drag);
  handle.addEventListener('keydown', key);
  handle.addEventListener('dblclick', () => {
    setPx(groupsWidth() / 2);
    save();
  });
  $('#o-file-max').addEventListener('click', () => setCollapsed(state.collapsed === 'term' ? '' : 'term'));
  for (const b of $$('.o-side-toggle')) b.addEventListener('click', toggleRight);
  addEventListener('keydown', shortcut);
  on('pane:toggled', () => syncToggle(view()));
  on('paneview:changed', rerender);
  // Only a change of layout re-renders; resizes inside one keep the DOM.
  // The files pane showing, hiding or resizing changes the groups' room too.
  const measure = () => {
    const now = groupsWidth() >= 2 * GROUP_MIN;
    if (now === wide) return;
    wide = now;
    rerender();
  };
  wide = groupsWidth() >= 2 * GROUP_MIN;
  const ro = new ResizeObserver(measure);
  ro.observe(center);
  ro.observe(files);
  on('setting:changed', k => k === 'layout.fileView' && rerender());
}
