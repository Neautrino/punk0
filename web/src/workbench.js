// web/src/workbench.js
// The orchestrator page (web/workbench.html), what a bare `punk0` opens.
// Bundled on its own into web/workbench.js by scripts/build-web.js; the
// modules it is built from live in web/src/orch/ and share only the theme,
// toast, status-bar metrics and settings modal modules with the editor app.
// The server side is workbench.go.
//
// Three panes: projects and workspaces on the left, the selected workspace's
// tabs in the middle, its files on the right. The middle holds two tab
// groups (orch/groups.js), each with its own tab bar, terminal view and
// file view, side by side or one at a time (split.js); a tab is dragged
// from one to the other (tabdrag.js).
import { $, $$, esc } from './state.js';
import { state, subscribe, reload, workspace, frontTab, groupOf, focusedGroup, projectOf, tildify, wsLabel, takePick, fileTabId, WHATSNEW_TAB } from './orch/store.js';
import { initStatusBar } from './orch/statusbar.js';
import { initSidebar } from './orch/sidebar.js';
import { initNewChat } from './orch/newchat.js';
import { initNewWorkspace } from './orch/newworkspace.js';
import { initRemoveDialog } from './orch/removedialog.js';
import { initAddProject } from './orch/addproject.js';
import { initLsp } from './orch/lsp.js';
import { initTabs } from './orch/tabs.js';
import { initTree } from './orch/tree.js';
import { initGit } from './orch/git.js';
import { initHistory } from './orch/history.js';
import { initPicker } from './orch/picker.js';
import { createFileView, initFileStatus } from './fileview.js';
import { call, pageURL } from './orch/api.js';
import { getSetting } from './orch/settings.js';
import { on } from './bus.js';
import { initChanges, showChanges, hideChanges } from './orch/changes.js';
import { initWhatsNew, showWhatsNew, hideWhatsNew } from './orch/whatsnew.js';
import { initUpdate } from './orch/update.js';
import { newTermView } from './orch/term.js';
import { initResize } from './orch/resize.js';
import { initSplit, layoutCenter, fileShown } from './orch/split.js';
import { initPaneViews, paneView, setPaneView } from './orch/paneviews.js';
import { initSettings } from './orch/settings.js';
import { initNotify } from './orch/notify.js';
import { watchSelects } from './selectbox.js';
import { mountTipBox } from './tips.js';

// Editor windows link back here by this name (main.js), so going back
// reuses this window instead of opening another orchestrator.
window.name = 'punk0-agents';

const groupEls = $$('.o-group');
const emptyPane = $('#o-empty');
const panes = []; // per group: the elements its views draw in

watchSelects(); // every <select> opens punk0's list, so before anything fills one
initResize(); // first: the panes take their widths before anything measures them
initSplit();
initPaneViews(); // before the views' modules, which ask paneView()
const pollResources = initStatusBar();
initSettings();
initAddProject(); // before the sidebar, which may open it for ?open=<url>
initSidebar();
initNewChat();
initNewWorkspace();
initRemoveDialog();
initLsp();
initTabs();
mountTipBox($('#o-empty-tip'), 'agents');
initGit();
initHistory();
initTree();
initPicker();
initFileStatus();
// One terminal view and one read-only file view per tab group, each drawing
// in its own section. The page's file status belongs to the group showing a
// file (split.js fileShown), else to the focused group, which clears it.
const statusGroup = () => fileShown()?.g ?? focusedGroup();
groupEls.forEach((el, g) => {
  panes[g] = {
    term: $('.o-term', el),
    head: $('.o-view-head', el),
    code: $('.o-code-vp', el),
    diff: $('.o-diff-vp', el),
    notes: $('.o-notes-vp', el),
    view: createFileView({
      vp: $('.o-code-vp', el), sizer: $('.o-code-sizer', el), rows: $('.o-code-rows', el),
      title: $('.o-file-title', el), meta: $('.o-file-meta', el),
    }, {
      load: (w, path, start, count) => call('GET', `${w.href}api/file?` + new URLSearchParams({ path, start: String(start), count: String(count) })),
      rawURL: (w, path) => pageURL(`${w.href}api/raw?` + new URLSearchParams({ path })),
      getSetting,
      ownsStatus: () => statusGroup() === g,
    }),
    terminal: newTermView($('.o-term', el), reload),
  };
});
// A file link in a terminal opens the file as a tab: its line is shown by
// the view of whatever group holds that tab.
on('file:reveal', ({ path, line, col }) => {
  const g = groupOf(fileTabId(path));
  panes[g < 0 ? focusedGroup() : g].view.revealLine(path, line, col);
});
initChanges();
initWhatsNew();
initNotify();
initUpdate();

