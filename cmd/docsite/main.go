package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	readmePath := flag.String("readme", "README.md", "path to the README source")
	groupsPath := flag.String("groups", "docs/groups.yml", "path to the groups mapping YAML")
	basePath := flag.String("base", "docs/mkdocs.base.yml", "path to the MkDocs base config")
	outDir := flag.String("out", "build/site", "directory for generated markdown pages")
	navPath := flag.String("nav", "build/mkdocs.yml", "output path for the merged mkdocs.yml")
	flag.Parse()

	if err := run(*readmePath, *groupsPath, *basePath, *outDir, *navPath); err != nil {
		fmt.Fprintln(os.Stderr, "docsite:", err)
		os.Exit(1)
	}
}

func run(readmePath, groupsPath, basePath, outDir, navPath string) error {
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", readmePath, err)
	}
	groupsRaw, err := os.ReadFile(groupsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", groupsPath, err)
	}
	base, err := os.ReadFile(basePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", basePath, err)
	}

	res, err := Split(readme)
	if err != nil {
		return fmt.Errorf("split: %w", err)
	}
	groups, err := ParseGroups(groupsRaw)
	if err != nil {
		return fmt.Errorf("parse groups.yml: %w", err)
	}
	if err := ValidateGroups(res.Pages, groups); err != nil {
		return err
	}
	RewriteAnchors(&res.Index, res.Pages)
	nav, err := GenerateNav(base, res.Pages, groups)
	if err != nil {
		return fmt.Errorf("generate nav: %w", err)
	}
	return WriteAll(outDir, navPath, res.Index, res.Pages, nav)
}
