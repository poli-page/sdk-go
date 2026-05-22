package main

import (
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
