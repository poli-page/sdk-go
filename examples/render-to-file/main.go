// Demonstrates: polipage.RenderToFile(ctx, client, in, path) — streams a PDF to disk.
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

	err := polipage.RenderToFile(
		context.Background(),
		client,
		polipage.ProjectModeInput{
			Project:  "billing",
			Template: "invoice",
			Data:     map[string]any{"invoiceNumber": "INV-001", "total": 1280},
		},
		"./invoices/INV-001.pdf",
	)
	if err != nil {
		panic(err)
	}

	// Streams response bytes directly to disk with bounded memory.
	// Parent directories are created automatically.
	fmt.Println("Wrote ./invoices/INV-001.pdf")
}
