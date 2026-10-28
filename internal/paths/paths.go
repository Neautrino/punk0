// Package paths says where punk0 keeps its files: settings in the config
// folder, its bookkeeping (records, logs, hooks, sessions) in the state
// folder, downloads in the cache folder. Unix follows the XDG base
// directories, macOS included (like gh and opencode); Windows uses AppData.
// punk0 before this layout kept everything in ~/.punk0: while that folder
// holds the file a standard folder lacks, punk0 keeps using it, so upgrading
// moves nothing until `punk0 migrate`.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

const app = "punk0"

// Legacy is the folder every punk0 before this layout used.
func Legacy() string {
	if h := home(); h != "" {
		return filepath.Join(h, ".punk0")
	}
	return filepath.Join(os.TempDir(), app)
}

// Config holds settings.json.
func Config() string { return legacyOr("XDG_CONFIG_HOME", StandardConfig(), "settings.json") }

// State holds records.json and everything else punk0 writes for itself.
func State() string { return legacyOr("XDG_STATE_HOME", StandardState(), "records.json") }

// Cache holds downloads punk0 can fetch again; it has no legacy fallback.
func Cache() string { return folder("XDG_CACHE_HOME", ".cache", "LOCALAPPDATA", "cache") }

// StandardConfig is where settings go once migrated.
func StandardConfig() string { return folder("XDG_CONFIG_HOME", ".config", "APPDATA", "") }

// StandardState is where the state goes once migrated. On Windows it is a
// subfolder: install.ps1 puts punk0.exe in %LOCALAPPDATA%\punk0.
func StandardState() string {
	return folder("XDG_STATE_HOME", filepath.Join(".local", "state"), "LOCALAPPDATA", "state")
}

// folder resolves one base directory: a set, absolute XDG variable wins
// (relative ones are invalid per the spec), else the platform default.
func folder(xdg, unixRel, winVar, winSub string) string {
	if v := os.Getenv(xdg); filepath.IsAbs(v) {
		return filepath.Join(v, app)
	}
	h := home()
	if h == "" {
		return filepath.Join(os.TempDir(), app)
	}
	if runtime.GOOS != "windows" {
		return filepath.Join(h, unixRel, app)
	}
	base := os.Getenv(winVar)
	if base == "" {
		sub := "Roaming"
		if winVar == "LOCALAPPDATA" {
			sub = "Local"
		}
		base = filepath.Join(h, "AppData", sub)
	}
	return filepath.Join(base, app, winSub)
}

// legacyOr is std, unless only ~/.punk0 has marker. A set XDG variable was
// honoured before this layout too, so it never falls back.
func legacyOr(xdg, std, marker string) string {
	if filepath.IsAbs(os.Getenv(xdg)) || exists(filepath.Join(std, marker)) || !exists(filepath.Join(Legacy(), marker)) {
		return std
	}
	return Legacy()
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
