# GitHub Pages Doc Site — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a GitHub Pages site that mirrors `README.md` as a multi-page, versioned (via mike) MkDocs Material site under `https://poli-page.github.io/sdk-go/`, with a Go splitter that fails loud on README/groups drift.

**Architecture:** A small Go CLI (`cmd/docsite`) reads `README.md`, splits it by `## ` into per-page markdown files, generates an `mkdocs.yml` nav by merging with `docs/mkdocs.base.yml` according to `docs/groups.yml`, and emits everything under `build/` (gitignored). A GitHub Actions workflow runs the splitter, then `mike deploy` to push to the `gh-pages` branch. GitHub Pages serves from that branch (legacy/branch mode).

**Tech Stack:** Go 1.25 (no new module deps), MkDocs Material 9.x, mike 2.x, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-05-22-gh-pages-doc-site-design.md` (commit `f6c355f`).

---

## File Structure

**Created:**
- `cmd/docsite/main.go` — CLI flag parsing + orchestration.
- `cmd/docsite/slug.go` — title → kebab-case slug.
- `cmd/docsite/split.go` — README → preamble + per-H2 pages.
- `cmd/docsite/anchor.go` — rewrite `#anchor` links to point at the right page.
- `cmd/docsite/groups.go` — parse `groups.yml`, validate against pages.
- `cmd/docsite/nav.go` — emit final `mkdocs.yml` by merging base + nav.
- `cmd/docsite/io.go` — write outputs to disk.
- `cmd/docsite/slug_test.go`
- `cmd/docsite/split_test.go`
- `cmd/docsite/anchor_test.go`
- `cmd/docsite/groups_test.go`
- `cmd/docsite/nav_test.go`
- `cmd/docsite/e2e_test.go` — end-to-end on the real `README.md`.
- `cmd/docsite/testdata/simple/README.md` — minimal fixture.
- `cmd/docsite/testdata/simple/groups.yml`
- `cmd/docsite/testdata/simple/mkdocs.base.yml`
- `docs/groups.yml` — H2 → group mapping for the real README.
- `docs/mkdocs.base.yml` — MkDocs Material + mike base config.
- `docs/requirements.txt` — pinned Python deps for CI.
- `.github/workflows/pages.yml` — build + deploy workflow.

**Modified:**
- `.gitignore` — add `/build/`.

**Rationale:** Each `cmd/docsite/*.go` file has one responsibility (slug, splitting, anchor rewriting, groups, nav, I/O). Files that change together stay together but no single file does too much. `main.go` is glue only.

---

## Task 1: Bootstrap package + slug helper (TDD)

**Files:**
- Create: `cmd/docsite/slug.go`
- Create: `cmd/docsite/slug_test.go`

- [ ] **Step 1: Write the failing test**

`cmd/docsite/slug_test.go`:

```go
package main

import "testing"

func TestSlug(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Install", "install"},
		{"Quick start", "quick-start"},
		{"Error handling", "error-handling"},
		{"Authentication & environments", "authentication-environments"},
		{"Retries & idempotency", "retries-idempotency"},
		{"Working with stored documents", "working-with-stored-documents"},
		{"  Trim me  ", "trim-me"},
		{"Multiple   spaces", "multiple-spaces"},
	}
	for _, c := range cases {
		got := slug(c.in)
		if got != c.want {
			t.Errorf("slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/docsite/ -run TestSlug -v`
Expected: FAIL — `slug` undefined.

- [ ] **Step 3: Write minimal implementation**

`cmd/docsite/slug.go`:

```go
package main

import "strings"

func slug(title string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/docsite/ -run TestSlug -v`
Expected: PASS, all cases.

- [ ] **Step 5: Run full suite + lint**

Run: `go test -race ./... && go vet ./... && gofmt -l .`
Expected: no failures, `gofmt -l .` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add cmd/docsite/slug.go cmd/docsite/slug_test.go
git commit -m "feat(docsite): add slug helper for section titles"
```

---

## Task 2: Split preamble into index page

**Files:**
- Create: `cmd/docsite/split.go`
- Create: `cmd/docsite/split_test.go`

- [ ] **Step 1: Write the failing test**

`cmd/docsite/split_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/docsite/ -run TestSplit_Preamble -v`
Expected: FAIL — `Split` and `SplitResult` undefined.

- [ ] **Step 3: Write minimal implementation**

`cmd/docsite/split.go`:

```go
package main

import (
	"bytes"
	"strings"
)

type Page struct {
	Slug    string
	Title   string
	Content string
}

type SplitResult struct {
	Index Page
	Pages []Page
}

