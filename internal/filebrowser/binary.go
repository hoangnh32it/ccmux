package filebrowser

import (
	"bytes"
	"errors"
	"io"
	"os"
	"unicode/utf8"
)

// SniffLen caps how much of a file is read to classify it as text or
// binary. 8000 bytes is git's own threshold (buffer_is_binary in
// xdiff-interface.c) and is more than enough to catch the NUL bytes
// every real binary format carries in its header.
const SniffLen = 8000

// IsBinary reports whether the file at path looks like binary content
// rather than text. Only the first SniffLen bytes are read, so this is
// cheap enough to call on the file under the cursor each time the
// selection moves — but not cheap enough to call for every row of a
// whole-project listing, which is why Entry carries no Binary field.
// Callers sniff lazily, on the file they are about to preview.
func IsBinary(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, SniffLen)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, err
	}
	return IsBinaryContent(buf[:n]), nil
}

// IsBinaryContent applies the text/binary sniff to an in-memory
// buffer. A buffer is binary if it holds a NUL byte or is not valid
// UTF-8.
//
// The UTF-8 half of the rule means a legacy single-byte-encoded file
// (Latin-1, Shift-JIS) is classified as binary. That is the intended
// trade-off: ccmux's preview pane writes to a UTF-8 terminal, so such
// a file would render as mojibake anyway, and the "binary file"
// placeholder is the more honest outcome.
//
// An empty buffer is text — an empty file previews fine as nothing.
func IsBinaryContent(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	// A buffer filled to the sniff limit was probably cut out of a
	// longer file, so its tail may be a partial rune.
	truncated := len(b) >= SniffLen
	if truncated {
		b = b[:SniffLen]
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return true
	}
	if truncated {
		b = trimPartialRune(b)
	}
	return !utf8.Valid(b)
}

// trimPartialRune drops a trailing byte sequence that is a valid start
// of a multi-byte rune but was cut short by the sniff window, so a
// perfectly good UTF-8 file isn't called binary over an accident of
// where the window happened to end.
func trimPartialRune(b []byte) []byte {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if !utf8.RuneStart(b[i]) {
			continue
		}
		// b[i:] is the final rune candidate. RuneError with a width of
		// one means it doesn't decode — either genuinely invalid or
		// truncated; both are safer to drop than to judge.
		if r, size := utf8.DecodeRune(b[i:]); r == utf8.RuneError && size <= 1 {
			return b[:i]
		}
		return b
	}
	return b
}
