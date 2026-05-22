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
