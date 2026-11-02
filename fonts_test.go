package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"regexp"
	"slices"
	"testing"
	"unicode/utf16"
)

// A family is listed once, monospaced when any of its faces is, whatever
// order fontconfig prints the faces in; proportional faces have no spacing.
func TestParseFcListMergesFacesByFamily(t *testing.T) {
	out := "Noto Sans\t\n" +
		"JetBrainsMono Nerd Font Mono\t100\n" +
		"Fira Code\t\n" + // a proportional-looking face first
		"Fira Code\t100\n" +
		"Unifont\t110\n" +
		"Dual\t90\n" +
		"\t100\n"
	got := parseFcList([]byte(out))
	want := []fontFamily{
		{"Dual", true}, {"Fira Code", true}, {"JetBrainsMono Nerd Font Mono", true}, {"Noto Sans", false}, {"Unifont", true},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

// sfntFile builds a minimal OpenType file: a name table with the given
// records (id -> name, Windows UTF-16BE), a post table and an OS/2 table
// carrying the two monospace markers.
func sfntFile(names map[uint16]string, fixedPitch bool, panoseMono bool) []byte {
	var strs bytes.Buffer
	type rec struct{ id, off, n uint16 }
	var recs []rec
	for _, id := range []uint16{1, 16} {
		s, ok := names[id]
		if !ok {
			continue
		}
		u := utf16.Encode([]rune(s))
		off := strs.Len()
		for _, c := range u {
			binary.Write(&strs, binary.BigEndian, c)
		}
		recs = append(recs, rec{id, uint16(off), uint16(2 * len(u))})
	}
	var name bytes.Buffer
	binary.Write(&name, binary.BigEndian, []uint16{0, uint16(len(recs)), uint16(6 + 12*len(recs))})
	for _, r := range recs {
		binary.Write(&name, binary.BigEndian, []uint16{3, 1, 0x409, r.id, r.n, r.off})
	}
	name.Write(strs.Bytes())

	post := make([]byte, 32)
	if fixedPitch {
		binary.BigEndian.PutUint32(post[12:], 1)
	}
	os2 := make([]byte, 96)
	if panoseMono {
		os2[32], os2[35] = 2, 9
	}

	tables := []struct {
		tag  string
		data []byte
	}{{"OS/2", os2}, {"name", name.Bytes()}, {"post", post}}
	var f bytes.Buffer
	binary.Write(&f, binary.BigEndian, uint32(0x00010000))
	binary.Write(&f, binary.BigEndian, []uint16{uint16(len(tables)), 0, 0, 0})
	off := 12 + 16*len(tables)
	for _, tb := range tables {
		f.WriteString(tb.tag)
		binary.Write(&f, binary.BigEndian, []uint32{0, uint32(off), uint32(len(tb.data))})
		off += len(tb.data)
	}
	for _, tb := range tables {
		f.Write(tb.data)
	}
	return f.Bytes()
}

func TestReadFontFileFamilyAndMonospace(t *testing.T) {
	cases := []struct {
		label       string
		names       map[uint16]string
		fixed, pano bool
		want        fontFamily
	}{
		// Weights other than regular/bold name their own family in ID 1;
		// CSS asks for the typographic family.
		{"typographic family wins", map[uint16]string{1: "Fira Code SemiBold", 16: "Fira Code"}, true, false, fontFamily{"Fira Code", true}},
		{"family alone", map[uint16]string{1: "Inter"}, false, false, fontFamily{"Inter", false}},
		// Nerd Font patches clear isFixedPitch but keep PANOSE monospaced.
		{"panose only", map[uint16]string{1: "JetBrainsMono NFM"}, false, true, fontFamily{"JetBrainsMono NFM", true}},
	}
	for _, c := range cases {
		got, err := readFontFile(bytes.NewReader(sfntFile(c.names, c.fixed, c.pano)))
		if err != nil || len(got) != 1 || got[0] != c.want {
			t.Errorf("%s: got %v, %v; want %v", c.label, got, err, c.want)
		}
	}

	// A collection lists each face.
	a, b := sfntFile(map[uint16]string{1: "A Mono"}, true, false), sfntFile(map[uint16]string{1: "B Sans"}, false, false)
	var ttc bytes.Buffer
	ttc.WriteString("ttcf")
	binary.Write(&ttc, binary.BigEndian, []uint32{0x00010000, 2, 20, uint32(20 + len(a))})
	ttc.Write(a)
	// Table offsets in a collection are from the file start: shift b's.
	shifted := slices.Clone(b)
	for i := range int(binary.BigEndian.Uint16(b[4:])) {
		p := 12 + 16*i + 8
		binary.BigEndian.PutUint32(shifted[p:], binary.BigEndian.Uint32(b[p:])+uint32(20+len(a)))
	}
	for i := range int(binary.BigEndian.Uint16(a[4:])) {
		p := 20 + 12 + 16*i + 8
		binary.BigEndian.PutUint32(ttc.Bytes()[p:], binary.BigEndian.Uint32(a[12+16*i+8:])+20)
	}
	ttc.Write(shifted)
	got, err := readFontFile(bytes.NewReader(ttc.Bytes()))
	if err != nil || !slices.Equal(got, []fontFamily{{"A Mono", true}, {"B Sans", false}}) {
		t.Fatalf("collection: got %v, %v", got, err)
	}

	if _, err := readFontFile(bytes.NewReader([]byte("not a font at all"))); err == nil {
		t.Fatal("a text file read as a font")
	}
}

// Every font web/style.css loads from punk0 must be one the server can
// fetch: a url missing from vendorCDNs answers 404 and the font silently
// falls back.
func TestStyleFontsAreServed(t *testing.T) {
	css, err := os.ReadFile("web/style.css")
	if err != nil {
		t.Fatal(err)
	}
	urls := regexp.MustCompile(`url\("(vendor/fonts/[^"]+)"\)`).FindAllSubmatch(css, -1)
	if len(urls) == 0 {
		t.Fatal("no punk0 fonts in web/style.css")
	}
	for _, m := range urls {
		if _, ok := vendorCDNs[string(m[1])]; !ok {
			t.Errorf("%s is in web/style.css but not in vendorCDNs", m[1])
		}
	}
}
