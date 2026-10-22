package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"px0/internal/metrics"
)

// lspServerDef describes one language server we know how to drive. Nothing here
// is required for px0 to work; a server is used only if its binary is found on
// PATH or in one of the folders installers commonly use (lspBinDirs).
type lspServerDef struct {
	Name        string
	Lang        string // language name, shown when offering to install the server
	Cmd         []string
	Exts        []string          // file extensions this server handles
	LangIDs     map[string]string // ext -> LSP languageId, when it differs
	DefaultLang string
	InitOptions map[string]any
	Install     []lspInstall // ways to get the binary, best first
	Docs        string       // the server's install instructions, for when no recipe fits
	// Usable, when set, must also accept the binary found for Cmd[0]: a
	// command name another version of the tool shares without the server.
	Usable func(bin string) bool
}

func (d lspServerDef) LanguageID(rel string) string {
	ext := strings.ToLower(filepath.Ext(rel))
	if id, ok := d.LangIDs[ext]; ok {
		return id
	}
	return d.DefaultLang
}

// lspRegistry is ordered: the first entry whose binary exists wins for a given
// extension, so a more capable server listed earlier takes precedence.
var lspRegistry = []lspServerDef{
	{
		Name: "gopls", Lang: "Go", Cmd: []string{"gopls"},
		Exts: []string{".go"}, DefaultLang: "go", Docs: "https://go.dev/gopls/",
		Install: []lspInstall{
			{Cmd: []string{"go", "install", "golang.org/x/tools/gopls@latest"}, Auto: true},
			{OS: "darwin", Cmd: []string{"brew", "install", "gopls"}, Auto: true},
		},
	},
	{
		Name: "rust-analyzer", Lang: "Rust", Cmd: []string{"rust-analyzer"},
		Exts: []string{".rs"}, DefaultLang: "rust", Docs: "https://rust-analyzer.github.io/",
		Install: []lspInstall{
			{Cmd: []string{"rustup", "component", "add", "rust-analyzer"}, Auto: true},
			{OS: "darwin", Cmd: []string{"brew", "install", "rust-analyzer"}, Auto: true},
		},
	},
	{
		Name: "pyright", Lang: "Python", Cmd: []string{"pyright-langserver", "--stdio"},
		Exts: []string{".py", ".pyi"}, DefaultLang: "python", Docs: "https://microsoft.github.io/pyright/",
		Install: []lspInstall{
			{Cmd: []string{"npm", "install", "-g", "pyright"}, Auto: true},
			{OS: "darwin", Cmd: []string{"brew", "install", "pyright"}, Auto: true},
		},
	},
	{
		Name: "pylsp", Lang: "Python", Cmd: []string{"pylsp"},
		Exts: []string{".py", ".pyi"}, DefaultLang: "python", Docs: "https://github.com/python-lsp/python-lsp-server",
		Install: []lspInstall{
			{Cmd: []string{"pipx", "install", "python-lsp-server"}, Auto: true},
			{OS: "darwin", Cmd: []string{"brew", "install", "python-lsp-server"}, Auto: true},
		},
	},
	{
		// Not offered for install: it lints, but answers no call hierarchy.
		Name: "ruff", Lang: "Python", Cmd: []string{"ruff", "server"},
		Exts: []string{".py"}, DefaultLang: "python", Docs: "https://docs.astral.sh/ruff/installation/",
	},
	{
		// TypeScript 7, the native port, serves LSP itself. Its tsc shares
		// the name with older versions' tsc, which do not (see tsNative).
		Name: "tsc", Lang: "TypeScript and JavaScript", Cmd: []string{"tsc", "--lsp", "--stdio"},
		Exts:        []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
		LangIDs:     map[string]string{".ts": "typescript", ".tsx": "typescriptreact", ".jsx": "javascriptreact"},
		DefaultLang: "javascript", Docs: "https://github.com/microsoft/typescript-go",
		Usable: tsNative,
		Install: []lspInstall{
			{Cmd: []string{"npm", "install", "-g", "typescript@latest"}, Auto: true},
		},
	},
	{
		// Drives tsserver, which TypeScript 7 no longer ships: pinned to 6.
		Name: "typescript", Lang: "TypeScript and JavaScript", Cmd: []string{"typescript-language-server", "--stdio"},
		Exts:        []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
		LangIDs:     map[string]string{".ts": "typescript", ".tsx": "typescriptreact", ".jsx": "javascriptreact"},
		DefaultLang: "javascript", Docs: "https://github.com/typescript-language-server/typescript-language-server",
		Install: []lspInstall{
			{Cmd: []string{"npm", "install", "-g", "typescript-language-server", "typescript@6"}, Auto: true},
			{OS: "darwin", Cmd: []string{"brew", "install", "typescript-language-server"}, Auto: true},
		},
	},
	{
		Name: "clangd", Lang: "C and C++", Cmd: []string{"clangd", "--background-index"},
		Exts:        []string{".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".hh", ".m", ".mm"},
		LangIDs:     map[string]string{".c": "c", ".h": "c"},
		DefaultLang: "cpp", Docs: "https://clangd.llvm.org/installation",
		Install: []lspInstall{
			{OS: "darwin", Cmd: []string{"brew", "install", "llvm"}, Auto: true},
			// System package managers want an administrator: shown, not run.
			{OS: "linux", Cmd: []string{"sudo", "apt", "install", "clangd"}},
			{OS: "windows", Cmd: []string{"winget", "install", "LLVM.LLVM"}},
		},
	},
	{
		Name: "zls", Lang: "Zig", Cmd: []string{"zls"},
		Exts: []string{".zig"}, DefaultLang: "zig", Docs: "https://zigtools.org/zls/install/",
		Install: []lspInstall{
			{OS: "darwin", Cmd: []string{"brew", "install", "zls"}, Auto: true},
		},
	},
	{
		Name: "lua", Lang: "Lua", Cmd: []string{"lua-language-server"},
		Exts: []string{".lua"}, DefaultLang: "lua", Docs: "https://luals.github.io/#install",
		Install: []lspInstall{
			{OS: "darwin", Cmd: []string{"brew", "install", "lua-language-server"}, Auto: true},
		},
	},
	{
		Name: "solargraph", Lang: "Ruby", Cmd: []string{"solargraph", "stdio"},
		Exts: []string{".rb"}, DefaultLang: "ruby", Docs: "https://solargraph.org/",
		Install: []lspInstall{
			{Cmd: []string{"gem", "install", "solargraph"}, Auto: true},
			{OS: "darwin", Cmd: []string{"brew", "install", "solargraph"}, Auto: true},
		},
	},
	{
		Name: "jdtls", Lang: "Java", Cmd: []string{"jdtls"},
		Exts: []string{".java"}, DefaultLang: "java", Docs: "https://github.com/eclipse-jdtls/eclipse.jdt.ls",
		Install: []lspInstall{
			{OS: "darwin", Cmd: []string{"brew", "install", "jdtls"}, Auto: true},
		},
	},
	{
		Name: "omnisharp", Lang: "C#", Cmd: []string{"omnisharp", "-lsp"},
		Exts: []string{".cs"}, DefaultLang: "csharp", Docs: "https://github.com/OmniSharp/omnisharp-roslyn",
	},
	{
		Name: "texlab", Lang: "LaTeX", Cmd: []string{"texlab"},
		Exts: []string{".tex"}, DefaultLang: "latex", Docs: "https://github.com/latex-lsp/texlab",
		Install: []lspInstall{
			{OS: "darwin", Cmd: []string{"brew", "install", "texlab"}, Auto: true},
		},
	},
}

// ---------------------------------------------------------------- manager

type lspState string

const (
	lspOff      lspState = "off"      // disabled, or no server installed for this type
	lspStarting lspState = "starting" // process spawned, handshake in flight
	lspIndexing lspState = "indexing" // up, but still chewing through the workspace
	lspReady    lspState = "ready"
	lspFailed   lspState = "failed"
)

// maxLSPRestarts bounds how often one crashed language server is respawned.
const maxLSPRestarts = 3

// lspManager coordinates background language servers across file types and extensions.
// It manages on-demand server lazy starting, discovery, restart on crash, installation jobs,
// and path allowlisting for external references (such as standard library files).
type lspManager struct {
	root    string
	enabled bool

	// mu guards the discovery/registry tables below. It is an RWMutex: the
	// read-heavy paths (defFor, State, Available, ServerStatus) take the read
	// lock, and only spawn/discover/Stop mutate. No I/O happens under it.
	mu        sync.RWMutex
	byExt     map[string]*lspServerDef // resolved by discover(), again on Rescan
	clients   map[string]*lspClient    // server name -> client
	starting  map[string]chan struct{}
	failed    map[string]string
	available []string
	restarts  map[string]int // crashes recovered from, per server name
	stopped   map[string]bool

	// The Language Servers settings, as applySettings last saw them. paused
	// is lsp.enabled off: nothing runs or starts here, while the folder's own
	// choice (enabled) is kept for when it is turned back on. disabled is
	// lsp.disabled; the map is replaced, never changed in place.
	paused   bool
	disabled map[string]bool
	// used is when client() last handed out each running server, in Unix
	// nanoseconds, for lsp.idleStop. The values are atomic so client()'s
	// read-locked fast path can record a use.
	used map[string]*atomic.Int64

	discovered bool // the first discover() has finished
	// discoverMu runs one discover() at a time, so a rescan started before a
	// change to lsp.disabled cannot land after the one started for it.
	discoverMu sync.Mutex

	quit      chan struct{} // closed by Close to end the settings reaper
	closeOnce sync.Once

	// The settings file as the reaper last applied it, and when the reaper
	// last swept for idle servers. Only the reaper (or a test standing in
	// for it) touches these.
	settingsSeen settingsStamp
	swept        time.Time

	jobMu sync.Mutex
	jobs  map[string]*lspJob // install runs, by server name

	// external holds absolute paths outside the indexed tree that a language
	// server pointed us at. Only these are openable beyond the root, so a
	// jump into the standard library works without opening up the filesystem.
	extMu    sync.RWMutex
	external map[string]bool
}

func (m *lspManager) allow(abs string) {
	m.extMu.Lock()
	if m.external == nil {
		m.external = map[string]bool{}
	}
	if len(m.external) < 20000 {
		m.external[abs] = true
	}
	m.extMu.Unlock()
}

// Allowed reports whether a language server has named this exact file.
func (m *lspManager) Allowed(abs string) bool {
	m.extMu.RLock()
	defer m.extMu.RUnlock()
	return m.external[abs]
}

// newLSPManager creates a new language server manager for root.
// If enabled is true, server discovery begins in the background without
// blocking startup, and a reaper applies the Language Servers settings.
func newLSPManager(root string, enabled bool) *lspManager {
	m := &lspManager{
		root: root, enabled: enabled,
		byExt:    map[string]*lspServerDef{},
		clients:  map[string]*lspClient{},
		starting: map[string]chan struct{}{},
		failed:   map[string]string{},
		stopped:  map[string]bool{},
	}
	if !enabled {
		return m
	}
	// Set before the first discover() and the first request, so a folder
	// opened while lsp.enabled is off, or with servers disabled, never starts
	// one even briefly. The file is stamped before it is read, so a write in
	// between is applied on the reaper's first check.
	m.settingsSeen, m.swept = statSettings(), time.Now()
	cfg := readSettings()
	m.paused, m.disabled = !cfg.lspEnabled(), cfg.lspDisabled()
	m.quit = make(chan struct{})
	// Discover available language servers in background so server startup is instantaneous.
	go m.discover()
	go m.reap()
	return m
}

// lspSettingsEvery is how often the reaper looks at the settings file. A
// save on the agents page is written by the parent, which cannot reach its
// children, so this is how a folder's editor process learns of it. A look is
// one stat; the file is read and applied only when it has changed.
const lspSettingsEvery = 2 * time.Second

// lspReapEvery is how often the reaper applies the settings whether or not
// the file changed, which is what stops idle servers.
const lspReapEvery = 30 * time.Second

// settingsStamp tells whether the settings file changed since it was last
// applied. The zero stamp is a missing file.
type settingsStamp struct {
	mod  time.Time
	size int64
}

func statSettings() settingsStamp {
	p := settingsPath()
	if p == "" {
		return settingsStamp{}
	}
	fi, err := os.Stat(p)
	if err != nil {
		return settingsStamp{}
	}
	return settingsStamp{fi.ModTime(), fi.Size()}
}

// reap runs checkSettings every lspSettingsEvery until Close. Reading the
// shared file, rather than being told, keeps the parent from having to wake
// every child to pass a change on.
func (m *lspManager) reap() {
	t := time.NewTicker(lspSettingsEvery)
	defer t.Stop()
	for {
		select {
		case <-m.quit:
			return
		case now := <-t.C:
			m.checkSettings(now)
		}
	}
}

// checkSettings applies the settings file at time now when it changed since
// it was last applied, or when lspReapEvery has passed since the last sweep.
// It reports whether it applied them.
func (m *lspManager) checkSettings(now time.Time) bool {
	st := statSettings()
	if st == m.settingsSeen && now.Sub(m.swept) < lspReapEvery {
		return false
	}
	m.settingsSeen, m.swept = st, now
	m.applySettings(now, readSettings())
	return true
}

// applySettings brings the running servers in line with cfg at time now:
// with lsp.enabled off every server stops and none starts; a server listed
// in lsp.disabled stops and discovery passes it over for the next one of its
// language; a server client() has not handed out for lsp.idleStop stops.
// None of these marks a server stopped by the user, so the next use after
// the setting allows it starts it again.
func (m *lspManager) applySettings(now time.Time, cfg settings) {
	if !m.enabled {
		return
	}
	paused, disabled, idle := !cfg.lspEnabled(), cfg.lspDisabled(), cfg.lspIdleStop()
	m.mu.Lock()
	rediscover := !maps.Equal(disabled, m.disabled)
	m.paused, m.disabled = paused, disabled
	var drop []*lspClient
	for name, c := range m.clients {
		u := m.used[name]
		if u == nil {
			// Not handed out since it started (a crash replacement in a
			// test, say): its idle time counts from now.
			m.setUsedLocked(name, now)
			u = m.used[name]
		}
		idleFor := now.Sub(time.Unix(0, u.Load()))
		if paused || disabled[name] || idle > 0 && idleFor > idle {
			drop = append(drop, c)
			delete(m.clients, name)
			delete(m.used, name)
		}
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, c := range drop {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.shutdown()
		}()
	}
	wg.Wait()
	if rediscover {
		m.discover()
	}
}

// setUsedLocked records a use of name at t. m.mu must be held for writing.
func (m *lspManager) setUsedLocked(name string, t time.Time) {
	if m.used == nil {
		m.used = map[string]*atomic.Int64{}
	}
	u := &atomic.Int64{}
	u.Store(t.UnixNano())
	m.used[name] = u
}

// touchLocked records that client() handed out name. m.mu must be held,
// for reading is enough.
func (m *lspManager) touchLocked(name string) {
	if u := m.used[name]; u != nil {
		u.Store(time.Now().UnixNano())
	}
}

// refusedLocked is why the settings keep name from running, or nil. m.mu
// must be held, for reading is enough.
func (m *lspManager) refusedLocked(name string) error {
	if m.paused {
		return errPaused
	}
	if m.disabled[name] {
		return errFailed{name + " is disabled in settings"}
	}
	return nil
}

// isDisabled reports whether lsp.disabled lists name.
func (m *lspManager) isDisabled(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.disabled[name]
}

// discover resolves which registry servers are installed. It can run again
// (Rescan) after something is installed; the result replaces the previous one
// in a single swap, so readers never see a half-built table. Servers listed
// in lsp.disabled are passed over, so the next one for their language wins.
func (m *lspManager) discover() {
	m.discoverMu.Lock()
	defer m.discoverMu.Unlock()
	m.mu.RLock()
	disabled := m.disabled
	m.mu.RUnlock()
	// Fresh eyes: an install or rescan may have changed what's on disk, so
	// drop memoized binary resolutions before re-resolving.
	invalidateBinaryCache()
	byExt := map[string]*lspServerDef{}
	var available []string
	for i := range lspRegistry {
		def := lspRegistry[i] // a copy: Cmd[0] becomes the resolved path
		if disabled[def.Name] {
			continue
		}
		bin, ok := findServer(&def)
		if !ok {
			continue
		}
		def.Cmd = append([]string{bin}, def.Cmd[1:]...)
		claimed := false
		for _, ext := range def.Exts {
			if byExt[ext] == nil {
				byExt[ext] = &def
				claimed = true
			}
		}
		if claimed {
			available = append(available, def.Name)
		}
	}
	m.mu.Lock()
	m.byExt, m.available, m.discovered = byExt, available, true
	m.mu.Unlock()
}

func (m *lspManager) isDiscovered() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.discovered
}

