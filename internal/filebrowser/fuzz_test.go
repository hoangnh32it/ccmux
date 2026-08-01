package filebrowser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzIsBinaryContent hammers the text/binary sniff. It is a heuristic
// over attacker-shaped bytes — a file the user's agent just wrote, a
// path from a tailnet peer — which is exactly the surface the repo's
// other fuzzers cover.
//
// Invariants, none of which depend on the verdict being "right":
//   - it never panics
//   - it never reads past SniffLen
//   - NUL anywhere in the window always means binary
//   - valid UTF-8 with no NUL always means text
func FuzzIsBinaryContent(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("package main\n"))
	f.Add([]byte("caf\xe9"))
	f.Add([]byte("with\x00nul"))
	f.Add([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0d"))
	f.Add([]byte("tiếng Việt 日本語 🎉"))
	f.Add([]byte(strings.Repeat("a", SniffLen+7)))
	f.Add([]byte{0xFF, 0xFE, 'h', 0x00})
	f.Add([]byte{0xE6}) // lone lead byte of a 3-byte rune

	f.Fuzz(func(t *testing.T, data []byte) {
		got := IsBinaryContent(data)

		window := data
		if len(window) > SniffLen {
			window = window[:SniffLen]
		}

		// A NUL inside the window is unambiguous.
		for _, b := range window {
			if b == 0 {
				if !got {
					t.Fatalf("NUL in the sniff window but IsBinaryContent = false: %q", window)
				}
				return
			}
		}
		// No NUL and the window is valid UTF-8 → text, always.
		if utf8.Valid(window) && got {
			t.Fatalf("valid UTF-8 with no NUL reported binary: %q", window)
		}
		// Content past the window must not influence the verdict.
		if len(data) > SniffLen {
			if IsBinaryContent(window) != got {
				t.Fatalf("bytes past SniffLen changed the verdict; the sniff is not bounded")
			}
		}
	})
}

// FuzzResolve is the security-shaped one. Tree.Resolve is the only
// thing standing between an arbitrary caller-supplied path and the
// filesystem, and `ccmux files read --host` feeds it strings off the
// network. The invariant is absolute: whatever Resolve returns without
// an error is inside the root.
func FuzzResolve(f *testing.F) {
	for _, seed := range []string{
		"", ".", "..", "ok.go", "./ok.go", "sub/deep.go", "sub/../ok.go",
		"../secret", "../../../../etc/passwd", "/etc/passwd",
		`\Windows\system32`, "sub/../../escape", "a/./b/../c",
		"....//....//etc", "nul\x00byte", "space file.txt", "日本/файл",
	} {
		f.Add(seed)
	}

	root := f.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		f.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.go"), []byte("package main"), 0o644); err != nil {
		f.Fatal(err)
	}
	// EvalSymlinks the root once: on macOS t.TempDir() sits under
	// /var → /private/var, and comparing an unresolved prefix would
	// make every case look like an escape.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		f.Fatal(err)
	}
	tree := Open(root)

	f.Fuzz(func(t *testing.T, rel string) {
		full, err := tree.Resolve(rel)
		if err != nil {
			return // rejection is always a safe answer
		}
		if !filepath.IsAbs(full) {
			t.Fatalf("Resolve(%q) returned a relative path %q", rel, full)
		}
		// Two checks, because Resolve deliberately allows paths that
		// don't exist yet (the caller's Open reports ENOENT, not this
		// function).
		//
		// Lexical containment against the root as given always holds.
		if !contained(full, root) {
			t.Fatalf("Resolve(%q) escaped lexically: %q is outside %q", rel, full, root)
		}
		// And where the path does exist, real containment against the
		// symlink-resolved root — which is not the same string on
		// macOS, where TempDir sits under /var → /private/var.
		if resolved, rerr := filepath.EvalSymlinks(full); rerr == nil {
			if !contained(resolved, realRoot) {
				t.Fatalf("Resolve(%q) escaped via symlink: %q is outside %q", rel, resolved, realRoot)
			}
		}
	})
}

// FuzzHighlight pushes arbitrary bytes through chroma with a real
// lexer attached. Tokenisers are state machines over untrusted input;
// the contract here is that a malformed file degrades to plain text
// rather than panicking or vanishing.
//
// Deliberately NOT in the Makefile's FUZZ_TARGETS. At CI's 100k-exec
// budget it runs about five and a half minutes — chroma matches each
// generated filename against every registered lexer's glob and then
// tokenises — which is most of a PR's fuzz job for one target. Its
// seed corpus still runs on every `go test ./...`, and a deep sweep is
// one command away:
//
//	go test ./internal/filebrowser/ -run '^$' -fuzz='^FuzzHighlight$' -fuzztime=5m
//
// Revisit if chroma gets cheaper or the nightly long-budget cron
// (docs/01_Specs/03_Testing_And_CI.md) lands, where five minutes is
// nothing.
func FuzzHighlight(f *testing.F) {
	f.Add("main.go", "package main\nfunc main() {}\n")
	f.Add("a.json", `{"k":`)              // truncated JSON
	f.Add("a.py", "def f(:\n  pass")      // syntax error
	f.Add("weird.xyzzy", "no lexer here") // unrecognized extension
	f.Add("a.go", "\x00\x01\x02")         // control bytes
	f.Add("", "")                         // no path at all
	f.Add("a.md", "# heading\n\n```go\n") // unterminated fence

	f.Fuzz(func(t *testing.T, path, content string) {
		// Cap the input so a sweep spends its budget on shapes rather
		// than on a handful of megabyte tokenisations. The size cap's
		// own behaviour is pinned by TestHighlight_OverLimitStaysPlain.
		if len(content) > 16<<10 {
			return
		}
		got := Highlight(path, content)

		if content == "" && got != "" {
			t.Fatalf("Highlight of empty content produced %q", got)
		}
		// Unhighlightable input must come back byte-identical: an
		// unknown extension, or anything past the size cap.
		if Lang(path) == "" && got != content {
			t.Fatalf("no lexer for %q but the content changed:\n in: %q\nout: %q", path, content, got)
		}
		// Highlighting only ever adds escapes, so the output can never
		// be shorter than the input.
		if len(got) < len(content) {
			t.Fatalf("Highlight shrank the content: %d → %d bytes", len(content), len(got))
		}
	})
}

// contained reports whether path is root itself or sits underneath it.
func contained(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
