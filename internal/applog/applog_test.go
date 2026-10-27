package applog

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestRotatorKeepsTwoOlderParts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	r, err := openRotator(p, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("a", 39) + "\n" // 40 bytes: two fit, the third rotates
	for i := range 9 {
		r.Write([]byte(fmt.Sprintf("%d%s", i, line[1:])))
	}
	read := func(f string) string {
		b, _ := os.ReadFile(f)
		return string(b)
	}
	cur, one, two := read(p), read(p+".1"), read(p+".2")
	// parts of two lines: 0 1 | 2 3 | 4 5 | 6 7 | 8
	if cur != "8"+line[1:] {
		t.Fatalf("current part %q", cur)
	}
	if !strings.HasPrefix(one, "6") || strings.Count(one, "\n") != 2 || !strings.HasPrefix(two, "4") {
		t.Fatalf(".1 %q .2 %q", one, two)
	}
	if _, err := os.Stat(p + ".3"); err == nil {
		t.Fatal("a third older part was kept")
	}
}

func TestRotatorWritesALineLongerThanTheCap(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	r, _ := openRotator(p, 10, 2)
	long := strings.Repeat("b", 50) + "\n"
	r.Write([]byte(long))
	if b, _ := os.ReadFile(p); string(b) != long {
		t.Fatalf("first line in an empty file must go in whole, got %q", b)
	}
}

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		"token ghp_" + strings.Repeat("A", 36) + " end":          "token [redacted] end",
		"key sk-ant-api03-" + strings.Repeat("x", 30):              "key [redacted]",
		"Authorization: Bearer abcdefgh12345678":                    "Authorization: Bearer [redacted]",
		"clone https://x-access-token:s3cretvalue@github.com/a/b":   "clone https://x-access-token:[redacted]@github.com/a/b",
		"plain https://github.com/a/b and the token was refreshed": "plain https://github.com/a/b and the token was refreshed",
	} {
		if got := string(Redact([]byte(in))); got != want {
			t.Errorf("Redact(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestReplacePathWholeElementOnly(t *testing.T) {
	for _, c := range []struct{ in, dir, want string }{
		{"dir=/home/ann/src x", "/home/ann", "dir=~/src x"},
		{"dir=/home/anna/src", "/home/ann", "dir=/home/anna/src"},
		{"a /home/ann b /home/ann", "/home/ann", "a ~ b ~"},
		// slog quotes a Windows path and doubles its backslashes
		{`dir="C:\\Users\\ann\\code"`, `C:\\Users\\ann`, `dir="~\\code"`},
	} {
		if got := string(replacePath([]byte(c.in), c.dir)); got != c.want {
			t.Errorf("replacePath(%q, %q) = %q, want %q", c.in, c.dir, got, c.want)
		}
	}
}

func TestStartChildAndTail(t *testing.T) {
	state := t.TempDir()
	prevSlog, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	t.Cleanup(func() {
		slog.SetDefault(prevSlog)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		debug.SetCrashOutput(nil, debug.CrashOptions{})
		mu.Lock()
		out, base = nil, nil
		mu.Unlock()
	})
	p, err := Start(state, slog.LevelInfo, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	slog.Info("parent says", "k", "v")
	log.Printf("from the log package")
	Child("my app", `time=2026-10-27T11:02:03.000+05:30 level=WARN msg="child says" k=1`)
	Child("api", "panic: boom")
	Child("api", "   ")
	Append(state, slog.LevelWarn, "hook report failed", "event", "SessionStart")

	lines, err := Tail(10)
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(state, "logs", FileName) || len(lines) != 5 {
		t.Fatalf("path %s, lines:\n%s", p, strings.Join(lines, "\n"))
	}
	for i, want := range []string{
		`msg="parent says" src=main k=v`,
		`msg="from the log package" src=main`,
		`level=WARN msg="child says" k=1 src=child ws="my app"`,
		`msg="panic: boom" src=child ws=api`,
		`level=WARN msg="hook report failed" src=hook event=SessionStart`,
	} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d = %q, want it to contain %q", i, lines[i], want)
		}
	}
	if st, _ := os.Stat(filepath.Dir(p)); st.Mode().Perm() != 0o700 && os.PathSeparator == '/' {
		t.Errorf("logs folder mode %v", st.Mode().Perm())
	}
}
