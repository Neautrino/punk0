// web/src/settings-form.js
// The settings modal, shared by the editor and the agents page: one schema
// (GET api/settings), one settings.json. It knows neither page; each passes
// apply(key, value) to put a value into effect. Only state.js is imported
// (no imports of its own), so the agents page bundle stays free of the editor.
import { $, esc, withKeys } from './state.js';

// Rendered when api/settings cannot be reached, so the modal still opens.
// Mirrors settingsSchema in settings.go.
const BUILTIN_SCHEMA = [
  {
    key: "general.openBrowser",
    title: "Open Browser on Start",
    description: "Open the agents page in the browser when punk0 starts. The -no-open flag turns it off regardless. Opening a folder (punk0 <dir>) still opens the agents page on it.",
    category: "General",
    type: "boolean",
    default: true
  },
  {
    key: "update.auto",
    title: "Update Automatically",
    description: "Check for a newer punk0 release in the background on start and install it. The -no-update flag turns it off regardless.",
    category: "General",
    type: "boolean",
    default: true
  },
  {
    key: "tips.enabled",
    title: "Show Tips in Interface",
    description: "Controls whether punk0 periodically shows helpful tips and tricks in the interface.",
    category: "General",
    type: "boolean",
    default: true
  },
  {
    key: "workbench.colorTheme",
    title: "Color Theme",
    description: "The color theme of the agents page and the editor.",
    category: "Appearance",
    type: "select",
    default: "catppuccin-mocha",
    options: ["catppuccin-mocha", "catppuccin-latte", "github-dark", "dark", "light", "dracula", "gruvbox-dark", "gruvbox-light", "monokai", "nord", "one-dark", "rose-pine", "solarized-dark", "solarized-light"]
  },
  {
    key: "workbench.uiFontFamily",
    title: "Interface Font Family",
    description: "The font of menus, lists and labels on both pages. Empty uses the theme's font.",
    category: "Appearance",
    type: "string",
    default: ""
  },
  {
    key: "layout.fileView",
    title: "File View",
    description: "Where files open on the agents page. beside shows them in a panel of their own right of the terminals; tabs puts them among the terminal tabs. beside uses tabs while terminals and open files have less than 640 px between them (the files pane not counted).",
    category: "Appearance",
    type: "select",
    default: "beside",
    options: ["beside", "tabs"]
  },
  {
    key: "agents.default",
    title: "Default Agent",
    description: "The agent New chat and New workspace start with until you pick another, listed first in the + menu and on the welcome screen. An agent that is not installed or is hidden falls back to the first installed one.",
    category: "Agents",
    type: "select",
    default: "claude",
    options: ["claude", "codex", "opencode", "agy", "cursor-agent", "shell"]
  },
  {
    key: "agents.hidden",
    title: "Hidden Agents",
    description: "Agents left out of the + menu, the welcome screen, New chat and New workspace. Tabs already running them keep working.",
    category: "Agents",
    type: "multiselect",
    default: [],
    options: ["claude", "codex", "opencode", "agy", "cursor-agent", "shell"]
  },
  {
    key: "agents.claude.args",
    title: "Claude Code Arguments",
    description: "Extra arguments for every Claude Code tab, placed right after the program name (e.g. --model opus). Shell syntax applies.",
    category: "Agents",
    type: "string",
    default: "",
    exec: true
  },
  {
    key: "agents.codex.args",
    title: "Codex Arguments",
    description: "Extra arguments for every Codex tab, placed right after the program name. Shell syntax applies.",
    category: "Agents",
    type: "string",
    default: "",
    exec: true
  },
  {
    key: "agents.opencode.args",
    title: "OpenCode Arguments",
    description: "Extra arguments for every OpenCode tab, placed right after the program name. Shell syntax applies.",
    category: "Agents",
    type: "string",
    default: "",
    exec: true
  },
  {
    key: "agents.agy.args",
    title: "Antigravity Arguments",
    description: "Extra arguments for every Antigravity tab, placed right after the program name. Shell syntax applies.",
    category: "Agents",
    type: "string",
    default: "",
    exec: true
  },
  {
    key: "agents.cursor-agent.args",
    title: "Cursor Agent Arguments",
    description: "Extra arguments for every Cursor Agent tab, placed right after the program name. Shell syntax applies.",
    category: "Agents",
    type: "string",
    default: "",
    exec: true
  },
  {
    key: "terminal.fontSize",
    title: "Font Size",
    description: "The font size of terminal tabs, in pixels.",
    category: "Terminal",
    type: "number",
    default: 13,
    min: 8,
    max: 32,
    step: 0.5
  },
  {
    key: "terminal.fontFamily",
    title: "Font Family",
    description: "The font of terminal tabs. Empty uses the code font.",
    category: "Terminal",
    type: "string",
    default: ""
  },
  {
    key: "terminal.lineHeight",
    title: "Line Height",
    description: "The line height of terminal tabs, as a multiple of the font size.",
    category: "Terminal",
    type: "number",
    default: 1,
    min: 1,
    max: 2,
    step: 0.05
  },
  {
    key: "terminal.scrollback",
    title: "Scrollback",
    description: "Lines a terminal tab keeps for scrolling back in the page.",
    category: "Terminal",
    type: "number",
    default: 10000,
    min: 1000,
    max: 100000,
    step: 1000
  },
  {
    key: "terminal.cursorStyle",
    title: "Cursor Style",
    description: "The shape of the terminal cursor.",
    category: "Terminal",
    type: "select",
    default: "block",
    options: ["block", "bar", "underline"]
  },
  {
    key: "terminal.cursorBlink",
    title: "Cursor Blinking",
    description: "Whether the terminal cursor blinks.",
    category: "Terminal",
    type: "boolean",
    default: true
  },
  {
    key: "terminal.copyOnSelect",
    title: "Copy on Select",
    description: "Copy terminal text to the clipboard as soon as it is selected with the mouse, on the agents page and in the editor. Needs the page on localhost or https.",
    category: "Terminal",
    type: "boolean",
    default: false
  },
  {
    key: "terminal.confirmCloseRunning",
    title: "Confirm Closing a Running Tab",
    description: "Ask before closing a tab whose shell or agent is still running.",
    category: "Terminal",
    type: "boolean",
    default: true
  },
  {
    key: "terminal.shell",
    title: "Shell",
    description: "The program new tabs run. Empty uses $SHELL, else /bin/sh. Applies to tabs started afterwards.",
    category: "Terminal",
    type: "string",
    default: "",
    exec: true
  },
  {
    key: "terminal.env",
    title: "Environment",
    description: "KEY=VALUE per line, added to every tab's environment (shells and agents). A value starting with ~/ starts in your home folder. Blank lines and lines starting with # are ignored.",
    category: "Terminal",
    type: "textarea",
    default: "",
    exec: true
  },
  {
    key: "terminal.idleStop",
    title: "Stop Idle Terminal After (Minutes)",
    description: "Controls how long a terminal tab stays open with nothing typed or printed in it. A shell is only stopped at its prompt; an agent is stopped like any other quiet tab, except a Claude that says it is working or waiting for an answer, and picks its chat up again when reopened. 0 never stops them.",
    category: "Terminal",
    type: "number",
    default: 30,
    min: 0,
    max: 1440,
    step: 1
  },
  {
    key: "workspace.branchPrefix",
    title: "Branch Prefix",
    description: "Put in front of the branch a new worktree workspace makes from its name (e.g. you/ turns login into you/login). The folder keeps the plain name. Existing branches and pull requests are left as they are.",
    category: "Workspaces",
    type: "string",
    default: ""
  },
  {
    key: "workspace.fetchBase",
    title: "Fetch Base Before Creating",
    description: "Fetch the base branch from its remote before creating a worktree. Off uses the branches you already have.",
    category: "Workspaces",
    type: "boolean",
    default: true
  },
  {
    key: "workspace.deleteMergedBranch",
    title: "Delete Merged Branch with Worktree",
    description: "When a worktree workspace is deleted, also delete its branch if it is merged. An unmerged branch is kept.",
    category: "Workspaces",
    type: "boolean",
    default: false
  },
  {
    key: "workspace.idleStop",
    title: "Stop Idle Workspace After (Minutes)",
    description: "Controls how long a workspace's files keep being served after the last request before punk0 stops it to free memory; the next request starts it again. 0 never stops it.",
    category: "Workspaces",
    type: "number",
    default: 5,
    min: 0,
    max: 1440,
    step: 1
  },
  {
    key: "notifications.desktop",
    title: "Desktop Notifications",
    description: "Show a browser notification when an agent needs you or finishes. Unfocused only notifies while the agents page is in the background. Only agents that report their status (Claude Code) notify, and only while the agents page is open.",
    category: "Notifications",
    type: "select",
    default: "off",
    options: ["off", "unfocused", "always"]
  },
  {
    key: "notifications.onWaiting",
    title: "When an Agent Needs You",
    description: "Notify when an agent waits for your input or permission.",
    category: "Notifications",
    type: "boolean",
    default: true
  },
  {
    key: "notifications.onDone",
    title: "When an Agent Finishes",
    description: "Notify when an agent stops working and is back at its prompt.",
    category: "Notifications",
    type: "boolean",
    default: true
  },
  {
    key: "notifications.sound",
    title: "Sound",
    description: "Play a short tone with each notification.",
    category: "Notifications",
    type: "boolean",
    default: false
  },
  {
    key: "notifications.titleBadge",
    title: "Count in Page Title",
    description: "Show how many agents need you in the agents page's title, e.g. (2) punk0.",
    category: "Notifications",
    type: "boolean",
    default: true
  },
  {
    key: "editor.fontSize",
    title: "Font Size",
    description: "Controls the font size in pixels for the code viewer.",
    category: "Text Editor",
    type: "number",
    default: 13.5,
    min: 9.0,
    max: 32.0,
    step: 0.5
  },
  {
    key: "editor.fontFamily",
    title: "Font Family",
    description: "Controls the font family used in the code viewer.",
    category: "Text Editor",
    type: "string",
    default: '"JetBrains Mono", "Fira Code", "Cascadia Code", "SF Mono", Menlo, Consolas, ui-monospace, monospace'
  },
  {
    key: "editor.lineHeight",
    title: "Line Height",
    description: "Controls the line height in pixels for the code viewer.",
    category: "Text Editor",
    type: "number",
    default: 21.0,
    min: 14.0,
    max: 48.0,
    step: 1.0
  },
  {
    key: "editor.tabSize",
    title: "Tab Size",
    description: "The number of spaces a tab is equal to.",
    category: "Text Editor",
    type: "select",
    default: 4,
    options: ["2", "4", "8"]
  },
  {
    key: "editor.wordWrap",
    title: "Word Wrap",
    description: "Controls whether lines should wrap around or scroll horizontally.",
    category: "Text Editor",
    type: "select",
    default: "on",
    options: ["on", "off"]
  },
  {
    key: "editor.lineNumbers",
    title: "Line Numbers",
    description: "Controls the display of line numbers in the gutter.",
    category: "Text Editor",
    type: "select",
    default: "on",
    options: ["on", "off"]
  },
  {
    key: "editor.vimMode",
    title: "Vim Keybindings",
    description: "Enable Vim modal navigation (Normal mode, Visual mode, motions, search, and LSP shortcuts).",
    category: "Text Editor",
    type: "boolean",
    default: false
  },
  {
    key: "editor.cursorStyle",
    title: "Cursor Style",
    description: "Controls the cursor style in the code viewer.",
    category: "Text Editor",
    type: "select",
    default: "line",
    options: ["line", "block", "underline"]
  },
  {
    key: "editor.cursorBlinking",
    title: "Cursor Blinking",
    description: "Controls the cursor animation style.",
    category: "Text Editor",
    type: "select",
    default: "smooth",
    options: ["blink", "smooth", "solid"]
  },
  {
    key: "editor.renderLineHighlight",
    title: "Render Line Highlight",
    description: "Controls how the editor should render the current line highlight.",
    category: "Text Editor",
    type: "select",
    default: "line",
    options: ["line", "none"]
  },
  {
    key: "editor.occurrencesHighlight",
    title: "Occurrences Highlight",
    description: "Controls whether the editor should highlight occurrences of the selected word.",
    category: "Text Editor",
    type: "boolean",
    default: true
  },
  {
    key: "editor.scrollBeyondLastLine",
    title: "Scroll Beyond Last Line",
    description: "Controls whether the editor will scroll beyond the last line of the file.",
    category: "Text Editor",
    type: "boolean",
    default: true
  },
  {
    key: "editor.bracketPairColorization",
    title: "Bracket Pair Colorization",
    description: "Controls whether bracket pair colorization and matching is enabled.",
    category: "Text Editor",
    type: "boolean",
    default: true
  },
  {
    key: "editor.renderWhitespace",
    title: "Render Whitespace",
    description: "Controls how whitespace characters are rendered in the viewer.",
    category: "Text Editor",
    type: "select",
    default: "selection",
    options: ["none", "boundary", "selection", "all"]
  },
  {
    key: "editor.minimap.enabled",
    title: "Minimap Hits",
    description: "Controls whether search hit indicators are shown in the scroll minimap gutter.",
    category: "Text Editor",
    type: "boolean",
    default: true
  },
  {
    key: "diffEditor.renderSideBySide",
    title: "Diff Side By Side",
    description: "Controls whether the diff editor shows changes in split (side-by-side) or unified mode.",
    category: "Workbench",
    type: "boolean",
    default: true
  },
  {
    key: "diffEditor.ignoreTrimWhitespace",
    title: "Diff: Ignore Trim Whitespace",
    description: "Controls whether the diff viewer ignores changes in leading or trailing whitespace.",
    category: "Git & Diff",
    type: "boolean",
    default: true
  },
  {
    key: "git.gutterIndicators",
    title: "Git Gutter Indicators",
    description: "Controls whether changed line indicators are shown in the editor gutter.",
    category: "Git & Diff",
    type: "boolean",
    default: true
  },
  {
    key: "markdown.preview.open",
    title: "Markdown Preview",
    description: "Controls whether Markdown files open in rendered preview by default.",
    category: "Workbench",
    type: "boolean",
    default: true
  },
  {
    key: "table.preview.open",
    title: "Table View",
    description: "Controls whether CSV and TSV files open as a table by default.",
    category: "Workbench",
    type: "boolean",
    default: true
  },
  {
    key: "explorer.compactFolders",
    title: "Compact Folders",
    description: "Controls whether the file tree renders single-child directory chains compactly.",
    category: "Files & Explorer",
    type: "boolean",
    default: true
  },
  {
    key: "explorer.autoReveal",
    title: "Auto Reveal",
    description: "Controls whether the file explorer automatically scrolls to and reveals active tabs.",
    category: "Files & Explorer",
    type: "boolean",
    default: true
  },
  {
    key: "files.exclude",
    title: "Files Exclude Patterns",
    description: "Configure glob patterns for excluding files and folders from search and trees.",
    category: "Files & Explorer",
    type: "string",
    default: "**/.git, **/node_modules, **/target, **/.DS_Store"
  },
  {
    key: "search.smartCase",
    title: "Smart Case Search",
    description: "Searches case-insensitively when query is lowercase, and case-sensitively when uppercase characters exist.",
    category: "Search",
    type: "boolean",
    default: true
  },
  {
    key: "search.maxResults",
    title: "Max Search Results",
    description: "Controls the maximum number of results returned in workspace-wide searches.",
    category: "Search",
    type: "number",
    default: 1000.0,
    min: 50.0,
    max: 10000.0,
    step: 50.0
  },
  {
    key: "lsp.enabled",
    title: "Language Servers",
    description: "Master switch. Off stops every running language server in every folder and starts none; each folder's own choice is kept for when you turn this back on.",
    category: "Language Servers",
    type: "boolean",
    default: true
  },
  {
    key: "lsp.idleStop",
    title: "Idle Stop (minutes)",
    description: "Stop a language server nobody has used for this many minutes, even while its editor is open. The next use starts it again. 0 never stops it.",
    category: "Language Servers",
    type: "number",
    default: 15,
    min: 0,
    max: 1440,
    step: 1
  },
  {
    key: "lsp.disabled",
    title: "Disabled Servers",
    description: "Language servers punk0 never starts. Another installed server for the same language is used instead.",
    category: "Language Servers",
    type: "multiselect",
    default: [],
    options: ["gopls", "rust-analyzer", "pyright", "pylsp", "ruff", "tsc", "typescript", "clangd", "zls", "lua", "solargraph", "jdtls", "omnisharp", "texlab"]
  },
  {
    key: "lsp.memoryWarning",
    title: "Memory Warning (MB)",
    description: "On the agents page, show the language server memory total in red when all folders together use more than this. 0 never warns.",
    category: "Language Servers",
    type: "number",
    default: 2048,
    min: 0,
    step: 256
  },
  {
    key: "lsp.hover.enabled",
    title: "Hover Documentation",
    description: "Controls whether hovercards with documentation and type signatures appear on hover.",
    category: "Language Servers",
    type: "boolean",
    default: true
  },
  {
    key: "workspace.lspDefault",
    title: "Language Servers for New Worktrees",
    description: "The default of the New workspace form's Language Servers checkbox for worktrees. Each project then remembers your last choice.",
    category: "Language Servers",
    type: "boolean",
    default: false
  },
  {
    key: "agent.harness",
    title: "Coding Harness",
    description: "Coding agent harness invoked for code edits (e.g. claude, gemini, cursor-agent, agy, opencode, codex, aider, goose).",
    category: "Agent / AI",
    type: "string",
    default: "",
    exec: true
  },
  {
    key: "agent.timeoutSeconds",
    title: "Agent Timeout (Seconds)",
    description: "Controls the maximum execution time in seconds for agent edits before canceling.",
    category: "Agent / AI",
    type: "number",
    default: 600.0,
    min: 10.0,
    max: 600.0,
    step: 10.0
  },
  {
    key: "agent.autoAcceptEdits",
    title: "Auto Accept Agent Edits",
    description: "Controls whether agent-generated code diffs are accepted without manual confirmation.",
    category: "Agent / AI",
    type: "boolean",
    default: false
  },
  {
    key: "server.basePath",
    title: "Base Path",
    description: "Base URL path prefix for the punk0 server and web interface (e.g. /rev-123/).",
    category: "Server",
    type: "string",
    default: "/"
  }
];

