// web/src/orch/statusbar.js
// The status bar under the three panes. Left: version, then the theme picker,
// GitHub and settings. Right: one button with the running terminals, running
// agents and total RAM, which opens the resource panel (resources.js).
import { $, esc } from '../state.js';
import { listThemes, currentTheme, setTheme, getStoredTheme, chooseThemePreference } from '../theme.js';
import { openEditor } from './api.js';
import { state, subscribe, workspace } from './store.js';
import { initResources, refreshResources } from './resources.js';

const themeMenu = $('#wb-theme-menu');
let themeSel = 0;
let themeBefore = null;

// The theme button sits at the left of the bar, so the menu opens rightward.
function placeThemeMenu() {
  const r = $('#btn-theme').getBoundingClientRect();
  themeMenu.style.bottom = (innerHeight - r.top + 6) + 'px';
  themeMenu.style.left = Math.max(8, r.left) + 'px';
}

function renderThemeMenu() {
  themeMenu.innerHTML = listThemes().map((t, i) => `<li role="option" data-id="${esc(t.id)}" aria-selected="${i === themeSel}">
    <span>${esc(t.name)}</span><span class="wb-faint">${esc(t.id === themeBefore ? 'current' : t.scheme)}</span></li>`).join('');
  themeMenu.children[themeSel]?.scrollIntoView({ block: 'nearest' });
}

// Like the app's Select Theme palette: arrows preview, Enter or click keeps,
// Esc restores the theme from before.
function openThemeMenu() {
  themeBefore = currentTheme();
  themeSel = Math.max(0, listThemes().findIndex(t => t.id === themeBefore));
  renderThemeMenu();
  themeMenu.hidden = false;
  placeThemeMenu();
}

function closeThemeMenu(keep) {
  if (themeMenu.hidden) return;
  themeMenu.hidden = true;
  if (keep) setTheme(currentTheme());
  else if (themeBefore) setTheme(themeBefore, false);
}

function previewTheme(i) {
  const all = listThemes();
  themeSel = (i + all.length) % all.length;
  setTheme(all[themeSel].id, false);
  renderThemeMenu();
}

function renderStatus() {
  const { version } = state.wb;
  $('#o-empty-ver').textContent = version ? 'v' + version : '';
  $('#st-ver').textContent = version ? 'v' + version : '';
  const running = state.projects.flatMap(p => p.workspaces).flatMap(w => w.tabs).filter(t => t.running);
  const agents = running.filter(t => t.kind === 'claude').length;
  const terms = running.length - agents;
  $('#st-terms-n').textContent = terms;
  $('#st-agents-n').textContent = agents;
  $('#st-res').title = `${terms} terminal${terms === 1 ? '' : 's'} and ${agents} agent${agents === 1 ? '' : 's'} running. Click for what each uses`;
  $('#btn-settings').disabled = !settingsTarget();
}

// Settings live in the editor: the workbench saves only its theme
// (handleSettings in workbench.go). The gear opens the selected workspace's
// editor, or the first one whose folder still exists, with settings showing.
function settingsTarget() {
  const w = workspace();
  if (w && !w.missing) return w;
  return state.projects.flatMap(p => p.workspaces).find(x => !x.missing) || null;
}

export function initStatusBar() {
  // The server rendered the configured theme; a theme picked in this browser
  // wins the same way it does in the app (theme-preference.js).
  setTheme(chooseThemePreference(getStoredTheme(), currentTheme()), false);

  $('#btn-settings').addEventListener('click', () => {
    const w = settingsTarget();
    if (w) openEditor(w.href, '?settings');
  });
  $('#btn-theme').addEventListener('click', e => {
    e.stopPropagation();
    if (themeMenu.hidden) openThemeMenu();
    else closeThemeMenu(true);
  });
  themeMenu.addEventListener('click', e => {
    const li = e.target.closest('li[data-id]');
    if (!li) return;
    setTheme(li.dataset.id, false);
    closeThemeMenu(true);
  });
  themeMenu.addEventListener('mousemove', e => {
    const li = e.target.closest('li[data-id]');
    if (!li) return;
    const i = [...themeMenu.children].indexOf(li);
    if (i !== themeSel) previewTheme(i);
  });
  addEventListener('click', e => {
    if (!e.target.closest('#wb-theme-menu, #btn-theme')) closeThemeMenu(false);
  });
  addEventListener('keydown', e => {
    if (themeMenu.hidden) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      previewTheme(themeSel + (e.key === 'ArrowDown' ? 1 : -1));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      closeThemeMenu(true);
    } else if (e.key === 'Escape') {
      closeThemeMenu(false);
    }
  });

  initResources();
  subscribe(renderStatus);
  return refreshResources;
}
