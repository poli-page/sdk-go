// Demonstrates: client.Render.PDF(ctx, in) — renders the project's copy published in the key's environment.
package main

import (
	"context"
	"fmt"
	"os"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

func main() {
	client := polipage.NewClient(
		option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
	)

	pdf, err := client.Render.PDF(context.Background(), polipage.RenderInput{
		Project:  "billing",
		Template: "invoice",
		Data:     map[string]any{"invoiceNumber": "INV-001", "total": 1280},
	})
	if err != nil {
		panic(err)
	}

	// pdf is a []byte of PDF bytes.
	fmt.Printf("Rendered %d bytes\n", len(pdf))
}