let settingsData = {
  settings: {},
  defaults: Object.fromEntries(BUILTIN_SCHEMA.map(s => [s.key, s.default])),
  schema: BUILTIN_SCHEMA,
  raw: '{\n}\n',
  path: '~/.punk0/settings.json'
};
let modalEl = null;
let page = { apply: () => {}, toast: null, cardActions: {}, escape: true };
let activeCategory = 'Commonly Used';
let viewMode = 'ui'; // 'ui' | 'json'
let filterQuery = '';
// Values typed but not yet committed by a change event; the explicit Save sends them.
let pendingChanges = {};

const COMMONLY_USED_KEYS = new Set([
  'editor.fontSize',
  'workbench.colorTheme',
  'terminal.fontSize',
  'agents.default',
  'notifications.desktop',
  'editor.wordWrap',
  'editor.lineNumbers',
  'editor.vimMode',
  'editor.tabSize',
  'diffEditor.renderSideBySide',
  'editor.cursorStyle',
  'explorer.autoReveal',
  'explorer.autoRelveal',
  'search.smartCase',
  'lsp.hover.enabled',
  'agent.harness',
]);

const EXEC_NOTE = 'Can only be changed when punk0 listens on 127.0.0.1';
const SAVE_ICON = '<path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/>';
const CHECK_ICON = '<polyline points="20 6 9 17 4 12"/>';

