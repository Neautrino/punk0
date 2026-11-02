package main

// Agents typed at a tab's prompt report to punk0 as the ones punk0 starts do.
// Each tab's zsh, bash or fish reads the user's own startup files first and
// then punk0's agent functions: every agent of tabAgents becomes a shell
// function that runs the real program with punk0's reporting added
// (reportVia), and tells the tab when it starts and when it exits
// (`punk0 hook start|exit <agent>`). A function wins over PATH, so the user's
// rc files may order PATH as they like; nothing of the user's is edited. An
// alias of the user's still applies: it expands before the function runs.

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"px0/internal/settings"
	"px0/internal/ui"
)

// agentShellDir holds the startup files a tab's shell reads (writeAgentShell).
func agentShellDir() string { return filepath.Join(workbenchStateDir(), "shell") }

// reportDir is where one agent's reporting files go, beside the startup
// files a tab's shell reads (writeAgentShell).
func reportDir(kind string) string { return filepath.Join(agentShellDir(), kind) }

// hookLine is the command line that runs this punk0's `hook` with args,
// quoted for a POSIX shell; a -dev punk0's hook gets its -dev=<dir>.
func hookLine(exe string, args ...string) string {
	line := shellQuote(exe) + " hook"
	if a := devArg(); a != "" {
		line += " " + shellQuote(a)
	}
	for _, a := range args {
		line += " " + shellQuote(a)
	}
	return line
}

// codexHookEvents are the Codex events punk0 registers: the session id, and
// what the tab's status dot shows.
var codexHookEvents = []struct{ name, key string }{
	{"SessionStart", "session_start"},
	{"UserPromptSubmit", "user_prompt_submit"},
	{"PermissionRequest", "permission_request"},
	{"PostToolUse", "post_tool_use"},
	{"Stop", "stop"},
}

// codexHookArgs are the -c overrides that give one codex run punk0's hooks.
// Codex runs a hook only once its definition is trusted, and keeps that trust
// in hooks.state by a hash of the definition; a session flag may carry that
// state too, so these overrides trust exactly punk0's own hooks for this run.
// Hooks from anywhere else still need the user's review. The hash is Codex's
// own (codex-rs hooks/src/engine/discovery.rs hook_hash): sha256 of the
// normalized definition as compact JSON with sorted keys; a Codex that hashes
// differently skips the hooks, and the tab still learns start and exit.
func codexHookArgs(command string) []string {
	var args []string
	state := make([]string, 0, len(codexHookEvents))
	for _, ev := range codexHookEvents {
		args = append(args, "-c", "hooks."+ev.name+"=[{hooks=[{type=\"command\",command="+tomlString(command)+"}]}]")
		key := "/<session-flags>/config.toml:" + ev.key + ":0:0"
		state = append(state, tomlString(key)+"={trusted_hash="+tomlString(codexHookHash(ev.key, command))+"}")
	}
	return append(args, "-c", "hooks.state={"+strings.Join(state, ",")+"}")
}

