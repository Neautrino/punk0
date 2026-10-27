// Package main implements px0: a fast, local-first code navigator and review tool
// that opens any repository or pull request in a responsive browser UI.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"px0/internal/applog"
	"px0/internal/settings"
	"px0/internal/telemetry"
	"px0/internal/ui"
	"px0/internal/update"
)

//go:embed VERSION
var rawVersion string

var version = strings.TrimSpace(rawVersion)

// posthogKey can be injected at build time: go build -ldflags="-X main.posthogKey=phc_..."
var posthogKey string

func main() {
	// `punk0 hook` is what Claude Code runs for a tab's hook events: it has
	// to report them and get out of the way, before flags, updates or
	// anything else that could print.
	if len(os.Args) == 2 && os.Args[1] == "hook" {
		runHook()
		return
	}
	var (
		port         = flag.Int("port", 7777, "port to listen on (0 picks a free one)")
		host         = flag.String("host", "127.0.0.1", "address to bind")
		noOpen       = flag.Bool("no-open", false, "do not launch a browser")
		noLSP        = flag.Bool("no-lsp", false, "do not use language servers, even if installed")
		noGit        = flag.Bool("no-git", false, "disable git awareness")
		showVer      = flag.Bool("version", false, "print version and exit")
		showVerShort = flag.Bool("v", false, "print version and exit (shorthand)")
		doUpdate     = flag.Bool("update", false, "check for and install latest version of punk0")
		noUpdate     = flag.Bool("no-update", false, "do not auto-update punk0 on startup")
		noColor      = flag.Bool("no-color", false, "disable colour output")
		quiet        = flag.Bool("quiet", false, "suppress narration")
		verbose      = flag.Bool("verbose", false, "log startup steps, requests, searches, symbols, and agent prompts to terminal")
		noTelemetry  = flag.Bool("no-telemetry", false, "disable anonymous usage telemetry")
		agentCmd     = flag.String("agent", "", "pin the coding harness used for edits (claude, gemini, cursor-agent, agy, opencode, codex, aider, goose, or a command template containing {prompt}); detected and chosen in the UI when omitted")
		noAgent      = flag.Bool("no-agent", false, "do not offer editing through a coding harness")
		_            = flag.Bool("y", false, "answer yes to prompts (deprecated; PRs are always opened without prompt)")
		_            = flag.Bool("yes", false, "answer yes to prompts (alias for -y)")
		basePathFlag = flag.String("base-path", "", "base URL path prefix to serve endpoints and assets from (e.g. /rev-123/)")
		wbChild      = flag.Bool("workbench-child", false, "internal: run as a workspace of the punk0 workbench")
		wbHost       = flag.String("workbench-host", "", "internal: the address the workbench that started this child listens on")
		workspaceIDFlag = flag.String("workspace-id", "", "internal: workspace id when run under workbench")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "punk0 %s - an agent orchestrator and code navigator\n\nusage:\n  punk0 [flags]                      open the agents page\n  punk0 [flags] <file or directory>  open its workspace there\n  punk0 [flags] <pr-url>             review a pull request\n\nflags:\n", version)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *noColor {
		f := false
		ui.ForcedColor = &f
	}
	if *quiet || *wbChild {
		ui.Quiet = true
	}
	if *verbose {
		ui.Verbose = true
	}
	if *noGit {
		gitDisabled = true
	}
	if *wbChild {
		startChildLog()
	}

	if *showVer || *showVerShort || (flag.NArg() == 1 && flag.Arg(0) == "version") {
		fmt.Printf("punk0 %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return
	}

	if *doUpdate {
		if err := update.RunSelfUpdate(version); err != nil {
			fatal(err)
		}
		return
	}

	// A full pull or merge request URL (see prURLExamples)
	// checks out the PR's full source tree instead of resolving a local file/directory.
	// Only full URLs via "punk0 <url>" are supported for PR review.
	target := "."
	var prProvider GitProvider
	var prTarget PRTarget
	isPR := false
	if flag.NArg() > 0 {
		arg0 := flag.Arg(0)
		if arg0 == "pr" {
			fatal(fmt.Errorf("'punk0 pr' is no longer supported; open pull requests directly with: punk0 <url>"))
		}
		if provider, pt, ok := DetectPRURL(arg0); ok {
			prProvider, prTarget, isPR = provider, pt, true
		} else {
			target = arg0
		}
	}
	if isPR && gitDisabled {
		fatal(fmt.Errorf("punk0: git is required for PR review; remove -no-git"))
	}

	// Everything that is not a pull request opens in the orchestrator: the
	// page that lists projects and agents, which serves each folder through a
	// child punk0 (workbench.go). A bare `punk0` just opens the page; a
	// folder or file opens its editor view there. Only a child started by
	// that parent serves a folder on its own.
	if !isPR && !*wbChild {
		var childArgs []string
		for _, f := range []struct {
			on   bool
			flag string
		}{{*noLSP, "-no-lsp"}, {*noGit, "-no-git"}, {*noAgent, "-no-agent"}, {*noTelemetry, "-no-telemetry"}} {
			if f.on {
				childArgs = append(childArgs, f.flag)
			}
		}
		if *agentCmd != "" {
			childArgs = append(childArgs, "-agent", *agentCmd)
		}
		initial := ""
		if flag.NArg() > 0 {
			initial = target
		}
		runWorkbench(*host, *port, *basePathFlag, !*noOpen, !*noUpdate && settings.SettingOr[bool]("update.auto"), childArgs, initial)
		return
	}

	var pr *prSession
	var root, initialFile string
	var initialLine int
	var targetDur time.Duration
	if isPR {
		if !*wbChild {
			startLog()
		}
		sp := ui.NewSpinner(fmt.Sprintf("Preparing PR #%d (%s/%s)...", prTarget.Number, prTarget.Owner, prTarget.Repo), os.Stdout)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		// Outside a clone of the PR's repository, use one punk0 knows (the
		// remembered clone or a registered project), so the checkout is a
		// worktree rather than a fresh clone.
		cloneDir := "."
		if top, _ := findPRRemote(".", prTarget.Owner, prTarget.Repo); top == "" {
			st := &wbStore{path: workbenchStorePath()}
			if known := knownClone(st.Load(), prTarget.Owner+"/"+prTarget.Repo, recordedProjectDirs(workbenchStateDir())...); known != "" {
				cloneDir = known
			}
		}
		p, err := checkoutPR(ctx, prProvider, prTarget, cloneDir, func(msg string) {
			sp.Update(msg)
		})
		cancel()
		if err != nil {
			sp.Fail(fmt.Sprintf("Failed to prepare PR #%d: %v", prTarget.Number, err))
			fatal(fmt.Errorf("punk0: %w", err))
		}
		if p.meta.Merged {
			sp.Success(fmt.Sprintf("PR #%d checked out [merged] (%s)", prTarget.Number, p.meta.Title))
		} else {
			sp.Success(fmt.Sprintf("PR #%d checked out (%s)", prTarget.Number, p.meta.Title))
		}
		pr = p
		root = p.Root()
	} else {
		tStart := time.Now()
		r, f, l, err := resolveTarget(target)
		if err != nil {
			fatal(err)
		}
		targetDur = time.Since(tStart)
		root, initialFile, initialLine = r, f, l
	}

	tListen := time.Now()
	ln, addr, err := listen(*host, *port)
	if err != nil {
		fatal(err)
	}
	listenDur := time.Since(tListen)
	if !*wbChild {
		slog.Info("punk0 started", startupAttrs(addr)...)
	}

	tInit := time.Now()
	ix := NewIndex(root)
	lsp := newLSPManager(root, !*noLSP)
	// telemetry.enabled: false in the settings file opts out like -no-telemetry.
	cfgTel := settings.Read().TelemetryEnabled
	tel := telemetry.New(posthogKey, version, *noTelemetry || (cfgTel != nil && !*cfgTel))
	defer tel.Close("normal")

	configuredBasePath := "/"
	if *basePathFlag != "" {
		configuredBasePath = cleanBasePath(*basePathFlag)
	} else if cfg := settings.Read(); cfg.ServerBasePath != nil && *cfg.ServerBasePath != "" {
		configuredBasePath = cleanBasePath(*cfg.ServerBasePath)
	}

	pxSrv := NewServer(ix, lsp, configuredBasePath)
	pxSrv.tel = tel
	// A workbench child always listens on 127.0.0.1 behind the workbench's
	// proxy, which keeps the browser's Host: what counts is where the
	// workbench itself listens.
	publicHost := *host
	if *wbChild && *wbHost != "" {
		publicHost = *wbHost
	}
	pxSrv.localOnly = loopbackBind(publicHost)
	if *wbChild {
		pxSrv.workbenchURL = workbenchBaseOf(configuredBasePath)
		pxSrv.workspaceID = *workspaceIDFlag
	} else if pr != nil {
		it := wbPRItem(prTarget)
		it.Title = pr.meta.Title
		go recordRecent(it)
	}
	if pr != nil {
		pxSrv.SetPR(pr)
	}
	var agent *agentManager
	if !*noAgent {
		agent, err = newAgentManager(root, *agentCmd, lsp)
		if err != nil {
			fatal(fmt.Errorf("-agent: %w", err))
		}
		pxSrv.SetAgent(agent)
	}
	initDur := time.Since(tInit)

	srv := &http.Server{Handler: pxSrv}
	srv.RegisterOnShutdown(pxSrv.EndStreams)
	if *wbChild {
		// The workbench reads this line for the address it proxies to.
		fmt.Printf("%s%s\n", wbReadyPrefix, addr)
	}

	url := viewerURL(addr, initialFile, initialLine, configuredBasePath)
	ui.Heading("punk0 "+version, nil, os.Stdout)
	if pr != nil {
		prTitle := fmt.Sprintf("#%d %s", pr.meta.Number, pr.meta.Title)
		if pr.meta.Merged {
			prTitle += " " + ui.Paint("[MERGED]", ui.ColorWarn, true, os.Stdout)
		}
		ui.KV("PR", prTitle, 11, os.Stdout)
		if pr.token == "" {
			ui.KV("access", ui.Dim(fmt.Sprintf("read-only (no %s token: %s to submit reviews)", pr.provider.Info().Label, pr.provider.Info().TokenHint), os.Stdout), 11, os.Stdout)
		}
	}
	ui.KV("workspace", root, 11, os.Stdout)
	ui.KV("url", ui.Accent(url, os.Stdout), 11, os.Stdout)
	if *host == "0.0.0.0" {
		for _, networkURL := range networkURLs(addr, initialFile, initialLine) {
			ui.KV("network", ui.Accent(networkURL, os.Stdout), 11, os.Stdout)
		}
	}
	ui.Hint("ctrl-c to stop", os.Stdout)

	if ui.Verbose {
		if !isPR {
			ui.Status("ok", "resolved workspace target", ui.FmtDuration(targetDur), 0, os.Stdout)
		}
		ui.Status("ok", fmt.Sprintf("bound TCP listener on %s", addr), ui.FmtDuration(listenDur), 0, os.Stdout)
		ui.Status("ok", "initialized HTTP server and services", ui.FmtDuration(initDur), 0, os.Stdout)
	}

	// Launch browser immediately without blocking startup.
	if !*noOpen {
		tBrowser := time.Now()
		go openBrowser(url)
		if ui.Verbose {
			ui.Status("ok", "spawned browser launcher", ui.FmtDuration(time.Since(tBrowser)), 0, os.Stdout)
		}
	}

	// Index workspace asynchronously so the server and UI respond in <1ms.
	go func() {
		ix.Build()
		if pxSrv.gitWatcher != nil {
			pxSrv.gitWatcher.Trigger()
		}
		n, _, ms := ix.Stats()
		ui.Status("ok", fmt.Sprintf("indexed %d files", n), fmt.Sprintf("%dms", ms), 0, os.Stdout)
		tLSP := time.Now()
		names := lsp.Available()
		lspDur := time.Since(tLSP)
		if len(names) > 0 {
			if ui.Verbose {
				ui.Status("ok", fmt.Sprintf("discovered language servers: %s", strings.Join(names, ", ")), ui.FmtDuration(lspDur), 0, os.Stdout)
			} else {
				ui.Bullet(fmt.Sprintf("language servers: %s (started on first use)", strings.Join(names, ", ")), os.Stdout)
			}
		} else if ui.Verbose {
			ui.Status("ok", "checked language servers (none installed)", ui.FmtDuration(lspDur), 0, os.Stdout)
		}
		if agent != nil {
			tAgent := time.Now()
			var found []string
			for _, h := range agent.Detect() {
				if h.Installed {
					item := h.Name
					if h.Model != "" {
						item = fmt.Sprintf("%s (%s)", h.Name, h.Model)
					}
					found = append(found, item)
				}
			}
			agentDur := time.Since(tAgent)
			if ui.Verbose {
				if len(found) > 0 {
					ui.Status("ok", fmt.Sprintf("detected coding harnesses: %s", strings.Join(found, ", ")), ui.FmtDuration(agentDur), 0, os.Stdout)
				} else {
					ui.Status("ok", "checked coding harnesses (none found)", ui.FmtDuration(agentDur), 0, os.Stdout)
				}
			}
		}

		tel.Track("session_started", map[string]any{
			"files_bucket": telemetry.FilesBucket(n),
			"index_ms":     ms,
			"has_git":      gitAvailable(root),
			"has_lsp":      len(lsp.Available()) > 0,
		})
	}()

	// Check for updates in the background once the server is already up, so
	// px0 never makes a network call before serving the first request.
	if !*noUpdate && settings.SettingOr[bool]("update.auto") {
		go update.AutoUpdate(version)
	}

	// Language servers are children that can hold gigabytes. Shut them down on
	// the way out rather than leaving them for the OS to reap.
	interrupted := false
	shutDown := make(chan struct{}) // closes once Shutdown has drained every request
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	if *wbChild {
		// The workbench holds our stdin open; it closes when the workbench
		// stops this workspace or exits for any reason.
		go func() {
			io.Copy(io.Discard, os.Stdin)
			stop <- syscall.SIGTERM
		}()
	}
	go func() {
		<-stop
		interrupted = true
		fmt.Print("\r")
		ui.Status("info", "punk0 stopped", "", 0, os.Stderr)
		go func() {
			<-stop // Second interrupt forces immediate exit
			os.Exit(130)
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		close(shutDown)
	}()

	err = srv.Serve(ln)
	if err == http.ErrServerClosed {
		// Serve returns as soon as Shutdown starts; exiting then would cut
		// the responses it is still draining.
		<-shutDown
	}
	lsp.Close()
	agent.Close()
	pr.Close()

	if interrupted {
		tel.Close("interrupted")
		os.Exit(130)
	}

	tel.Close("normal")
	if err != nil && err != http.ErrServerClosed {
		fatal(err)
	}
}

// runWorkbench serves the orchestrator until interrupted, then stops every
// terminal and workspace it started. initial is the folder or file
// `punk0 <target>` named: it is registered as a project and the agents page
// opens on it; a file also opens in the folder's editor.
func runWorkbench(host string, port int, basePathFlag string, open, doAutoUpdate bool, childArgs []string, initial string) {
	// One orchestrator per machine: a second punk0 hands its folder to the
	// one already running instead of serving a rival page.
	if u := runningWorkbench(); u != "" {
		handOff(u, initial, port, open)
		return
	}
	startLog()
	exe, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	basePath := "/"
	if basePathFlag != "" {
		basePath = cleanBasePath(basePathFlag)
	} else if cfg := settings.Read(); cfg.ServerBasePath != nil && *cfg.ServerBasePath != "" {
		basePath = cleanBasePath(*cfg.ServerBasePath)
	}

	ln, addr, err := listen(host, port)
	if err != nil {
		fatal(err)
	}
	slog.Info("punk0 started", startupAttrs(addr)...)
	wb, err := newWorkbench(basePath, cwd, exe, append(childArgs, "-workbench-host", host))
	if err != nil {
		fatal(err)
	}
	wb.localOnly = loopbackBind(host)
	srv := &http.Server{Handler: wb}

	u := viewerURL(addr, "", 0, basePath)
	_, boundPort, _ := net.SplitHostPort(addr)
	// Agents report their hooks to punk0 itself, never through whatever
	// address the browser reached it on.
	self := "http://" + net.JoinHostPort("127.0.0.1", boundPort) + basePath
	writeWorkbenchLock(self)
	defer removeWorkbenchLock()
	wb.serveFrom(self)

	// `punk0 <dir>` opens the agents page on that folder's workspace; the
	// editor only opens for a file, since only it can show one. The page
	// is opened at ?ws=<id>, which is not printed unless no browser opens:
	// the project line says where it lands.
	editor, target, project := "", u, ""
	if initial != "" {
		root, file, line, err := resolveTarget(initial)
		if err != nil {
			fatal(err)
		}
		_, ws, err := wb.records.Open(root)
		if err != nil {
			fatal(err)
		}
		if file != "" {
			editor = editorURL(u, wb.childHref(wbItemID("dir", ws.Dir)), file, line)
		}
		target = agentsURL(u, ws.ID)
		project = wb.records.WorkspaceLabel(ws.ID) + "  " + ui.Dim(tildePath(ws.Dir), os.Stdout)
	}
	shown := u
	if !open {
		shown = target
	}

	ui.Heading("punk0 "+version, nil, os.Stdout)
	ui.KV("agents", ui.Accent(shown, os.Stdout), 11, os.Stdout)
	if project != "" {
		ui.KV("project", project, 11, os.Stdout)
	}
	if editor != "" {
		ui.KV("editor", ui.Accent(editor, os.Stdout), 11, os.Stdout)
	}
	if host == "0.0.0.0" {
		for _, networkURL := range networkURLs(addr, "", 0) {
			ui.KV("network", ui.Accent(networkURL, os.Stdout), 11, os.Stdout)
		}
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		ui.Status("warn", "anyone who can reach this address can browse and open any directory on this machine", "", 0, os.Stdout)
	}
	ui.Hint("ctrl-c to stop", os.Stdout)
	if !ui.Quiet {
		fmt.Println() // the event lines start below
	}
	// Nothing reads this terminal: keys typed into it would only echo
	// between the event lines. Every way out puts echo back.
	restoreEcho := ui.NoEcho(os.Stdin)
	defer restoreEcho()
	if open {
		// general.openBrowser skips only a bare start's agents page: the
		// folder or file `punk0 <target>` asked for still opens, as a
		// hand-off's does.
		if initial != "" || settings.SettingOr[bool]("general.openBrowser") {
			go openBrowser(target)
		}
		if editor != "" {
			go openBrowser(editor)
		}
	}
	if doAutoUpdate {
		go update.AutoUpdate(version)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	interrupted := false
	go func() {
		<-stop
		interrupted = true
		slog.Info("punk0 stopped", "reason", "interrupted")
		fmt.Print("\r")
		ui.Status("info", "punk0 stopped", "", 0, os.Stderr)
		go func() {
			<-stop
			restoreEcho()
			os.Exit(130)
		}()
		// Children first: their open event streams would hold Shutdown up.
		wb.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	err = srv.Serve(ln)
	wb.Close()
	if interrupted {
		removeWorkbenchLock()
		restoreEcho()
		os.Exit(130)
	}
	if err != nil && err != http.ErrServerClosed {
		removeWorkbenchLock()
		restoreEcho()
		fatal(err)
	}
	slog.Info("punk0 stopped")
}

// handOff gives a folder to the punk0 that is already running, so
// `punk0 <dir>` in a second terminal lands in the one orchestrator instead of
// starting another: an agents page already open switches to the folder, and
// only with none open does a browser tab open on it. A file also opens in
// the folder's editor.
func handOff(base, initial string, port int, open bool) {
	if flagSet("host") || flagSet("port") {
		if u, err := url.Parse(base); err == nil && u.Port() != strconv.Itoa(port) {
			ui.Status("warn", "punk0 already runs on "+base, "-host and -port only apply to the first one", 0, os.Stdout)
		}
	}
	if initial == "" {
		ui.Status("ok", "punk0 already running", base, 0, os.Stdout)
		if open {
			openBrowser(base)
		}
		return
	}
	root, file, line, err := resolveTarget(initial)
	if err != nil {
		fatal(err)
	}
	var added struct {
		Workspace string `json:"workspace"`
		Href      string `json:"href"`
	}
	if err := postWorkbench(base, "api/projects", map[string]string{"dir": root}, &added); err != nil {
		fatal(fmt.Errorf("punk0 on %s: %w", base, err))
	}
	// A punk0 from before api/wb/show answers 404: as if no page were open.
	var shown struct {
		Pages int `json:"pages"`
	}
	_ = postWorkbench(base, "api/wb/show", map[string]string{"workspace": added.Workspace}, &shown)
	agents := agentsURL(base, added.Workspace)
	switch {
	case shown.Pages > 0:
		ui.Status("ok", "punk0 already running", "switched the open agents page to "+tildePath(root), 0, os.Stdout)
	case open:
		ui.Status("ok", "punk0 already running", "opening "+tildePath(root)+" in a new browser tab", 0, os.Stdout)
		openBrowser(agents)
	default:
		ui.Status("ok", "punk0 already running", agents, 0, os.Stdout)
	}
	if file != "" {
		editor := editorURL(base, added.Href, file, line)
		ui.KV("editor", ui.Accent(editor, os.Stdout), 11, os.Stdout)
		if open {
			openBrowser(editor)
		}
	}
}

// agentsURL is the agents page at base opened on workspace id; the page
// selects it as if clicked (web/src/orch/store.js).
func agentsURL(base, id string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	if u.Path == "" {
		u.Path = "/"
	}
	q := u.Query()
	q.Set("ws", id)
	u.RawQuery = q.Encode()
	return u.String()
}

// postWorkbench posts body as JSON to path under the running punk0 at base and
// decodes its answer into out. Registering a folder it already holds is the
// same request and gives the same answer.
func postWorkbench(base, path string, body, out any) error {
	origin, err := url.Parse(base)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(base, "/")+"/"+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin.Scheme+"://"+origin.Host)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return errors.New(e.Error)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// editorURL is a workspace's editor view under the orchestrator, opened on
// the file and line the command line named (see viewerURL).
func editorURL(base, href, initialFile string, initialLine int) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	ref, err := url.Parse(href)
	if err != nil {
		return base
	}
	target := u.ResolveReference(ref)
	q := target.Query()
	if initialFile != "" {
		q.Set("path", filepath.ToSlash(initialFile))
	}
	if initialLine > 0 {
		q.Set("line", strconv.Itoa(initialLine))
	}
	if len(q) > 0 {
		target.RawQuery = q.Encode()
	}
	return target.String()
}

// flagSet reports whether a flag was given on the command line, as opposed to
// left at its default.
func flagSet(name string) bool {
	seen := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}

// workbenchBaseOf turns a workspace's base path (/<wb>/w/<id>/) back into the
// workbench's own.
func workbenchBaseOf(childBase string) string {
	trimmed := strings.TrimSuffix(childBase, "/")
	if i := strings.LastIndex(trimmed, "/w/"); i >= 0 {
		return trimmed[:i+1]
	}
	return "/"
}

// resolveTarget turns a directory or file into a workspace root, along with an optional
// initial file to open and optional line number.
// If the target is inside a git repository, that repository root is used as the workspace root.
// Otherwise, for relative paths within the current working directory, the working directory
// is used. Standalone files fall back to their parent directory.
func resolveTarget(target string) (root, initialFile string, initialLine int, err error) {
	cleanedTarget, line := splitTargetLine(target)
	abs, err := filepath.Abs(cleanedTarget)
	if err != nil {
		return "", "", 0, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid target %s: %w", abs, err)
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid target %s: %w", abs, err)
	}
	if st.IsDir() {
		return resolved, "", 0, nil
	}
	if !st.Mode().IsRegular() {
		return "", "", 0, fmt.Errorf("not a regular file or directory: %s", abs)
	}

	parentDir := filepath.Dir(resolved)

	// If the file is inside a git repository, use the git repo root.
	if info := gitProbe(parentDir); info.ok && info.toplevel != "" {
		if rel, err := filepath.Rel(info.toplevel, resolved); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			return info.toplevel, filepath.ToSlash(rel), line, nil
		}
	}

	// If the target was specified as a relative path within the current working directory,
	// use the current working directory as the workspace root.
	if !filepath.IsAbs(cleanedTarget) {
		if wd, err := os.Getwd(); err == nil {
			if resolvedWd, err := filepath.EvalSymlinks(wd); err == nil {
				if rel, err := filepath.Rel(resolvedWd, resolved); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
					return resolvedWd, filepath.ToSlash(rel), line, nil
				}
			}
		}
	}

	return parentDir, filepath.Base(resolved), line, nil
}

// splitTargetLine separates trailing :line or :line:col from target if the candidate path exists.
func splitTargetLine(target string) (path string, line int) {
	if _, err := os.Stat(target); err == nil {
		return target, 0
	}
	lastColon := strings.LastIndex(target, ":")
	if lastColon <= 0 {
		return target, 0
	}
	vol := filepath.VolumeName(target)
	if lastColon <= len(vol) {
		return target, 0
	}
	suffix := target[lastColon+1:]
	num, err := strconv.Atoi(suffix)
	if err != nil || num <= 0 {
		return target, 0
	}
	rest := target[:lastColon]
	secondColon := strings.LastIndex(rest, ":")
	if secondColon > len(vol) {
		secondSuffix := rest[secondColon+1:]
		if lineNum, err := strconv.Atoi(secondSuffix); err == nil && lineNum > 0 {
			candidate := rest[:secondColon]
			if _, err := os.Stat(candidate); err == nil {
				return candidate, lineNum
			}
		}
	}
	if _, err := os.Stat(rest); err == nil {
		return rest, num
	}
	return target, 0
}

func viewerURL(addr, initialFile string, initialLine int, basePath ...string) string {
	u := url.URL{Scheme: "http", Host: addr}
	bp := "/"
	if len(basePath) > 0 && basePath[0] != "" {
		bp = cleanBasePath(basePath[0])
	}
	if bp != "/" {
		u.Path = bp
	}
	q := u.Query()
	if initialFile != "" {
		q.Set("path", filepath.ToSlash(initialFile))
	}
	if initialLine > 0 {
		q.Set("line", strconv.Itoa(initialLine))
	}
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// networkURLs returns URLs for the machine's non-loopback IPv4 addresses.
// The supplied address contributes the bound port; its host is intentionally
// ignored because a bind address is not necessarily a usable destination.
func networkURLs(addr, initialFile string, initialLine int) []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	return networkURLsFromAddrs(addr, initialFile, initialLine, addrs)
}

func networkURLsFromAddrs(addr, initialFile string, initialLine int, addrs []net.Addr) []string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	urls := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		var ip net.IP
		switch a := addr.(type) {
		case *net.IPNet:
			ip = a.IP
		case *net.IPAddr:
			ip = a.IP
		default:
			continue
		}

		ip = ip.To4()
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}

		host := ip.String()
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		urls = append(urls, viewerURL(net.JoinHostPort(host, port), initialFile, initialLine))
	}

	sort.Strings(urls)
	return urls
}

