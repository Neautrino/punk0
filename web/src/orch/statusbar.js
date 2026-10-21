// web/src/orch/statusbar.js
// The status bar under the three panes: version, what is running, the
// CPU · RAM · LSP chip shared with the app (metricsview.js), and the theme
// picker.
import { $, esc } from '../state.js';
import { listThemes, currentTheme, setTheme, getStoredTheme, chooseThemePreference } from '../theme.js';
import { showMetrics, initMetricsMenu } from '../metricsview.js';
import { call } from './api.js';
import { state, subscribe } from './store.js';

const themeMenu = $('#wb-theme-menu');
let themeSel = 0;
let themeBefore = null;

function placeThemeMenu() {
  const r = $('#btn-theme').getBoundingClientRect();
  themeMenu.style.bottom = (innerHeight - r.top + 6) + 'px';
  themeMenu.style.right = Math.max(8, innerWidth - r.right) + 'px';
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

async function refreshMetrics() {
  const res = await call('GET', 'api/metrics');
  if (res.ok) showMetrics(res.data);
}

function renderStatus() {
  const { version } = state.wb;
  $('#st-ver').textContent = version ? 'v' + version : '';
  const ws = state.projects.flatMap(p => p.workspaces);
  const terms = ws.reduce((n, w) => n + w.tabs.filter(t => t.running).length, 0);
  const editors = ws.filter(w => w.backend === 'running').length + state.reviews.filter(r => r.state === 'running').length;
  const parts = [];
  if (terms) parts.push(`${terms} terminal${terms === 1 ? '' : 's'}`);
  if (editors) parts.push(`${editors} editor${editors === 1 ? '' : 's'}`);
  $('#st-running').textContent = parts.length ? parts.join(' · ') + ' running' : '';
}

export function initStatusBar() {
  // The server rendered the configured theme; a theme picked in this browser
  // wins the same way it does in the app (theme-preference.js).
  setTheme(chooseThemePreference(getStoredTheme(), currentTheme()), false);

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

  initMetricsMenu(refreshMetrics);
  subscribe(renderStatus);
  return refreshMetrics;
}
