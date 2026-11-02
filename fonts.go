package main

// Installed fonts for the settings modal's font pickers (GET api/fonts).
// fontconfig answers where it is installed (Linux, BSD, a Homebrew Mac): it
// is what the browser itself resolves names with there. Elsewhere the font
// folders are read, taking each file's family name and whether it is
// monospaced from its own tables (readFontFile).

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// fontFamily is one family a font-family name can ask for. Mono is set when
// any face of it has fixed-width letters.
type fontFamily struct {
	Name string `json:"name"`
	Mono bool   `json:"mono"`
}

// fontsFresh is how long a listing is reused: a font installed meanwhile
// shows the next time the modal opens after it.
const fontsFresh = time.Minute

var fontsCache struct {
	sync.Mutex
	at   time.Time
	list []fontFamily
}

// serveFonts is GET api/fonts for both pages: the families installed where
// punk0 runs, sorted by name.
func serveFonts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if !trustedGet(w, r) {
		return
	}
	writeJSON(w, map[string]any{"fonts": installedFonts()})
}

func installedFonts() []fontFamily {
	fontsCache.Lock()
	defer fontsCache.Unlock()
	if fontsCache.list != nil && time.Since(fontsCache.at) < fontsFresh {
		return fontsCache.list
	}
	var list []fontFamily
	if out, err := exec.Command("fc-list", "--format", "%{family[0]}\t%{spacing}\n").Output(); err == nil {
		list = parseFcList(out)
	} else {
		list = scanFontDirs(fontDirs())
	}
	fontsCache.at, fontsCache.list = time.Now(), list
	return list
}

// parseFcList reads `fc-list --format '%{family[0]}\t%{spacing}\n'`: one
// line per face, its first family name and fontconfig's spacing (90 dual
// width, 100 mono, 110 charcell; empty for proportional).
func parseFcList(out []byte) []fontFamily {
	mono := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		name, spacing, _ := strings.Cut(sc.Text(), "\t")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		n, _ := strconv.Atoi(strings.TrimSpace(spacing))
		mono[name] = mono[name] || n >= 90
	}
	return sortedFamilies(mono)
}

func sortedFamilies(mono map[string]bool) []fontFamily {
	list := make([]fontFamily, 0, len(mono))
	for name, m := range mono {
		list = append(list, fontFamily{Name: name, Mono: m})
	}
	slices.SortFunc(list, func(a, b fontFamily) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.Name, b.Name))
	})
	return list
}

// fontDirs are the folders fonts are installed in on this OS, the user's own
// last.
func fontDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{"/System/Library/Fonts", "/Library/Fonts", filepath.Join(home, "Library", "Fonts")}
	case "windows":
		return []string{
			filepath.Join(cmp.Or(os.Getenv("WINDIR"), `C:\Windows`), "Fonts"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Windows", "Fonts"),
		}
	}
	return []string{"/usr/share/fonts", "/usr/local/share/fonts", filepath.Join(home, ".local", "share", "fonts"), filepath.Join(home, ".fonts")}
}

// scanFontDirs reads every font file below dirs. A file that cannot be read
// or parsed is skipped.
func scanFontDirs(dirs []string) []fontFamily {
	mono := map[string]bool{}
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".ttf", ".otf", ".ttc", ".otc":
			default:
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			faces, err := readFontFile(f)
			if err != nil {
				return nil
			}
			for _, fc := range faces {
				mono[fc.Name] = mono[fc.Name] || fc.Mono
			}
			return nil
		})
	}
	return sortedFamilies(mono)
}

var errNotFont = errors.New("not a font file")