// listen binds the requested port, walking forward if it is already taken so a
// second instance does not simply fail. If a block of ports is busy, it falls
// back to an OS-assigned free port.
func listen(host string, port int) (net.Listener, string, error) {
	if port == 0 {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return nil, "", err
		}
		return ln, ln.Addr().String(), nil
	}
	for p := port; p < port+100; p++ {
		addr := net.JoinHostPort(host, fmt.Sprint(p))
		if ln, err := net.Listen("tcp", addr); err == nil {
			return ln, addr, nil
		}
	}
	// Fallback to any free port assigned by the OS if port range is busy
	if ln, err := net.Listen("tcp", net.JoinHostPort(host, "0")); err == nil {
		return ln, ln.Addr().String(), nil
	}
	return nil, "", fmt.Errorf("no free port available starting from %d", port)
}

func openBrowser(url string) {
	// If BROWSER environment variable is set, try that first
	if b := os.Getenv("BROWSER"); b != "" {
		if cmd := exec.Command(b, url); cmd.Start() == nil {
			return
		}
	}

	var cmds []*exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmds = []*exec.Cmd{exec.Command("open", url)}
	case "windows":
		cmds = []*exec.Cmd{
			exec.Command("rundll32", "url.dll,FileProtocolHandler", url),
			exec.Command("cmd.exe", "/c", "start", url),
		}
	default:
		// On Linux/Unix, detect WSL to open the browser on the Windows host seamlessly
		if isWSL() {
			// WSL2's localhost port-forwarding relay needs a moment to notice a
			// freshly bound listener before Windows can reach it. Without this,
			// the browser can open before the relay catches up and shows a
			// connection error until the next reload.
			time.Sleep(500 * time.Millisecond)
			cmds = append(cmds,
				exec.Command("wslview", url),
				exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Start-Process", fmt.Sprintf(`"%s"`, url)),
				exec.Command("cmd.exe", "/c", "start", "", url),
			)
		}

		// On Linux/Unix desktop, try xdg-open, sensible-browser, gio, or common browsers
		cmds = append(cmds,
			exec.Command("xdg-open", url),
			exec.Command("sensible-browser", url),
			exec.Command("gio", "open", url),
			exec.Command("google-chrome", url),
			exec.Command("firefox", url),
			exec.Command("chromium", url),
		)
	}

	for _, cmd := range cmds {
		if err := cmd.Start(); err == nil {
			return
		}
	}
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	data, err := os.ReadFile("/proc/version")
	if err == nil && (strings.Contains(strings.ToLower(string(data)), "microsoft") || strings.Contains(strings.ToLower(string(data)), "wsl")) {
		return true
	}
	return false
}

func fatal(err error) {
	if applog.Enabled() {
		slog.Error("fatal", "err", err)
	}
	fmt.Fprintln(os.Stderr, "punk0:", err)
	os.Exit(1)
}
