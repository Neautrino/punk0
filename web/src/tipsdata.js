// web/src/tipsdata.js
// Every tip punk0 shows, on both pages (tips.js shows them). Each one is true
// of this version, names the UI as it is labelled, and fits on two lines.
//
//   id    stable: the rotation remembers tips by id (punk0.tips.seen).
//   page  'agents', 'editor' or 'both': where it is shown. Help › Tips on
//         either page lists every tip.
//   text  {Mod+K} is a key combination drawn as the reader's keyboard labels
//         it (state.js keyCaps); **x** is a UI label, in bold.
//   only  [key, value]: shown only while that setting has that value, so a
//         tip about turning something on goes quiet once it is on.
//   os    'mac' or 'other': shown only there.
//
// A new user-facing feature, shortcut or command gets a tip here.
export const TIPS = [
  // The agents page
  { id: 'a-persist', page: 'agents', text: 'Terminals keep running when you close this page. Open it again and they are still there.' },
  { id: 'a-enter', page: 'agents', text: 'With no tab open and nothing focused, **Enter** starts your default agent.' },
  { id: 'a-stop', page: 'agents', text: 'Right-click a running tab and pick **Stop**: it stays in the bar, and an agent picks its chat up when you open it again.' },
  { id: 'a-rename', page: 'agents', text: 'Double-click a terminal tab to rename it.' },
  { id: 'a-idle', page: 'agents', text: 'Quiet tabs stop after 30 minutes; a shell only at its prompt. **Stop Idle Terminal After (Minutes)** in Settings changes that.', only: ['terminal.idleStop', 30] },
  { id: 'a-chats', page: 'agents', text: '**Chats** on the right lists your Claude Code, Codex and OpenCode chats, even ones started outside punk0. Click one, then **Resume**.' },
  { id: 'a-removed', page: 'agents', text: 'Removed a chat by mistake? **⋯ › Show removed** in Chats lists it; click it, then **Restore**.' },
  { id: 'a-needyou', page: 'agents', text: 'Click the **need you** count in the status bar to go to the next agent waiting for you. The **working** count does the same for busy ones.' },
  { id: 'a-notify', page: 'agents', text: '**Desktop Notifications** in Settings can tell you when an agent needs you while you are in another window.', only: ['notifications.desktop', 'off'] },
  { id: 'a-shiftenter', page: 'agents', text: 'In Claude Code, {Shift+Enter} adds a new line instead of sending.' },
  { id: 'a-copy', page: 'agents', text: 'Selecting text in a terminal copies it, without the left margin agents draw.', only: ['terminal.copyOnSelect', true] },
  { id: 'a-clipkeys', page: 'agents', text: 'In a terminal, {Ctrl+C} copies when text is selected and is still ^C when not. {Ctrl+Shift+V} pastes.', os: 'other' },
  { id: 'a-links', page: 'agents', text: 'Click a link in terminal output to open it in a new browser tab.' },
  { id: 'a-panes', page: 'agents', text: '{Mod+B} hides the projects pane, {Mod+Alt+B} the right side. Click outside the terminal first.' },
  { id: 'a-border', page: 'agents', text: 'Double-click a pane border to reset it.' },
  { id: 'a-worktree', page: 'agents', text: 'In **New workspace…**, tick **Create a new worktree** to give a branch its own folder, so agents work side by side without touching each other\'s files.' },
  { id: 'a-scratch', page: 'agents', text: '**Scratch** at the top of the sidebar is for work without a repository: each chat gets a folder of its own.' },
  { id: 'a-allchanges', page: 'agents', text: 'Click **± All changes** above the changed files to read every change as one diff.' },
  { id: 'a-stage', page: 'agents', text: 'Tick a changed file in the files pane to stage it. The branch bar below opens the editor\'s git panel to commit.' },
  { id: 'a-resources', page: 'agents', text: 'Click the terminal, agent and RAM counts in the status bar to see **Resources**: CPU and memory for each workspace and terminal.' },
  { id: 'a-pr', page: 'agents', text: 'Paste a pull request URL into **Add project** to open it for review.' },
  { id: 'a-agents', page: 'agents', text: '**Default Agent** in Settings sets what Enter and the first welcome button start. **Hidden Agents** keeps agents out of the + menu.' },
  { id: 'a-editor', page: 'agents', text: '**Open in editor** opens this workspace with search, go to definition and the git panel.' },

  // The editor
  { id: 'e-quickedit', page: 'editor', text: 'Select code and press {Mod+K} to describe a change. **Quick Edit** has your agent write it; {Mod+Enter} accepts it.' },
  { id: 'e-addchat', page: 'editor', text: 'Select code and press {Alt+L} to type it, with its path and lines, into the agent in the Terminal panel.' },
  { id: 'e-copyctx', page: 'editor', text: '{Alt+A} copies the selection with its path and line numbers, ready to paste to any agent. {Alt+C} copies just the reference.' },
  { id: 'e-rightclick', page: 'editor', text: 'Right-click a selection for Quick Edit, Add to Chat, Copy Ref, Copy with Context and Find Usages.' },
  { id: 'e-diff', page: 'editor', text: '{Mod+D} switches the open file between its source and its diff against HEAD.' },
  { id: 'e-hunks', page: 'editor', text: 'In a diff, **]c** and **[c** jump between changes, **]f** and **[f** between changed files.' },
  { id: 'e-edit', page: 'editor', text: '{Mod+E} edits the open file right here; {Mod+S} saves it.' },
  { id: 'e-calls', page: 'editor', text: '{Alt+Shift+H} on a name shows its call trail: who calls it and what it calls. It needs a language server.' },
  { id: 'e-usages', page: 'editor', text: 'Select a name and press {Alt+U} to find every place it is used.' },
  { id: 'e-back', page: 'editor', text: '{Alt+Left} takes you back to where you were before a jump, {Alt+Right} forward again.' },
  { id: 'e-reopen', page: 'editor', text: '{Alt+Shift+T} reopens the tab you just closed.' },
  { id: 'e-tabs', page: 'editor', text: '{Alt+1} to {Alt+9} pick a tab; {Ctrl+Tab} and {Ctrl+Shift+Tab} step through them.' },
  { id: 'e-occur', page: 'editor', text: 'Double-click a word to highlight every place it appears. {Esc} clears it.' },
  { id: 'e-preview', page: 'editor', text: '{Alt+M} switches Markdown, CSV and TSV files between rendered and source.' },
  { id: 'e-mermaid', page: 'editor', text: 'Double-click a Mermaid diagram to switch between fit and 100%.' },
  { id: 'e-image', page: 'editor', text: 'In an image, **0** fits it, **1** shows actual size, **b** changes the background and **p** turns smoothing off.' },
  { id: 'e-terminal', page: 'editor', text: '{Alt+T} jumps to the Terminal panel; {Mod+J} hides or shows the right side.' },
  { id: 'e-vim', page: 'editor', text: 'Prefer Vim keys? Run **Preferences: Toggle Vim Keybindings** from the command palette.', only: ['editor.vimMode', false] },
  { id: 'e-theme', page: 'editor', text: '**Select Theme…** in the command palette previews each theme as you arrow through them.' },
  { id: 'e-reindex', page: 'editor', text: '{Mod+Shift+R} re-indexes the workspace after files change outside punk0.' },
  { id: 'e-commit', page: 'editor', text: 'In the git panel, {Mod+Enter} in the message box commits: the staged files, or everything if nothing is staged.' },
  { id: 'e-lsp', page: 'editor', text: 'Click **LSP** in the status bar to start, stop or install language servers.' },
  { id: 'e-pr', page: 'editor', text: 'In a pull request review, **Batch Apply** hands your line comments to your agent to apply.' },

  // Both pages
  { id: 'b-settings', page: 'both', text: '{Mod+,} opens Settings. **JSON** at its top edits settings.json directly.' },
  { id: 'b-help', page: 'both', text: 'Every tip is listed under **Tips** in Help: the **?** button at the bottom left.' },
];
