package filebrowser

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestIsBinaryContent(t *testing.T) {
	// A PNG header: the signature's second byte is 'P', but the NUL in
	// the IHDR length field is what gives it away.
	png := append([]byte("\x89PNG\r\n\x1a\n"), 0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R')

	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{"empty file is text", nil, false},
		{"ascii source", []byte("package main\n\nfunc main() {}\n"), false},
		{"utf8 with accents and CJK", []byte("// tiếng Việt, 日本語, emoji 🎉\n"), false},
		{"crlf line endings", []byte("line one\r\nline two\r\n"), false},
		{"tabs and form feed are text", []byte("a\tb\fc\v\n"), false},
		{"embedded NUL", []byte("text\x00more text"), true},
		{"leading NUL", []byte("\x00\x00\x00\x01"), true},
		{"png header", png, true},
		{"utf16le has interleaved NULs", []byte{0xFF, 0xFE, 'h', 0x00, 'i', 0x00}, true},
		{"invalid utf8 (latin-1 é)", []byte("caf\xe9 au lait\n"), true},
		{"lone continuation byte", []byte("ok then \x80 nope"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsBinaryContent(tc.in); got != tc.want {
				t.Errorf("IsBinaryContent(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestIsBinaryContent_TruncatedRuneAtWindowEdge: a UTF-8 file longer
// than the sniff window can be cut mid-rune. That is an artifact of
// where we stopped reading, not evidence of binary content.
func TestIsBinaryContent_TruncatedRuneAtWindowEdge(t *testing.T) {
	// "日" is 3 bytes. Pad so the window boundary lands inside one.
	const cjk = "日"
	if utf8.RuneLen([]rune(cjk)[0]) != 3 {
		t.Fatalf("test assumes a 3-byte rune")
	}
	body := strings.Repeat("a", SniffLen-2) + cjk + strings.Repeat("b", 100)
	window := []byte(body)[:SniffLen] // ends after 2 of the rune's 3 bytes

	if IsBinaryContent(window) {
		t.Error("a UTF-8 file cut mid-rune at the sniff boundary was called binary")
	}

	// The same buffer as a complete short file (below the sniff limit)
	// really is invalid UTF-8 and should be flagged.
	short := []byte("abc" + cjk)[:5] // 3 ascii + 2 of 3 rune bytes
	if !IsBinaryContent(short) {
		t.Error("a complete file ending in a truncated rune should be binary")
	}
}

// TestIsBinaryContent_NULBeyondWindowIsMissed documents the sniff's
// deliberate bound: content past SniffLen is not examined.
func TestIsBinaryContent_NULBeyondWindowIsMissed(t *testing.T) {
	buf := append(bytes.Repeat([]byte("a"), SniffLen+10), 0x00)
	if IsBinaryContent(buf) {
		t.Error("IsBinaryContent looked past SniffLen; the sniff must stay bounded")
	}
}

func TestIsBinary_File(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, body []byte) string {
		t.Helper()
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, body, 0o644); err != nil {
			t.Fatal(err)
		}
		return full
	}

	cases := []struct {
		name string
		body []byte
		want bool
	}{
		{"source.go", []byte("package main\n"), false},
		{"empty.txt", nil, false},
		{"image.png", append([]byte("\x89PNG\r\n\x1a\n"), 0x00, 0x00, 0x00, 0x0D), true},
		{"big.txt", bytes.Repeat([]byte("hello world\n"), 5000), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsBinary(write(tc.name, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("IsBinary(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestIsBinary_MissingFile(t *testing.T) {
	if _, err := IsBinary(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("IsBinary on a missing file = nil error, want one")
	}
}
