// Demonstrates: client.Documents.Delete(ctx, id) — soft-delete a stored document.
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

	if err := client.Documents.Delete(context.Background(), "doc_abc123"); err != nil {
		panic(err)
	}

	// Returns nil. The PDF is purged; metadata is retained for audit.
	fmt.Println("Deleted.")
}
