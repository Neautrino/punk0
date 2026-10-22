// web/src/orch/settings.js
// Settings on the agents page: the shared modal (settings-form.js) opened in
// place by the gear or Mod+, and the agents-page half of applying a value
// live. Theme and UI font are applied here; other modules follow
// 'setting:changed' on the bus, and read current values with getSetting.
import { $ } from '../state.js';
import { showToast } from '../ui.js';
import { initSettingsModal, getSetting, openSettings } from '../settings-form.js';
import { setTheme } from '../theme.js';
import { emit } from '../bus.js';

export { getSetting };

const THEME_KEY = 'px0.theme';

// Keys seen once. The first value of the theme is not applied: on load the
// page already shows the theme this browser picked (statusbar.js), which
// wins over settings.json the same way it does in the editor.
const seen = new Set();

function apply(key, value) {
  const first = !seen.has(key);
  seen.add(key);
  if (key === 'workbench.colorTheme') {
    if (first || !value) return;
    if (setTheme(value, false)) {
      try { localStorage.setItem(THEME_KEY, value); } catch {}
    }
    return;
  }
  if (key === 'workbench.uiFontFamily') {
    if (value) document.documentElement.style.setProperty('--ui', value);
    else document.documentElement.style.removeProperty('--ui');
  }
  emit('setting:changed', key, value);
}

export function initSettings() {
  $('#btn-settings').addEventListener('click', () => openSettings());
  // Mod+, as in most editors; inside the terminal the key belongs to the
  // program running there, as with Mod+B (resize.js).
  addEventListener('keydown', e => {
    if (e.key !== ',' || !(e.ctrlKey || e.metaKey) || e.shiftKey || e.altKey) return;
    if (e.target.closest?.('#o-term')) return;
    e.preventDefault();
    openSettings();
  });
  initSettingsModal({ apply, toast: (accent, text) => showToast(accent, text) });
}

// visibleAgents is what new tabs and chats can run: the installed agents of
// GET api/agents, then a plain shell, without those in agents.hidden.
export function visibleAgents(agents) {
  const hidden = new Set(getSetting('agents.hidden') || []);
  return [...agents.filter(a => a.installed), { id: 'shell', label: 'Shell', installed: true }]
    .filter(a => !hidden.has(a.id));
}

// defaultAgent is agents.default when it is among visible, else the first.
export function defaultAgent(visible) {
  const want = getSetting('agents.default');
  return (visible.find(a => a.id === want) || visible[0])?.id || '';
}
