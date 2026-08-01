package filebrowser

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Preview is a file loaded and classified for the preview pane.
//
// It carries the file's *raw* text, not rendered output. Choosing a
// renderer is the caller's job: the TUI runs Glamour on .md and
// Highlight on everything else, while `ccmux files read` prints
// Content as-is. Putting that fork in here would force the CLI to
// strip ANSI back out of its own output.
type Preview struct {
	Rel  string // tree-relative path this was loaded from
	Size int64  // full size on disk, even when Content is truncated

	// Content is the file's text, empty when Binary is true.
	Content string

	// Binary is set when the content sniff says this isn't text. The
	// preview pane shows a placeholder; Content is left empty rather
	// than filled with bytes no one should print to a terminal.
	Binary bool

	// Truncated is set when the file was longer than PreviewLimit and
	// Content holds only its head.
	Truncated bool

	// Lang is the chroma lexer's display name ("Go", "Rust"), or ""
	// when the extension is unrecognized.
	Lang string

	// Highlightable reports whether Highlight would actually colour
	// this content — false for binary files, unrecognized extensions,
	// and anything over HighlightLimit. The preview pane uses it to
	// label the header honestly instead of implying colour it isn't
	// showing.
	Highlightable bool
}

// Preview reads the file at rel (a tree-relative slash path) and
// classifies it for display. Reading stops at PreviewLimit bytes, so
// selecting a multi-gigabyte file costs the same as selecting a small
// one.
func (t Tree) Preview(rel string) (Preview, error) {
	full, err := t.Resolve(rel)
	if err != nil {
		return Preview{}, err
	}
	f, err := os.Open(full)
	if err != nil {
		return Preview{}, err
	}
	defer f.Close()

	p := Preview{Rel: rel}
	if info, statErr := f.Stat(); statErr == nil {
		p.Size = info.Size()
		if info.IsDir() {
			return Preview{}, errors.New("filebrowser: " + rel + " is a directory")
		}
	}

	// Read one byte past the limit so a file sitting exactly at it
	// isn't reported as truncated.
	buf := make([]byte, PreviewLimit+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Preview{}, err
	}
	data := buf[:n]
	if len(data) > PreviewLimit {
		data = data[:PreviewLimit]
		p.Truncated = true
	}

	if IsBinaryContent(data) {
		p.Binary = true
		return p, nil
	}

	p.Content = string(data)
	p.Lang = Lang(full)
	p.Highlightable = p.Lang != "" && len(p.Content) <= HighlightLimit
	return p, nil
}

// Render returns the preview's content marked up for the terminal, or
// a placeholder line when there is nothing printable. Markdown is left
// alone: the Files screen hands .md to Glamour, which needs the raw
// source.
func (p Preview) Render() string {
	switch {
	case p.Binary:
		return "(binary file — no preview)"
	case p.Content == "":
		return "(empty file)"
	case IsMarkdown(p.Rel):
		return p.Content
	case !p.Highlightable:
		return p.Content
	}
	return Highlight(p.Rel, p.Content)
}

// IsMarkdown reports whether rel names a markdown file, which the
// Files screen renders with Glamour rather than chroma — the same
// renderer the Notes screen uses, so a .md file looks identical in
// both places.
func IsMarkdown(rel string) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".md", ".markdown":
		return true
	}
	return false
}
