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
		fmt.Fprintf(&sb, "  README has H2 %q but no group lists it\n", t)
	}
	for _, t := range orphanGroup {
		fmt.Fprintf(&sb, "  groups.yml lists %q but no such H2 in README\n", t)
	}
	return errors.New(sb.String())
}

func hasHeader(lines []string, key string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) == key {
			return true
		}
	}
	return false
}
