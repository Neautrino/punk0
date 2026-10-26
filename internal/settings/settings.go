package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// punk0 keeps no state inside a workspace. The user's preferences and choices
// are stored with the other per-user files punk0 writes (~/.punk0/settings.json),
// never in the working tree.

// Settings stores user configuration written to ~/.punk0/settings.json (or XDG_CONFIG_HOME/punk0/settings.json).
// All settings are optional pointers so omitted values fall back to application defaults.
type Settings struct {
	Agent  string            `json:"agent,omitempty"`
	Models map[string]string `json:"models,omitempty"`

	EditorFontSize              *float64 `json:"editor.fontSize,omitempty"`
	EditorFontFamily            *string  `json:"editor.fontFamily,omitempty"`
	EditorLineHeight            *float64 `json:"editor.lineHeight,omitempty"`
	EditorTabSize               *int     `json:"editor.tabSize,omitempty"`
	EditorWordWrap              *string  `json:"editor.wordWrap,omitempty"`
	EditorLineNumbers           *string  `json:"editor.lineNumbers,omitempty"`
	EditorVimMode               *bool    `json:"editor.vimMode,omitempty"`
	EditorRenderWhitespace      *string  `json:"editor.renderWhitespace,omitempty"`
	EditorMinimapEnabled        *bool    `json:"editor.minimap.enabled,omitempty"`
	WorkbenchColorTheme         *string  `json:"workbench.colorTheme,omitempty"`
	DiffEditorRenderSideBySide  *bool    `json:"diffEditor.renderSideBySide,omitempty"`
	MarkdownPreviewOpen         *bool    `json:"markdown.preview.open,omitempty"`
	TablePreviewOpen            *bool    `json:"table.preview.open,omitempty"`
	TelemetryEnabled            *bool    `json:"telemetry.enabled,omitempty"`
	GitHubToken                 *string  `json:"github.token,omitempty"`
	GitLabToken                 *string  `json:"gitlab.token,omitempty"`
	GitLabHost                  *string  `json:"gitlab.host,omitempty"`
	GiteaToken                  *string  `json:"gitea.token,omitempty"`
	GiteaHost                   *string  `json:"gitea.host,omitempty"`
	BitbucketToken              *string  `json:"bitbucket.token,omitempty"`
	BitbucketEmail              *string  `json:"bitbucket.email,omitempty"`
	GitCommitMessageInstruction *string  `json:"git.commitMessageInstruction,omitempty"`
	ServerBasePath              *string  `json:"server.basePath,omitempty"`
	WorkspaceIdleStop           *float64 `json:"workspace.idleStop,omitempty"`
	TerminalIdleStop            *float64 `json:"terminal.idleStop,omitempty"`
	ExplorerAutoReveal          *bool    `json:"explorer.autoReveal,omitempty"`
	TipsEnabled                 *bool    `json:"tips.enabled,omitempty"`
	LSPEnabled                  *bool    `json:"lsp.enabled,omitempty"`
	LSPIdleStop                 *float64 `json:"lsp.idleStop,omitempty"`
	LSPDisabled                 []string `json:"lsp.disabled,omitempty"`
}

// lspIdleStopDefault is lsp.idleStop's default, in minutes.
const lspIdleStopDefault = 15.0

// LSPOn is the lsp.enabled master switch, on unless turned off.
func (s Settings) LSPOn() bool { return s.LSPEnabled == nil || *s.LSPEnabled }

// LSPIdleStopAfter is how long a language server may go unused before it is
// stopped; 0 never stops one.
func (s Settings) LSPIdleStopAfter() time.Duration {
	minutes := lspIdleStopDefault
	if s.LSPIdleStop != nil && *s.LSPIdleStop >= 0 {
		minutes = *s.LSPIdleStop
	}
	return time.Duration(minutes * float64(time.Minute))
}

// LSPDisabledSet is the set of servers lsp.disabled keeps from ever starting.
func (s Settings) LSPDisabledSet() map[string]bool {
	set := make(map[string]bool, len(s.LSPDisabled))
	for _, name := range s.LSPDisabled {
		set[name] = true
	}
	return set
}

// LSPServerNames lists every language server the root package's registry
// knows, in registry order, for the lsp.disabled choices. The root package
// sets it at init; until then the setting offers no choices.
var LSPServerNames = func() []string { return nil }

var settingsMu sync.Mutex

// Path mirrors stateFilePath in internal/update: honour the XDG location when
// it is set, otherwise fall back to ~/.punk0.
func Path() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "punk0", "settings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".punk0", "settings.json")
}

