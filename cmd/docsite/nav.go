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
