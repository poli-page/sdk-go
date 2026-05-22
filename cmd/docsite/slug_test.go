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
		{"", ""},
	}
	for _, c := range cases {
		got := slug(c.in)
		if got != c.want {
			t.Errorf("slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
