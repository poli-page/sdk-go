package main

import (
	"strings"
	"testing"
)

func TestSplit_Preamble(t *testing.T) {
	in := `# Title

Some intro text.

[![Badge](x.png)](y)

## Install

How to install.
`
	res, err := Split([]byte(in))
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	wantIndex := "# Title\n\nSome intro text.\n\n[![Badge](x.png)](y)\n"
	if res.Index.Content != wantIndex {
		t.Errorf("index content mismatch:\ngot:  %q\nwant: %q", res.Index.Content, wantIndex)
	}
	if res.Index.Slug != "index" {
		t.Errorf("index slug = %q, want %q", res.Index.Slug, "index")
	}
}

func TestSplit_OneFilePerH2(t *testing.T) {
	in := `# Title

intro

## Install

install body

## Quick start

quick body

### Sub

sub body

## Errors

errors body
`
	res, err := Split([]byte(in))
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(res.Pages) != 3 {
		t.Fatalf("got %d pages, want 3", len(res.Pages))
	}
	gotTitles := []string{res.Pages[0].Title, res.Pages[1].Title, res.Pages[2].Title}
	wantTitles := []string{"Install", "Quick start", "Errors"}
	for i := range gotTitles {
		if gotTitles[i] != wantTitles[i] {
			t.Errorf("page %d title = %q, want %q", i, gotTitles[i], wantTitles[i])
		}
	}
	if !strings.Contains(res.Pages[1].Content, "### Sub") {
		t.Errorf("Quick start page should contain its H3 subsection, got:\n%s", res.Pages[1].Content)
	}
	gotSlugs := []string{res.Pages[0].Slug, res.Pages[1].Slug, res.Pages[2].Slug}
	wantSlugs := []string{"install", "quick-start", "errors"}
	for i := range gotSlugs {
		if gotSlugs[i] != wantSlugs[i] {
			t.Errorf("page %d slug = %q, want %q", i, gotSlugs[i], wantSlugs[i])
		}
	}
}

func TestSplit_H2DemotedToH1(t *testing.T) {
	in := "## Install\n\nbody\n"
	res, err := Split([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("want 1 page, got %d", len(res.Pages))
	}
	want := "# Install\n\nbody\n"
	if res.Pages[0].Content != want {
		t.Errorf("page content = %q, want %q", res.Pages[0].Content, want)
	}
}

func TestSplit_PageContentEndsWithSingleNewline(t *testing.T) {
	in := "# Title\n\nintro\n\n## A\n\nbody A\n\n\n## B\n\nbody B\n"
	res, err := Split([]byte(in))
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	for i, p := range res.Pages {
		if !strings.HasSuffix(p.Content, "\n") {
			t.Errorf("page %d (%q) does not end with newline: %q", i, p.Title, p.Content)
		}
		if strings.HasSuffix(p.Content, "\n\n") {
			t.Errorf("page %d (%q) ends with multiple newlines: %q", i, p.Title, p.Content)
		}
	}
}
