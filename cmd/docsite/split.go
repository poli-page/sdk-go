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
	var preambleLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			break
		}
		preambleLines = append(preambleLines, line)
	}
	// Drop trailing blank lines that precede the first ## heading.
	for len(preambleLines) > 0 && strings.TrimRight(preambleLines[len(preambleLines)-1], "\n") == "" {
		preambleLines = preambleLines[:len(preambleLines)-1]
	}
	var preamble bytes.Buffer
	for _, line := range preambleLines {
		preamble.WriteString(line)
	}
	return SplitResult{
		Index: Page{Slug: "index", Content: preamble.String()},
	}, nil
}