// lspBinDirs lists folders installers put binaries in that are often missing
// from PATH, so a server installed from px0, or by hand after px0 started, is
// found without restarting the shell px0 was launched from.
func lspBinDirs() []string {
	var dirs []string
	add := func(elem ...string) { dirs = append(dirs, filepath.Join(elem...)) }
	if v := os.Getenv("GOBIN"); v != "" {
		add(v)
	}
	for _, p := range filepath.SplitList(os.Getenv("GOPATH")) {
		if p != "" {
			add(p, "bin")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(home, "go", "bin")     // go install, default GOPATH
		add(home, ".cargo", "bin") // rustup, cargo install
		add(home, ".local", "bin") // pipx
		add(home, ".opencode", "bin")
		add(home, ".codex", "bin")
	}
	// npm puts global packages beside its own executable unless the prefix was moved.
	if npm, err := exec.LookPath("npm"); err == nil {
		add(filepath.Dir(npm))
	}
	switch runtime.GOOS {
	case "darwin":
		add("/opt/homebrew/bin")
		add("/usr/local/bin")
		// Homebrew's llvm is keg-only: clangd is installed but never linked.
		add("/opt/homebrew/opt/llvm/bin")
		add("/usr/local/opt/llvm/bin")
	case "windows":
		if v := os.Getenv("APPDATA"); v != "" {
			add(v, "npm")
		}
		if v := os.Getenv("ProgramFiles"); v != "" {
			add(v, "LLVM", "bin")
		}
	}
	return dirs
}

// binDirCacheTTL bounds how long the binary caches trust their snapshot. The
// directories come from env vars and an npm probe, and ServerStatus re-resolves
// every installed binary and install-tool on each status refresh, so uncached
// lookups are pure repeated I/O on a hot path. 30s keeps "installed after px0
// started" discovery working while cutting the steady state cost to zero.
const binDirCacheTTL = 30 * time.Second

// binResolve is one memoized lookPathIn result: path is the resolved binary
// ("" when missing) and ok says whether it was found.
type binResolve struct {
	path string
	ok   bool
}

var binDirCache struct {
	sync.RWMutex
	dirs []string
	bins map[string]binResolve // binary name -> resolution, against dirs
	at   time.Time
}

// cachedLookPath resolves name like lookPathIn (PATH, then the installer
// dirs from lspBinDirs), with both the dirs and the result memoized for
// binDirCacheTTL. ServerStatus calls this per binary and per install tool
// on every refresh; without the memo each call re-probes PATH and every dir.
//
// Cache hits take only the read lock. Misses take the write lock and re-check,
// so concurrent misses for the same name still probe once; the filesystem
// probe itself runs under the write lock, but misses are rare (one per binary
// per TTL) while hits are the hot path.
func cachedLookPath(name string) (string, bool) {
	binDirCache.RLock()
	if time.Since(binDirCache.at) < binDirCacheTTL && binDirCache.dirs != nil {
		if r, ok := binDirCache.bins[name]; ok {
			binDirCache.RUnlock()
			return r.path, r.ok
		}
	}
	binDirCache.RUnlock()

	binDirCache.Lock()
	defer binDirCache.Unlock()
	if time.Since(binDirCache.at) >= binDirCacheTTL || binDirCache.dirs == nil {
		binDirCache.dirs = lspBinDirs()
		binDirCache.bins = map[string]binResolve{}
		binDirCache.at = time.Now()
	}
	if r, ok := binDirCache.bins[name]; ok {
		return r.path, r.ok
	}
	p, ok := lookPathIn(name, binDirCache.dirs)
	binDirCache.bins[name] = binResolve{path: p, ok: ok}
	return p, ok
}

// findServer resolves a server's binary, and has it pass the server's own
// Usable check when it has one.
func findServer(def *lspServerDef) (string, bool) {
	bin, ok := cachedLookPath(def.Cmd[0])
	if !ok || def.Usable == nil {
		return bin, ok
	}
	return bin, usableOnce(bin, def.Usable)
}

// usable remembers a Usable verdict per binary, keyed by its path, size and
// modification time: a check runs the binary, and the menu asks on every
// refresh, but an upgrade in place replaces the file and asks again.
var usable sync.Map

func usableOnce(bin string, check func(string) bool) bool {
	st, err := os.Stat(bin)
	if err != nil {
		return false
	}
	key := fmt.Sprintf("%s|%d|%d", bin, st.Size(), st.ModTime().UnixNano())
	if v, ok := usable.Load(key); ok {
		return v.(bool)
	}
	ok := check(bin)
	usable.Store(key, ok)
	return ok
}

// tsNative reports whether tsc is TypeScript 7 or later ("Version 7.0.2"),
// whose native compiler answers LSP with --lsp. Earlier tsc has no LSP.
func tsNative(bin string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return false
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return false
	}
	major, _, _ := strings.Cut(f[len(f)-1], ".")
	n, err := strconv.Atoi(major)
	return err == nil && n >= 7
}