// codexHookHash is the hash Codex trusts a command hook of event (its key
// label, e.g. "stop") by, with Codex's defaults filled in: no matcher, not
// async, a 600 s timeout.
func codexHookHash(event, command string) string {
	identity := map[string]any{
		"event_name": event,
		"hooks": []map[string]any{{
			"async": false, "command": command, "timeout": 600, "type": "command",
		}},
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // serde_json leaves <, > and & as they are
	enc.Encode(identity)
	sum := sha256.Sum256(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// tomlString is s as a TOML basic string; JSON's escapes are TOML's, but
// for the HTML ones JSON adds (<, > and & as \u003c...), which TOML reads the
// same but a reader of the command line would not.
func tomlString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// reportVia is one mechanism that gets punk0's reporting into a single run of
// an agent. setup writes that run's files, once at startup (writeAgentShell),
// and says what the agent's shell function must add: argv before the user's
// arguments, env for that one command. unhooked is what a tab adds instead
// where punk0 could not hook the shell: argv on the command line it writes
// (agentCommand) and env in the tab's environment (Satellite.Shell). unhooked
// reads no file, so a tab may ask before setup has run.
type reportVia interface {
	setup(exe, kind string) (argv, env []string, err error)
	unhooked(kind string) (argv, env []string)
}

// claudeSettings reports through the settings file a claude run takes on its
// command line (writeClaudeHooks).
type claudeSettings struct{}

func (v claudeSettings) setup(exe, kind string) (argv, env []string, err error) {
	if err := writeClaudeHooks(exe); err != nil {
		return nil, nil, err
	}
	argv, env = v.unhooked(kind)
	return argv, env, nil
}

func (claudeSettings) unhooked(string) (argv, env []string) {
	return []string{"--settings", claudeHooksPath()}, nil
}

// codexOverrides reports through the -c overrides one codex run is given
// (codexHookArgs), so punk0 writes no file for it. They stay in the shell
// function punk0 wrote, where they are quoted for a POSIX shell: punk0 puts
// no TOML on a Windows command line, and a codex tab there reports nothing.
type codexOverrides struct{}

func (codexOverrides) setup(exe, kind string) (argv, env []string, err error) {
	return codexHookArgs(hookLine(exe, kind)), nil, nil
}

func (codexOverrides) unhooked(string) (argv, env []string) { return nil, nil }

// opencodeVia writes the plugin one opencode run reports through. What a run
// needs to load it is the tab's own (opencodeEnv): those configs carry the
// user's own and the tab's project, so they are made per tab, not once here.
type opencodeVia struct{}

func (opencodeVia) setup(exe, kind string) (argv, env []string, err error) {
	for _, name := range []string{"server.js", "tui.js"} {
		if err := writeBytesAtomic(filepath.Join(opencodePluginDir(), name), []byte(opencodePlugin)); err != nil {
			return nil, nil, err
		}
	}
	return nil, nil, nil
}

func (opencodeVia) unhooked(string) (argv, env []string) { return nil, nil }

// qwenHookEvents are the qwen events punk0 registers. Not Notification: qwen
// 0.26.5 sends idle_prompt just before a permission prompt, and
// PermissionRequest already says the agent waits for the user.
var qwenHookEvents = []string{
	"SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse",
	"PostToolUse", "PermissionRequest", "Stop", "StopFailure",
}

// qwenDefaults reports through the lowest of qwen's settings layers, a file
// QWEN_CODE_SYSTEM_DEFAULTS_PATH names for one run: qwen adds its hooks to
// the user's and lets every other setting of the user's win. The file is
// alone in its folder because qwen also loads a system-defaults.json beside a
// settings file it is pointed at. $version is qwen's settings version: a file
// without it qwen migrates, rewriting it.
type qwenDefaults struct{}

func (v qwenDefaults) setup(exe, kind string) (argv, env []string, err error) {
	h := settingsHook{Type: "command", Command: hookLine(exe, kind), Timeout: 10}
	hooks := make(map[string][]settingsHooks, len(qwenHookEvents))
	for _, ev := range qwenHookEvents {
		hooks[ev] = []settingsHooks{{Matcher: "*", Hooks: []settingsHook{h}}}
	}
	if err := writeFileAtomic(qwenDefaultsPath(kind), map[string]any{"$version": 4, "hooks": hooks}); err != nil {
		return nil, nil, err
	}
	argv, env = v.unhooked(kind)
	return argv, env, nil
}

func (qwenDefaults) unhooked(kind string) (argv, env []string) {
	return nil, []string{"QWEN_CODE_SYSTEM_DEFAULTS_PATH=" + qwenDefaultsPath(kind)}
}

func qwenDefaultsPath(kind string) string { return filepath.Join(reportDir(kind), "defaults.json") }

// droidHookEvents are the droid events punk0 registers. droid uses Claude's
// names, and Claude's Notification values for a permission prompt.
var droidHookEvents = []string{
	"SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse",
	"PostToolUse", "Notification", "Stop",
}

// droidSettings reports through the settings file one droid run takes with
// --settings, which droid merges over the user's own for that run only and
// keeps out of its tools' environment.
type droidSettings struct{}

func (v droidSettings) setup(exe, kind string) (argv, env []string, err error) {
	h := settingsHook{Type: "command", Command: hookLine(exe, kind)}
	hooks := make(map[string][]settingsHooks, len(droidHookEvents))
	for _, ev := range droidHookEvents {
		m := settingsHooks{Hooks: []settingsHook{h}}
		if ev == "PreToolUse" || ev == "PostToolUse" {
			m.Matcher = "*" // every tool, as the hooks droid ran live were registered
		}
		hooks[ev] = []settingsHooks{m}
	}
	if err := writeFileAtomic(droidSettingsPath(kind), map[string]any{"hooks": hooks}); err != nil {
		return nil, nil, err
	}
	argv, env = v.unhooked(kind)
	return argv, env, nil
}

func (droidSettings) unhooked(kind string) (argv, env []string) {
	return []string{"--settings", droidSettingsPath(kind)}, nil
}

func droidSettingsPath(kind string) string { return filepath.Join(reportDir(kind), "droid.json") }

// writeAgentShell writes the startup files a tab's shell reads (shellHook):
// agents.sh and agents.fish with one function per agent, zsh's startup files
// in zsh/, and bash's rcfile, after running each agent's reporting setup
// (reportVia). exe is this punk0, as for writeClaudeHooks.
func writeAgentShell(exe string) error {
	dir := agentShellDir()
	argv := map[string][]string{}
	envOf := map[string][]string{}
	off := map[string]bool{}
	for _, kind := range tabAgentOrder {
		a := tabAgents[kind]
		if a.report == nil {
			continue
		}
		args, vars, err := a.report.via.setup(exe, kind)
		if err != nil {
			off[kind] = true
			ui.Status("warn", kind+" reporting", err.Error(), 0, os.Stderr)
			slog.Warn("agent reports nothing until punk0 restarts", "agent", kind, "err", err)
			continue
		}
		argv[kind], envOf[kind] = args, vars
	}
	reportOff.mu.Lock()
	reportOff.kind = off
	reportOff.mu.Unlock()
	start, exit := hookLine(exe, "start"), hookLine(exe, "exit")
	var sh, fish strings.Builder
	sh.WriteString(`# punk0 (agentshell.go): agents typed here report to the tab.
_punk0_agent() {
  local _punk0_name=$1 _punk0_rc
  shift
  if ! (unset -f "$_punk0_name"; command -v "$_punk0_name") >/dev/null 2>&1; then
    command "$_punk0_name" "$@"
    _punk0_rc=$?
    ` + exit + ` "$_punk0_name" "$_punk0_rc"
    return "$_punk0_rc"
  fi
  ` + start + ` "$_punk0_name"
  command "$_punk0_name" "$@"
  _punk0_rc=$?
  ` + exit + ` "$_punk0_name" "$_punk0_rc"
  return "$_punk0_rc"
}
`)
	fish.WriteString(`# punk0 (agentshell.go): agents typed here report to the tab.
function _punk0_agent
  set -l name $argv[1]
  set -e argv[1]
  if not command -sq $name
    command $name $argv
    set -l rc $status
    ` + exit + ` $name $rc
    return $rc
  end
  ` + start + ` $name
  command $name $argv
  set -l rc $status
  ` + exit + ` $name $rc
  return $rc
end
`)
	for _, kind := range tabAgentOrder {
		bin := tabAgents[kind].bin
		var shArgs, fishArgs string
		for _, a := range argv[kind] {
			shArgs += " " + shellQuote(a)
			fishArgs += " " + fishQuote(a)
		}
		// An agent's variables are set on its one command, so the shell
		// left at the prompt does not carry them.
		var shEnv, fishEnv string
		for _, e := range envOf[kind] {
			k, v, _ := strings.Cut(e, "=")
			shEnv += k + "=" + shellQuote(v) + " "
			fishEnv += k + "=" + fishQuote(v) + " "
		}
		call := shEnv + "_punk0_agent " + bin + shArgs + ` "$@"`
		fcall := fishEnv + "_punk0_agent " + bin + fishArgs + " $argv"
		if kind == tabOpenCode {
			// The tab's OpenCode configs (opencodeEnv) ride in variables of
			// their own, so an opencode the user starts some other way keeps
			// the user's own.
			fmt.Fprintf(&sh, "function %[1]s { if [ -n \"${PUNK0_OPENCODE_CONFIG-}\" ]; then OPENCODE_CONFIG_CONTENT=$PUNK0_OPENCODE_CONFIG OPENCODE_CLI_CONFIG_CONTENT=$PUNK0_OPENCODE_CLI_CONFIG %[2]s; else OPENCODE_CLI_CONFIG_CONTENT=$PUNK0_OPENCODE_CLI_CONFIG %[2]s; fi; }\n", bin, call)
			fmt.Fprintf(&fish, "function %[1]s\n  if set -q PUNK0_OPENCODE_CONFIG\n    OPENCODE_CONFIG_CONTENT=$PUNK0_OPENCODE_CONFIG OPENCODE_CLI_CONFIG_CONTENT=$PUNK0_OPENCODE_CLI_CONFIG %[2]s\n  else\n    OPENCODE_CLI_CONFIG_CONTENT=$PUNK0_OPENCODE_CLI_CONFIG %[2]s\n  end\nend\n", bin, fcall)
			continue
		}
		fmt.Fprintf(&sh, "function %s { %s; }\n", bin, call)
		fmt.Fprintf(&fish, "function %s\n  %s\nend\n", bin, fcall)
	}
	agents := filepath.Join(dir, "agents.sh")
	files := map[string]string{
		agents:                                 sh.String(),
		filepath.Join(dir, "agents.fish"):      fish.String(),
		filepath.Join(dir, "bashrc"):           bashRC(agents),
		filepath.Join(dir, "zsh", ".zshenv"):   zshStartup(".zshenv", ""),
		filepath.Join(dir, "zsh", ".zprofile"): zshStartup(".zprofile", ""),
		filepath.Join(dir, "zsh", ".zshrc"):    zshStartup(".zshrc", agents),
		filepath.Join(dir, "zsh", ".zlogin"):   zshStartup(".zlogin", ""),
	}
	for p, body := range files {
		if err := writeBytesAtomic(p, []byte(body)); err != nil {
			return err
		}
	}
	return nil
}

// opencodePlugin reports what an OpenCode v2 runs in a tab does; a plugin is
// a folder holding it twice, as server.js and tui.js.
//
//go:embed opencode-plugin.js
var opencodePlugin string

// opencodePluginDir is the plugin folder a tab's opencode loads (opencodeEnv).
func opencodePluginDir() string { return filepath.Join(agentShellDir(), "opencode-plugin") }

// opencodeEnv is what a tab gives the opencode it runs. OPENCODE_CLI_CONFIG
// loads punk0's plugin (opencodeCLIConfig); in a project, OPENCODE_CONFIG
// points worktree.directory at the Workspaces Folder, where punk0 lists
// OpenCode's worktrees (opencodeConfig). In a hooked shell they ride in
// PUNK0_OPENCODE_CONFIG and PUNK0_OPENCODE_CLI_CONFIG on every tab, for the
// opencode function; elsewhere they are OpenCode's own variables, on an
// opencode tab only.
func opencodeEnv(kind, project string, hooked bool) []string {
	server, cli := "PUNK0_OPENCODE_CONFIG", "PUNK0_OPENCODE_CLI_CONFIG"
	if !hooked {
		if kind != tabOpenCode {
			return nil
		}
		server, cli = "OPENCODE_CONFIG_CONTENT", "OPENCODE_CLI_CONFIG_CONTENT"
	}
	var env []string
	if project != "" {
		if root, err := codeRoot(); err == nil {
			env = append(env, server+"="+opencodeConfig(userEnv("OPENCODE_CONFIG_CONTENT"), filepath.Join(root, "workspaces", project)))
		} else {
			slog.Warn("opencode worktrees stay where opencode puts them", "err", err)
		}
	}
	return append(env, cli+"="+opencodeCLIConfig(userEnv("OPENCODE_CLI_CONFIG_CONTENT"), opencodeCLIFile(), opencodePluginDir()))
}

// userEnv is what the user set key to for every tab: terminal.env, else
// punk0's own environment, as for the shell itself.
func userEnv(key string) string {
	v := os.Getenv(key)
	if lines, err := settings.ParseEnvLines(settings.SettingOr[string]("terminal.env")); err == nil {
		if u := lastEnv(lines, key); u != "" {
			v = u
		}
	}
	return v
}

// opencodeConfig is the user's inline OpenCode config with
// worktree.directory pointing at dir: OpenCode puts every worktree it makes
// straight in there, one folder per worktree. A config of the user's that
// chooses a directory itself, or that does not parse, comes back untouched.
func opencodeConfig(user, dir string) string {
	cfg, worktree := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	if user = strings.TrimSpace(user); user != "" {
		if json.Unmarshal([]byte(user), &cfg) != nil {
			return user
		}
		if raw, ok := cfg["worktree"]; ok {
			if json.Unmarshal(raw, &worktree) != nil {
				return user
			}
			if d, ok := worktree["directory"]; ok && len(d) > 0 && string(d) != "null" {
				return user
			}
		}
	}
	worktree["directory"], _ = json.Marshal(dir)
	cfg["worktree"], _ = json.Marshal(worktree)
	out, err := json.Marshal(cfg)
	if err != nil {
		return user
	}
	return string(out)
}

// opencodeCLIFile is OpenCode's cli.json, where the user's TUI plugins are.
func opencodeCLIFile() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "opencode", "cli.json")
}

// opencodeCLIConfig is the inline CLI config that adds plugin, a folder, to
// the TUI's plugins. OpenCode lays the inline config over cli.json but lets
// its plugins list replace cli.json's, so the user's own list goes in with
// it: the inline one the user set, else cli.json's. A user's inline config
// that does not parse comes back untouched.
func opencodeCLIConfig(user, cliFile, plugin string) string {
	cfg := map[string]json.RawMessage{}
	if user = strings.TrimSpace(user); user != "" && json.Unmarshal([]byte(user), &cfg) != nil {
		return user
	}
	var plugins []string
	raw, ok := cfg["plugins"]
	if !ok {
		var file struct {
			Plugins json.RawMessage `json:"plugins"`
		}
		if data, err := os.ReadFile(cliFile); err == nil && json.Unmarshal(data, &file) == nil {
			raw = file.Plugins
		}
	}
	if len(raw) > 0 && json.Unmarshal(raw, &plugins) != nil {
		return user // entries punk0 cannot read would be lost
	}
	if !slices.Contains(plugins, plugin) {
		plugins = append(plugins, plugin)
	}
	cfg["plugins"], _ = json.Marshal(plugins)
	out, err := json.Marshal(cfg)
	if err != nil {
		return user
	}
	return string(out)
}

// zshStartup is punk0's copy of one of zsh's startup files. zsh reads them
// from ZDOTDIR, which a tab points at punk0's folder: each one reads the
// user's own file from the user's ZDOTDIR (PUNK0_USER_ZDOTDIR, else HOME)
// with ZDOTDIR set back to it, so the user's files see what they always do,
// and notes where a user's file moved ZDOTDIR to for the files after it.
// .zshrc then defines the agent functions and leaves ZDOTDIR as the user's,
// for whatever the shell runs.
func zshStartup(name, agents string) string {
	body := `# punk0 (agentshell.go): zsh reads this instead of your ` + name + `, which it reads first.
_punk0_zdotdir=$ZDOTDIR
ZDOTDIR=${PUNK0_USER_ZDOTDIR:-$HOME}
if [[ $ZDOTDIR != $_punk0_zdotdir && -f $ZDOTDIR/` + name + ` ]]; then
  source $ZDOTDIR/` + name + `
fi
export PUNK0_USER_ZDOTDIR=$ZDOTDIR
`
	if agents == "" {
		return body + "ZDOTDIR=$_punk0_zdotdir\n"
	}
	return body + "source " + shellQuote(agents) + "\nunset _punk0_zdotdir\n"
}

// bashRC is the rcfile a tab's bash reads instead of ~/.bashrc: the user's,
// then the agent functions.
func bashRC(agents string) string {
	return `# punk0 (agentshell.go): bash reads this instead of your ~/.bashrc, which it reads first.
if [ -f ~/.bashrc ]; then . ~/.bashrc; fi
. ` + shellQuote(agents) + "\n"
}

// fishQuote quotes s for fish: inside single quotes only \ and ' are special.
func fishQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// shellHook is how one shell is made to read punk0's agent functions: args
// go before anything else it is given, env is added to its environment, and
// stay is what a tab running a command first execs afterwards, so the shell
// left at the prompt has them too. A shell punk0 cannot hook is nil: an agent
// typed there is not seen, and one punk0 starts there gets its hooks on its
// command line (agentCommand).
type shellHook struct {
	args []string
	env  []string
	stay string
}

// hookFor is the shellHook of shell, nil for one punk0 cannot hook: anything
// but zsh, bash and fish, and every shell on Windows, where tabs run
// PowerShell or cmd.exe.
func hookFor(shell string, env []string) *shellHook {
	if runtime.GOOS == "windows" || !agentShellReady() {
		return nil
	}
	dir := agentShellDir()
	switch strings.TrimSuffix(filepath.Base(shell), ".exe") {
	case "zsh":
		zdot := filepath.Join(dir, "zsh")
		h := &shellHook{env: []string{"ZDOTDIR=" + zdot}, stay: "ZDOTDIR=" + shellQuote(zdot) + ` exec "$SHELL"`}
		// The user's ZDOTDIR, from terminal.env or punk0's own environment,
		// is where punk0's startup files find the user's.
		if user := lastEnv(env, "ZDOTDIR"); user != "" {
			h.env = append(h.env, "PUNK0_USER_ZDOTDIR="+user)
		}
		return h
	case "bash":
		rc := filepath.Join(dir, "bashrc")
		return &shellHook{args: []string{"--rcfile", rc}, stay: `exec "$SHELL" --rcfile ` + shellQuote(rc)}
	case "fish":
		init := "source " + fishQuote(filepath.Join(dir, "agents.fish"))
		return &shellHook{args: []string{"-C", init}, stay: `exec "$SHELL" -C ` + shellQuote(init)}
	}
	return nil
}

// lastEnv is the value of key in env, the last one winning as in a process
// environment; "" when it is not there.
func lastEnv(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], key+"="); ok {
			return v
		}
	}
	return ""
}

// agentShellReady reports whether the startup files are there for a shell
// to read; a tab started before they are written gets no hook.
func agentShellReady() bool {
	_, err := os.Stat(filepath.Join(agentShellDir(), "agents.sh"))
	return err == nil
}
