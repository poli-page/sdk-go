package main

import (
	"reflect"
	"strings"
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
		[]byte("groups:\n  - sections:\n      - x\n"), // missing name
		[]byte("groups:\n  - name: Foo\n"),            // missing sections
	}
	for _, c := range cases {
		if _, err := ParseGroups(c); err == nil {
			t.Errorf("ParseGroups(%q) returned nil error, want error", c)
		}
	}
}

func TestValidateGroups_OrphanH2(t *testing.T) {
	pages := []Page{
		{Slug: "install", Title: "Install"},
		{Slug: "quick-start", Title: "Quick start"},
		{Slug: "forgot-me", Title: "Forgot me"}, // not in groups
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
		{Slug: "install", Title: "Install"},
	}
	groups := []Group{
		{Name: "Getting started", Sections: []string{"Install", "Removed section"}},
	}
	err := ValidateGroups(pages, groups)
	if err == nil || !strings.Contains(err.Error(), "Removed section") {
		t.Errorf("expected error mentioning 'Removed section', got %v", err)
	}
}

func TestValidateGroups_SlugCollision(t *testing.T) {
	pages := []Page{
		{Slug: "errors", Title: "Errors"},
		{Slug: "errors", Title: "ERRORS"}, // would slug() down to the same thing in practice
	}
	groups := []Group{
		{Name: "x", Sections: []string{"Errors", "ERRORS"}},
	}
	err := ValidateGroups(pages, groups)
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Errorf("expected slug collision error, got %v", err)
	}
}

func TestValidateGroups_AllAligned(t *testing.T) {
	pages := []Page{
		{Slug: "install", Title: "Install"},
		{Slug: "quick-start", Title: "Quick start"},
	}
	groups := []Group{
		{Name: "Getting started", Sections: []string{"Install", "Quick start"}},
	}
	if err := ValidateGroups(pages, groups); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