const MODAL_HTML = () => `
  <div class="settings-dialog" role="dialog" aria-modal="true" aria-labelledby="settings-title">
    <header class="settings-header">
      <div class="settings-title-group">
        <h2 id="settings-title">Settings</h2>
        <span id="settings-path" class="settings-path-badge" title="Click to copy path">~/.punk0/settings.json</span>
      </div>
      <div class="settings-header-actions">
        <div class="settings-mode-switch" role="group" aria-label="Settings View Mode">
          <button type="button" id="settings-mode-ui" class="active" title="Settings UI">UI</button>
          <button type="button" id="settings-mode-json" title="Open Settings (JSON)">JSON</button>
        </div>
        <button id="settings-close" class="mini settings-close-btn" title="Close (Esc)">&#10005;</button>
      </div>
    </header>
    <div id="settings-search-bar" class="settings-search-bar">
      <svg class="settings-search-icon" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
      <input type="text" id="settings-search" placeholder="Search settings..." autocomplete="off" spellcheck="false">
      <button id="settings-search-clear" class="mini" title="Clear search">&#10005;</button>
    </div>
    <div id="settings-ui-container" class="settings-body">
      <nav id="settings-nav" class="settings-nav" aria-label="Settings categories"></nav>
      <section id="settings-list" class="settings-list" aria-label="Settings list"></section>
    </div>
    <div id="settings-json-container" class="settings-json-body" hidden>
      <div class="settings-json-toolbar">
        <span class="settings-json-hint">Edit settings.json directly. Changes apply immediately upon saving.</span>
        <div class="settings-json-actions">
          <button id="btn-settings-reset-raw" class="settings-btn-secondary" title="Discard unsaved changes">Discard</button>
          <button id="btn-settings-save-raw" class="settings-btn-primary" title="${esc(withKeys('Save changes to settings.json ({Mod+S})'))}">Save</button>
        </div>
      </div>
      <div id="settings-raw-error" class="settings-error-banner" hidden></div>
      <textarea id="settings-raw-editor" spellcheck="false" autocomplete="off" wrap="off"></textarea>
    </div>
    <footer id="settings-footer" class="settings-footer">
      <div id="settings-footer-status" class="settings-footer-status is-saved">
        <svg id="settings-status-icon" viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">${CHECK_ICON}</svg>
        <span id="settings-status-text">All changes saved</span>
      </div>
      <div class="settings-footer-actions">
        <button type="button" id="btn-settings-save" class="settings-btn-save" title="${esc(withKeys('Save settings ({Mod+S})'))}">
          <svg class="settings-btn-save-icon" viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${SAVE_ICON}</svg>
          <span id="settings-btn-save-label">Save</span>
        </button>
      </div>
    </footer>
  </div>`;

