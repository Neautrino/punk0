// web/src/orch/split.js
// The center's layout. The two tab groups (store.js, orch/groups.js) show
// side by side, each with its own tab bar and view, as in VS Code's Agents
// window; with layout.fileView "tabs" files open among the terminal tabs,
// so one group holds everything. The split needs GROUP_MIN for each group
// beside the files pane: with less room one group shows at a time, its bar
// listing both groups' tabs, until there is enough again, without touching
// the setting or where a tab belongs.
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
// the first group, the second group and the files pane, and open both
// again. In Chats mode (paneviews.js) the chats take the second group's
// column and the files pane's, and the right side shows while either has
// room. There Maximize hides the first group and the chats take the whole
// center; in Files mode it is for the second group alone, so only while
// there is one (a maximized tree would be a narrow list stretched over
// empty width).
import { $, $$ } from '../state.js';
import { on } from '../bus.js';
import { rerender, setCollapsed, collapsedGroup, workspace, groupsOf, tabIn, focusedGroup } from './store.js';
import { paneView } from './paneviews.js';
import { getSetting } from './settings.js';
import { isHidden, setHidden } from './resize.js';

const GROUP_MIN = 320; // also in workbench.css (#o-center.g2-open)
const STEP = 16; // arrow keys; Shift moves four steps
const KEY = 'punk0.split';

let center, handle, files, groupEls = [];
let ratio = 0.5; // the first group's share of the two
let wide = true; // the center fits both groups beside the files pane
let frame = 0;

export function isSplit() {
  return wide && getSetting('layout.fileView') !== 'tabs';
}

// view is what the center shows for the selected workspace: whether the
// groups stand side by side, which one is collapsed, and per group whether
// it shows, the tab it shows (null while this page opened none of its tabs)
// and whether the welcome view is up.
export function view() {
  const w = workspace();
  const s = groupsOf(w);
  const split = isSplit() && s.groups.length === 2;
  // Files mode collapses a group only in the split layout; Chats mode
  // maximizes the chats over the first group while they show: in the second
  // group's column, or in the pane's while it is not hidden. With no
  // workspace the second group can only hold the What's New tab, and there
  // are no terminals to show beside it.
  const chatsMax = collapsedGroup() === 0 && paneView() === 'chats' && (split || !isHidden('right'));
  const collapsed = chatsMax || (!w && split) ? 0 : split ? collapsedGroup() : -1;
  // Without the split one group shows at a time: the focused one, or the
  // first while this page has opened none of its tabs (the welcome view).
  const solo = tabIn(s.focus, w) ? s.focus : 0;
  const groups = s.groups.map((g, i) => ({
    shown: collapsed !== i && (split || solo === i),
    tab: tabIn(i, w),
    welcome: i === 0 && !tabIn(0, w),
  }));
  return { split, collapsed, groups };
}

// fileShown is the file tab the page follows: the one in view in the
// focused group, else in the other group; null with none. Its group's view
// writes the status bar, the tree marks its path, and the path row's
// source / diff switch is for it.
export function fileShown() {
  const v = view();
  const first = focusedGroup();
  for (const g of [first, 1 - first]) {
    const e = v.groups[g];
    if (e?.shown && e.tab?.kind === 'file') return { g, tab: e.tab };
  }
  return null;
}

// layoutCenter puts view() into the center's and the groups' classes and
// returns it. Only the leftmost group in view carries the projects pane's
// show button, only the rightmost the right side's buttons.
export function layoutCenter() {
  const v = view();
  // The chats take the second group's column while both groups stand side
  // by side, so only the first can be in view then; maximized they leave
  // none, and its head stays beside them for the mode switch and Restore.
  const chats = paneView() === 'chats';
  const inView = v.groups.map((g, i) => g.shown && !(chats && i === 1 && (v.split || v.collapsed === 0)));
  const shown = inView.flatMap((x, i) => (x ? [i] : []));
  const actsOnly = chats && !shown.length;
  center.classList.toggle('g2-open', v.split);
  center.classList.toggle('g0-off', v.collapsed === 0);
  center.classList.toggle('g1-off', v.collapsed === 1);
  center.classList.toggle('acts-only', actsOnly);
  groupEls.forEach((el, i) => {
    el.hidden = !inView[i] && !(actsOnly && i === 0);
    el.classList.toggle('o-head-only', actsOnly && i === 0);
    el.classList.toggle('o-leftmost', i === (shown[0] ?? 0));
    el.classList.toggle('o-rightmost', i === (shown[shown.length - 1] ?? 0));
    if (v.groups[i]) v.groups[i].inView = !!inView[i];
  });
  syncToggle(v);
  for (const max of $$('.o-file-max')) {
    max.title = v.collapsed === 0 ? 'Restore the first group' : chats ? 'Maximize chats' : 'Maximize this group';
    max.setAttribute('aria-label', max.title);
  }
  return v;
}

// rightOpen says whether everything right of the first group shows: the
// second group (when there is one) and the files pane; in Chats mode, the
// chats in whatever of their columns has room.
const groupShown = v => v.split && v.collapsed !== 1;
const rightOpen = v => (paneView() === 'chats' && groupShown(v)) || (!isHidden('right') && !(v.split && v.collapsed === 1));

function syncToggle(v) {
  for (const b of $$('.o-side-toggle')) b.setAttribute('aria-pressed', String(rightOpen(v)));
}

// toggleRight closes the right side as a whole, the second group and the
// files pane, or opens whatever of it is closed: the ◫ button and
// Ctrl+Alt+B. It is also how a group collapsed by a drag comes back.
function toggleRight() {
  const open = !rightOpen(view());
  setHidden('right', !open);
  const collapsed = open ? -1 : groupsOf().groups.length > 1 ? 1 : collapsedGroup();
  if (collapsed !== collapsedGroup()) setCollapsed(collapsed);
  else rerender();
}

// Ctrl+Alt+B (Cmd+Alt+B), as VS Code's side panel; not inside a terminal,
// which owns the key (resize.js has Ctrl+B).
function shortcut(e) {
  if (e.key.toLowerCase() !== 'b' && e.code !== 'KeyB') return;
  if (!(e.ctrlKey || e.metaKey) || !e.altKey || e.shiftKey || e.target.closest?.('.o-term')) return;
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

// groupsWidth is what the two groups share: the center less the files
// pane's column at its right (nothing while hidden). Not the pane's own
// width: in Chats mode it spans the second group's column too.
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
    const snap = x < GROUP_MIN / 2 ? 0 : w - x < GROUP_MIN / 2 ? 1 : -1;
    if (snap < 0) setPx(x);
    else if (ratio !== start) {
      ratio = start;
      schedule();
    }
    if (snap !== collapsedGroup()) setCollapsed(snap);
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
  groupEls = $$('.o-group');
  ratio = load();
  apply();
  handle.addEventListener('pointerdown', drag);
  handle.addEventListener('keydown', key);
  handle.addEventListener('dblclick', () => {
    setPx(groupsWidth() / 2);
    save();
  });
  for (const b of $$('.o-file-max')) b.addEventListener('click', () => setCollapsed(collapsedGroup() === 0 ? -1 : 0));
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