// invalidateBinaryCache drops the memoized dirs and binary resolutions, e.g.
// after an install or rescan may have changed what's on disk. discover() calls
// this itself, so Rescan and post-install verification always see fresh state.
func invalidateBinaryCache() {
	binDirCache.Lock()
	binDirCache.dirs = nil
	binDirCache.bins = nil
	binDirCache.at = time.Time{}
	binDirCache.Unlock()
}

// lookPathIn finds a command on PATH, then in dirs. On Windows exec.LookPath
// also tries PATHEXT extensions for a joined path, so "npm" finds npm.cmd.
func lookPathIn(name string, dirs []string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	for _, d := range dirs {
		if p, err := exec.LookPath(filepath.Join(d, name)); err == nil {
			return p, true
		}
	}
	return "", false
}

func (m *lspManager) Available() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.available == nil {
		return []string{}
	}
	cp := make([]string, len(m.available))
	copy(cp, m.available)
	return cp
}

// Enabled reports whether language servers may run here now: -no-lsp turns
// them off process-wide, and lsp.enabled off pauses them.
func (m *lspManager) Enabled() bool {
	if !m.enabled {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.paused
}

// Paused reports whether lsp.enabled has turned off servers -no-lsp allows.
func (m *lspManager) Paused() bool {
	return m.enabled && !m.Enabled()
}

// memBytes returns the combined resident memory of every currently running
// language server, best-effort: a server whose RSS can't be read (exited,
// unsupported platform, no permission) contributes 0.
func (m *lspManager) memBytes() uint64 {
	m.mu.RLock()
	pids := make([]int, 0, len(m.clients))
	for _, c := range m.clients {
		if c.cmd != nil && c.cmd.Process != nil && c.alive() == nil {
			pids = append(pids, c.cmd.Process.Pid)
		}
	}
	m.mu.RUnlock()
	if len(pids) == 0 {
		return 0
	}
	procs := metrics.Processes()
	var total uint64
	for _, pid := range pids {
		total += lspTreeRSS(procs, pid)
	}
	return total
}

// lspTreeRSS is the memory of a server and everything it started: much of
// typescript-language-server's is in the tsserver processes under it. procs
// is one snapshot shared by every server measured; where none can be taken
// (Windows) only the server's own process counts.
func lspTreeRSS(procs metrics.Table, pid int) uint64 {
	if procs != nil {
		return procs.Tree(pid).RSS
	}
	return readRSSForPID(pid)
}

// HasClients reports whether any server process exists, so a caller can skip
// snapshotting the process table when there is nothing to measure.
func (m *lspManager) HasClients() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients) > 0
}

