package filebrowser

import (
	"strings"
	"testing"
)

// hasANSI reports whether s carries terminal escape sequences — the
// observable difference between "highlighted" and "plain".
func hasANSI(s string) bool { return strings.Contains(s, "\x1b[") }

func TestLang(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"internal/tui/app.go", "Go"},
		{"src/lib.rs", "Rust"},
		{"package.json", "JSON"},
		{"README.md", "markdown"},
		{"script.py", "Python"},
		// Unrecognized extensions and extensionless files chroma has
		// no filename rule for must report nothing, which is what
		// drives the plain-text fallback.
		{"data.xyzzy", ""},
		{"no-extension-at-all", ""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := Lang(tc.path); got != tc.want {
				t.Errorf("Lang(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestHighlight_RecognizedLanguage covers the spec scenario
// "Recognized language is highlighted": a .go file comes back with
// escapes, and with its text intact underneath them.
func TestHighlight_RecognizedLanguage(t *testing.T) {
	const src = "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	got := Highlight("main.go", src)

	if !hasANSI(got) {
		t.Fatalf("Highlight(.go) produced no escape sequences: %q", got)
	}
	// Every word of the source must survive the round-trip; colour is
	// added, nothing is dropped.
	for _, tok := range []string{"package", "main", "func", "println", "hi"} {
		if !strings.Contains(got, tok) {
			t.Errorf("Highlight dropped %q from the output", tok)
		}
	}
}

// TestHighlight_UnrecognizedExtension covers the spec scenario
// "Unrecognized extension falls back to plain text".
func TestHighlight_UnrecognizedExtension(t *testing.T) {
	const src = "some arbitrary content\nwith two lines\n"
	got := Highlight("mystery.xyzzy", src)
	if got != src {
		t.Errorf("Highlight(unknown ext) = %q, want the input unchanged", got)
	}
	if hasANSI(got) {
		t.Error("Highlight(unknown ext) added escape sequences")
	}
}

func TestHighlight_Empty(t *testing.T) {
	if got := Highlight("main.go", ""); got != "" {
		t.Errorf("Highlight(\"\") = %q, want empty", got)
	}
}

// TestHighlight_OverLimitStaysPlain pins task 2.4's size cap: past
// HighlightLimit the content comes back untouched.
func TestHighlight_OverLimitStaysPlain(t *testing.T) {
	line := "x := 1 // a line of go\n"
	big := strings.Repeat(line, (HighlightLimit/len(line))+64)
	if len(big) <= HighlightLimit {
		t.Fatalf("test setup: %d bytes did not exceed the %d limit", len(big), HighlightLimit)
	}
	got := Highlight("big.go", big)
	if got != big {
		t.Error("Highlight over HighlightLimit modified the content; the cap did not apply")
	}

	// Just under the limit, the same content is highlighted — proving
	// the cap is what made the difference above, not the content.
	small := strings.Repeat(line, 8)
	if !hasANSI(Highlight("small.go", small)) {
		t.Error("Highlight under the limit produced no escapes")
	}
}

// TestHighlightWith_UnknownNamesDegrade: a bad style or formatter name
// must still yield printable text, never an error or empty output.
func TestHighlightWith_UnknownNamesDegrade(t *testing.T) {
	const src = "package main\n"

	// An unknown formatter falls through to chroma's noop formatter,
	// which walks the tokens and writes them with no escapes.
	plain := HighlightWith("main.go", src, DefaultStyle, "no-such-formatter")
	if hasANSI(plain) {
		t.Errorf("unknown formatter still emitted escapes: %q", plain)
	}
	if !strings.Contains(plain, "package main") {
		t.Errorf("unknown formatter lost the content: %q", plain)
	}

	// An unknown style falls back to chroma's default style; the
	// formatter still colours.
	styled := HighlightWith("main.go", src, "no-such-style", DefaultFormatter)
	if !strings.Contains(stripANSI(styled), "package main") {
		t.Errorf("unknown style lost the content: %q", styled)
	}
}

// TestHighlight_NoopFormatterIsPlainText documents the seam golden
// tests use to keep escapes out of their snapshots.
func TestHighlight_NoopFormatterIsPlainText(t *testing.T) {
	const src = "package main\n\nfunc main() {}\n"
	got := HighlightWith("main.go", src, DefaultStyle, "noop")
	if got != src {
		t.Errorf("noop formatter = %q, want the input unchanged", got)
	}
}

// stripANSI removes CSI sequences so a test can assert on the text
// under the colour.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++ // skip the 'm'
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
