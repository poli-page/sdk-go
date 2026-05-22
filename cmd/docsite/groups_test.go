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
		[]byte("groups:\n  - sections:\n      - x\n"), // missing name
		[]byte("groups:\n  - name: Foo\n"),            // missing sections
	}
	for _, c := range cases {
		if _, err := ParseGroups(c); err == nil {
			t.Errorf("ParseGroups(%q) returned nil error, want error", c)
		}
	}
}
