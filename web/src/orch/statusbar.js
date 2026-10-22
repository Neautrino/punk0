// web/src/orch/statusbar.js
// The status bar under the three panes. Left: version, then help (the
// keyboard shortcuts sheet), GitHub and settings (settings.js). Right: one button with the
// running terminals, running agents and total RAM, which opens the resource
// panel (resources.js).
import { $, esc, keyCaps } from '../state.js';
import { setTheme, getStoredTheme, chooseThemePreference, currentTheme } from '../theme.js';
import { state, subscribe } from './store.js';
import { initResources, refreshResources } from './resources.js';

// The agents page's own keys; the editor's live in shortcuts.js.
const SHORTCUTS = [
  [['Mod+,'], 'Settings (outside a terminal)'],
  [['Mod+B'], 'Show / hide the projects pane (outside a terminal)'],
  [['Mod+Alt+B'], 'Show / hide the files pane (outside a terminal)'],
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
}

export function initStatusBar() {
  // The server rendered the configured theme; a theme picked in this browser
  // wins the same way it does in the app (theme-preference.js).
  setTheme(chooseThemePreference(getStoredTheme(), currentTheme()), false);

  $('#btn-help').addEventListener('click', showHelp);
  helpSheet.addEventListener('click', () => { helpSheet.hidden = true; });
  addEventListener('keydown', e => {
    if (e.key === 'Escape' && !helpSheet.hidden) helpSheet.hidden = true;
  });

  initResources();
  subscribe(renderStatus);
  return refreshResources;
}
