package applog

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var (
	// tokenRE matches credentials by their published prefixes: GitHub,
	// GitLab, Anthropic/OpenAI style sk- keys, Slack.
	tokenRE = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{20,}|sk-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,})`)
	// authRE matches the value of an Authorization header.
	authRE = regexp.MustCompile(`(?i)\b(bearer|basic|authorization:\s*token) [A-Za-z0-9._~+/=-]{8,}`)
	// userinfoRE matches the password of a URL like https://user:secret@host.
	userinfoRE = regexp.MustCompile(`(://[^/\s:@"]+:)[^@\s/"]+@`)

	homeOnce  sync.Once
	homeForms []string // the home folder as paths and slog's quoting write it
)

// Redact replaces credentials with [redacted] and the home folder with ~.
func Redact(p []byte) []byte {
	p = tokenRE.ReplaceAll(p, []byte("[redacted]"))
	p = authRE.ReplaceAll(p, []byte("$1 [redacted]"))
	p = userinfoRE.ReplaceAll(p, []byte("${1}[redacted]@"))
	homeOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil || len(home) < 2 {
			return
		}
		homeForms = []string{home}
		if q := strings.ReplaceAll(home, `\`, `\\`); q != home {
			homeForms = append(homeForms, q)
		}
		if s := filepath.ToSlash(home); s != home {
			homeForms = append(homeForms, s)
		}
	})
	for _, h := range homeForms {
		p = replacePath(p, h)
	}
	return p
}

// replacePath puts ~ for dir where it is a whole path element: /home/ann
// in /home/ann/x, not in /home/anna.
func replacePath(p []byte, dir string) []byte {
	d := []byte(dir)
	var out []byte
	for {
		i := bytes.Index(p, d)
		if i < 0 {
			break
		}
		end := i + len(d)
		if end < len(p) && isNameByte(p[end]) {
			out = append(out, p[:end]...)
			p = p[end:]
			continue
		}
		out = append(append(out, p[:i]...), '~')
		p = p[end:]
	}
	if out == nil {
		return p
	}
	return append(out, p...)
}

func isNameByte(c byte) bool {
	return c == '_' || c == '-' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}
