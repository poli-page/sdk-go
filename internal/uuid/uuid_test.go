package uuid

import (
	"regexp"
	"testing"
)

var uuid4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNew_matchesRFC4122v4Layout(t *testing.T) {
	t.Parallel()
	got := New()
	if !uuid4Pattern.MatchString(got) {
		t.Fatalf("New() = %q, want a string matching %s", got, uuid4Pattern)
	}
}

func TestNew_setsVersionNibbleTo4(t *testing.T) {
	t.Parallel()
	got := New()
	if got[14] != '4' {
		t.Fatalf("New() = %q, want char at index 14 == '4' (version nibble), got %q", got, string(got[14]))
	}
}

func TestNew_setsVariantNibbleTo8To11(t *testing.T) {
	t.Parallel()
	got := New()
	switch got[19] {
	case '8', '9', 'a', 'b':
		// OK — RFC 4122 §4.1.1 variant 10xx
	default:
		t.Fatalf("New() = %q, want char at index 19 in {8,9,a,b} (variant nibble), got %q", got, string(got[19]))
	}
}

func TestNew_returnsDistinctValues(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		v := New()
		if _, dup := seen[v]; dup {
			t.Fatalf("New() returned duplicate %q after %d samples", v, i)
		}
		seen[v] = struct{}{}
	}
}