// Both servers answer errors as {"error": "..."}, a guard in front of them
// may answer plain text; either way the text reaches the save status.
async function request(init) {
  const r = await fetch(new URL('api/settings', document.baseURI), init);
  const text = await r.text();
  let body = null;
  try { body = JSON.parse(text); } catch {}
  if (!r.ok || !body || body.error) throw new Error(body?.error || text.trim() || `HTTP ${r.status}`);
  return body;
}

const postSettings = payload => request({
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(payload),
});

export async function loadSettings() {
  try {
    const data = await request();
    if (data.schema && data.schema.length > 0) {
      settingsData = data;
    } else {
      settingsData.settings = data.settings || {};
      settingsData.raw = data.raw || settingsData.raw;
      settingsData.path = data.path || settingsData.path;
      if (data.defaults) settingsData.defaults = { ...settingsData.defaults, ...data.defaults };
    }
  } catch (err) {
    console.warn('Using built-in settings schema (offline/fallback):', err);
  }
  return settingsData;
}

// getSetting is the stored value, else the default, as of the last load or save.
export function getSetting(key) {
  const v = settingsData.settings?.[key];
  return v !== undefined ? v : settingsData.defaults?.[key];
}

const sameValue = (a, b) => JSON.stringify(a) === JSON.stringify(b);

// commit takes a save reply as the new state and applies every key it
// changed, plus the keys posted (the page may have shown another value).
function commit(res, posted = {}) {
  const before = settingsData.settings || {};
  if (res.settings) settingsData.settings = res.settings;
  if (res.raw) settingsData.raw = res.raw;
  const after = settingsData.settings || {};
  const keys = new Set([...Object.keys(posted), ...Object.keys(before), ...Object.keys(after)]);
  for (const k of keys) {
    if (k in posted || !sameValue(before[k], after[k])) page.apply(k, getSetting(k));
  }
}

