package main

import (
	"os"
	"regexp"
	"testing"
)

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
