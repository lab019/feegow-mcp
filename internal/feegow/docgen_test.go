package feegow

import (
	"os"
	"strings"
	"testing"
)

// TestDocsMarkdown_MatchesRegistry is the enforcement half of "docs/
// feegow-api.md is generated from the registry, not maintained by hand":
// the committed file must equal RenderMarkdownDocs()'s output byte for
// byte. Add or edit a Registry entry without regenerating the doc, and
// this test is what catches the drift.
func TestDocsMarkdown_MatchesRegistry(t *testing.T) {
	want := RenderMarkdownDocs()

	got, err := os.ReadFile("../../docs/feegow-api.md")
	if err != nil {
		t.Fatalf("reading docs/feegow-api.md: %v", err)
	}

	if string(got) != want {
		t.Fatalf(
			"docs/feegow-api.md is out of sync with internal/feegow.Registry.\n"+
				"Regenerate it from RenderMarkdownDocs() (e.g. via a throwaway `go run` "+
				"that imports this package and prints RenderMarkdownDocs()) and commit the result.\n\n"+
				"--- committed file (%d bytes) ---\n%s\n--- generated from registry (%d bytes) ---\n%s",
			len(got), got, len(want), want,
		)
	}
}

// TestRenderMarkdownDocs_OneRowPerEndpoint is a lighter sanity check
// independent of the committed file: every Registry entry produces
// exactly one row (matched by its ID appearing as a table cell).
func TestRenderMarkdownDocs_OneRowPerEndpoint(t *testing.T) {
	md := RenderMarkdownDocs()
	for id := range Registry {
		needle := "| " + string(id) + " |"
		if !strings.Contains(md, needle) {
			t.Errorf("RenderMarkdownDocs() output missing a row for %q", id)
		}
	}
}