function applyAll() {
  const keys = new Set([...Object.keys(settingsData.defaults || {}), ...Object.keys(settingsData.settings || {})]);
  for (const k of keys) page.apply(k, getSetting(k));
}

/**
 * Builds the modal once, loads settings and applies every value.
 * @param {{
 *   apply: (key: string, value: any) => void,
 *   toast?: (accent: string, text: string) => void,
 *   cardActions?: Record<string, { label: string, kbd?: string, run: () => void }>,
 *   escape?: boolean,
 * }} opts escape: close on Escape (false when the page orders Escape itself).
 */
export function initSettingsModal(opts) {
  page = { ...page, ...opts };
  ensureModal();
  // Another page or tool may have saved meanwhile: catch up when this one
  // is looked at again.
  addEventListener('focus', refreshSettings);
  document.addEventListener('visibilitychange', () => document.visibilityState === 'visible' && refreshSettings());
  return loadSettings().then(applyAll);
}

const REFRESH_GAP = 3000; // ms: a focus storm makes at most one request
let refreshedAt = 0;

// refreshSettings re-reads api/settings and applies every key whose value
// changed, the path a save takes (commit), without posting anything, so it
// cannot loop. An open modal shows the new values unless the user is editing.
async function refreshSettings() {
  if (Date.now() - refreshedAt < REFRESH_GAP) return;
  refreshedAt = Date.now();
  let data;
  try { data = await request(); } catch { return; }
  const oldRaw = settingsData.raw;
  const changed = !sameValue(settingsData.settings || {}, data.settings || {}) || (data.raw && data.raw !== oldRaw);
  if (!changed) return;
  commit({ settings: data.settings || {}, raw: data.raw });
  if (!isSettingsOpen()) return;
  if (viewMode === 'json') {
    const rawEditor = $('#settings-raw-editor');
    if (rawEditor.value === oldRaw) rawEditor.value = settingsData.raw || '{\n}\n';
  } else if (!Object.keys(pendingChanges).length && !document.activeElement?.closest?.('#settings-list')) {
    renderSettingsList();
  }
}

function ensureModal() {
  if (modalEl) return;
  modalEl = document.createElement('div');
  modalEl.id = 'settings-modal';
  modalEl.className = 'modal-overlay';
  modalEl.hidden = true;
  modalEl.innerHTML = MODAL_HTML();
  document.body.appendChild(modalEl);
  wireModal();
}

export function openSettings(mode = 'ui', category = null, highlightKey = null) {
  ensureModal();
  viewMode = mode === 'json' ? 'json' : 'ui';
  modalEl.hidden = false;
  pendingChanges = {};
  setSaveStatus('saved', 'All changes saved');

  if (category && viewMode === 'ui') {
    activeCategory = category;
    filterQuery = '';
    $('#settings-search').value = '';
  }

  showView();

  // Render now from what is known, again once the latest settings arrive.
  loadSettings().then(() => {
    showView();
    if (highlightKey && viewMode === 'ui') focusSettingCard(highlightKey);
  });

  if (viewMode === 'ui' && !category) setTimeout(() => $('#settings-search')?.focus(), 50);
  if (highlightKey && viewMode === 'ui') setTimeout(() => focusSettingCard(highlightKey), 30);
}

function focusSettingCard(key) {
  const card = modalEl?.querySelector(`[data-setting="${CSS.escape(key)}"]`);
  if (card) {
    card.scrollIntoView({ block: 'center', behavior: 'smooth' });
    const input = card.querySelector('textarea, input, select');
    if (input) input.focus();
  }
}

export function closeSettings() {
  if (modalEl) modalEl.hidden = true;
}

export function isSettingsOpen() {
  return !!modalEl && !modalEl.hidden;
}

function updateSettingsHeader() {
  const pathEl = $('#settings-path');
  if (settingsData.path) {
    pathEl.textContent = settingsData.path;
    pathEl.title = 'Click to copy path: ' + settingsData.path;
  }
  $('#settings-mode-ui').classList.toggle('active', viewMode === 'ui');
  $('#settings-mode-json').classList.toggle('active', viewMode === 'json');
}

function showView() {
  if (viewMode === 'json') showSettingsJSONView();
  else showSettingsUIView();
}

function showSettingsUIView() {
  viewMode = 'ui';
  updateSettingsHeader();
  $('#settings-ui-container').hidden = false;
  $('#settings-json-container').hidden = true;
  $('#settings-search-bar').hidden = false;
  renderSettingsNav();
  renderSettingsList();
}

function showSettingsJSONView() {
  viewMode = 'json';
  updateSettingsHeader();
  $('#settings-ui-container').hidden = true;
  $('#settings-json-container').hidden = false;
  $('#settings-search-bar').hidden = true;
  const rawEditor = $('#settings-raw-editor');
  rawEditor.value = settingsData.raw || '{\n}\n';
  rawEditor.focus();
  $('#settings-raw-error').hidden = true;
}

// Categories in the order the schema first names them, after Commonly Used.
function getSettingCategories() {
  const cats = ['Commonly Used'];
  const seen = new Set(cats);
  for (const item of (settingsData.schema || [])) {
    if (item.category && !seen.has(item.category)) {
      cats.push(item.category);
      seen.add(item.category);
    }
  }
  return cats;
}

function renderSettingsNav() {
  $('#settings-nav').innerHTML = getSettingCategories().map(cat => {
    const active = cat === activeCategory ? ' active' : '';
    return `<button class="settings-nav-item${active}" data-cat="${esc(cat)}">${esc(cat)}</button>`;
  }).join('');
}

// Multiselect values are sets: order does not make a value modified.
const sortedList = v => (Array.isArray(v) ? [...v].map(String).sort() : []);

function isSettingModified(val, defVal) {
  if (val === undefined || val === null) return false;
  if (Array.isArray(val) || Array.isArray(defVal)) return !sameValue(sortedList(val), sortedList(defVal));
  if (defVal === undefined || defVal === null) return val !== '';
  if (typeof defVal === 'number') return Number(val) !== Number(defVal);
  if (typeof defVal === 'boolean') return Boolean(val) !== Boolean(defVal);
  return String(val) !== String(defVal);
}

const shown = v => (Array.isArray(v) ? JSON.stringify(v) : String(v));

