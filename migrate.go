package main

// `punk0 migrate` moves an old install's settings and state out of ~/.punk0
// into the standard folders (internal/paths). Worktrees, scratch chats,
// clones and the trash stay where they are: records.json holds their absolute
// paths, and moving a worktree needs git.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"px0/internal/paths"
)

// legacyState is what moves out of ~/.punk0, records.json **last**: it is the
// marker paths.State() follows, so a failure part-way leaves punk0 on the
// legacy folder with all of its files.
var legacyState = []string{"workbench.json", "update_check.json", "anonymous_id", "logs", "sessions", "records.json"}

// folderInfo tells the page where punk0 keeps its files and whether it still
// runs out of the legacy ~/.punk0, which `punk0 migrate` ends.
func folderInfo() map[string]any {
	legacy := paths.Legacy()
	return map[string]any{
		"config": tildePath(paths.Config()),
		"state":  tildePath(paths.State()),
		"legacy": paths.Config() == legacy || paths.State() == legacy,
	}
}

func runMigrate() { os.Exit(migrate(os.Stdout)) }

// migrate is `punk0 migrate`; it returns the process's exit status.
func migrate(w io.Writer) int {
	config, stdConfig := paths.Config(), paths.StandardConfig()
	state, stdState := paths.State(), paths.StandardState()
	if config == stdConfig && state == stdState {
		fmt.Fprintf(w, "punk0 already uses %s and %s\n", tildePath(stdConfig), tildePath(stdState))
		return 0
	}
	if url := runningWorkbench(); url != "" {
		fmt.Fprintf(w, "stop punk0 first: it is running at %s\n", url)
		return 1
	}

	if config != stdConfig {
		if err := moveAll(filepath.Join(config, "settings.json"), filepath.Join(stdConfig, "settings.json")); err != nil {
			fmt.Fprintf(w, "settings.json: %v\n", err)
			return 1
		}
		fmt.Fprintf(w, "settings are now in %s\n", tildePath(stdConfig))
	}

	if state != stdState {
		for _, name := range legacyState {
			if err := moveAll(filepath.Join(state, name), filepath.Join(stdState, name)); err != nil {
				fmt.Fprintf(w, "%s: %v\n", name, err)
				return 1
			}
		}
		os.Remove(filepath.Join(state, "claude-hooks.json")) // rewritten on every start
		os.RemoveAll(filepath.Join(state, "cache"))          // downloads punk0 fetches again
		fmt.Fprintf(w, "state is now in %s\n", tildePath(stdState))
	}

	fmt.Fprintf(w, "worktrees, scratch chats, clones and the trash stay in %s: punk0 keeps opening them there\n", tildePath(state))
	return 0
}

// moveAll moves src onto dst, copying and removing instead when a rename
// cannot cross the two filesystems. A missing src is nothing to do.
func moveAll(src, dst string) error {
	if _, err := os.Lstat(src); err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyAll(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// copyAll copies a file or a whole folder, keeping permission bits.
func copyAll(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if st.IsDir() {
		if err := os.MkdirAll(dst, st.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyAll(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !st.Mode().IsRegular() {
		return nil // punk0 writes no symlinks or devices here
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
