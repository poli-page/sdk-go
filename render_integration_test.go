//go:build integration

package polipage_test

import (
	"context"
	"os"
	"testing"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

// Integration tests run against a live API. Gated by POLI_PAGE_API_KEY.
// Run with: go test -tags=integration ./...
//
// Defaults mirror the Node integration tests (sdk-node/tests/integration):
//   - POLI_PAGE_BASE_URL defaults to https://api-develop.poli.page
//   - The getting-started/welcome/1.0.0 template is provisioned for every
//     org so it works out of the box for any pp_test_* key.

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func integrationClient(t *testing.T) *polipage.Client {
	t.Helper()
	apiKey := os.Getenv("POLI_PAGE_API_KEY")
	if apiKey == "" {
		t.Skip("POLI_PAGE_API_KEY not set — skipping integration test")
	}
	return polipage.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(envOr("POLI_PAGE_BASE_URL", "https://api-develop.poli.page")),
	)
}

func TestIntegration_Render_Preview_inlineMode(t *testing.T) {
	client := integrationClient(t)
	res, err := client.Render.Preview(context.Background(), polipage.InlineModeInput{
		Template: "<p>{{ name }}</p>",
		Data:     map[string]any{"name": "Preview Test"},
	})
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if res.HTML == "" {
		t.Error("Preview returned empty HTML")
	}
	// Deployed API returns totalPages: 0 for tiny inline content (no page
	// breaks). Just confirm it is non-negative — mirror Node's tolerance.
	if res.TotalPages < 0 {
		t.Errorf("TotalPages = %d, want >= 0", res.TotalPages)
	}
	if res.Environment != polipage.EnvironmentSandbox && res.Environment != polipage.EnvironmentLive {
		t.Errorf("Environment = %q, want sandbox|live", res.Environment)
	}
}

func TestIntegration_Render_Preview_projectMode(t *testing.T) {
	client := integrationClient(t)
	res, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project:  envOr("POLI_PAGE_TEST_PROJECT", "getting-started"),
		Template: envOr("POLI_PAGE_TEST_TEMPLATE", "welcome"),
		Version:  polipage.Opt(envOr("POLI_PAGE_TEST_VERSION", "1.0.0")),
		Data:     map[string]any{"name": "Integration Test"},
	})
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if res.HTML == "" {
		t.Error("Preview returned empty HTML")
	}
}