// A secure context is the only place Notification exists; a denied
// permission cannot be asked again from the page.
function notificationWarning(value) {
  if (value === 'off') return '';
  if (typeof Notification === 'undefined') return 'Notifications need https or localhost';
  if (Notification.permission === 'denied') return 'Your browser blocked notifications for this page';
  return '';
}

function renderSettingsList() {
  const container = $('#settings-list');
  const q = filterQuery.trim().toLowerCase();
  const schema = settingsData.schema || [];
  const currentSettings = settingsData.settings || {};
  const defaults = settingsData.defaults || {};

  let items;
  if (q) {
    items = schema.filter(s => {
      const key = (s.key || '').toLowerCase();
      const isAutoReveal = key === 'explorer.autoreveal' || key === 'explorer.autorelveal';
      const matchTypo = isAutoReveal && (q.includes('relveal') || q.includes('reveal'));
      return (s.title || '').toLowerCase().includes(q) || key.includes(q)
        || (s.description || '').toLowerCase().includes(q) || (s.category || '').toLowerCase().includes(q) || matchTypo;
    });
  } else if (activeCategory === 'Commonly Used') {
    items = schema.filter(s => COMMONLY_USED_KEYS.has(s.key));
  } else {
    items = schema.filter(s => s.category === activeCategory);
  }

  if (items.length === 0) {
    container.innerHTML = `<div class="settings-empty">No matching settings found for "${esc(q || activeCategory)}".</div>`;
    return;
  }

  container.innerHTML = items.map(item => renderCard(item, currentSettings, defaults)).join('');
}

function renderCard(item, currentSettings, defaults) {
  const key = item.key;
  const title = item.title || key;
  const desc = item.description || '';
  const cat = item.category || 'General';
  const type = item.type || 'string';
  const def = defaults[key] !== undefined ? defaults[key] : item.default;
  const val = currentSettings[key] !== undefined ? currentSettings[key] : def;
  const modified = isSettingModified(currentSettings[key], def);

  let controlHtml = '';
  let aptValuesHtml = '';

  if (type === 'boolean') {
    const isT = val === true || val === 'true';
    controlHtml = `
      <label class="settings-switch">
        <input type="checkbox" data-key="${esc(key)}" ${isT ? 'checked' : ''}>
        <span class="settings-slider"></span>
      </label>`;
    aptValuesHtml = `
      <div class="settings-apt-bar">
        <span class="settings-apt-label">Allowed Values:</span>
        <div class="settings-apt-pills">
          <button type="button" class="settings-pill-tag${isT ? ' active' : ''}" data-set-key="${esc(key)}" data-set-val="true" title="Set to true">true</button>
          <button type="button" class="settings-pill-tag${!isT ? ' active' : ''}" data-set-key="${esc(key)}" data-set-val="false" title="Set to false">false</button>
        </div>
      </div>`;
  } else if (type === 'select') {
    const opts = item.options || [];
    const optHtml = opts.map(o => `<option value="${esc(o)}" ${String(o) === String(val) ? 'selected' : ''}>${esc(o)}</option>`).join('');
    controlHtml = `<select class="settings-select" data-key="${esc(key)}">${optHtml}</select>`;
    const pills = opts.map(o => {
      const isSel = String(o) === String(val);
      return `<button type="button" class="settings-pill-tag${isSel ? ' active' : ''}" data-set-key="${esc(key)}" data-set-val="${esc(String(o))}" title="Select ${esc(String(o))}">${esc(String(o))}</button>`;
    }).join('');
    aptValuesHtml = `
      <div class="settings-apt-bar">
        <span class="settings-apt-label">Options:</span>
        <div class="settings-apt-pills">
          ${pills}
        </div>
      </div>`;
  } else if (type === 'multiselect') {
    // The checkboxes carry no data-key: the list's change handler reads the
    // whole group as one array value.
    const chosen = new Set(Array.isArray(val) ? val.map(String) : []);
    controlHtml = `<div class="settings-multiselect" data-multi-key="${esc(key)}">${(item.options || []).map(o => `
      <label class="settings-multiselect-option">
        <input type="checkbox" value="${esc(String(o))}"${chosen.has(String(o)) ? ' checked' : ''}>
        <span>${esc(String(o))}</span>
      </label>`).join('')}
    </div>`;
  } else if (type === 'number') {
    const { min, max, step } = item;
    const minAttr = min !== undefined ? `min="${min}"` : '';
    const maxAttr = max !== undefined ? `max="${max}"` : '';
    const stepAttr = step !== undefined ? `step="${step}"` : 'step="1"';
    controlHtml = `<input type="number" class="settings-input settings-input-num" data-key="${esc(key)}" value="${esc(String(val))}" ${minAttr} ${maxAttr} ${stepAttr}>`;

    let numberPresets = [];
    if (key === 'editor.fontSize') numberPresets = [12, 13, 13.5, 14, 16, 18];
    else if (key === 'editor.lineHeight') numberPresets = [18, 20, 21, 24, 28];
    else if (key === 'search.maxResults') numberPresets = [200, 500, 1000, 5000];
    else if (key === 'agent.timeoutSeconds') numberPresets = [60, 120, 180, 300];

    const presetPills = numberPresets.length ? `
      <span class="settings-apt-label">Presets:</span>
      <div class="settings-apt-pills">
        ${numberPresets.map(n => `<button type="button" class="settings-pill-tag${Number(val) === n ? ' active' : ''}" data-set-key="${esc(key)}" data-set-val="${n}">${n}</button>`).join('')}
      </div>` : '';

    aptValuesHtml = `
      <div class="settings-apt-bar">
        <span class="settings-tag tag-range">Min: <b>${min !== undefined ? min : '—'}</b></span>
        <span class="settings-tag tag-range">Max: <b>${max !== undefined ? max : '—'}</b></span>
        ${step !== undefined ? `<span class="settings-tag tag-step">Step: <b>${step}</b></span>` : ''}
        ${presetPills}
      </div>`;
  } else if (type === 'textarea') {
    const placeholder = key === 'git.commitMessageInstruction'
      ? 'Enter custom instructions for AI-written pull request merge messages (e.g. Follow Conventional Commits, reference issue key from branch)...'
      : 'Enter instructions...';
    controlHtml = `<textarea class="settings-input settings-textarea" data-key="${esc(key)}" rows="6" spellcheck="false" placeholder="${esc(placeholder)}">${esc(String(val || ''))}</textarea>`;
  } else {
    const isSecret = item.secret;
    controlHtml = `<input type="${isSecret ? 'password' : 'text'}" class="settings-input" data-key="${esc(key)}" value="${esc(String(val || ''))}"${isSecret ? ' autocomplete="off"' : ''}>`;
    const stringPresets = key === 'agent.harness' ? ['claude', 'gemini', 'cursor-agent', 'agy', 'aider'] : [];
    aptValuesHtml = stringPresets.length ? `
      <div class="settings-apt-bar">
        <span class="settings-apt-label">Suggestions:</span>
        <div class="settings-apt-pills">
          ${stringPresets.map(s => `<button type="button" class="settings-pill-tag${String(val) === s ? ' active' : ''}" data-set-key="${esc(key)}" data-set-val="${esc(s)}">${esc(s)}</button>`).join('')}
        </div>
      </div>` : '';
  }

  const resetBtn = modified
    ? `<button class="settings-reset-btn" data-reset="${esc(key)}" title="Reset to default (${esc(shown(def))})">Reset</button>`
    : '';

  const action = page.cardActions?.[key];
  const extraAction = action ? `
    <div style="margin: 6px 0 2px;">
      <button type="button" class="settings-btn-link settings-card-action" data-action-key="${esc(key)}" style="cursor:pointer;font-size:11.5px;display:inline-flex;align-items:center;gap:4px;color:var(--accent-fg);">
        <span>${esc(action.label)}</span>${action.kbd ? `<kbd class="footer-kbd" style="font-size:10px;">${esc(action.kbd)}</kbd>` : ''}
      </button>
    </div>` : '';

  const notes = [];
  if (item.exec) notes.push(`<div class="settings-card-note">${esc(EXEC_NOTE)}</div>`);
  const warning = key === 'notifications.desktop' ? notificationWarning(val) : '';
  if (warning) notes.push(`<div class="settings-card-note is-warning">${esc(warning)}</div>`);

  const cardClass = `settings-card${modified ? ' is-modified' : ''}${type === 'textarea' ? ' settings-card-textarea' : ''}`;

  return `
    <div class="${cardClass}" data-setting="${esc(key)}">
      <div class="settings-card-left">
        <div class="settings-card-header">
          <span class="settings-card-title">${esc(title)}</span>
          <span class="settings-card-key">${esc(key)}</span>
          <span class="settings-tag tag-cat">${esc(cat)}</span>
          <span class="settings-tag tag-type">${esc(type)}</span>
        </div>
        <div class="settings-card-desc">${esc(desc)}</div>
        ${notes.join('')}
        ${aptValuesHtml}
        ${extraAction}
        <div class="settings-card-meta">
          ${type !== 'textarea' ? `<span class="settings-tag tag-current">Current: <b>${esc(shown(val))}</b></span>` : ''}
          <span class="settings-tag tag-default">Default: <code>${esc(shown(def))}</code></span>
          ${modified ? `<span class="settings-tag tag-modified">Modified</span>` : ''}
          ${resetBtn}
        </div>
      </div>
      <div class="settings-card-right">
        ${controlHtml}
      </div>
    </div>
  `;
}