// Reloads re-render everything; a group only switches what it shows when
// that changes, so a refresh never drops a terminal or an open file. A
// group's terminal stays attached while something else of that group is in
// front, or while the group is out of view: it only lets go when another
// terminal takes its place or the tab leaves the group, since attaching
// one again would start a stopped tab.
const shownFile = []; // per group: "<workspace id>\n<tab id>…" of the file it draws
const shownTerm = []; // per group: id of the terminal tab its xterm is attached to
let front = null; // id of the tab in front
let statusAt = -1; // the group whose view last wrote the file status
let selected = ''; // "<focused group>\n<tab in front>" of the last render
subscribe(() => {
  // Only the render a click caused may report its tab as picked.
  const pick = takePick();
  const w = workspace();
  const open = state.files[w?.id] || [];
  for (const p of panes) p.view.forget(w?.id || '', open);
  const v = layoutCenter();
  // No selection: "pick one" once there is anything to pick, else "add one".
  const any = state.projects.some(p => p.workspaces.length);
  emptyPane.dataset.ws = !w ? (any ? 'pick' : 'none') : w.missing ? 'missing' : 'yes';
  const p = w && projectOf(w.id);
  $('#o-empty-ws').innerHTML = w ? `<b>${esc(p && !p.scratch ? p.name + ' / ' + w.name : wsLabel(w))}</b> <span class="wb-faint">${esc(tildify(w.dir))}</span>` : '';
  const welcome = !!v.groups[0].shown && v.groups[0].welcome;
  emptyPane.hidden = !welcome;
  // Picking a workspace that opens on the welcome view puts Enter on its
  // first button, as picking one with a terminal focuses the terminal.
  if (pick && welcome && emptyPane.dataset.ws === 'yes') $('#o-empty-main')?.focus({ preventScroll: true });

  // A terminal is attached in the group whose active tab it is, in view or
  // not, and stays while a file of the same group is in front. One moved to
  // the other group leaves its old view first, so this page never gives one
  // PTY two viewers.
  const wantTerm = panes.map((_, g) => {
    const t = v.groups[g]?.tab;
    return t && t.kind !== 'file' ? t.id : null;
  });
  panes.forEach((p, g) => {
    const keep = shownTerm[g] && groupOf(shownTerm[g]) === g && (!wantTerm[g] || wantTerm[g] === shownTerm[g]);
    if (shownTerm[g] && !keep) p.terminal.attach((shownTerm[g] = null));
  });
  const was = front;
  front = frontTab()?.id || null;
  panes.forEach((p, g) => {
    // A file tab shows its text, or its diff as the Changes tab shows all
    // of them; switching between the two is a different key. A file's diff
    // keeps its details in the status bar; the Changes tab is no one file,
    // and the What's New tab no file at all.
    const tab = v.groups[g]?.shown ? v.groups[g].tab : null;
    const file = tab?.kind === 'file' ? tab : null;
    const notes = file?.id === WHATSNEW_TAB;
    const diff = !notes && !!(file?.changes || file?.diff);
    p.term.hidden = !tab || !!file;
    p.head.hidden = !file;
    p.code.hidden = !file || diff || notes;
    p.diff.hidden = !diff;
    p.notes.hidden = !notes;
    const key = file ? `${w?.id || ''}\n${file.id}\n${diff}\n${file.whatsNew || ''}` : null;
    if (key !== shownFile[g]) {
      shownFile[g] = key;
      if (notes) showWhatsNew(g, file.whatsNew);
      else hideWhatsNew(g);
      if (file && !file.changes && !notes) p.view.show(w, file.path, diff);
      else p.view.hide();
      if (!diff) hideChanges(g);
    }
    // The other group collapsed leaves this one the whole center, where a
    // diff fits side by side.
    if (diff) showChanges(g, w, v.collapsed === 1 - g, file.changes ? '' : file.path);
    if (wantTerm[g] && wantTerm[g] !== shownTerm[g]) {
      p.terminal.attach((shownTerm[g] = wantTerm[g]), { pick: pick && g === focusedGroup(), focus: g === focusedGroup() });
    } else if (wantTerm[g] && front === wantTerm[g] && was !== front) {
      p.terminal.focus();
    }
  });
  // A tab selected or dropped in the second group would sit under the
  // chats: the files side comes back so it is in view. Only a change of
  // what is in front does that, so switching to Chats by hand stays.
  const sig = `${focusedGroup()}\n${front}`;
  if (sig !== selected) {
    selected = sig;
    if (paneView() === 'chats' && focusedGroup() === 1 && v.groups[1]?.tab && !v.groups[1].inView) setPaneView('files');
  }
  // The group that owns the status may change without its own file
  // changing: then it draws the file details, or clears them.
  if (statusGroup() !== statusAt) {
    statusAt = statusGroup();
    panes[statusAt].view.claimStatus();
  }
});

// Keep tab states, editor states and the status bar current while the page
// is in view.
const tick = () => {
  if (document.hidden) return;
  reload();
  pollResources();
};
setInterval(tick, 3000);
document.addEventListener('visibilitychange', tick);
tick();
