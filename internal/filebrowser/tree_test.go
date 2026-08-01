package filebrowser

import (
	"reflect"
	"strings"
	"testing"
)

// entriesFor builds a sorted entry slice from slash paths the same way
// List would, without touching the filesystem.
func entriesFor(paths ...string) []Entry {
	out := make([]Entry, 0, len(paths))
	for _, rel := range paths {
		out = append(out, Entry{Rel: rel, Dir: dirOf(rel), Name: rel[strings.LastIndex(rel, "/")+1:]})
	}
	return out
}

// describe renders rows as "kind:dir[:rel]@depth" so a mismatch reads
// like a tree rather than a struct dump.
func describe(entries []Entry, rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		if r.Kind == RowFolder {
			out[i] = "dir " + r.Dir + " @" + string(rune('0'+r.Depth))
			continue
		}
		out[i] = "file " + entries[r.EntryIdx].Rel + " @" + string(rune('0'+r.Depth))
	}
	return out
}

func TestParentDir(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"docs", ""},
		{"docs/01_Specs", "docs"},
		{"internal/tui/styles", "internal/tui"},
	}
	for _, tc := range cases {
		if got := ParentDir(tc.in); got != tc.want {
			t.Errorf("ParentDir(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFolderDirs checks that ancestors holding no direct files are
// still synthesized — "internal" exists as a folder even though every
// file lives two levels down.
func TestFolderDirs(t *testing.T) {
	entries := entriesFor(
		"go.mod",
		"internal/tui/app.go",
		"internal/tui/styles/tokens.go",
		"docs/guide.md",
	)
	want := []string{"docs", "internal", "internal/tui", "internal/tui/styles"}
	if got := FolderDirs(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("FolderDirs() = %v, want %v", got, want)
	}
}

func TestFolderDirs_NoFolders(t *testing.T) {
	if got := FolderDirs(entriesFor("README.md", "go.mod")); len(got) != 0 {
		t.Errorf("FolderDirs(root-only) = %v, want empty", got)
	}
}

func TestDefaultFolds(t *testing.T) {
	entries := entriesFor("internal/tui/app.go", "docs/guide.md")

	if got := DefaultFolds(entries, false); len(got) != 0 {
		t.Errorf("DefaultFolds(expand=false) = %v, want everything collapsed", got)
	}

	expanded := DefaultFolds(entries, true)
	for _, d := range []string{"docs", "internal", "internal/tui"} {
		if !expanded[d] {
			t.Errorf("DefaultFolds(expand=true)[%q] = false, want true", d)
		}
	}
	if len(expanded) != 3 {
		t.Errorf("DefaultFolds(expand=true) = %v, want exactly 3 folders", expanded)
	}
}

// TestVisibleRows_CollapsedByDefault: with no fold state, only
// root-level files and top-level folder headers show.
func TestVisibleRows_CollapsedByDefault(t *testing.T) {
	entries := entriesFor(
		"README.md",
		"go.mod",
		"docs/guide.md",
		"internal/tui/app.go",
	)
	got := describe(entries, VisibleRows(entries, map[string]bool{}))
	want := []string{
		"file README.md @0",
		"file go.mod @0",
		"dir docs @0",
		"dir internal @0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("VisibleRows(collapsed) =\n  %v\nwant\n  %v", got, want)
	}
}

// TestVisibleRows_ExpandOneLevel: expanding "internal" reveals its
// sub-folder header but not that sub-folder's contents.
func TestVisibleRows_ExpandOneLevel(t *testing.T) {
	entries := entriesFor(
		"go.mod",
		"internal/util.go",
		"internal/tui/app.go",
	)
	got := describe(entries, VisibleRows(entries, map[string]bool{"internal": true}))
	want := []string{
		"file go.mod @0",
		"dir internal @0",
		"file internal/util.go @1",
		"dir internal/tui @1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("VisibleRows(internal expanded) =\n  %v\nwant\n  %v", got, want)
	}
}

// TestVisibleRows_FullyExpanded pins the whole ordering contract:
// root files first, then a depth-first walk where each folder's own
// files precede its sub-folders, with depth tracking nesting.
func TestVisibleRows_FullyExpanded(t *testing.T) {
	entries := entriesFor(
		"README.md",
		"docs/guide.md",
		"internal/util.go",
		"internal/tui/app.go",
		"internal/tui/styles/tokens.go",
	)
	got := describe(entries, VisibleRows(entries, DefaultFolds(entries, true)))
	want := []string{
		"file README.md @0",
		"dir docs @0",
		"file docs/guide.md @1",
		"dir internal @0",
		"file internal/util.go @1",
		"dir internal/tui @1",
		"file internal/tui/app.go @2",
		"dir internal/tui/styles @2",
		"file internal/tui/styles/tokens.go @3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("VisibleRows(all expanded) =\n  %v\nwant\n  %v", got, want)
	}
}

// TestVisibleRows_ExpandingLeafSkipsAncestor: fold state on a nested
// folder is inert while an ancestor is collapsed.
func TestVisibleRows_ExpandingLeafSkipsAncestor(t *testing.T) {
	entries := entriesFor("internal/tui/app.go")
	got := describe(entries, VisibleRows(entries, map[string]bool{"internal/tui": true}))
	want := []string{"dir internal @0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("VisibleRows(leaf expanded, ancestor collapsed) =\n  %v\nwant\n  %v", got, want)
	}
}

func TestVisibleRows_Empty(t *testing.T) {
	if got := VisibleRows(nil, map[string]bool{}); len(got) != 0 {
		t.Errorf("VisibleRows(nil) = %v, want empty", got)
	}
}

// TestVisibleRows_EntryIdxRoundTrips: a file row must point back at
// the entry it came from, since that is how the preview pane resolves
// the selection.
func TestVisibleRows_EntryIdxRoundTrips(t *testing.T) {
	entries := entriesFor("README.md", "docs/guide.md", "internal/tui/app.go")
	for _, r := range VisibleRows(entries, DefaultFolds(entries, true)) {
		if r.Kind == RowFolder {
			if r.EntryIdx != -1 {
				t.Errorf("folder row %q has EntryIdx %d, want -1", r.Dir, r.EntryIdx)
			}
			continue
		}
		if r.EntryIdx < 0 || r.EntryIdx >= len(entries) {
			t.Fatalf("file row EntryIdx %d out of range", r.EntryIdx)
		}
		if entries[r.EntryIdx].Dir != r.Dir {
			t.Errorf("row Dir %q != entry Dir %q", r.Dir, entries[r.EntryIdx].Dir)
		}
	}
}

func TestDescendantOf(t *testing.T) {
	cases := []struct {
		name string
		row  Row
		dir  string
		want bool
	}{
		{"file directly inside", Row{Kind: RowFile, Dir: "internal"}, "internal", true},
		{"file nested deeper", Row{Kind: RowFile, Dir: "internal/tui"}, "internal", true},
		{"nested folder header", Row{Kind: RowFolder, Dir: "internal/tui"}, "internal", true},
		{"the folder's own header", Row{Kind: RowFolder, Dir: "internal"}, "internal", false},
		{"sibling with a shared prefix", Row{Kind: RowFile, Dir: "internal2"}, "internal", false},
		{"unrelated folder", Row{Kind: RowFile, Dir: "docs"}, "internal", false},
		{"root is nobody's parent here", Row{Kind: RowFile, Dir: "docs"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DescendantOf(tc.row, tc.dir); got != tc.want {
				t.Errorf("DescendantOf(%+v, %q) = %v, want %v", tc.row, tc.dir, got, tc.want)
			}
		})
	}
}