func (m *lspManager) defFor(rel string) *lspServerDef {
	if !m.enabled {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.paused {
		return nil
	}
	return m.byExt[strings.ToLower(filepath.Ext(rel))]
}

// State reports what a caller can expect for this file without starting
// anything, so the UI can say "indexing" instead of silently showing regex hits.
//
// Everything State needs is snapshotted under a single read-lock hold: this
// runs on the hot UI/navigation path, so the lock/unlock churn of several
// separate lookups (stopped, def, discovered, client tables) is collapsed
// into one.
func (m *lspManager) State(rel string) (lspState, string) {
	ext := strings.ToLower(filepath.Ext(rel))
	m.mu.RLock()
	stoppedName := ""
	if m.stopped != nil {
		for _, reg := range registryFor(rel) {
			if m.stopped[reg.Name] {
				stoppedName = reg.Name
				break
			}
		}
	}
	enabled := m.enabled && !m.paused
	var def *lspServerDef
	if enabled {
		def = m.byExt[ext] // defFor without the extra lock round-trip
	}
	discovered := m.discovered
	var c *lspClient
	var why string
	var bad, pending, stopped bool
	if def != nil {
		stopped = m.stopped != nil && m.stopped[def.Name]
		c = m.clients[def.Name]
		why, bad = m.failed[def.Name]
		_, pending = m.starting[def.Name]
	}
	m.mu.RUnlock()

	if stoppedName != "" {
		return lspOff, stoppedName
	}
	if def == nil {
		// Discovery runs in the background at startup. Until it finishes, a
		// file type px0 knows may still get a server, so keep the UI asking.
		if enabled && !discovered && len(registryFor(rel)) > 0 {
			return lspStarting, ""
		}
		return lspOff, ""
	}
	if stopped {
		return lspOff, def.Name
	}
	switch {
	case bad:
		return lspFailed, why
	case pending:
		return lspStarting, def.Name
	case c == nil:
		return lspStarting, def.Name // not spawned yet; the next call will
	case c.alive() != nil:
		return lspFailed, def.Name
	case c.busy():
		return lspIndexing, def.Name
	}
	return lspReady, def.Name
}

// client returns a started client for rel, spawning one on first use. Callers
// that cannot wait should pass a short context; the spawn continues regardless
// so the next request finds it ready.
//
// The loop's fast path is read-only (RLock): an already-running client is
// returned without serializing against other readers. Mutation (restart
// accounting, claiming the spawn slot) re-checks under the write lock.
func (m *lspManager) client(ctx context.Context, rel string) (*lspClient, error) {
	m.mu.RLock()
	paused := m.paused
	m.mu.RUnlock()
	if paused {
		return nil, errPaused
	}
	def := m.defFor(rel)
	if def == nil {
		return nil, errNoServer
	}
	m.mu.RLock()
	stopped := m.stopped != nil && m.stopped[def.Name]
	m.mu.RUnlock()
	if stopped {
		return nil, errStopped
	}
	for {
		// The settings can change while this waits on a spawn: def was
		// resolved before lsp.disabled named it, or lsp.enabled went off.
		m.mu.RLock()
		refused := m.refusedLocked(def.Name)
		why, bad := m.failed[def.Name]
		c, ok := m.clients[def.Name]
		wait, starting := m.starting[def.Name]
		var aliveErr error
		if ok {
			aliveErr = c.alive()
		}
		if refused == nil && ok && aliveErr == nil {
			m.touchLocked(def.Name)
		}
		m.mu.RUnlock()

		if refused != nil {
			return nil, refused
		}
		if bad {
			return nil, errFailed{why}
		}
		if ok && aliveErr == nil {
			return c, nil
		}
		if starting {
			select {
			case <-wait:
				continue // loop back and pick up the result
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		m.mu.Lock()
		// Re-check under the write lock: another goroutine may have finished
		// starting, recorded a failure, or replaced the client meanwhile.
		if err := m.refusedLocked(def.Name); err != nil {
			m.mu.Unlock()
			return nil, err
		}
		if why, bad := m.failed[def.Name]; bad {
			m.mu.Unlock()
			return nil, errFailed{why}
		}
		if c, ok := m.clients[def.Name]; ok {
			err := c.alive()
			if err == nil {
				m.touchLocked(def.Name)
				m.mu.Unlock()
				return c, nil
			}
			// A crashed server would otherwise take hover, definitions and
			// references down with it for the rest of the session. Start a
			// fresh one, but stop after a few crashes so a server that dies on
			// every request is not respawned forever.
			if m.restarts == nil {
				m.restarts = map[string]int{}
			}
			if m.restarts[def.Name] >= maxLSPRestarts {
				m.mu.Unlock()
				return nil, err
			}
			m.restarts[def.Name]++
			delete(m.clients, def.Name)
			delete(m.used, def.Name)
			m.mu.Unlock()
			go c.shutdown()
			continue
		}
		if wait, ok := m.starting[def.Name]; ok {
			m.mu.Unlock()
			select {
			case <-wait:
				continue // loop back and pick up the result
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		done := make(chan struct{})
		m.starting[def.Name] = done
		m.mu.Unlock()

		go m.spawn(def, done)

		select {
		case <-done:
			continue
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (m *lspManager) spawn(def *lspServerDef, done chan struct{}) {
	// The handshake gets a generous budget of its own: some servers do real
	// work before answering initialize.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := newLSPClient(*def, m.root)
	err := c.start(ctx)
	if err != nil {
		// A server can refuse initialize and keep running (typescript-
		// language-server without a TypeScript it can use): nothing would
		// ever talk to it again, so it must not be left holding memory.
		go c.shutdown()
	}

	m.mu.Lock()
	// The settings may have turned this server off while it started.
	refused := err == nil && m.refusedLocked(def.Name) != nil
	switch {
	case err != nil:
		m.failed[def.Name] = err.Error()
	case !refused:
		m.clients[def.Name] = c
		m.setUsedLocked(def.Name, time.Now())
	}
	delete(m.starting, def.Name)
	m.mu.Unlock()
	if refused {
		go c.shutdown()
	}
	close(done)
}

func (m *lspManager) CloseDoc(abs, rel string) {
	def := m.defFor(rel)
	if def == nil {
		return
	}
	m.mu.RLock()
	c := m.clients[def.Name]
	m.mu.RUnlock()
	if c != nil {
		c.closeDoc(abs)
	}
}

func (m *lspManager) EnsureOpen(ctx context.Context, abs, rel string) error {
	c, err := m.client(ctx, rel)
	if err != nil {
		return err
	}
	return c.ensureOpen(abs, rel)
}

func (m *lspManager) SyncDoc(abs, rel string) error {
	def := m.defFor(rel)
	if def == nil {
		return nil
	}
	m.mu.RLock()
	c := m.clients[def.Name]
	m.mu.RUnlock()
	if c != nil {
		return c.syncDoc(abs, rel)
	}
	return nil
}

func (m *lspManager) RefreshOpenDocs() {
	m.mu.RLock()
	clients := make([]*lspClient, 0, len(m.clients))
	for _, c := range m.clients {
		clients = append(clients, c)
	}
	m.mu.RUnlock()

	for _, c := range clients {
		c.mu.RLock()
		opened := make([]string, 0, len(c.opened))
		for uri := range c.opened {
			opened = append(opened, uri)
		}
		c.mu.RUnlock()

		for _, uri := range opened {
			abs, err := uriToPath(uri)
			if err != nil {
				continue
			}
			if _, err := os.Stat(abs); os.IsNotExist(err) {
				c.closeDoc(abs)
				continue
			}
			rel, err := filepath.Rel(m.root, abs)
			if err != nil {
				rel = abs
			}
			rel = filepath.ToSlash(rel)
			_ = c.syncDoc(abs, rel)
		}
	}
}

func (m *lspManager) Close() {
	m.closeOnce.Do(func() {
		if m.quit != nil {
			close(m.quit)
		}
	})
	m.mu.Lock()
	clients := make([]*lspClient, 0, len(m.clients))
	for _, c := range m.clients {
		clients = append(clients, c)
	}
	m.clients = map[string]*lspClient{}
	m.used = nil
	m.mu.Unlock()
	// Shut down in parallel: each shutdown is bounded (2s call timeout, 200ms
	// exit flush, 1s kill), but a stuck server must not delay the others.
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.shutdown()
		}()
	}
	wg.Wait()
}

type errFailed struct{ why string }

func (e errFailed) Error() string { return e.why }

var (
	errNoServer = errFailed{"no language server for this file type"}
	errStopped  = errFailed{"language server stopped"}
	errPaused   = errFailed{"language servers are turned off in settings"}
)

// Stop stops the named server process, or all running servers if name is empty or "all".
// The server is marked stopped to prevent auto-spawning until explicitly restarted.
func (m *lspManager) Stop(name string) {
	m.mu.Lock()
	if m.stopped == nil {
		m.stopped = map[string]bool{}
	}
	if name == "" || name == "all" {
		for _, def := range lspRegistry {
			m.stopped[def.Name] = true
		}
		clients := make([]*lspClient, 0, len(m.clients))
		for _, c := range m.clients {
			clients = append(clients, c)
		}
		m.clients = map[string]*lspClient{}
		m.used = nil
		m.mu.Unlock()
		for _, c := range clients {
			c.shutdown()
		}
		return
	}

	m.stopped[name] = true
	c := m.clients[name]
	delete(m.clients, name)
	delete(m.used, name)
	m.mu.Unlock()
	if c != nil {
		c.shutdown()
	}
}

// StartServer unmarks the server as stopped and spawns the background language
// server process. The settings win over the user's start: nothing starts while
// lsp.enabled is off, nor a server lsp.disabled lists.
func (m *lspManager) StartServer(name string) error {
	if !m.enabled {
		return fmt.Errorf("lsp disabled")
	}
	m.mu.Lock()
	if err := m.refusedLocked(name); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.stopped != nil {
		delete(m.stopped, name)
	}
	if m.failed != nil {
		delete(m.failed, name)
	}
	m.mu.Unlock()

	var targetDef *lspServerDef
	m.mu.RLock()
	for _, def := range m.byExt {
		if def.Name == name {
			targetDef = def
			break
		}
	}
	m.mu.RUnlock()

	if targetDef == nil {
		for i := range lspRegistry {
			if lspRegistry[i].Name == name {
				def := lspRegistry[i]
				if bin, ok := findServer(&def); ok {
					def.Cmd = append([]string{bin}, def.Cmd[1:]...)
					targetDef = &def
				}
				break
			}
		}
	}

	if targetDef == nil {
		return fmt.Errorf("language server %q is not installed", name)
	}

	m.mu.Lock()
	if c, ok := m.clients[name]; ok && c.alive() == nil {
		m.mu.Unlock()
		return nil
	}
	if _, ok := m.starting[name]; ok {
		m.mu.Unlock()
		return nil
	}
	done := make(chan struct{})
	m.starting[name] = done
	m.mu.Unlock()

	go m.spawn(targetDef, done)
	return nil
}

// AnyRunning reports whether at least one language server client is actively running or starting.
//
// The clients are snapshotted under the read lock and then probed without
// holding it: alive() takes the client's own lock, and nesting the manager
// lock around it is unnecessary serialization on a polling path.
func (m *lspManager) AnyRunning() bool {
	m.mu.RLock()
	clients := make([]*lspClient, 0, len(m.clients))
	for _, c := range m.clients {
		clients = append(clients, c)
	}
	starting := len(m.starting) > 0
	m.mu.RUnlock()

	for _, c := range clients {
		if c.alive() == nil {
			return true
		}
	}
	return starting
}

func shortLangName(lang string) string {
	switch lang {
	case "TypeScript and JavaScript":
		return "TS / JS"
	case "TypeScript":
		return "TS"
	case "JavaScript":
		return "JS"
	case "C and C++":
		return "C / C++"
	default:
		return lang
	}
}

// ServerStatus returns the status summary of a language server definition.
// procs is the process table its memory is measured from (see lspTreeRSS).
func (m *lspManager) ServerStatus(def *lspServerDef, procs metrics.Table) map[string]any {
	m.mu.RLock()
	c, ok := m.clients[def.Name]
	_, starting := m.starting[def.Name]
	why, failed := m.failed[def.Name]
	isStopped := m.stopped != nil && m.stopped[def.Name]
	m.mu.RUnlock()

	// Binary and tool existence is memoized (30s TTL): a status refresh
	// re-resolves every server and install tool, and uncached that is a
	// PATH scan per binary per refresh.
	_, installed := findServer(def)

	var opts []map[string]any
	autoIndex := -1
	for i, in := range def.installsFor(runtime.GOOS) {
		_, has := cachedLookPath(in.Cmd[0])
		if in.Auto && has && autoIndex == -1 {
			autoIndex = i
		}
		opts = append(opts, map[string]any{
			"index":   i,
			"cmd":     strings.Join(in.Cmd, " "),
			"auto":    in.Auto,
			"tool":    in.Cmd[0],
			"hasTool": has,
		})
	}

	res := map[string]any{
		"name":        def.Name,
		"userStopped": isStopped,
		"lang":        shortLangName(def.Lang),
		"fullLang":    def.Lang,
		"exts":        def.Exts,
		"installed":   installed,
		"running":     false,
		"state":       "stopped",
		"options":     opts,
		"docs":        def.Docs,
		"autoOpt":     autoIndex,
	}

	if j := m.job(def.Name); j != nil {
		res["job"] = map[string]any{
			"running": j.Running,
			"cmd":     j.Cmd,
			"error":   j.Error,
		}
		if j.Running {
			res["state"] = "installing"
			return res
		}
		if j.Error != "" && !installed {
			res["state"] = "missing"
			res["error"] = j.Error
			return res
		}
	}

	if !installed {
		res["state"] = "missing"
		return res
	}

	if isStopped {
		res["state"] = "stopped"
		return res
	}

	if failed {
		res["state"] = "failed"
		res["error"] = why
		return res
	}

	if starting {
		res["state"] = "starting"
		res["running"] = true
		return res
	}

	if ok && c != nil && c.alive() == nil {
		res["running"] = true
		if c.busy() {
			res["state"] = "indexing"
		} else {
			res["state"] = "ready"
		}
		if c.cmd != nil && c.cmd.Process != nil {
			pid := c.cmd.Process.Pid
			res["pid"] = pid
			res["memBytes"] = lspTreeRSS(procs, pid)
		}
		return res
	}

	res["state"] = "stopped"
	return res
}
