package main

import (
	"slices"
	"testing"
)

func TestRestartArgv(t *testing.T) {
	ours := []string{"-restarted", "-no-open", "-port", "7778"}
	with := func(parts ...[]string) []string { return slices.Concat(parts...) }
	cases := []struct {
		name string
		args []string
		nArg int
		want []string
	}{
		{"bare", []string{"punk0"}, 0, with([]string{"punk0"}, ours)},
		{"flags and a folder", []string{"punk0", "-no-lsp", "dir"}, 1, with([]string{"punk0", "-no-lsp"}, ours, []string{"dir"})},
		{"own port loses to the bound one", []string{"punk0", "-port", "7777", "--", "dir"}, 1,
			with([]string{"punk0", "-port", "7777"}, ours, []string{"--", "dir"})},
		{"dash-leading folder", []string{"punk0", "--", "-weird"}, 1, with([]string{"punk0"}, ours, []string{"--", "-weird"})},
		{"dev and no-open kept", []string{"punk0", "-dev=/x", "-no-open"}, 0, with([]string{"punk0", "-dev=/x", "-no-open"}, ours)},
		{"a restart restarted", with([]string{"punk0", "-no-lsp", "-restarted", "-no-open", "-port", "7777"}, []string{"dir"}), 1,
			with([]string{"punk0", "-no-lsp"}, ours, []string{"dir"})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := restartArgv(c.args, c.nArg, "7778"); !slices.Equal(got, c.want) {
				t.Fatalf("restartArgv(%q) =\n %q\nwant\n %q", c.args, got, c.want)
			}
		})
	}
}