function setSaveStatus(state, msg) {
  const statusEl = $('#settings-footer-status');
  const statusText = $('#settings-status-text');
  const statusIcon = $('#settings-status-icon');
  const saveBtn = $('#btn-settings-save');
  const saveLabel = $('#settings-btn-save-label');
  const saveIcon = saveBtn.querySelector('.settings-btn-save-icon');

  statusEl.className = 'settings-footer-status';

  if (state === 'saving') {
    statusEl.classList.add('is-saving');
    statusText.textContent = msg || 'Saving changes...';
    statusIcon.style.animation = 'spin 0.8s linear infinite';
    statusIcon.innerHTML = '<circle cx="12" cy="12" r="9" stroke="currentColor" stroke-dasharray="28" stroke-dashoffset="14" stroke-width="2.5"/>';
    saveBtn.disabled = true;
    saveLabel.textContent = 'Saving...';
  } else if (state === 'saved') {
    statusEl.classList.add('is-saved');
    statusText.textContent = msg || 'All changes saved';
    statusIcon.style.animation = '';
    statusIcon.innerHTML = CHECK_ICON;
    saveBtn.disabled = false;
    saveIcon.innerHTML = CHECK_ICON;
    saveLabel.textContent = 'Saved';
    setTimeout(() => {
      saveIcon.innerHTML = SAVE_ICON;
      saveLabel.textContent = 'Save';
    }, 2000);
  } else if (state === 'unsaved') {
    statusEl.classList.add('is-unsaved');
    statusText.textContent = msg || 'Unsaved changes';
    statusIcon.style.animation = '';
    statusIcon.innerHTML = '<circle cx="12" cy="12" r="5" fill="currentColor"/>';
    saveBtn.disabled = false;
    saveIcon.innerHTML = SAVE_ICON;
    saveLabel.textContent = 'Save';
  } else if (state === 'error') {
    statusEl.classList.add('is-error');
    statusText.textContent = msg || 'Failed to save';
    statusIcon.style.animation = '';
    statusIcon.innerHTML = '<circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>';
    saveBtn.disabled = false;
    saveLabel.textContent = 'Retry Save';
  }
}

// A rejected value (400 validation, 403 off loopback) is not kept: the list
// re-renders with the stored value and the server's reason stays in the footer.
async function handleSettingChange(key, value) {
  // The change event is the user gesture the permission prompt needs, so ask
  // before the first await.
  if (key === 'notifications.desktop' && value !== 'off' && typeof Notification !== 'undefined'
    && Notification.permission === 'default') {
    Notification.requestPermission().then(renderSettingsList, () => {});
  }
  delete pendingChanges[key];
  try {
    setSaveStatus('saving');
    commit(await postSettings({ [key]: value }), { [key]: value });
    setSaveStatus('saved', 'All changes saved');
  } catch (err) {
    console.error(`Failed to save setting ${key}:`, err);
    setSaveStatus('error', 'Failed to save: ' + (err.message || 'unknown error'));
  }
  renderSettingsList();
}

