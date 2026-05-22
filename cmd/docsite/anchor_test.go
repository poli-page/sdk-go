package main

import "testing"

func TestRewriteAnchors_CrossPage(t *testing.T) {
	pages := []Page{
		{Slug: "install", Title: "Install", Content: "# Install\nSee [errors](#error-handling).\n"},
		{Slug: "error-handling", Title: "Error handling", Content: "# Error handling\nbody\n"},
	}
	index := Page{Slug: "index", Content: "Jump to [install](#install).\n"}
	RewriteAnchors(&index, pages)
	if want := "Jump to [install](install/).\n"; index.Content != want {
		t.Errorf("index = %q, want %q", index.Content, want)
	}
	if want := "# Install\nSee [errors](error-handling/).\n"; pages[0].Content != want {
		t.Errorf("install page = %q, want %q", pages[0].Content, want)
	}
}

func TestRewriteAnchors_LeavesExternalLinksAlone(t *testing.T) {
	pages := []Page{
		{Slug: "install", Title: "Install", Content: "# Install\n[link](https://example.com#frag) and ![img](img.png)\n"},
	}
	index := Page{Slug: "index"}
	RewriteAnchors(&index, pages)
	want := "# Install\n[link](https://example.com#frag) and ![img](img.png)\n"
	if pages[0].Content != want {
		t.Errorf("got %q, want %q", pages[0].Content, want)
	}
}

func TestRewriteAnchors_UnknownAnchorUnchanged(t *testing.T) {
	pages := []Page{
		{Slug: "install", Title: "Install", Content: "# Install\nSee [x](#nonexistent).\n"},
	}
	index := Page{Slug: "index"}
	RewriteAnchors(&index, pages)
	want := "# Install\nSee [x](#nonexistent).\n"
	if pages[0].Content != want {
		t.Errorf("got %q, want %q", pages[0].Content, want)
	}
}
