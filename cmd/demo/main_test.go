package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIsAPIKey(t *testing.T) {
	hex32 := strings.Repeat("0123456789abcdef", 2)
	cases := []struct {
		key  string
		want bool
	}{
		{"pp_sandbox_" + hex32, true},
		{"pp_live_" + hex32, true},
		{"pp_staging_" + hex32, true},
		{"pp_eu-west-2_" + hex32, true},
		{"pp_" + strings.Repeat("a", 20) + "_" + hex32, true},
		// Any environment name passes: only the format is checked.
		{"pp_qa_" + hex32, true},
		{"pp_" + strings.Repeat("a", 21) + "_" + hex32, false},
		{"pp_sandbox_" + strings.ToUpper(hex32), false},
		{"pp_sandbox_" + hex32[:31], false},
		{"pp_sandbox_" + hex32 + "0", false},
		{"pp_Sandbox_" + hex32, false},
		{"pp_1sandbox_" + hex32, false},
		{"pp_-sandbox_" + hex32, false},
		{"pp_sandbox-_" + hex32, false},
		{"pp_sand--box_" + hex32, false},
		{"pp_sand_box_" + hex32, false},
		{"pp__" + hex32, false},
		{"pk_sandbox_" + hex32, false},
		{" pp_sandbox_" + hex32, false},
		{"pp_sandbox_x", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isAPIKey(c.key); got != c.want {
			t.Errorf("isAPIKey(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestNotFoundInputSendsEmptyDataObject(t *testing.T) {
	in := notFoundInput()
	if in.Project != "getting-started" || in.Template != "does-not-exist" {
		t.Fatalf("notFoundInput() = %s/%s, want getting-started/does-not-exist", in.Project, in.Template)
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"project":"getting-started","template":"does-not-exist","data":{}}`
	if string(body) != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
}
