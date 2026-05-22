package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteAll writes the index page, every section page, and the mkdocs.yml nav
// to disk. outDir is cleared (recreated) before writes to avoid stale files.
func WriteAll(outDir, navPath string, index Page, pages []Page, nav []byte) error {
	if err := os.RemoveAll(outDir); err != nil {
		return fmt.Errorf("clear %s: %w", outDir, err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", outDir, err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "index.md"), []byte(index.Content), 0o644); err != nil {
		return fmt.Errorf("write index.md: %w", err)
	}
	for _, p := range pages {
		path := filepath.Join(outDir, p.Slug+".md")
		if err := os.WriteFile(path, []byte(p.Content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(navPath), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", navPath, err)
	}
	if err := os.WriteFile(navPath, nav, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", navPath, err)
	}
	return nil
}
