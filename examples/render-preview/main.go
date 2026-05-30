// Demonstrates: client.Render.Preview(ctx, in) — accepts project mode OR inline mode.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

func main() {
	client := polipage.NewClient(
		option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
	)

	// Project mode: render the stored template's HTML preview.
	preview, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project:  "billing",
		Template: "invoice",
		Data:     map[string]any{"invoiceNumber": "INV-001", "total": 1280},
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("Preview: %d pages, %s env\n", preview.TotalPages, preview.Environment)
	fmt.Printf("HTML length: %d\n", len(preview.HTML))
}
