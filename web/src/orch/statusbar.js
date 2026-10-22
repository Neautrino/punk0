// web/src/orch/statusbar.js
// The status bar under the three panes. Left: version, then help (the
// keyboard shortcuts sheet), GitHub and settings (settings.js). Middle: the
// agents needing you and working, each a button to the next such tab; the
// open file's details are viewer.js's. Right: one button with the running
// terminals, running agents and total RAM, which opens the resource panel
// (resources.js).
import { $, esc, keyCaps } from '../state.js';
import { setTheme, getStoredTheme, chooseThemePreference, currentTheme } from '../theme.js';
import { state, subscribe, activeTab, selectWorkspace, selectTab } from './store.js';
import { initResources, refreshResources } from './resources.js';

// The agents page's own keys; the editor's live in shortcuts.js.
const SHORTCUTS = [
  [['Mod+,'], 'Settings (outside a terminal)'],
  [['Mod+B'], 'Show / hide the projects pane (outside a terminal)'],
  [['Mod+Alt+B'], 'Show / hide the files pane (outside a terminal)'],
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
      '<dd>' + esc(v) + '</dd>').join('') + '</dl></div>';
  helpSheet.hidden = false;
}

// agentTabs lists the running tabs whose agent reports status, in sidebar
// order: what the needs-you and working buttons count and step through.
function agentTabs(status) {
  return state.projects.flatMap(p => p.workspaces).filter(w => !w.archived)
    .flatMap(w => w.tabs.filter(t => t.running && t.status === status).map(t => ({ w, t })));
}

// nextAgent shows the tab after the one in front with that status, so
// clicking again walks through all of them.
function nextAgent(status) {
  const list = agentTabs(status);
  if (!list.length) return;
  const at = list.findIndex(({ w, t }) => w.id === state.ws && t.id === activeTab()?.id);
  const { w, t } = list[(at + 1) % list.length];
  if (w.id !== state.ws) selectWorkspace(w.id);
  selectTab(t.id);
}

// renderState shows n with its verb, which agrees with it: "1 needs you".
function renderState(el, n, verb) {
  const v = verb[n === 1 ? 0 : 1];
  el.hidden = !n;
  el.textContent = n ? `${n} ${v}` : '';
  el.title = n ? `${n} agent${n === 1 ? '' : 's'} ${v}: click for the next one` : '';
}

function renderStatus() {
  const { version } = state.wb;
  $('#o-empty-ver').textContent = version ? 'v' + version : '';
  $('#st-ver').textContent = version ? 'v' + version : '';
  const running = state.projects.flatMap(p => p.workspaces).flatMap(w => w.tabs).filter(t => t.running);
  const agents = running.filter(t => t.kind !== 'shell').length;
  const terms = running.length - agents;
  $('#st-terms-n').textContent = terms;
  $('#st-agents-n').textContent = agents;
  $('#st-res').title = `${terms} terminal${terms === 1 ? '' : 's'} and ${agents} agent${agents === 1 ? '' : 's'} running. Click for what each uses`;
  renderState($('#st-waiting'), agentTabs('waiting').length, ['needs you', 'need you']);
  renderState($('#st-working'), agentTabs('working').length, ['working', 'working']);
}

export function initStatusBar() {
  // The server rendered the configured theme; a theme picked in this browser
  // wins the same way it does in the app (theme-preference.js).
  setTheme(chooseThemePreference(getStoredTheme(), currentTheme()), false);

  $('#btn-help').addEventListener('click', showHelp);
  $('#st-waiting').addEventListener('click', () => nextAgent('waiting'));
  $('#st-working').addEventListener('click', () => nextAgent('working'));
  helpSheet.addEventListener('click', () => { helpSheet.hidden = true; });
  addEventListener('keydown', e => {
    if (e.key === 'Escape' && !helpSheet.hidden) helpSheet.hidden = true;
  });

  initResources();
  subscribe(renderStatus);
  return refreshResources;
}
