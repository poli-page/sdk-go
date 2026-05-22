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
	var pages []Page
	var cur *Page
	flush := func() {
		if cur != nil {
			cur.Content = strings.TrimRight(cur.Content, "\n") + "\n"
			pages = append(pages, *cur)
			cur = nil
		}
	}
	for _, line := range lines {
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
		if cur != nil {
			cur.Content += line
		} else {
			preamble.WriteString(line)
		}
	}
	flush()
	// Drop trailing blank lines from the preamble so it ends with exactly one \n.
	raw := preamble.String()
	var preambleStr string
	if raw != "" {
		preambleStr = strings.TrimRight(raw, "\n") + "\n"
	}
	return SplitResult{
		Index: Page{Slug: "index", Content: preambleStr},
		Pages: pages,
	}, nil
}