// readFontFile reads the family of each face in an OpenType/TrueType file or
// collection. The name is the typographic family (name ID 16), which groups
// weights the way CSS asks for them, else the family (ID 1). A face is
// monospaced when post.isFixedPitch is set or its OS/2 PANOSE proportion
// says so: patched fonts (Nerd Fonts) often clear the first and keep the
// second.
func readFontFile(r io.ReaderAt) ([]fontFamily, error) {
	var tag [4]byte
	if _, err := r.ReadAt(tag[:], 0); err != nil {
		return nil, errNotFont
	}
	if string(tag[:]) != "ttcf" {
		fc, err := readFace(r, 0)
		if err != nil {
			return nil, err
		}
		return []fontFamily{fc}, nil
	}
	var hdr [12]byte
	if _, err := r.ReadAt(hdr[:], 0); err != nil {
		return nil, errNotFont
	}
	n := min(binary.BigEndian.Uint32(hdr[8:]), 256)
	offs := make([]byte, 4*n)
	if _, err := r.ReadAt(offs, 12); err != nil {
		return nil, errNotFont
	}
	var faces []fontFamily
	for i := range n {
		if fc, err := readFace(r, int64(binary.BigEndian.Uint32(offs[4*i:]))); err == nil {
			faces = append(faces, fc)
		}
	}
	if len(faces) == 0 {
		return nil, errNotFont
	}
	return faces, nil
}

// readFace reads one face whose table directory starts at off.
func readFace(r io.ReaderAt, off int64) (fontFamily, error) {
	var hdr [12]byte
	if _, err := r.ReadAt(hdr[:], off); err != nil {
		return fontFamily{}, errNotFont
	}
	switch v := binary.BigEndian.Uint32(hdr[:4]); v {
	case 0x00010000, 0x4f54544f, 0x74727565: // TrueType, 'OTTO', 'true'
	default:
		return fontFamily{}, errNotFont
	}
	numTables := int(binary.BigEndian.Uint16(hdr[4:]))
	dir := make([]byte, 16*numTables)
	if _, err := r.ReadAt(dir, off+12); err != nil {
		return fontFamily{}, errNotFont
	}
	table := func(name string) []byte {
		for i := range numTables {
			rec := dir[16*i:]
			if string(rec[:4]) != name {
				continue
			}
			o, l := binary.BigEndian.Uint32(rec[8:]), binary.BigEndian.Uint32(rec[12:])
			if l > 4<<20 {
				return nil
			}
			b := make([]byte, l)
			if _, err := r.ReadAt(b, int64(o)); err != nil {
				return nil
			}
			return b
		}
		return nil
	}
	name := familyName(table("name"))
	if name == "" {
		return fontFamily{}, errNotFont
	}
	mono := false
	if post := table("post"); len(post) >= 16 && binary.BigEndian.Uint32(post[12:]) != 0 {
		mono = true
	}
	// OS/2: PANOSE starts at byte 32; bFamilyType 2 (Latin text) with
	// bProportion 9 is monospaced.
	if os2 := table("OS/2"); len(os2) >= 42 && os2[32] == 2 && os2[35] == 9 {
		mono = true
	}
	return fontFamily{Name: name, Mono: mono}, nil
}

// familyName picks name ID 16, else 1, from a name table, preferring the
// Windows Unicode records (platform 3, UTF-16BE) to the Mac Roman ones.
func familyName(t []byte) string {
	if len(t) < 6 {
		return ""
	}
	count := int(binary.BigEndian.Uint16(t[2:]))
	strs := int(binary.BigEndian.Uint16(t[4:]))
	best, bestRank := "", 0
	for i := range count {
		rec := 6 + 12*i
		if rec+12 > len(t) {
			break
		}
		platform := binary.BigEndian.Uint16(t[rec:])
		id := binary.BigEndian.Uint16(t[rec+6:])
		length := int(binary.BigEndian.Uint16(t[rec+8:]))
		start := strs + int(binary.BigEndian.Uint16(t[rec+10:]))
		if (id != 1 && id != 16) || start+length > len(t) {
			continue
		}
		raw := t[start : start+length]
		var s string
		rank := 0
		switch platform {
		case 0, 3:
			u := make([]uint16, len(raw)/2)
			for j := range u {
				u[j] = binary.BigEndian.Uint16(raw[2*j:])
			}
			s, rank = string(utf16.Decode(u)), 2
		case 1:
			s, rank = string(raw), 1 // ASCII names read the same in Mac Roman
		default:
			continue
		}
		if id == 16 {
			rank += 2
		}
		if s = strings.TrimSpace(s); s != "" && rank > bestRank {
			best, bestRank = s, rank
		}
	}
	return best
}