async function handleResetSetting(key) {
  const def = settingsData.defaults?.[key];
  if (def !== undefined) await handleSettingChange(key, def);
}

async function handleExplicitSave() {
  if (viewMode === 'json') return handleSaveRawSettings();

  // A value still being typed has not fired its change event yet.
  const activeEl = /** @type {HTMLInputElement|HTMLTextAreaElement|null} */ (document.activeElement);
  if (activeEl && activeEl.dataset?.key && activeEl.closest('#settings-list')) {
    let val = /** @type {any} */ (activeEl.value);
    if (activeEl instanceof HTMLInputElement && activeEl.type === 'checkbox') val = activeEl.checked;
    else if (activeEl.type === 'number') val = parseFloat(val);
    pendingChanges[activeEl.dataset.key] = val;
  }

  setSaveStatus('saving');
  try {
    const toSave = Object.keys(pendingChanges).length > 0
      ? { ...pendingChanges }
      : { ...(settingsData.settings || {}) };
    commit(await postSettings(toSave), toSave);
    pendingChanges = {};
    renderSettingsList();
    setSaveStatus('saved', 'Settings saved successfully');
    page.toast?.('✓', 'Settings saved');
  } catch (err) {
    setSaveStatus('error', 'Failed to save: ' + (err.message || 'unknown error'));
    page.toast?.('!', err.message || 'Failed to save settings');
  }
}

async function handleSaveRawSettings() {
  const rawText = $('#settings-raw-editor').value;
  const errEl = $('#settings-raw-error');
  try {
    JSON.parse(rawText);
    errEl.hidden = true;
  } catch (err) {
    errEl.textContent = 'JSON Syntax Error: ' + err.message;
    errEl.hidden = false;
    setSaveStatus('error', 'JSON syntax error');
    return;
  }

  setSaveStatus('saving');
  try {
    commit(await postSettings({ raw: rawText }));
    errEl.textContent = 'Settings saved successfully.';
    errEl.hidden = false;
    errEl.classList.add('success');
    setTimeout(() => {
      errEl.hidden = true;
      errEl.classList.remove('success');
    }, 2500);
    setSaveStatus('saved', 'Settings saved successfully');
    page.toast?.('✓', 'Settings saved');
  } catch (err) {
    errEl.textContent = 'Failed to save: ' + err.message;
    errEl.hidden = false;
    setSaveStatus('error', 'Failed to save: ' + (err.message || 'unknown error'));
  }
}

function wireModal() {
  $('#settings-close').addEventListener('click', closeSettings);
  modalEl.addEventListener('click', e => {
    if (e.target === modalEl) closeSettings();
  });

  $('#btn-settings-save').addEventListener('click', handleExplicitSave);
  modalEl.addEventListener('keydown', e => {
    if ((e.ctrlKey || e.metaKey) && (e.key === 's' || e.key === 'S')) {
      e.preventDefault();
      handleExplicitSave();
    }
  });
  if (page.escape) {
    addEventListener('keydown', e => {
      if (e.key === 'Escape' && isSettingsOpen()) closeSettings();
    });
  }

  $('#settings-mode-ui').addEventListener('click', showSettingsUIView);
  $('#settings-mode-json').addEventListener('click', showSettingsJSONView);

  $('#settings-path').addEventListener('click', () => {
    if (!settingsData.path) return;
    navigator.clipboard?.writeText(settingsData.path);
    page.toast?.('✓', 'Copied settings path to clipboard');
  });

  const searchInput = $('#settings-search');
  searchInput.addEventListener('input', e => {
    filterQuery = e.target.value;
    renderSettingsList();
  });
  $('#settings-search-clear').addEventListener('click', () => {
    searchInput.value = '';
    filterQuery = '';
    renderSettingsList();
    searchInput.focus();
  });

  $('#settings-nav').addEventListener('click', e => {
    const btn = e.target.closest('.settings-nav-item');
    if (!btn) return;
    activeCategory = btn.dataset.cat;
    filterQuery = '';
    searchInput.value = '';
    renderSettingsNav();
    renderSettingsList();
  });

  const listEl = $('#settings-list');
  listEl.addEventListener('input', e => {
    const target = e.target;
    const key = target.dataset.key;
    if (!key) return;
    pendingChanges[key] = target.type === 'number' ? parseFloat(target.value) : target.value;
    setSaveStatus('unsaved', 'Unsaved changes');
  });

  listEl.addEventListener('change', e => {
    const target = e.target;
    const multi = target.closest('.settings-multiselect');
    if (multi) {
      const value = [...multi.querySelectorAll('input:checked')].map(i => i.value);
      handleSettingChange(multi.dataset.multiKey, value);
      return;
    }
    const key = target.dataset.key;
    if (!key) return;
    let value;
    if (target.type === 'checkbox') value = target.checked;
    else if (target.type === 'number') value = parseFloat(target.value);
    else value = target.value;
    handleSettingChange(key, value);
  });

  listEl.addEventListener('click', e => {
    const pill = e.target.closest('.settings-pill-tag');
    if (pill) {
      const key = pill.dataset.setKey;
      let value = pill.dataset.setVal;
      if (value === 'true') value = true;
      else if (value === 'false') value = false;
      else if (!isNaN(Number(value)) && value.trim() !== '') value = Number(value);
      if (key) handleSettingChange(key, value);
      return;
    }
    const resetBtn = e.target.closest('.settings-reset-btn');
    if (resetBtn) {
      handleResetSetting(resetBtn.dataset.reset);
      return;
    }
    const actionBtn = e.target.closest('.settings-card-action');
    if (actionBtn) page.cardActions?.[actionBtn.dataset.actionKey]?.run();
  });

  $('#btn-settings-save-raw').addEventListener('click', handleSaveRawSettings);
  $('#btn-settings-reset-raw').addEventListener('click', () => {
    $('#settings-raw-editor').value = settingsData.raw || '{\n}\n';
    $('#settings-raw-error').hidden = true;
  });
}
