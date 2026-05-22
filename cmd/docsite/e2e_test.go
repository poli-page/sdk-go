package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEndToEnd_SimpleFixture(t *testing.T) {
	out := t.TempDir()
	nav := filepath.Join(out, "mkdocs.yml")
	pages := filepath.Join(out, "site")
	err := run(
		"testdata/simple/README.md",
		"testdata/simple/groups.yml",
		"testdata/simple/mkdocs.base.yml",
		pages,
		nav,
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, name := range []string{"index.md", "install.md", "quick-start.md", "errors.md"} {
		if _, err := os.Stat(filepath.Join(pages, name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
	navBytes, err := os.ReadFile(nav)
	if err != nil {
		t.Fatalf("read nav: %v", err)
	}
	for _, want := range []string{"- Install: install.md", "- Quick start: quick-start.md", "- Errors: errors.md"} {
		if !strings.Contains(string(navBytes), want) {
			t.Errorf("nav missing %q\n%s", want, navBytes)
		}
	}
}

func TestEndToEnd_RealReadme(t *testing.T) {
	// Skip if the real groups.yml and mkdocs.base.yml are not yet committed.
	for _, p := range []string{"../../README.md", "../../docs/groups.yml", "../../docs/mkdocs.base.yml"} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("missing %s — run after Task 12/13: %v", p, err)
		}
	}
	out := t.TempDir()
	pages := filepath.Join(out, "site")
	nav := filepath.Join(out, "mkdocs.yml")
	if err := run("../../README.md", "../../docs/groups.yml", "../../docs/mkdocs.base.yml", pages, nav); err != nil {
		t.Fatalf("run on real README: %v", err)
	}
	entries, err := os.ReadDir(pages)
	if err != nil {
		t.Fatalf("read out dir: %v", err)
	}
	// 16 H2s in README + 1 index = 17 files. If this assertion fires,
	// update the README, groups.yml, or both — the spec says the number
	// of pages is the number of H2s plus index.
	if len(entries) != 17 {
		t.Errorf("got %d pages, want 17 (16 H2s + index)", len(entries))
	}
}