// SchemaItem describes a configurable setting for dynamic rendering in the settings modal.
type SchemaItem struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Type        string   `json:"type"` // "string", "number", "boolean", "select"
	Default     any      `json:"default"`
	Options     []string `json:"options,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Step        *float64 `json:"step,omitempty"`
	Secret      bool     `json:"secret,omitempty"` // render as a masked input; still returned in plaintext by /api/settings, same trust model as every other local setting
	// Exec marks a value that ends up in a command line or a process
	// environment. It can only be changed while punk0 listens on loopback:
	// in remote mode anyone reaching the port could otherwise plant a command
	// that runs the next time a local tab or agent edit starts.
	Exec bool `json:"exec,omitempty"`
}

func numPtr(v float64) *float64 { return &v }

// The modal lists categories in the order they first appear here, after
// Commonly Used: punk0's own sections first, then px0's.
var Schema = []SchemaItem{
	{
		Key:         "general.openBrowser",
		Title:       "Open Browser on Start",
		Description: "Open the agents page in the browser when punk0 starts. The -no-open flag turns it off regardless. Opening a folder (punk0 <dir>) still opens the agents page on it.",
		Category:    "General",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "update.auto",
		Title:       "Update Automatically",
		Description: "Check for a newer punk0 release in the background on start and install it. The -no-update flag turns it off regardless.",
		Category:    "General",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "tips.enabled",
		Title:       "Show Tips in Interface",
		Description: "Controls whether punk0 periodically shows helpful tips and tricks in the interface.",
		Category:    "General",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "workbench.colorTheme",
		Title:       "Color Theme",
		Description: "The color theme of the agents page and the editor.",
		Category:    "Appearance",
		Type:        "select",
		Default:     "catppuccin-mocha",
		Options: []string{
			"catppuccin-mocha", "catppuccin-latte",
			"github-dark", "dark", "light",
			"dracula", "gruvbox-dark", "gruvbox-light",
			"monokai", "nord", "one-dark", "rose-pine",
			"solarized-dark", "solarized-light",
		},
	},
	{
		Key:         "workbench.uiFontFamily",
		Title:       "Interface Font Family",
		Description: "The font of menus, lists and labels on both pages. Empty uses the theme's font.",
		Category:    "Appearance",
		Type:        "string",
		Default:     "",
	},
	{
		Key:         "layout.fileView",
		Title:       "File View",
		Description: "Where files open on the agents page. beside shows them in a panel of their own right of the terminals; tabs puts them among the terminal tabs. beside uses tabs while terminals and open files have less than 640 px between them (the files pane not counted).",
		Category:    "Appearance",
		Type:        "select",
		Default:     "beside",
		Options:     []string{"beside", "tabs"},
	},
	{
		Key:         "agents.default",
		Title:       "Default Agent",
		Description: "The agent New chat and New workspace start with until you pick another, listed first in the + menu and on the welcome screen. An agent that is not installed or is hidden falls back to the first installed one.",
		Category:    "Agents",
		Type:        "select",
		Default:     "claude",
		Options:     []string{"claude", "codex", "opencode", "agy", "cursor-agent", "shell"},
	},
	{
		Key:         "agents.hidden",
		Title:       "Hidden Agents",
		Description: "Agents left out of the + menu, the welcome screen, New chat and New workspace. Tabs already running them keep working.",
		Category:    "Agents",
		Type:        "multiselect",
		Default:     []string{},
		Options:     []string{"claude", "codex", "opencode", "agy", "cursor-agent", "shell"},
	},
	{
		Key:         "agents.claude.args",
		Title:       "Claude Code Arguments",
		Description: "Extra arguments for every Claude Code tab, placed right after the program name (e.g. --model opus). Shell syntax applies.",
		Category:    "Agents",
		Type:        "string",
		Default:     "",
		Exec:        true,
	},
	{
		Key:         "agents.codex.args",
		Title:       "Codex Arguments",
		Description: "Extra arguments for every Codex tab, placed right after the program name. Shell syntax applies.",
		Category:    "Agents",
		Type:        "string",
		Default:     "",
		Exec:        true,
	},
	{
		Key:         "agents.opencode.args",
		Title:       "OpenCode Arguments",
		Description: "Extra arguments for every OpenCode tab, placed right after the program name. Shell syntax applies.",
		Category:    "Agents",
		Type:        "string",
		Default:     "",
		Exec:        true,
	},
	{
		Key:         "agents.agy.args",
		Title:       "Antigravity Arguments",
		Description: "Extra arguments for every Antigravity tab, placed right after the program name. Shell syntax applies.",
		Category:    "Agents",
		Type:        "string",
		Default:     "",
		Exec:        true,
	},
	{
		Key:         "agents.cursor-agent.args",
		Title:       "Cursor Agent Arguments",
		Description: "Extra arguments for every Cursor Agent tab, placed right after the program name. Shell syntax applies.",
		Category:    "Agents",
		Type:        "string",
		Default:     "",
		Exec:        true,
	},
	{
		Key:         "terminal.fontSize",
		Title:       "Font Size",
		Description: "The font size of terminal tabs, in pixels.",
		Category:    "Terminal",
		Type:        "number",
		Default:     13.0,
		Min:         numPtr(8.0),
		Max:         numPtr(32.0),
		Step:        numPtr(0.5),
	},
	{
		Key:         "terminal.fontFamily",
		Title:       "Font Family",
		Description: "The font of terminal tabs. Empty uses the code font.",
		Category:    "Terminal",
		Type:        "string",
		Default:     "",
	},
	{
		Key:         "terminal.lineHeight",
		Title:       "Line Height",
		Description: "The line height of terminal tabs, as a multiple of the font size.",
		Category:    "Terminal",
		Type:        "number",
		Default:     1.0,
		Min:         numPtr(1.0),
		Max:         numPtr(2.0),
		Step:        numPtr(0.05),
	},
	{
		Key:         "terminal.scrollback",
		Title:       "Scrollback",
		Description: "Lines a terminal tab keeps for scrolling back in the page.",
		Category:    "Terminal",
		Type:        "number",
		Default:     10000.0,
		Min:         numPtr(1000.0),
		Max:         numPtr(100000.0),
		Step:        numPtr(1000.0),
	},
	{
		Key:         "terminal.cursorStyle",
		Title:       "Cursor Style",
		Description: "The shape of the terminal cursor.",
		Category:    "Terminal",
		Type:        "select",
		Default:     "block",
		Options:     []string{"block", "bar", "underline"},
	},
	{
		Key:         "terminal.cursorBlink",
		Title:       "Cursor Blinking",
		Description: "Whether the terminal cursor blinks.",
		Category:    "Terminal",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "terminal.copyOnSelect",
		Title:       "Copy on Select",
		Description: "Copy text to the clipboard as soon as it is selected with the mouse. Needs the page on localhost or https.",
		Category:    "Terminal",
		Type:        "boolean",
		Default:     false,
	},
	{
		Key:         "terminal.confirmCloseRunning",
		Title:       "Confirm Closing a Running Tab",
		Description: "Ask before closing a tab whose shell or agent is still running.",
		Category:    "Terminal",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "terminal.shell",
		Title:       "Shell",
		Description: "The program new tabs run. Empty uses $SHELL, else /bin/sh; on Windows PowerShell 7, else Windows PowerShell, else cmd. Applies to tabs started afterwards.",
		Category:    "Terminal",
		Type:        "string",
		Default:     "",
		Exec:        true,
	},
	{
		Key:         "terminal.env",
		Title:       "Environment",
		Description: "KEY=VALUE per line, added to every tab's environment (shells and agents). A value starting with ~/ starts in your home folder. Blank lines and lines starting with # are ignored.",
		Category:    "Terminal",
		Type:        "textarea",
		Default:     "",
		Exec:        true,
	},
	{
		Key:         "terminal.idleStop",
		Title:       "Stop Idle Terminal After (Minutes)",
		Description: "Controls how long a terminal tab stays open with nothing typed or printed in it. A shell is only stopped at its prompt; an agent is stopped like any other quiet tab, except a Claude that says it is working or waiting for an answer, and picks its chat up again when reopened. 0 never stops them.",
		Category:    "Terminal",
		Type:        "number",
		Default:     30.0,
		Min:         numPtr(0),
		Max:         numPtr(1440.0),
		Step:        numPtr(1.0),
	},
	{
		Key:         "workspace.branchPrefix",
		Title:       "Branch Prefix",
		Description: "Put in front of the branch a new worktree workspace makes from its name (e.g. you/ turns login into you/login). The folder keeps the plain name. Existing branches and pull requests are left as they are.",
		Category:    "Workspaces",
		Type:        "string",
		Default:     "",
	},
	{
		Key:         "workspace.fetchBase",
		Title:       "Fetch Base Before Creating",
		Description: "Fetch the base branch from its remote before creating a worktree. Off uses the branches you already have.",
		Category:    "Workspaces",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "workspace.deleteMergedBranch",
		Title:       "Delete Merged Branch with Worktree",
		Description: "When a worktree workspace is deleted, also delete its branch if it is merged. An unmerged branch is kept.",
		Category:    "Workspaces",
		Type:        "boolean",
		Default:     false,
	},
	{
		Key:         "workspace.idleStop",
		Title:       "Stop Idle Workspace After (Minutes)",
		Description: "Controls how long a workspace's files keep being served after the last request before punk0 stops it to free memory; the next request starts it again. 0 never stops it.",
		Category:    "Workspaces",
		Type:        "number",
		Default:     5.0,
		Min:         numPtr(0),
		Max:         numPtr(1440.0),
		Step:        numPtr(1.0),
	},
	{
		Key:         "notifications.desktop",
		Title:       "Desktop Notifications",
		Description: "Show a browser notification when an agent needs you or finishes. Unfocused only notifies while the agents page is in the background. Only agents that report their status (Claude Code) notify, and only while the agents page is open.",
		Category:    "Notifications",
		Type:        "select",
		Default:     "off",
		Options:     []string{"off", "unfocused", "always"},
	},
	{
		Key:         "notifications.onWaiting",
		Title:       "When an Agent Needs You",
		Description: "Notify when an agent waits for your input or permission.",
		Category:    "Notifications",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "notifications.onDone",
		Title:       "When an Agent Finishes",
		Description: "Notify when an agent stops working and is back at its prompt.",
		Category:    "Notifications",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "notifications.sound",
		Title:       "Sound",
		Description: "Play a short tone with each notification.",
		Category:    "Notifications",
		Type:        "boolean",
		Default:     false,
	},
	{
		Key:         "notifications.titleBadge",
		Title:       "Count in Page Title",
		Description: "Show how many agents need you in the agents page's title, e.g. (2) punk0.",
		Category:    "Notifications",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "editor.fontSize",
		Title:       "Font Size",
		Description: "Controls the font size in pixels for the code viewer.",
		Category:    "Text Editor",
		Type:        "number",
		Default:     13.5,
		Min:         numPtr(9.0),
		Max:         numPtr(32.0),
		Step:        numPtr(0.5),
	},
	{
		Key:         "editor.fontFamily",
		Title:       "Font Family",
		Description: "Controls the font family used in the code viewer.",
		Category:    "Text Editor",
		Type:        "string",
		Default:     `"JetBrains Mono", "Fira Code", "Cascadia Code", "SF Mono", Menlo, Consolas, ui-monospace, monospace`,
	},
	{
		Key:         "editor.lineHeight",
		Title:       "Line Height",
		Description: "Controls the line height in pixels for the code viewer.",
		Category:    "Text Editor",
		Type:        "number",
		Default:     21.0,
		Min:         numPtr(14.0),
		Max:         numPtr(48.0),
		Step:        numPtr(1.0),
	},
	{
		Key:         "editor.tabSize",
		Title:       "Tab Size",
		Description: "The number of spaces a tab is equal to.",
		Category:    "Text Editor",
		Type:        "select",
		Default:     4,
		Options:     []string{"2", "4", "8"},
	},
	{
		Key:         "editor.wordWrap",
		Title:       "Word Wrap",
		Description: "Controls whether lines should wrap around or scroll horizontally.",
		Category:    "Text Editor",
		Type:        "select",
		Default:     "on",
		Options:     []string{"on", "off"},
	},
	{
		Key:         "editor.lineNumbers",
		Title:       "Line Numbers",
		Description: "Controls the display of line numbers in the gutter.",
		Category:    "Text Editor",
		Type:        "select",
		Default:     "on",
		Options:     []string{"on", "off"},
	},
	{
		Key:         "editor.vimMode",
		Title:       "Vim Keybindings",
		Description: "Enable Vim modal navigation (Normal mode, Visual mode, motions, search, and LSP shortcuts).",
		Category:    "Text Editor",
		Type:        "boolean",
		Default:     false,
	},
	{
		Key:         "editor.renderWhitespace",
		Title:       "Render Whitespace",
		Description: "Controls how whitespace characters are rendered in the viewer.",
		Category:    "Text Editor",
		Type:        "select",
		Default:     "selection",
		Options:     []string{"none", "boundary", "selection", "all"},
	},
	{
		Key:         "editor.minimap.enabled",
		Title:       "Minimap Hits",
		Description: "Controls whether search hit indicators are shown in the scroll minimap gutter.",
		Category:    "Text Editor",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "diffEditor.renderSideBySide",
		Title:       "Diff Side By Side",
		Description: "Controls whether the diff editor shows changes in split (side-by-side) or unified mode.",
		Category:    "Workbench",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "markdown.preview.open",
		Title:       "Markdown Preview",
		Description: "Controls whether Markdown files open in rendered preview by default.",
		Category:    "Workbench",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "table.preview.open",
		Title:       "Table View",
		Description: "Controls whether CSV and TSV files open as a table by default.",
		Category:    "Workbench",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "editor.cursorStyle",
		Title:       "Cursor Style",
		Description: "Controls the cursor style in the code viewer.",
		Category:    "Text Editor",
		Type:        "select",
		Default:     "line",
		Options:     []string{"line", "block", "underline"},
	},
	{
		Key:         "editor.cursorBlinking",
		Title:       "Cursor Blinking",
		Description: "Controls the cursor animation style.",
		Category:    "Text Editor",
		Type:        "select",
		Default:     "smooth",
		Options:     []string{"blink", "smooth", "solid"},
	},
	{
		Key:         "editor.renderLineHighlight",
		Title:       "Render Line Highlight",
		Description: "Controls how the editor should render the current line highlight.",
		Category:    "Text Editor",
		Type:        "select",
		Default:     "line",
		Options:     []string{"line", "none"},
	},
	{
		Key:         "editor.occurrencesHighlight",
		Title:       "Occurrences Highlight",
		Description: "Controls whether the editor should highlight occurrences of the selected word.",
		Category:    "Text Editor",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "editor.scrollBeyondLastLine",
		Title:       "Scroll Beyond Last Line",
		Description: "Controls whether the editor will scroll beyond the last line of the file.",
		Category:    "Text Editor",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "editor.bracketPairColorization",
		Title:       "Bracket Pair Colorization",
		Description: "Controls whether bracket pair colorization and matching is enabled.",
		Category:    "Text Editor",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "explorer.compactFolders",
		Title:       "Compact Folders",
		Description: "Controls whether the file tree renders single-child directory chains compactly.",
		Category:    "Files & Explorer",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "explorer.autoReveal",
		Title:       "Auto Reveal",
		Description: "Controls whether the file explorer automatically scrolls to and reveals active tabs.",
		Category:    "Files & Explorer",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "files.exclude",
		Title:       "Files Exclude Patterns",
		Description: "Configure glob patterns for excluding files and folders from search and trees.",
		Category:    "Files & Explorer",
		Type:        "string",
		Default:     "**/.git, **/node_modules, **/target, **/.DS_Store",
	},
	{
		Key:         "search.smartCase",
		Title:       "Smart Case Search",
		Description: "Searches case-insensitively when query is lowercase, and case-sensitively when uppercase characters exist.",
		Category:    "Search",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "search.maxResults",
		Title:       "Max Search Results",
		Description: "Controls the maximum number of results returned in workspace-wide searches.",
		Category:    "Search",
		Type:        "number",
		Default:     1000.0,
		Min:         numPtr(50.0),
		Max:         numPtr(10000.0),
		Step:        numPtr(50.0),
	},
	{
		Key:         "diffEditor.ignoreTrimWhitespace",
		Title:       "Diff: Ignore Trim Whitespace",
		Description: "Controls whether the diff viewer ignores changes in leading or trailing whitespace.",
		Category:    "Git & Diff",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "git.gutterIndicators",
		Title:       "Git Gutter Indicators",
		Description: "Controls whether changed line indicators are shown in the editor gutter.",
		Category:    "Git & Diff",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "lsp.enabled",
		Title:       "Language Servers",
		Description: "Master switch. Off stops every running language server in every folder and starts none; each folder's own choice is kept for when you turn this back on.",
		Category:    "Language Servers",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "lsp.idleStop",
		Title:       "Idle Stop (minutes)",
		Description: "Stop a language server nobody has used for this many minutes, even while its editor is open. The next use starts it again. 0 never stops it.",
		Category:    "Language Servers",
		Type:        "number",
		Default:     lspIdleStopDefault,
		Min:         numPtr(0),
		Max:         numPtr(1440.0),
		Step:        numPtr(1.0),
	},
	{
		Key:         "lsp.disabled",
		Title:       "Disabled Servers",
		Description: "Language servers punk0 never starts. Another installed server for the same language is used instead.",
		Category:    "Language Servers",
		Type:        "multiselect",
		Default:     []string{},
		// Options are filled in by SchemaNow from the running registry.
	},
	{
		Key:         "lsp.memoryWarning",
		Title:       "Memory Warning (MB)",
		Description: "On the agents page, show the language server memory total in red when all folders together use more than this. 0 never warns.",
		Category:    "Language Servers",
		Type:        "number",
		Default:     2048.0,
		Min:         numPtr(0),
		Step:        numPtr(256.0),
	},
	{
		Key:         "lsp.hover.enabled",
		Title:       "Hover Documentation",
		Description: "Controls whether hovercards with documentation and type signatures appear on hover.",
		Category:    "Language Servers",
		Type:        "boolean",
		Default:     true,
	},
	{
		Key:         "workspace.lspDefault",
		Title:       "Language Servers for New Worktrees",
		Description: "The default of the New workspace form's Language Servers checkbox for worktrees. Each project then remembers your last choice.",
		Category:    "Language Servers",
		Type:        "boolean",
		Default:     false,
	},
	{
		Key:         "agent.harness",
		Title:       "Coding Harness",
		Description: "Coding agent harness invoked for code edits (e.g. claude, gemini, cursor-agent, agy, opencode, codex, aider, goose).",
		Category:    "Agent / AI",
		Type:        "string",
		Default:     "",
		Exec:        true,
	},
	{
		Key:         "agent.timeoutSeconds",
		Title:       "Agent Timeout (Seconds)",
		Description: "Controls the maximum execution time in seconds for agent edits before canceling.",
		Category:    "Agent / AI",
		Type:        "number",
		Default:     600.0,
		Min:         numPtr(10.0),
		Max:         numPtr(600.0),
		Step:        numPtr(10.0),
	},
	{
		Key:         "agent.autoAcceptEdits",
		Title:       "Auto Accept Agent Edits",
		Description: "Controls whether agent-generated code diffs are accepted without manual confirmation.",
		Category:    "Agent / AI",
		Type:        "boolean",
		Default:     false,
	},
	{
		Key:         "git.commitMessageInstruction",
		Title:       "Commit Message Instructions",
		Description: "Extra instructions given to the coding harness when it writes a pull request's merge or squash commit message (e.g. \"Follow Conventional Commits\" or \"Reference the ticket number in the branch name\").",
		Category:    "Git & Diff",
		Type:        "textarea",
		Default:     "",
	},
	{
		Key:         "github.token",
		Title:       "GitHub Token",
		Description: "Personal access token used to check out and review pull requests (punk0 <url>). Takes precedence over the GITHUB_TOKEN environment variable and 'gh auth token'.",
		Category:    "GitHub",
		Type:        "string",
		Default:     "",
		Secret:      true,
	},
	{
		Key:         "gitlab.token",
		Title:       "GitLab Token",
		Description: "Personal access token (api scope) used to check out, review and merge GitLab merge requests, on gitlab.com or a self-hosted instance. Sent only to the GitLab Host below; other self-hosted instances use the token 'glab auth login' stored for them. Takes precedence over the GITLAB_TOKEN environment variable.",
		Category:    "GitLab",
		Type:        "string",
		Default:     "",
		Secret:      true,
	},
	{
		Key:         "gitlab.host",
		Title:       "GitLab Host",
		Description: "The GitLab instance the GitLab token belongs to (e.g. gitlab.example.com). Merge requests on any other host are opened without it. Falls back to GITLAB_HOST, then gitlab.com.",
		Category:    "GitLab",
		Type:        "string",
		Default:     "",
	},
	{
		Key:         "gitea.token",
		Title:       "Gitea Token",
		Description: "Access token (repository and issue read/write scopes) for Gitea, Forgejo or Codeberg pull request review. Sent only to the Gitea Host below. Takes precedence over the GITEA_TOKEN environment variable.",
		Category:    "Gitea",
		Type:        "string",
		Default:     "",
		Secret:      true,
	},
	{
		Key:         "gitea.host",
		Title:       "Gitea Host",
		Description: "The Gitea or Forgejo instance the Gitea token belongs to (e.g. gitea.example.com). Pull requests on any other host are opened without it. Falls back to GITEA_HOST, then codeberg.org.",
		Category:    "Gitea",
		Type:        "string",
		Default:     "",
	},
	{
		Key:         "bitbucket.token",
		Title:       "Bitbucket Token",
		Description: "Bitbucket Cloud token for pull request review: a repository, project or workspace access token, or an Atlassian API token (then also set Bitbucket Email). Takes precedence over the BITBUCKET_TOKEN environment variable.",
		Category:    "Bitbucket",
		Type:        "string",
		Default:     "",
		Secret:      true,
	},
	{
		Key:         "bitbucket.email",
		Title:       "Bitbucket Email",
		Description: "The Atlassian account email an API token belongs to. Leave empty when Bitbucket Token is an access token. Falls back to BITBUCKET_EMAIL. Checking push access (and so Approve and Merge) needs an API token.",
		Category:    "Bitbucket",
		Type:        "string",
		Default:     "",
	},
	{
		Key:         "server.basePath",
		Title:       "Base Path",
		Description: "Base URL path prefix for the punk0 server and web interface (e.g. /rev-123/).",
		Category:    "Server",
		Type:        "string",
		Default:     "/",
	},
}

func DefaultMap() map[string]any {
	res := make(map[string]any, len(Schema)+3)
	for _, item := range Schema {
		res[item.Key] = item.Default
	}
	res["explorer.autoRelveal"] = true
	res["agent"] = ""
	res["models"] = map[string]string{}
	return res
}

// SettingOr returns key's stored value, or its schema default when the file
// has none or holds a value of another type. JSON numbers are float64,
// arrays []any.
func SettingOr[T any](key string) T {
	settingsMu.Lock()
	raw := ReadRawMap()
	settingsMu.Unlock()
	if v, ok := raw[key].(T); ok {
		return v
	}
	var zero T
	for _, it := range Schema {
		if it.Key == key {
			if d, ok := it.Default.(T); ok {
				return d
			}
			break
		}
	}
	return zero
}

// ParseEnvLines reads terminal.env: KEY=VALUE per line, blank lines and
// lines starting with # skipped. A value starting with ~/ is expanded to the
// home folder, since nothing else expands an environment value.
func ParseEnvLines(text string) ([]string, error) {
	var out []string
	home, _ := os.UserHomeDir()
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("line %d: want KEY=VALUE, got %q", i+1, line)
		}
		if strings.HasPrefix(v, "~/") && home != "" {
			v = filepath.Join(home, v[2:])
		}
		out = append(out, k+"="+v)
	}
	return out, nil
}

// ReadRawMap returns the raw JSON contents unmarshaled into a map.
// It never fails: a missing or corrupt file returns an empty map.
func ReadRawMap() map[string]any {
	p := Path()
	if p == "" {
		return map[string]any{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// Read never fails: a missing or corrupt file simply means no choice
// has been made yet, which is the same as a fresh install.
func Read() Settings {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return readSettingsLocked()
}

func readSettingsLocked() Settings {
	var s Settings
	p := Path()
	if p == "" {
		return s
	}
	data, err := os.ReadFile(p)
	if err != nil || len(data) == 0 {
		return s
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil || len(raw) == 0 {
		return s
	}

	// Unmarshal known typed fields directly from data without re-marshaling raw
	_ = json.Unmarshal(data, &s)

	// Bi-directional bridge between agent <-> agent.harness
	if s.Agent == "" {
		if h, ok := raw["agent.harness"].(string); ok && h != "" {
			s.Agent = h
		}
	}
	// Support server.basePath and basePath fallback
	if s.ServerBasePath == nil {
		if bp, ok := raw["server.basePath"].(string); ok && bp != "" {
			s.ServerBasePath = &bp
		} else if bp, ok := raw["basePath"].(string); ok && bp != "" {
			s.ServerBasePath = &bp
		}
	}
	// Bi-directional bridge between models <-> agent.models
	if s.Models == nil || len(s.Models) == 0 {
		if am, ok := raw["agent.models"].(map[string]any); ok {
			s.Models = make(map[string]string, len(am))
			for k, v := range am {
				if vs, ok := v.(string); ok {
					s.Models[k] = vs
				}
			}
		}
	}
	// Support explorer.autoReveal and explorer.autoRelveal
	if s.ExplorerAutoReveal == nil {
		if ar, ok := raw["explorer.autoReveal"].(bool); ok {
			s.ExplorerAutoReveal = &ar
		} else if ar, ok := raw["explorer.autoRelveal"].(bool); ok {
			s.ExplorerAutoReveal = &ar
		} else if ar, ok := raw["autoReveal"].(bool); ok {
			s.ExplorerAutoReveal = &ar
		} else if ar, ok := raw["autoRelveal"].(bool); ok {
			s.ExplorerAutoReveal = &ar
		}
	}

	return s
}

// ReadMergedMap returns all settings, overlaying stored settings onto defaults.
func ReadMergedMap() map[string]any {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	res := DefaultMap()
	raw := ReadRawMap()

	for k, v := range raw {
		res[k] = v
	}

	// Synchronize agent / agent.harness
	if ag, ok := raw["agent"].(string); ok && ag != "" {
		res["agent.harness"] = ag
	} else if ah, ok := raw["agent.harness"].(string); ok && ah != "" {
		res["agent"] = ah
	}

	// Synchronize models / agent.models
	if m, ok := raw["models"].(map[string]any); ok && len(m) > 0 {
		res["agent.models"] = m
	} else if am, ok := raw["agent.models"].(map[string]any); ok && len(am) > 0 {
		res["models"] = am
	}

	// Synchronize server.basePath / basePath
	if bp, ok := raw["server.basePath"].(string); ok && bp != "" {
		res["server.basePath"] = bp
	} else if bp, ok := raw["basePath"].(string); ok && bp != "" {
		res["server.basePath"] = bp
	}

	// Synchronize explorer.autoReveal / explorer.autoRelveal
	if ar, ok := raw["explorer.autoReveal"].(bool); ok {
		res["explorer.autoReveal"] = ar
		res["explorer.autoRelveal"] = ar
	} else if ar, ok := raw["explorer.autoRelveal"].(bool); ok {
		res["explorer.autoReveal"] = ar
		res["explorer.autoRelveal"] = ar
	} else if ar, ok := raw["autoReveal"].(bool); ok {
		res["explorer.autoReveal"] = ar
		res["explorer.autoRelveal"] = ar
	} else if ar, ok := raw["autoRelveal"].(bool); ok {
		res["explorer.autoReveal"] = ar
		res["explorer.autoRelveal"] = ar
	} else {
		res["explorer.autoReveal"] = true
		res["explorer.autoRelveal"] = true
	}

	return res
}

// ReadRawJSON returns formatted settings.json file content as string.
func ReadRawJSON() string {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	p := Path()
	if p == "" {
		return "{}\n"
	}
	data, err := os.ReadFile(p)
	if err != nil || len(data) == 0 {
		return "{\n}\n"
	}
	// Pretty format if possible
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err == nil {
		if formatted, err := json.MarshalIndent(raw, "", "  "); err == nil {
			return string(formatted) + "\n"
		}
	}
	return string(data)
}

// Write saves the agent and models choices while preserving other settings.
func Write(s Settings) error {
	p := Path()
	if p == "" {
		return errors.New("no home directory to save settings in")
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()

	raw := ReadRawMap()
	if s.Agent != "" {
		raw["agent"] = s.Agent
		raw["agent.harness"] = s.Agent
	} else {
		delete(raw, "agent")
		delete(raw, "agent.harness")
	}

	if s.Models != nil && len(s.Models) > 0 {
		raw["models"] = s.Models
		raw["agent.models"] = s.Models
	} else if s.Agent == "" {
		delete(raw, "models")
		delete(raw, "agent.models")
	}

	return writeRawMapLocked(p, raw)
}

// UpdateMap merges key-value pairs into settings.json without losing existing keys.
func UpdateMap(updates map[string]any) error {
	p := Path()
	if p == "" {
		return errors.New("no home directory to save settings in")
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()

	raw := ReadRawMap()
	for k, v := range updates {
		if v == nil {
			delete(raw, k)
		} else {
			raw[k] = v
		}

		// Keep agent / agent.harness in sync
		if k == "agent" {
			if v == nil || v == "" {
				delete(raw, "agent.harness")
			} else {
				raw["agent.harness"] = v
			}
		} else if k == "agent.harness" {
			if v == nil || v == "" {
				delete(raw, "agent")
			} else {
				raw["agent"] = v
			}
		}

		// Keep models / agent.models in sync
		if k == "models" {
			if v == nil {
				delete(raw, "agent.models")
			} else {
				raw["agent.models"] = v
			}
		} else if k == "agent.models" {
			if v == nil {
				delete(raw, "models")
			} else {
				raw["models"] = v
			}
		}

		// Keep server.basePath / basePath in sync
		if k == "server.basePath" {
			if v == nil || v == "" {
				delete(raw, "server.basePath")
				delete(raw, "basePath")
			} else {
				raw["server.basePath"] = v
			}
		} else if k == "basePath" {
			if v == nil || v == "" {
				delete(raw, "server.basePath")
				delete(raw, "basePath")
			} else {
				raw["server.basePath"] = v
				raw["basePath"] = v
			}
		}

		// Keep explorer.autoReveal / explorer.autoRelveal in sync
		if k == "explorer.autoReveal" || k == "explorer.autoRelveal" || k == "autoReveal" || k == "autoRelveal" {
			if v == nil {
				delete(raw, "explorer.autoReveal")
				delete(raw, "explorer.autoRelveal")
				delete(raw, "autoReveal")
				delete(raw, "autoRelveal")
			} else {
				raw["explorer.autoReveal"] = v
				raw["explorer.autoRelveal"] = v
			}
		}
	}

	return writeRawMapLocked(p, raw)
}

// SaveRawJSON parses and validates raw JSON text and writes it formatted.
func SaveRawJSON(rawJSON []byte) error {
	var m map[string]any
	if err := json.Unmarshal(rawJSON, &m); err != nil {
		return err
	}

	p := Path()
	if p == "" {
		return errors.New("no home directory to save settings in")
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()

	// Sync agent bridges if present
	if ag, ok := m["agent"].(string); ok && ag != "" {
		m["agent.harness"] = ag
	} else if ah, ok := m["agent.harness"].(string); ok && ah != "" {
		m["agent"] = ah
	}

	return writeRawMapLocked(p, m)
}

func writeRawMapLocked(p string, raw map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o644)
}

// withLiveOptions fills in the choices a static schema cannot hold:
// lsp.disabled lists the servers the registry knows at run time.
func withLiveOptions(it SchemaItem) SchemaItem {
	if it.Key == "lsp.disabled" {
		it.Options = LSPServerNames()
	}
	return it
}

// SchemaNow is Schema with those run-time choices filled in: it is what the
// settings modal renders and what a POST is validated against.
func SchemaNow() []SchemaItem {
	out := make([]SchemaItem, len(Schema))
	for i, it := range Schema {
		out[i] = withLiveOptions(it)
	}
	return out
}

func schemaItem(key string) (SchemaItem, bool) {
	for _, it := range Schema {
		if it.Key == key {
			return withLiveOptions(it), true
		}
	}
	return SchemaItem{}, false
}

// CoerceQuery types a query-string value by the key's schema, so ?key=value
// posts validate like JSON ones. Unparseable values stay strings and fail
// validation.
func CoerceQuery(key, v string) any {
	it, ok := schemaItem(key)
	if !ok {
		return v
	}
	switch it.Type {
	case "boolean":
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	case "number":
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	case "multiselect":
		out := []any{}
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return v
}

// Validate checks every schema key in m against its type, options and range.
// nil means removal and unknown keys are stored as given.
func Validate(m map[string]any) error {
	for k, v := range m {
		it, ok := schemaItem(k)
		if !ok || v == nil {
			continue
		}
		if err := validateSetting(it, v); err != nil {
			return fmt.Errorf("%s: %s", k, err)
		}
	}
	return nil
}

func validateSetting(it SchemaItem, v any) error {
	switch it.Type {
	case "boolean":
		if _, ok := v.(bool); !ok {
			return errors.New("want true or false")
		}
	case "number":
		f, ok := v.(float64)
		if !ok {
			return errors.New("want a number")
		}
		if it.Min != nil && f < *it.Min || it.Max != nil && f > *it.Max {
			return fmt.Errorf("%v is out of range %v..%v", f, deref(it.Min), deref(it.Max))
		}
	case "select":
		// Options are strings; a numeric select (editor.tabSize) stores numbers.
		var s string
		switch x := v.(type) {
		case string:
			s = x
		case float64:
			s = strconv.FormatFloat(x, 'f', -1, 64)
		}
		if !slices.Contains(it.Options, s) {
			return fmt.Errorf("want one of %s", strings.Join(it.Options, ", "))
		}
	case "multiselect":
		list, ok := v.([]any)
		if !ok {
			return errors.New("want a list")
		}
		for _, e := range list {
			if s, ok := e.(string); !ok || !slices.Contains(it.Options, s) {
				return fmt.Errorf("%v is not one of %s", e, strings.Join(it.Options, ", "))
			}
		}
	default: // string, textarea
		s, ok := v.(string)
		if !ok {
			return errors.New("want a string")
		}
		if it.Key == "terminal.env" {
			if _, err := ParseEnvLines(s); err != nil {
				return err
			}
		}
	}
	return nil
}

func deref(p *float64) any {
	if p == nil {
		return ""
	}
	return *p
}

// ChangedExecKey returns the first Exec key whose value in updates differs
// from the stored one (absent counts as the default). Removal and reset to
// the default are allowed: they only take a command away. "agent" is the
// older spelling of agent.harness.
func ChangedExecKey(updates, stored map[string]any) string {
	for k, v := range updates {
		key := k
		if k == "agent" {
			key = "agent.harness"
		}
		it, ok := schemaItem(key)
		if !ok || !it.Exec || v == nil || reflect.DeepEqual(v, it.Default) {
			continue
		}
		cur, has := stored[key]
		if key == "agent.harness" {
			if ag, ok := stored["agent"].(string); ok && ag != "" {
				cur, has = ag, true
			}
		}
		if !has {
			cur = it.Default
		}
		if !reflect.DeepEqual(v, cur) {
			return k
		}
	}
	return ""
}
