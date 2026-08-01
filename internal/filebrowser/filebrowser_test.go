package filebrowser

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeTree materializes a slash-keyed map of files under a fresh temp
// dir and returns the root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func rels(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Rel
	}
	return out
}

func TestDirOf(t *testing.T) {
	cases := []struct{ in, want string }{
		{"README.md", ""},
		{"go.mod", ""},
		{"internal/tui/app.go", "internal/tui"},
		{"docs/01_Specs/00_Vision.md", "docs/01_Specs"},
	}
	for _, tc := range cases {
		if got := dirOf(tc.in); got != tc.want {
			t.Errorf("dirOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSkipDir pins filebrowser's prune rule to internal/notes' — the
// two screens must agree on what counts as part of the project.
func TestSkipDir(t *testing.T) {
	for _, d := range []string{".git", ".obsidian", ".ccmux", "node_modules", "vendor", "dist", "build", "target", "__pycache__"} {
		if !skipDir(d) {
			t.Errorf("skipDir(%q) = false, want true", d)
		}
	}
	for _, d := range []string{"docs", "openspec", "internal", "src", "cmd", "target_practice"} {
		if skipDir(d) {
			t.Errorf("skipDir(%q) = true, want false", d)
		}
	}
}

func TestList_MissingRoot(t *testing.T) {
	got, err := Open(filepath.Join(t.TempDir(), "nope")).List()
	if err != nil {
		t.Fatalf("List on a missing root should be empty, not an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %v, want empty", got)
	}
}

func TestList_EmptyRoot(t *testing.T) {
	got, err := Open(t.TempDir()).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %v, want empty", got)
	}
}

// TestList_EveryFileTypeAndPruning covers the capability spec's two
// tree scenarios at once: every file type is listed (not just .md),
// and noise directories are pruned.
func TestList_EveryFileTypeAndPruning(t *testing.T) {
	root := writeTree(t, map[string]string{
		// Every file type must survive — this is the whole point of
		// Files existing next to Notes.
		"main.go":             "package main",
		"src/lib.rs":          "fn main() {}",
		"package.json":        "{}",
		"README.md":           "# r",
		"assets/logo.svg":     "<svg/>",
		".gitignore":          "bin/",
		"docs/guide/setup.md": "# setup",
		// Pruned trees.
		".git/config":               "[core]",
		".obsidian/workspace.json":  "{}",
		"node_modules/dep/index.js": "module.exports={}",
		"vendor/pkg/vendored.go":    "package pkg",
		"target/debug/out.txt":      "built",
		"__pycache__/mod.pyc":       "compiled",
	})

	got, err := Open(root).List()
	if err != nil {
		t.Fatal(err)
	}
	gotRels := rels(got)

	want := []string{
		".gitignore",
		"README.md",
		"main.go",
		"package.json",
		"assets/logo.svg",
		"docs/guide/setup.md",
		"src/lib.rs",
	}
	if len(gotRels) != len(want) {
		t.Fatalf("List() = %v (%d entries), want %v (%d)", gotRels, len(gotRels), want, len(want))
	}
	for i := range want {
		if gotRels[i] != want[i] {
			t.Errorf("List()[%d] = %q, want %q (full: %v)", i, gotRels[i], want[i], gotRels)
		}
	}

	for _, pruned := range []string{".git", ".obsidian", "node_modules", "vendor", "target", "__pycache__"} {
		for _, rel := range gotRels {
			if strings.HasPrefix(rel, pruned+"/") {
				t.Errorf("pruned dir %q leaked: %s", pruned, rel)
			}
		}
	}
}

// TestList_Metadata checks the fields the preview pane and its size
// cap depend on.
func TestList_Metadata(t *testing.T) {
	root := writeTree(t, map[string]string{"internal/tui/app.go": "package tui\n"})
	got, err := Open(root).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("List() = %d entries, want 1", len(got))
	}
	e := got[0]
	if e.Rel != "internal/tui/app.go" {
		t.Errorf("Rel = %q", e.Rel)
	}
	if e.Dir != "internal/tui" {
		t.Errorf("Dir = %q", e.Dir)
	}
	if e.Name != "app.go" {
		t.Errorf("Name = %q", e.Name)
	}
	if e.Size != int64(len("package tui\n")) {
		t.Errorf("Size = %d, want %d", e.Size, len("package tui\n"))
	}
	if e.Modified.IsZero() {
		t.Error("Modified is zero")
	}
	if want := filepath.Join(root, "internal", "tui", "app.go"); e.Path != want {
		t.Errorf("Path = %q, want %q", e.Path, want)
	}
}

// TestList_SkipsNonRegularFiles guards the walk against symlinks:
// following one risks leaving the tree or looping.
func TestList_SkipsNonRegularFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}
	root := writeTree(t, map[string]string{
		"real.go":      "package main",
		"sub/other.go": "package sub",
	})
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "subline")); err != nil {
		t.Fatal(err)
	}

	got, err := Open(root).List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"real.go", "sub/other.go"}
	gotRels := rels(got)
	if len(gotRels) != len(want) {
		t.Fatalf("List() = %v, want %v", gotRels, want)
	}
	for i := range want {
		if gotRels[i] != want[i] {
			t.Errorf("List()[%d] = %q, want %q", i, gotRels[i], want[i])
		}
	}
}

