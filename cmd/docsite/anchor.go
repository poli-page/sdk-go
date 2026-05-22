package main

import "regexp"

var linkRE = regexp.MustCompile(`\]\(#([^)]+)\)`)

// RewriteAnchors rewrites in-document anchor links of the form `](#slug)` so
// that they point to the page whose slug or H1 matches. Links whose anchor
// matches no known section are left untouched. External links (those with a
// scheme before `#`) are not matched by the regex because the `#` is not
// preceded by `](`.
func RewriteAnchors(index *Page, pages []Page) {
	known := make(map[string]string, len(pages))
	for _, p := range pages {
		known[p.Slug] = p.Slug
	}
	rewrite := func(content string) string {
		return linkRE.ReplaceAllStringFunc(content, func(match string) string {
			anchor := match[3 : len(match)-1]
			if dest, ok := known[anchor]; ok {
				return "](" + dest + "/)"
			}
			return match
		})
	}
	if index != nil {
		index.Content = rewrite(index.Content)
	}
	for i := range pages {
		pages[i].Content = rewrite(pages[i].Content)
	}
}
