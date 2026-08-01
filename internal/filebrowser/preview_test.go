package filebrowser

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreview_TextFile(t *testing.T) {
	const src = "package main\n\nfunc main() {}\n"
	root := writeTree(t, map[string]string{"main.go": src})

	p, err := Open(root).Preview("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if p.Binary {
		t.Error("a .go file was classified as binary")
	}
	if p.Truncated {
		t.Error("a short file was marked truncated")
	}
	if p.Content != src {
		t.Errorf("Content = %q, want %q", p.Content, src)
	}
	if p.Size != int64(len(src)) {
		t.Errorf("Size = %d, want %d", p.Size, len(src))
	}
	if p.Lang != "Go" {
		t.Errorf("Lang = %q, want Go", p.Lang)
	}
	if !p.Highlightable {
		t.Error("a small .go file should be highlightable")
	}
	if !hasANSI(p.Render()) {
		t.Error("Render() of a .go file produced no highlighting")
	}
}

// TestPreview_BinaryFile covers the spec scenario "Binary files are
// not rendered as text": the placeholder shows, and the bytes are
// never carried in Content.
func TestPreview_BinaryFile(t *testing.T) {
	root := t.TempDir()
	png := append([]byte("\x89PNG\r\n\x1a\n"), 0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R')
	if err := os.WriteFile(filepath.Join(root, "logo.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := Open(root).Preview("logo.png")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Binary {
		t.Fatal("a PNG was not classified as binary")
	}
	if p.Content != "" {
		t.Errorf("Content = %q, want empty for a binary file", p.Content)
	}
	if p.Highlightable {
		t.Error("a binary file must never be highlightable")
	}
	if got := p.Render(); !strings.Contains(got, "binary") {
		t.Errorf("Render() = %q, want a binary placeholder", got)
	}
}

// TestPreview_UnknownExtensionIsPlain covers the plain-text fallback
// end to end, through Preview rather than Highlight alone.
func TestPreview_UnknownExtensionIsPlain(t *testing.T) {
	const src = "just some words\n"
	root := writeTree(t, map[string]string{"notes.xyzzy": src})

	p, err := Open(root).Preview("notes.xyzzy")
	if err != nil {
		t.Fatal(err)
	}
	if p.Binary {
		t.Error("plain text was classified as binary")
	}
	if p.Lang != "" {
		t.Errorf("Lang = %q, want empty for an unrecognized extension", p.Lang)
	}
	if p.Highlightable {
		t.Error("an unrecognized extension must not be highlightable")
	}
	if got := p.Render(); got != src {
		t.Errorf("Render() = %q, want the raw content", got)
	}
}

// TestPreview_MarkdownStaysRaw: the Files screen hands .md to Glamour,
// which needs the source, so Render must not chroma-highlight it.
func TestPreview_MarkdownStaysRaw(t *testing.T) {
	const src = "# Title\n\nSome *prose*.\n"
	root := writeTree(t, map[string]string{"README.md": src})

	p, err := Open(root).Preview("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Render(); got != src {
		t.Errorf("Render(.md) = %q, want the raw markdown for Glamour", got)
	}
	if p.Lang != "markdown" {
		t.Errorf("Lang = %q, want markdown", p.Lang)
	}
}

func TestIsMarkdown(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{"README.md", true},
		{"docs/GUIDE.MD", true},
		{"notes.markdown", true},
		{"main.go", false},
		{"mdfile", false},
		{"a.md.bak", false},
	}
	for _, tc := range cases {
		if got := IsMarkdown(tc.rel); got != tc.want {
			t.Errorf("IsMarkdown(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}

// TestPreview_TruncatesOversizedFile pins task 2.4's read cap: a file
// past PreviewLimit is loaded head-only, and Size still reports the
// real length so the pane can say so.
func TestPreview_TruncatesOversizedFile(t *testing.T) {
	root := t.TempDir()
	body := bytes.Repeat([]byte("abcdefgh\n"), (PreviewLimit/9)+1024)
	if int64(len(body)) <= PreviewLimit {
		t.Fatalf("test setup: %d bytes did not exceed the %d limit", len(body), PreviewLimit)
	}
	if err := os.WriteFile(filepath.Join(root, "huge.log"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := Open(root).Preview("huge.log")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Truncated {
		t.Error("an oversized file was not marked truncated")
	}
	if len(p.Content) != PreviewLimit {
		t.Errorf("Content = %d bytes, want exactly PreviewLimit (%d)", len(p.Content), PreviewLimit)
	}
	if p.Size != int64(len(body)) {
		t.Errorf("Size = %d, want the full on-disk size %d", p.Size, len(body))
	}
	// Over HighlightLimit as well, so it must not be highlighted.
	if p.Highlightable {
		t.Error("a PreviewLimit-sized file must be past HighlightLimit too")
	}
}

// TestPreview_ExactlyAtLimitIsNotTruncated guards the off-by-one at
// the read boundary.
func TestPreview_ExactlyAtLimitIsNotTruncated(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "exact.txt"), bytes.Repeat([]byte("a"), PreviewLimit), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Open(root).Preview("exact.txt")
	if err != nil {
		t.Fatal(err)
	}
	if p.Truncated {
		t.Error("a file exactly at PreviewLimit was reported truncated")
	}
	if len(p.Content) != PreviewLimit {
		t.Errorf("Content = %d bytes, want %d", len(p.Content), PreviewLimit)
	}
}

func TestPreview_EmptyFile(t *testing.T) {
	root := writeTree(t, map[string]string{"empty.txt": ""})
	p, err := Open(root).Preview("empty.txt")
	if err != nil {
		t.Fatal(err)
	}
	if p.Binary {
		t.Error("an empty file was classified as binary")
	}
	if got := p.Render(); !strings.Contains(got, "empty") {
		t.Errorf("Render() = %q, want an empty-file placeholder", got)
	}
}

func TestPreview_Errors(t *testing.T) {
	root := writeTree(t, map[string]string{"sub/file.go": "package sub"})
	tree := Open(root)

	if _, err := tree.Preview("missing.go"); err == nil {
		t.Error("Preview of a missing file = nil error, want one")
	}
	if _, err := tree.Preview("../escape"); err == nil {
		t.Error("Preview of an escaping path = nil error, want one")
	}
	if _, err := tree.Preview("sub"); err == nil {
		t.Error("Preview of a directory = nil error, want one")
	}
}
