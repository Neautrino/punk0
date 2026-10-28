// web/src/orch/statusbar.js
// The status bar under the three panes. Left: version, then help (the
// keyboard shortcuts sheet, whose foot opens the logs folder and copies
// diagnostics), GitHub and settings (settings.js). Middle: the
// agents needing you and working, each a button to the next such tab; the
// open file's details are viewer.js's. Right: the resource button and panel
// (resources.js, respanel.js).
import { $, esc, keyCaps } from '../state.js';
import { showToast, copyToClipboard } from '../ui.js';
import { call, failed } from './api.js';
import { drawAgentStates } from '../statusparts.js';
import { setTheme, getStoredTheme, chooseThemePreference, currentTheme } from '../theme.js';
import { state, subscribe, termTab, selectWorkspace, selectTab } from './store.js';
import { initResources, refreshResources } from './resources.js';

// The agents page's own keys; the editor's live in shortcuts.js.
const SHORTCUTS = [
  [['Mod+,'], 'Settings (outside a terminal)'],
  [['Mod+B'], 'Show / hide the projects pane (outside a terminal)'],
  [['Mod+Alt+B'], 'Close / open the open files and the files pane (outside a terminal)'],
  [['Mod+G'], 'Go to line in the open file (outside a terminal)'],
  [['Left', 'Right'], 'Move a focused pane border 16 px'],
  [['Shift+Left', 'Shift+Right'], 'Move a focused pane border 64 px'],
  [['Home', 'End'], 'Pane border to its narrowest / widest'],
  [['Enter'], 'Pane border back to 260 px'],
  [['Tab'], 'Complete a folder path'],
  [['Up', 'Down'], 'Choose a folder or file in a list'],
  [['Esc'], 'Clear the search, close a menu or panel'],
];

const helpSheet = $('#helpsheet');

function showHelp() {
  const ver = state.wb.version ? ` <span class="help-version">v${esc(state.wb.version)}</span>` : '';
  helpSheet.innerHTML = '<div class="help-card"><div class="help-header"><h2>Keyboard Shortcuts</h2>' + ver +
    '</div><dl class="help-grid">' +
    SHORTCUTS.map(([combos, v]) => '<dt>' + combos.map(keyCaps).join('<span class="key-or">/</span>') + '</dt>' +
      '<dd>' + esc(v) + '</dd>').join('') + '</dl>' + legacyNotice() +
    '<div class="help-foot"><button type="button" class="wb-btn" id="help-logs">Open logs folder</button>' +
    '<button type="button" class="wb-btn" id="help-diag">Copy diagnostics</button>' +
    '<span class="help-foot-note">The log never holds terminal output or prompts.</span></div></div>';
  $('#help-logs').addEventListener('click', openLogs);
  $('#help-diag').addEventListener('click', copyDiagnostics);
  $('#help-migrate')?.addEventListener('click', e =>
    copyToClipboard(MIGRATE_CMD, 'Command copied', e.currentTarget));
  helpSheet.hidden = false;
}

const MIGRATE_CMD = 'punk0 migrate';

// legacyNotice offers the move to the standard folders to installs still
// running out of ~/.punk0; migrating needs punk0 stopped, so it is a command
// to copy, not a button.
function legacyNotice() {
  if (!state.wb.folders?.legacy) return '';
  return '<p class="help-note">punk0 keeps its files in <code>~/.punk0</code>. To use the standard folders, ' +
    'stop punk0 and run <code>' + MIGRATE_CMD + '</code>.' +
    '<button type="button" class="wb-btn" id="help-migrate">Copy command</button></p>';
}

async function openLogs() {
  const res = await call('POST', 'api/logs/open');
  if (!res.ok) return failed(res);
  showToast('✓', 'Opened ' + res.data.dir, 3000);
}

// copyDiagnostics puts what a bug report needs on the clipboard: versions,
// paths, workspace states and the end of the log.
async function copyDiagnostics() {
  const res = await call('GET', 'api/diagnostics');
  if (!res.ok) return failed(res);
  try {
    await navigator.clipboard.writeText(res.data.text);
    showToast('✓', 'Diagnostics copied: paste them into the issue', 3000);
  } catch {
    showToast('!', 'The browser would not let punk0 write to the clipboard; the log is ' + res.data.log, 6000);
  }
}

// agentTabs lists the running tabs whose agent reports status, in sidebar
// order: what the needs-you and working buttons count and step through.
function agentTabs(status) {
  return state.projects.flatMap(p => p.workspaces).filter(w => !w.archived)
    .flatMap(w => w.tabs.filter(t => t.running && t.status === status).map(t => ({ w, t })));
}

// nextAgent shows the tab after the selected workspace's terminal tab with
// that status, so clicking again walks through all of them.
function nextAgent(status) {
  const list = agentTabs(status);
  if (!list.length) return;
  const at = list.findIndex(({ w, t }) => w.id === state.ws && t.id === termTab()?.id);
  const { w, t } = list[(at + 1) % list.length];
  if (w.id !== state.ws) selectWorkspace(w.id);
  selectTab(t.id);
}

function renderStatus() {
  const { version } = state.wb;
  $('#o-empty-ver').textContent = version ? 'v' + version : '';
  $('#st-ver').textContent = version ? 'v' + version : '';
  drawAgentStates(agentTabs('waiting').length, agentTabs('working').length);
}

export function initStatusBar() {
  // The server rendered the configured theme; a theme picked in this browser
  // wins the same way it does in the app (theme-preference.js).
  setTheme(chooseThemePreference(getStoredTheme(), currentTheme()), false);

  $('#btn-help').addEventListener('click', showHelp);
  $('#st-waiting').addEventListener('click', () => nextAgent('waiting'));
  $('#st-working').addEventListener('click', () => nextAgent('working'));
  helpSheet.addEventListener('click', e => {
    if (!e.target.closest('.help-foot')) helpSheet.hidden = true;
  });
  addEventListener('keydown', e => {
    if (e.key === 'Escape' && !helpSheet.hidden) helpSheet.hidden = true;
  });

  initResources();
  subscribe(renderStatus);
  return refreshResources;
}