func Split(readme []byte) (SplitResult, error) {
	lines := strings.SplitAfter(string(readme), "\n")
	var preamble bytes.Buffer
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			break
		}
		preamble.WriteString(line)
	}
	return SplitResult{
		Index: Page{Slug: "index", Content: preamble.String()},
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/docsite/ -run TestSplit_Preamble -v`
Expected: PASS.

- [ ] **Step 5: Run full suite + lint**

Run: `go test -race ./... && go vet ./... && gofmt -l .`

- [ ] **Step 6: Commit**

```bash
git add cmd/docsite/split.go cmd/docsite/split_test.go
git commit -m "feat(docsite): extract README preamble into index page"
```

---

## Task 3: One page per H2 section

**Files:**
- Modify: `cmd/docsite/split.go`
- Modify: `cmd/docsite/split_test.go`

- [ ] **Step 1: Add failing test**

Append to `cmd/docsite/split_test.go`:

```go
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
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/docsite/ -run TestSplit_OneFilePerH2 -v`
Expected: FAIL — `len(res.Pages) != 3`, currently 0.

- [ ] **Step 3: Update implementation**

Replace `Split` in `cmd/docsite/split.go` with:

```go
func Split(readme []byte) (SplitResult, error) {
	lines := strings.SplitAfter(string(readme), "\n")
	var preamble bytes.Buffer
	var pages []Page
	var cur *Page
	flush := func() {
		if cur != nil {
			pages = append(pages, *cur)
			cur = nil
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			cur = &Page{
				Slug:  slug(title),
				Title: title,
			}
			continue
		}
		if cur != nil {
			cur.Content += line
		} else {
			preamble.WriteString(line)
		}
	}
	flush()
	return SplitResult{
		Index: Page{Slug: "index", Content: preamble.String()},
		Pages: pages,
	}, nil
}
```

- [ ] **Step 4: Run both split tests**

Run: `go test ./cmd/docsite/ -run TestSplit -v`
Expected: PASS for `TestSplit_Preamble` and `TestSplit_OneFilePerH2`.

- [ ] **Step 5: Run full suite + lint**

Run: `go test -race ./... && go vet ./... && gofmt -l .`

- [ ] **Step 6: Commit**

```bash
git add cmd/docsite/split.go cmd/docsite/split_test.go
git commit -m "feat(docsite): split README into per-H2 pages"
```

---

## Task 4: Demote H2 to H1 on each page

**Files:**
- Modify: `cmd/docsite/split.go`
- Modify: `cmd/docsite/split_test.go`

- [ ] **Step 1: Add failing test**

Append to `cmd/docsite/split_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/docsite/ -run TestSplit_H2DemotedToH1 -v`
Expected: FAIL — page content is missing the `# Install` line (currently the H2 header line is dropped entirely).

- [ ] **Step 3: Update implementation**

In `cmd/docsite/split.go`, replace the H2 branch with one that emits the demoted heading as the first content line:

```go
		if strings.HasPrefix(line, "## ") {
			flush()
			title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			cur = &Page{
				Slug:    slug(title),
				Title:   title,
				Content: "# " + title + "\n",
			}
			continue
		}
```

- [ ] **Step 4: Run all split tests**

Run: `go test ./cmd/docsite/ -run TestSplit -v`
Expected: all PASS, including `TestSplit_OneFilePerH2` which uses content checks (no H1 assertion there, so still passes).

- [ ] **Step 5: Run full suite + lint**

Run: `go test -race ./... && go vet ./... && gofmt -l .`

- [ ] **Step 6: Commit**

```bash
git add cmd/docsite/split.go cmd/docsite/split_test.go
git commit -m "feat(docsite): demote H2 headings to H1 on generated pages"
```

---

## Task 5: Anchor rewriting (cross-page links)

**Files:**
- Create: `cmd/docsite/anchor.go`
- Create: `cmd/docsite/anchor_test.go`

- [ ] **Step 1: Write the failing test**

`cmd/docsite/anchor_test.go`:

```go
package main

import "testing"

func TestRewriteAnchors_CrossPage(t *testing.T) {
	pages := []Page{
		{Slug: "install", Title: "Install", Content: "# Install\nSee [errors](#error-handling).\n"},
		{Slug: "error-handling", Title: "Error handling", Content: "# Error handling\nbody\n"},
	}
	index := Page{Slug: "index", Content: "Jump to [install](#install).\n"}
	RewriteAnchors(&index, pages)
	if want := "Jump to [install](install/).\n"; index.Content != want {
		t.Errorf("index = %q, want %q", index.Content, want)
	}
	if want := "# Install\nSee [errors](error-handling/).\n"; pages[0].Content != want {
		t.Errorf("install page = %q, want %q", pages[0].Content, want)
	}
}

func TestRewriteAnchors_LeavesExternalLinksAlone(t *testing.T) {
	pages := []Page{
		{Slug: "install", Title: "Install", Content: "# Install\n[link](https://example.com#frag) and ![img](img.png)\n"},
	}
	index := Page{Slug: "index"}
	RewriteAnchors(&index, pages)
	want := "# Install\n[link](https://example.com#frag) and ![img](img.png)\n"
	if pages[0].Content != want {
		t.Errorf("got %q, want %q", pages[0].Content, want)
	}
}

func TestRewriteAnchors_UnknownAnchorUnchanged(t *testing.T) {
	pages := []Page{
		{Slug: "install", Title: "Install", Content: "# Install\nSee [x](#nonexistent).\n"},
	}
	index := Page{Slug: "index"}
	RewriteAnchors(&index, pages)
	want := "# Install\nSee [x](#nonexistent).\n"
	if pages[0].Content != want {
		t.Errorf("got %q, want %q", pages[0].Content, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/docsite/ -run TestRewriteAnchors -v`
Expected: FAIL — `RewriteAnchors` undefined.

- [ ] **Step 3: Write implementation**

`cmd/docsite/anchor.go`:

```go
package main

import "regexp"

var linkRE = regexp.MustCompile(`\]\(#([^)]+)\)`)

// RewriteAnchors rewrites in-document anchor links of the form `](#slug)` so
// that they point to the page whose slug or H1 matches. Links whose anchor
// matches no known section are left untouched. External links (those with a
// scheme before `#`) are not matched by the regex because the `#` is not
// preceded by `](`.
func RewriteAnchors(index *Page, pages []Page) {
	known := make(map[string]string, len(pages))
	for _, p := range pages {
		known[p.Slug] = p.Slug
		known[slug(p.Title)] = p.Slug
	}
	rewrite := func(content string) string {
		return linkRE.ReplaceAllStringFunc(content, func(match string) string {
			anchor := match[3 : len(match)-1]
			if dest, ok := known[anchor]; ok {
				return "](" + dest + "/)"
			}
			return match
		})
	}
	if index != nil {
		index.Content = rewrite(index.Content)
	}
	for i := range pages {
		pages[i].Content = rewrite(pages[i].Content)
	}
}
```

- [ ] **Step 4: Run anchor tests**

Run: `go test ./cmd/docsite/ -run TestRewriteAnchors -v`
Expected: all three tests PASS.

- [ ] **Step 5: Run full suite + lint**

Run: `go test -race ./... && go vet ./... && gofmt -l .`

- [ ] **Step 6: Commit**

```bash
git add cmd/docsite/anchor.go cmd/docsite/anchor_test.go
git commit -m "feat(docsite): rewrite cross-page anchor links to MkDocs paths"
```

---

## Task 6: Groups YAML parser

**Files:**
- Create: `cmd/docsite/groups.go`
- Create: `cmd/docsite/groups_test.go`

The YAML format we accept is constrained (top-level `groups:` list of `{name, sections: [string]}`). A hand-rolled parser keeps the SDK module dependency-free as the spec requires.

- [ ] **Step 1: Write the failing test**

`cmd/docsite/groups_test.go`:

```go
package main

import (
	"reflect"
	"testing"
)

func TestParseGroups(t *testing.T) {
	in := []byte(`groups:
  - name: Getting started
    sections:
      - Install
      - Quick start
  - name: Production
    sections:
      - Error handling
      - Cancellation
`)
	got, err := ParseGroups(in)
	if err != nil {
		t.Fatalf("ParseGroups: %v", err)
	}
	want := []Group{
		{Name: "Getting started", Sections: []string{"Install", "Quick start"}},
		{Name: "Production", Sections: []string{"Error handling", "Cancellation"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseGroups_RejectsMalformed(t *testing.T) {
	cases := [][]byte{
		[]byte("not yaml at all"),
		[]byte("groups:\n  - sections:\n      - x\n"),    // missing name
		[]byte("groups:\n  - name: Foo\n"),                // missing sections
	}
	for _, c := range cases {
		if _, err := ParseGroups(c); err == nil {
			t.Errorf("ParseGroups(%q) returned nil error, want error", c)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/docsite/ -run TestParseGroups -v`
Expected: FAIL — `ParseGroups` and `Group` undefined.

- [ ] **Step 3: Write implementation**

`cmd/docsite/groups.go`:

```go
package main

import (
	"errors"
	"fmt"
	"strings"
)

type Group struct {
	Name     string
	Sections []string
}

// ParseGroups parses a constrained subset of YAML:
//
//	groups:
//	  - name: <string>
//	    sections:
//	      - <string>
//	      - <string>
//
// It tolerates blank lines and trims trailing whitespace.
func ParseGroups(data []byte) ([]Group, error) {
	lines := strings.Split(string(data), "\n")
	if !hasHeader(lines, "groups:") {
		return nil, errors.New("missing top-level `groups:` key")
	}
	var groups []Group
	var cur *Group
	inSections := false
	for i, raw := range lines {
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimLeft(line, " \t")
		switch {
		case trimmed == "" || trimmed == "groups:":
			continue
		case strings.HasPrefix(trimmed, "- name:"):
			if cur != nil {
				if err := validateGroup(*cur); err != nil {
					return nil, fmt.Errorf("line %d: %w", i+1, err)
				}
				groups = append(groups, *cur)
			}
			cur = &Group{Name: strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))}
			inSections = false
		case trimmed == "sections:":
			inSections = true
		case strings.HasPrefix(trimmed, "- "):
			if cur == nil {
				return nil, fmt.Errorf("line %d: list item before any group", i+1)
			}
			if !inSections {
				return nil, fmt.Errorf("line %d: list item outside `sections:`", i+1)
			}
			cur.Sections = append(cur.Sections, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		default:
			return nil, fmt.Errorf("line %d: unrecognized syntax %q", i+1, raw)
		}
	}
	if cur != nil {
		if err := validateGroup(*cur); err != nil {
			return nil, err
		}
		groups = append(groups, *cur)
	}
	if len(groups) == 0 {
		return nil, errors.New("no groups defined")
	}
	return groups, nil
}

func validateGroup(g Group) error {
	if g.Name == "" {
		return errors.New("group missing name")
	}
	if len(g.Sections) == 0 {
		return fmt.Errorf("group %q has no sections", g.Name)
	}
	return nil
}

func hasHeader(lines []string, key string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) == key {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run group tests**

Run: `go test ./cmd/docsite/ -run TestParseGroups -v`
Expected: both PASS.

- [ ] **Step 5: Run full suite + lint**

Run: `go test -race ./... && go vet ./... && gofmt -l .`

- [ ] **Step 6: Commit**

```bash
git add cmd/docsite/groups.go cmd/docsite/groups_test.go
git commit -m "feat(docsite): parse groups.yml mapping H2 titles to nav groups"
```

---

## Task 7: Orphan validation — H2 missing from groups, group entry missing from README

**Files:**
- Modify: `cmd/docsite/groups.go`
- Modify: `cmd/docsite/groups_test.go`

- [ ] **Step 1: Add failing tests**

Append to `cmd/docsite/groups_test.go`:

```go
func TestValidateGroups_OrphanH2(t *testing.T) {
	pages := []Page{
		{Title: "Install"},
		{Title: "Quick start"},
		{Title: "Forgot me"}, // not in groups
	}
	groups := []Group{
		{Name: "Getting started", Sections: []string{"Install", "Quick start"}},
	}
	err := ValidateGroups(pages, groups)
	if err == nil || !strings.Contains(err.Error(), "Forgot me") {
		t.Errorf("expected error mentioning 'Forgot me', got %v", err)
	}
}

func TestValidateGroups_OrphanGroupEntry(t *testing.T) {
	pages := []Page{
		{Title: "Install"},
	}
	groups := []Group{
		{Name: "Getting started", Sections: []string{"Install", "Removed section"}},
	}
	err := ValidateGroups(pages, groups)
	if err == nil || !strings.Contains(err.Error(), "Removed section") {
		t.Errorf("expected error mentioning 'Removed section', got %v", err)
	}
}

func TestValidateGroups_AllAligned(t *testing.T) {
	pages := []Page{
		{Title: "Install"},
		{Title: "Quick start"},
	}
	groups := []Group{
		{Name: "Getting started", Sections: []string{"Install", "Quick start"}},
	}
	if err := ValidateGroups(pages, groups); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
```

Add this import to the top of the test file if not already present:

```go
import "strings"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/docsite/ -run TestValidateGroups -v`
Expected: FAIL — `ValidateGroups` undefined.

- [ ] **Step 3: Write implementation**

Append to `cmd/docsite/groups.go`:

```go
// ValidateGroups checks that every page title is grouped, and every grouped
// section name corresponds to a real page. Returns a single error listing all
// mismatches so the maintainer fixes them in one pass.
func ValidateGroups(pages []Page, groups []Group) error {
	pageTitles := make(map[string]bool, len(pages))
	for _, p := range pages {
		pageTitles[p.Title] = true
	}
	grouped := make(map[string]bool)
	for _, g := range groups {
		for _, s := range g.Sections {
			grouped[s] = true
		}
	}
	var orphanH2, orphanGroup []string
	for _, p := range pages {
		if !grouped[p.Title] {
			orphanH2 = append(orphanH2, p.Title)
		}
	}
	for _, g := range groups {
		for _, s := range g.Sections {
			if !pageTitles[s] {
				orphanGroup = append(orphanGroup, s)
			}
		}
	}
	if len(orphanH2) == 0 && len(orphanGroup) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("groups.yml is out of sync with README:\n")
	for _, t := range orphanH2 {
		sb.WriteString(fmt.Sprintf("  README has H2 %q but no group lists it\n", t))
	}
	for _, t := range orphanGroup {
		sb.WriteString(fmt.Sprintf("  groups.yml lists %q but no such H2 in README\n", t))
	}
	return errors.New(sb.String())
}
```

- [ ] **Step 4: Run all group tests**

Run: `go test ./cmd/docsite/ -run TestValidateGroups -v && go test ./cmd/docsite/ -run TestParseGroups -v`
Expected: all PASS.

- [ ] **Step 5: Run full suite + lint**

Run: `go test -race ./... && go vet ./... && gofmt -l .`

- [ ] **Step 6: Commit**

```bash
git add cmd/docsite/groups.go cmd/docsite/groups_test.go
git commit -m "feat(docsite): fail loud on README/groups.yml drift"
```

---

## Task 8: Nav generation merged with base mkdocs config

**Files:**
- Create: `cmd/docsite/nav.go`
- Create: `cmd/docsite/nav_test.go`

- [ ] **Step 1: Write the failing test**

`cmd/docsite/nav_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/docsite/ -run TestGenerateNav -v`
Expected: FAIL — `GenerateNav` undefined.

- [ ] **Step 3: Write implementation**

`cmd/docsite/nav.go`:

```go
package main

import (
	"bytes"
	"fmt"
)

// GenerateNav returns the contents of mkdocs.yml: the base file verbatim,
// followed by a generated `nav:` block. ValidateGroups should have run before
// this function — but we double-check section→page mapping defensively.
func GenerateNav(base []byte, pages []Page, groups []Group) ([]byte, error) {
	pageBySection := make(map[string]Page, len(pages))
	for _, p := range pages {
		pageBySection[p.Title] = p
	}
	var out bytes.Buffer
	out.Write(base)
	if !bytes.HasSuffix(base, []byte("\n")) {
		out.WriteByte('\n')
	}
	out.WriteString("\nnav:\n")
	out.WriteString("  - Home: index.md\n")
	for _, g := range groups {
		fmt.Fprintf(&out, "  - %s:\n", g.Name)
		for _, sec := range g.Sections {
			p, ok := pageBySection[sec]
			if !ok {
				return nil, fmt.Errorf("group %q references section %q with no matching page", g.Name, sec)
			}
			fmt.Fprintf(&out, "    - %s: %s.md\n", sec, p.Slug)
		}
	}
	return out.Bytes(), nil
}
```

- [ ] **Step 4: Run nav tests**

Run: `go test ./cmd/docsite/ -run TestGenerateNav -v`
Expected: both PASS.

- [ ] **Step 5: Run full suite + lint**

Run: `go test -race ./... && go vet ./... && gofmt -l .`

- [ ] **Step 6: Commit**

```bash
git add cmd/docsite/nav.go cmd/docsite/nav_test.go
git commit -m "feat(docsite): generate mkdocs.yml nav from groups and pages"
```

---

## Task 9: Disk I/O wrapper

**Files:**
- Create: `cmd/docsite/io.go`

No test for this task — it's a thin wrapper around `os` calls. The end-to-end test in Task 11 exercises it.

- [ ] **Step 1: Write the I/O code**

`cmd/docsite/io.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteAll writes the index page, every section page, and the mkdocs.yml nav
// to disk. outDir is cleared (recreated) before writes to avoid stale files.
func WriteAll(outDir, navPath string, index Page, pages []Page, nav []byte) error {
	if err := os.RemoveAll(outDir); err != nil {
		return fmt.Errorf("clear %s: %w", outDir, err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", outDir, err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "index.md"), []byte(index.Content), 0o644); err != nil {
		return fmt.Errorf("write index.md: %w", err)
	}
	for _, p := range pages {
		path := filepath.Join(outDir, p.Slug+".md")
		if err := os.WriteFile(path, []byte(p.Content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(navPath), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", navPath, err)
	}
	if err := os.WriteFile(navPath, nav, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", navPath, err)
	}
	return nil
}
```

- [ ] **Step 2: Confirm it compiles**

Run: `go build ./cmd/docsite/`
Expected: no output, build succeeds.

- [ ] **Step 3: Lint**

Run: `go vet ./... && gofmt -l .`

- [ ] **Step 4: Commit**

```bash
git add cmd/docsite/io.go
git commit -m "feat(docsite): add disk I/O helper for splitter outputs"
```

---

## Task 10: CLI entry point

**Files:**
- Create: `cmd/docsite/main.go`

- [ ] **Step 1: Write main.go**

`cmd/docsite/main.go`:

```go
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	readmePath := flag.String("readme", "README.md", "path to the README source")
	groupsPath := flag.String("groups", "docs/groups.yml", "path to the groups mapping YAML")
	basePath := flag.String("base", "docs/mkdocs.base.yml", "path to the MkDocs base config")
	outDir := flag.String("out", "build/site", "directory for generated markdown pages")
	navPath := flag.String("nav", "build/mkdocs.yml", "output path for the merged mkdocs.yml")
	flag.Parse()

	if err := run(*readmePath, *groupsPath, *basePath, *outDir, *navPath); err != nil {
		fmt.Fprintln(os.Stderr, "docsite:", err)
		os.Exit(1)
	}
}

func run(readmePath, groupsPath, basePath, outDir, navPath string) error {
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", readmePath, err)
	}
	groupsRaw, err := os.ReadFile(groupsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", groupsPath, err)
	}
	base, err := os.ReadFile(basePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", basePath, err)
	}

	res, err := Split(readme)
	if err != nil {
		return fmt.Errorf("split: %w", err)
	}
	groups, err := ParseGroups(groupsRaw)
	if err != nil {
		return fmt.Errorf("parse groups.yml: %w", err)
	}
	if err := ValidateGroups(res.Pages, groups); err != nil {
		return err
	}
	RewriteAnchors(&res.Index, res.Pages)
	nav, err := GenerateNav(base, res.Pages, groups)
	if err != nil {
		return fmt.Errorf("generate nav: %w", err)
	}
	return WriteAll(outDir, navPath, res.Index, res.Pages, nav)
}
```

- [ ] **Step 2: Confirm it builds**

Run: `go build ./cmd/docsite/`
Expected: no output, build succeeds.

- [ ] **Step 3: Run full suite + lint**

Run: `go test -race ./... && go vet ./... && gofmt -l .`

- [ ] **Step 4: Commit**

```bash
git add cmd/docsite/main.go
git commit -m "feat(docsite): wire up CLI orchestration"
```

---

## Task 11: End-to-end test on the real README

**Files:**
- Create: `cmd/docsite/e2e_test.go`
- Create: `cmd/docsite/testdata/simple/README.md`
- Create: `cmd/docsite/testdata/simple/groups.yml`
- Create: `cmd/docsite/testdata/simple/mkdocs.base.yml`

This task covers two things at once: a synthetic minimal end-to-end run (against fixtures) and a run against the real `README.md` checked in at the repo root. They share one test file.

- [ ] **Step 1: Write the fixtures**

`cmd/docsite/testdata/simple/README.md`:

```markdown
# Demo SDK

Some intro.

## Install

install body.

## Quick start

quick body.

## Errors

errors body.
```

`cmd/docsite/testdata/simple/groups.yml`:

```yaml
groups:
  - name: Getting started
    sections:
      - Install
      - Quick start
  - name: Production
    sections:
      - Errors
```

`cmd/docsite/testdata/simple/mkdocs.base.yml`:

```yaml
site_name: Demo
theme:
  name: material
```

- [ ] **Step 2: Write the failing test**

`cmd/docsite/e2e_test.go`:

```go
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
	// 17 H2s in README + 1 index = 18 files. If this assertion fires,
	// update the README, groups.yml, or both — the spec says the number
	// of pages is the number of H2s plus index.
	if len(entries) != 18 {
		t.Errorf("got %d pages, want 18 (17 H2s + index)", len(entries))
	}
}
```

- [ ] **Step 3: Run the simple fixture test**

Run: `go test ./cmd/docsite/ -run TestEndToEnd_SimpleFixture -v`
Expected: PASS.

- [ ] **Step 4: Run the real-README test**

Run: `go test ./cmd/docsite/ -run TestEndToEnd_RealReadme -v`
Expected: SKIP (the real `docs/groups.yml` and `docs/mkdocs.base.yml` don't exist yet). The skip messages are intentional — they unblock Tasks 12–13.

- [ ] **Step 5: Run full suite + lint**

Run: `go test -race ./... && go vet ./... && gofmt -l .`

- [ ] **Step 6: Commit**

```bash
git add cmd/docsite/e2e_test.go cmd/docsite/testdata
git commit -m "test(docsite): add end-to-end fixture and real-README harness"
```

---

## Task 12: Write `docs/groups.yml` for the real README

**Files:**
- Create: `docs/groups.yml`

- [ ] **Step 1: Write the file**

`docs/groups.yml`:

```yaml
groups:
  - name: Getting started
    sections:
      - Install
      - Quick start
  - name: Concepts
    sections:
      - Working with stored documents
      - Authentication & environments
      - Methods
      - Configuration
  - name: Production
    sections:
      - Error handling
      - Cancellation
      - Observability
      - Retries & idempotency
  - name: Reference
    sections:
      - Type system
      - Concurrency
      - Runtime support
      - Requirements
  - name: Support
    sections:
      - Documentation & support
      - License
```

- [ ] **Step 2: Confirm structure with a sanity command**

Run: `grep -c '^      - ' docs/groups.yml`
Expected: `17` — matches the count of H2s in the README.

- [ ] **Step 3: Commit (do not run the end-to-end real-README test yet — base config still missing)**

```bash
git add docs/groups.yml
git commit -m "docs: add groups.yml mapping README H2s to nav sections"
```

---

## Task 13: Write `docs/mkdocs.base.yml`

**Files:**
- Create: `docs/mkdocs.base.yml`

- [ ] **Step 1: Write the file**

`docs/mkdocs.base.yml`:

```yaml
site_name: Poli Page Go SDK
site_url: https://poli-page.github.io/sdk-go/
repo_url: https://github.com/poli-page/sdk-go
repo_name: poli-page/sdk-go
edit_uri: edit/main/README.md

theme:
  name: material
  features:
    - navigation.tabs
    - navigation.sections
    - navigation.top
    - content.code.copy
    - search.suggest
  palette:
    - media: "(prefers-color-scheme: light)"
      scheme: default
      primary: indigo
      toggle:
        icon: material/weather-night
        name: Switch to dark mode
    - media: "(prefers-color-scheme: dark)"
      scheme: slate
      primary: indigo
      toggle:
        icon: material/weather-sunny
        name: Switch to light mode

markdown_extensions:
  - admonition
  - toc:
      permalink: true
  - pymdownx.highlight:
      anchor_linenums: true
  - pymdownx.superfences
  - pymdownx.details
  - pymdownx.inlinehilite

extra:
  version:
    provider: mike
  social:
    - icon: fontawesome/brands/github
      link: https://github.com/poli-page/sdk-go

docs_dir: site
```

Note: `docs_dir: site` tells MkDocs to look in `build/site/` (relative to wherever `mkdocs.yml` lives). The workflow writes `mkdocs.yml` to `build/mkdocs.yml` so the resolution `build/site/` matches.

- [ ] **Step 2: Run the real-README end-to-end test**

Run: `go test ./cmd/docsite/ -run TestEndToEnd_RealReadme -v`
Expected: PASS. 18 pages generated (17 H2s + index).

- [ ] **Step 3: Run full suite + lint**

Run: `go test -race ./... && go vet ./... && gofmt -l .`

- [ ] **Step 4: Commit**

```bash
git add docs/mkdocs.base.yml
git commit -m "docs: add MkDocs Material base config with mike provider"
```

---

## Task 14: Pin Python deps

**Files:**
- Create: `docs/requirements.txt`

- [ ] **Step 1: Write the file**

`docs/requirements.txt`:

```
mkdocs-material==9.*
mike==2.*
```

- [ ] **Step 2: Commit**

```bash
git add docs/requirements.txt
git commit -m "docs: pin mkdocs-material and mike major versions"
```

---

## Task 15: Update `.gitignore`

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: Verify current contents**

Run: `grep -n '^/build/$\|^build/$' .gitignore || echo "not present"`
Expected: `not present`.

- [ ] **Step 2: Append the entry**

Add this line to the end of `.gitignore`:

```
# docsite splitter output
/build/
```

- [ ] **Step 3: Verify**

Run: `grep -n '/build/' .gitignore`
Expected: one line matching the entry.

- [ ] **Step 4: Smoke-test the splitter end-to-end against the real layout**

Run: `go run ./cmd/docsite && ls build/site | wc -l && head -30 build/mkdocs.yml`
Expected: `18` files in `build/site`, `mkdocs.yml` shows site_name / theme / nav block.

- [ ] **Step 5: Verify build/ is now ignored**

Run: `git status --porcelain | grep -c build/ || echo 0`
Expected: `0` — `build/` is gitignored, status shows nothing.

- [ ] **Step 6: Commit**

```bash
git add .gitignore
git commit -m "chore: gitignore docsite splitter output (/build/)"
```

---

## Task 16: GitHub Actions workflow

**Files:**
- Create: `.github/workflows/pages.yml`

- [ ] **Step 1: Write the workflow**

`.github/workflows/pages.yml`:

```yaml
name: Pages

on:
  push:
    branches: [main]
    tags: ["v*.*.*"]
  workflow_dispatch:
    inputs:
      version:
        description: "Version label (e.g. v0.2.0 or dev)"
        required: true
      alias:
        description: "Optional alias to update (e.g. latest)"
        required: false

permissions:
  contents: write
  pages: write
  id-token: write

concurrency:
  group: pages
  cancel-in-progress: true

jobs:
  build-and-deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Configure git for mike
        run: |
          git config user.name  "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"

      - name: Make sure gh-pages exists locally (mike needs it)
        run: |
          git fetch origin gh-pages --depth=1 || \
            (git checkout --orphan gh-pages && git rm -rf . && \
             git commit --allow-empty -m "chore: initialize gh-pages" && \
             git push origin gh-pages && git checkout -)

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - uses: actions/setup-python@v5
        with:
          python-version: "3.12"
          cache: pip
          cache-dependency-path: docs/requirements.txt

      - name: Install MkDocs + mike
        run: pip install -r docs/requirements.txt

      - name: Generate site sources
        run: go run ./cmd/docsite

      - name: Deploy with mike
        env:
          REF: ${{ github.ref }}
          EVENT: ${{ github.event_name }}
          INPUT_VERSION: ${{ github.event.inputs.version }}
          INPUT_ALIAS:   ${{ github.event.inputs.alias }}
        run: |
          set -euo pipefail
          CFG=build/mkdocs.yml

          case "$EVENT" in
            workflow_dispatch)
              if [ -n "${INPUT_ALIAS}" ]; then
                mike deploy --config-file "$CFG" --push --update-aliases "$INPUT_VERSION" "$INPUT_ALIAS"
              else
                mike deploy --config-file "$CFG" --push "$INPUT_VERSION"
              fi
              ;;
            push)
              if [[ "$REF" == refs/tags/v*.*.* ]]; then
                TAG="${REF#refs/tags/}"
                mike deploy --config-file "$CFG" --push --update-aliases "$TAG" latest
                mike set-default --config-file "$CFG" --push latest
              else
                mike deploy --config-file "$CFG" --push dev
                # If no other versions exist yet (first run before any tag),
                # make sure the root URL serves something instead of 404.
                if [ "$(mike list --config-file "$CFG" | wc -l)" = "1" ]; then
                  mike set-default --config-file "$CFG" --push dev
                fi
              fi
              ;;
          esac
```

- [ ] **Step 2: Lint the YAML syntax**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/pages.yml')); print('ok')"`
Expected: `ok`.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/pages.yml
git commit -m "ci: build and deploy docs site with MkDocs Material + mike"
```

---

## Task 17: Pre-merge manual step — flip GitHub Pages to legacy/branch mode

This task is **not code** — it is a one-shot command run by the maintainer (Mickael) before the workflow first runs in production. Without it, the workflow pushes to `gh-pages` but GitHub keeps trying to use the (non-existent) workflow artifact instead.

- [ ] **Step 1: Verify current Pages configuration**

Run: `gh api repos/poli-page/sdk-go/pages`
Expected: `build_type` is `workflow` (current state, established earlier in the conversation).

- [ ] **Step 2: Switch to legacy/branch mode**

Run:

```bash
gh api -X PUT repos/poli-page/sdk-go/pages \
  -f source.branch=gh-pages \
  -f source.path=/ \
  -f build_type=legacy
```

Expected: returns the updated Pages config with `build_type: legacy`, `source.branch: gh-pages`.

If `gh-pages` does not yet exist as a remote branch, this call may fail. In that case, push a single commit to `gh-pages` first (the workflow's "Make sure gh-pages exists locally" step does this on its first run too) and retry.

- [ ] **Step 3: Re-verify**

Run: `gh api repos/poli-page/sdk-go/pages | jq '{build_type, source}'`
Expected: `build_type: "legacy"`, `source.branch: "gh-pages"`.

- [ ] **Step 4: Trigger the first workflow run**

The next push to `main` (the merge commit for this branch, or a subsequent push) will trigger the workflow. Watch it:

```bash
gh run watch
```

Expected: workflow succeeds, `mike list` on the `gh-pages` branch shows `dev`, and `https://poli-page.github.io/sdk-go/` resolves.

If the URL still 404s after the workflow succeeds, give it 1–2 minutes for GitHub's CDN to propagate. If it still 404s after 5 minutes, re-run Step 2.

---

## Verification checklist (after the final task)

Run end-to-end before declaring done:

- [ ] `go test -race ./...` — all green.
- [ ] `go vet ./... && gofmt -l . && golangci-lint run ./...` — all clean.
- [ ] `go run ./cmd/docsite` writes 18 files in `build/site/` and `build/mkdocs.yml`.
- [ ] `mkdocs serve -f build/mkdocs.yml` renders the site locally (optional — requires `pip install -r docs/requirements.txt` locally).
- [ ] CI green on the PR.
- [ ] `https://poli-page.github.io/sdk-go/` returns 200 with the Home page.
- [ ] `https://poli-page.github.io/sdk-go/dev/` or `latest/` reachable from the version selector dropdown.
- [ ] Renaming an H2 in README without touching `groups.yml` makes `go run ./cmd/docsite` exit non-zero with a clear message (sanity-check by hand once).

---

## Out of scope (deferred, per the spec)

- Custom domain (`go.poli.page` / `sdk-go.poli.page`).
- PR-preview deployments via artifact-only uploads.
- Automated purge of old mike versions.
- i18n.
- Importing `example_test.go` content into the site as runnable examples.
