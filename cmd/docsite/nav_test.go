package main

import (
	"strings"
	"testing"
)

func TestGenerateNav(t *testing.T) {
	base := []byte("site_name: Demo\ntheme:\n  name: material\n")
	pages := []Page{
		{Slug: "install", Title: "Install"},
		{Slug: "quick-start", Title: "Quick start"},
		{Slug: "error-handling", Title: "Error handling"},
	}
	groups := []Group{
		{Name: "Getting started", Sections: []string{"Install", "Quick start"}},
		{Name: "Production", Sections: []string{"Error handling"}},
	}
	got, err := GenerateNav(base, pages, groups)
	if err != nil {
		t.Fatalf("GenerateNav: %v", err)
	}
	gotStr := string(got)
	for _, want := range []string{
		"site_name: Demo",
		"theme:\n  name: material",
		"nav:",
		"- Home: index.md",
		"- Getting started:",
		"    - Install: install.md",
		"    - Quick start: quick-start.md",
		"- Production:",
		"    - Error handling: error-handling.md",
	} {
		if !strings.Contains(gotStr, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, gotStr)
		}
	}
}

func TestGenerateNav_FailsOnSectionWithoutPage(t *testing.T) {
	base := []byte("site_name: Demo\n")
	pages := []Page{{Slug: "install", Title: "Install"}}
	groups := []Group{
		{Name: "Getting started", Sections: []string{"Install", "Phantom"}},
	}
	_, err := GenerateNav(base, pages, groups)
	if err == nil {
		t.Error("expected error when group references non-existent section")
	}
}
