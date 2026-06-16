// Demonstrates: polipage.NewClient(options) — the only entry point.
package main

import (
	"os"
	"time"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

func main() {
	client := polipage.NewClient(
		option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
		option.WithTimeout(60*time.Second),
		option.WithMaxRetries(2),
	)

	// The same *Client is reused for every render and document call.
	_ = client
}
