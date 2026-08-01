package filebrowser

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Syntax highlighting for the Files preview pane.
//
// Glamour, already a direct dependency, is deliberately not used here.
// It is a *markdown* renderer that happens to call chroma for fenced
// code blocks; handed a bare .go file it would render the whole thing
// as an undifferentiated paragraph. The Files screen keeps Glamour for
// .md — where its layout work is the point — and calls chroma
// directly for everything else.

const (
	// DefaultStyle is the chroma style used for the preview pane.
	// Catppuccin Mocha is the same palette styles.DefaultPalette is
	// built from, so highlighted code sits in the TUI's own colour
	// world instead of clashing with the chrome around it.
	DefaultStyle = "catppuccin-mocha"

	// DefaultFormatter emits 256-colour ANSI. Not terminal16m: chroma
	// writes escapes straight to the string, bypassing lipgloss's
	// colour-profile downsampling, so the safest widely-supported
	// depth is the right default. Callers that need plain output —
	// golden tests, piping to a file — pass "noop", which walks the
	// same token stream and writes the text with no escapes at all.
	DefaultFormatter = "terminal256"

	// HighlightLimit is the size above which a file is shown as plain
	// text instead of being highlighted. Tokenising is linear but not
	// free, and it runs on the UI goroutine's critical path every time
	// the selection moves; past a few hundred KiB the colour is not
	// worth the stall. 256 KiB comfortably covers real source files.
	HighlightLimit = 256 << 10

	// PreviewLimit is the size above which only a file's head is read
	// into the preview pane. A multi-megabyte log has nothing useful
	// past this point on a screen showing at most a few hundred lines,
	// and reading it all would blow the render budget for no gain.
	PreviewLimit = 1 << 20
)

// Lang returns the display name of the chroma lexer that matches
// path's filename, or "" when chroma recognizes nothing. Used for the
// preview pane's header; also the cheapest way to ask "would
// Highlight actually do anything here?".
func Lang(path string) string {
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		return ""
	}
	return lexer.Config().Name
}

// Highlight returns content marked up with ANSI escapes for display in
// the preview pane, using DefaultStyle and DefaultFormatter.
//
// Every failure path returns content unchanged, so the caller always
// has something printable: an unrecognized extension, an oversized
// file, a lexer error, or a formatter error all degrade to plain text
// rather than an error the preview pane would have to render instead
// of the file.
func Highlight(path, content string) string {
	return HighlightWith(path, content, DefaultStyle, DefaultFormatter)
}

// HighlightWith is Highlight with an explicit chroma style and
// formatter. An unknown style name falls back to chroma's own default
// style; an unknown formatter name falls back to "noop", which emits
// the text with no escapes.
func HighlightWith(path, content, styleName, formatterName string) string {
	if content == "" {
		return ""
	}
	// The size cap lives here rather than at the call site so every
	// caller — TUI preview, CLI, tests — inherits the same ceiling.
	if len(content) > HighlightLimit {
		return content
	}
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		// No lexer for this extension. Returning content untouched
		// beats falling back to chroma's plaintext lexer, which would
		// re-emit the whole file wrapped in escapes that colour
		// nothing.
		return content
	}
	// Coalesce merges runs of same-type tokens, which cuts the number
	// of escape sequences in the output substantially.
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return content
	}
	var buf strings.Builder
	buf.Grow(len(content) + len(content)/4) // escapes add roughly a quarter
	if err := formatters.Get(formatterName).Format(&buf, styles.Get(styleName), iterator); err != nil {
		return content
	}
	return buf.String()
}
