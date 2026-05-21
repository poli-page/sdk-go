package polipage

import (
	"testing"

	"github.com/poli-page/sdk-go/internal/version"
)

func TestNewClient_returnsNonNil(t *testing.T) {
	t.Parallel()
	if NewClient() == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestVersion_isSet(t *testing.T) {
	t.Parallel()
	if version.Version == "" {
		t.Fatal("internal/version.Version must not be empty")
	}
}
