// web/src/settings.js
// The editor's half of settings: putting each value into effect in this page.
// The modal itself is settings-form.js, shared with the agents page.
import { $, S } from './state.js';
import { showToast } from './ui.js';
import { applyEditorTypography, toggleWordWrap, toggleLineNumbers } from './renderer.js';
import { setTheme } from './theme.js';
import { chooseThemePreference, DEFAULT_THEME } from './theme-preference.js';
import { setLayoutPref } from './diff.js';
import { setVimModeEnabled } from './vim.js';
import { showHelp } from './shortcuts.js';
import { initSettingsModal } from './settings-form.js';
import { drawFileDetails } from './status.js';

export function isAutoRevealEnabled() {
  if (S.settings) {
    if (S.settings['explorer.autoReveal'] !== undefined) {
      return S.settings['explorer.autoReveal'] === true || S.settings['explorer.autoReveal'] === 'true';
    }
    if (S.settings['explorer.autoRelveal'] !== undefined) {
      return S.settings['explorer.autoRelveal'] === true || S.settings['explorer.autoRelveal'] === 'true';
    }
    if (S.settings['autoReveal'] !== undefined) {
      return S.settings['autoReveal'] === true || S.settings['autoReveal'] === 'true';
    }
    if (S.settings['autoRelveal'] !== undefined) {
      return S.settings['autoRelveal'] === true || S.settings['autoRelveal'] === 'true';
    }
  }
  return true;
}

// The four typography keys share one applyEditorTypography, which relays the
// editor out: a batch of applied values (the first load, a save) puts them
// into effect once, after the other keys, rather than once per key.
let typographyQueued = false;
function queueTypography() {
  if (typographyQueued) return;
  typographyQueued = true;
  queueMicrotask(() => {
    typographyQueued = false;
    const fs = parseFloat(S.settings['editor.fontSize']) || 13.5;
    const ff = S.settings['editor.fontFamily'] || '';
    const lh = parseFloat(S.settings['editor.lineHeight']) || 21.0;
    const ts = parseInt(S.settings['editor.tabSize'], 10) || 4;
    applyEditorTypography(fs, ff, lh, ts);
    drawFileDetails(); // the bar's Tab Size and Col
  });
}

// S.settings mirrors every applied value for readers elsewhere in the editor
// (hover.js, tree.js, ...).
function applySettingLive(key, val) {
  if (!S.settings) S.settings = {};
  S.settings[key] = val;

  switch (key) {
    case 'editor.fontSize':
    case 'editor.fontFamily':
    case 'editor.lineHeight':
    case 'editor.tabSize':
      queueTypography();
      break;
    case 'editor.wordWrap': {
      const on = val === 'on' || val === true;
      toggleWordWrap(on);
      break;
    }
    case 'editor.lineNumbers': {
      const on = val === 'on' || val === true;
      toggleLineNumbers(on);
      break;
    }
    case 'editor.cursorStyle': {
      document.body.classList.remove('cursor-block', 'cursor-underline');
      if (val === 'block') document.body.classList.add('cursor-block');
      else if (val === 'underline') document.body.classList.add('cursor-underline');
      break;
    }
    case 'editor.cursorBlinking': {
      document.body.classList.remove('cursor-blink-smooth', 'cursor-blink-solid', 'cursor-blink-blink');
      if (val === 'solid') document.body.classList.add('cursor-blink-solid');
      else if (val === 'blink') document.body.classList.add('cursor-blink-blink');
      else document.body.classList.add('cursor-blink-smooth');
      break;
    }
    case 'editor.renderLineHighlight': {
      document.body.classList.toggle('no-line-highlight', val === 'none');
      break;
    }
    case 'editor.scrollBeyondLastLine': {
      document.body.classList.toggle('no-scroll-beyond', val === false || val === 'false');
      break;
    }
    case 'git.gutterIndicators': {
      document.body.classList.toggle('hide-git-gutter', val === false || val === 'false');
      break;
    }
    case 'editor.minimap.enabled': {
      const minimap = $('#minimap-hits');
      if (minimap) minimap.style.display = (val === false || val === 'false') ? 'none' : '';
      break;
    }
    case 'workbench.uiFontFamily': {
      if (val) document.documentElement.style.setProperty('--ui', val);
      else document.documentElement.style.removeProperty('--ui');
      break;
    }
    case 'workbench.colorTheme': {
      const storedTheme = localStorage.getItem('px0.theme');
      const nextTheme = chooseThemePreference(storedTheme, val, DEFAULT_THEME);
      if (nextTheme) setTheme(nextTheme, false);
      break;
    }
    case 'diffEditor.renderSideBySide': {
      const split = val === true || val === 'true';
      setLayoutPref(split ? 'split' : 'unified');
      break;
    }
    case 'markdown.preview.open': {
      S.mdPreview = val === true || val === 'true';
      try { localStorage.setItem('px0.mdPreview', S.mdPreview ? 'true' : 'false'); } catch {}
      break;
    }
    case 'table.preview.open': {
      S.tablePreview = val === true || val === 'true';
      try { localStorage.setItem('px0.tablePreview', S.tablePreview ? 'true' : 'false'); } catch {}
      break;
    }
    case 'editor.vimMode': {
      setVimModeEnabled(val === true || val === 'true', false);
      break;
    }
    case 'explorer.autoReveal':
    case 'explorer.autoRelveal':
    case 'autoReveal':
    case 'autoRelveal': {
      const on = val === true || val === 'true';
      S.settings['explorer.autoReveal'] = on;
      S.settings['explorer.autoRelveal'] = on;
      break;
    }
  }
}


export function initSettings() {
  initSettingsModal({
    apply: applySettingLive,
    toast: showToast,
    cardActions: {
      'editor.vimMode': { label: 'View Vim Keybindings Cheat Sheet', kbd: '?', run: () => showHelp('vim') },
    },
    // shortcuts.js orders Escape across the editor's overlays.
    escape: false,
  });
}