// TestList_UnreadableDirIsSkipped: one permission-denied directory
// must not cost the caller the whole tree.
func TestList_UnreadableDirIsSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0 does not deny directory reads on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	root := writeTree(t, map[string]string{
		"visible.go":       "package main",
		"locked/hidden.go": "package locked",
	})
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	got, err := Open(root).List()
	if err != nil {
		t.Fatalf("List should tolerate an unreadable dir, got %v", err)
	}
	gotRels := rels(got)
	if len(gotRels) != 1 || gotRels[0] != "visible.go" {
		t.Fatalf("List() = %v, want [visible.go]", gotRels)
	}
}

func TestRead(t *testing.T) {
	root := writeTree(t, map[string]string{"internal/tui/app.go": "package tui\n"})
	body, err := Open(root).Read("internal/tui/app.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "package tui\n" {
		t.Errorf("Read = %q", body)
	}
}

// TestResolve_RejectsEscapes is the containment contract behind
// `ccmux files read` — the rel argument comes from a command line, or
// over the network with --host.
func TestResolve_RejectsEscapes(t *testing.T) {
	root := writeTree(t, map[string]string{
		"ok.go":       "package main",
		"sub/deep.go": "package sub",
	})
	tree := Open(root)

	// Allowed: plain, nested, and lexically-redundant paths that stay
	// inside the tree, plus a file that does not exist yet.
	for _, rel := range []string{"ok.go", "sub/deep.go", "sub/../ok.go", "./ok.go", "missing.go"} {
		if _, err := tree.Resolve(rel); err != nil {
			t.Errorf("Resolve(%q) = error %v, want allowed", rel, err)
		}
	}

	// Rejected: traversal out of the tree, and absolute paths.
	for _, rel := range []string{"../outside.txt", "../../etc/passwd", "sub/../../escape", "/etc/passwd"} {
		if _, err := tree.Resolve(rel); err == nil {
			t.Errorf("Resolve(%q) = nil error, want rejection", rel)
		}
	}
}

// TestResolve_RejectsSymlinkEscape: lexical cleaning can't see a link
// inside the tree pointing out of it, so Resolve checks the real path
// too.
func TestResolve_RejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}
	root := writeTree(t, map[string]string{"ok.go": "package main"})
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	tree := Open(root)
	if _, err := tree.Resolve("link.txt"); err == nil {
		t.Error("Resolve(link out of tree) = nil error, want rejection")
	}
	if _, err := tree.Read("link.txt"); err == nil {
		t.Error("Read(link out of tree) = nil error, want rejection")
	}
	// A root that is itself a symlink (macOS /tmp → /private/tmp) must
	// not make every lookup fail.
	if _, err := tree.Resolve("ok.go"); err != nil {
		t.Errorf("Resolve(ok.go) = %v, want allowed", err)
	}
}
