package filebrowser

import (
	"sort"
	"strings"
)

// RowKind distinguishes a folder header row from a file row in the
// flattened tree.
type RowKind int

const (
	RowFolder RowKind = iota
	RowFile
)

// Row is one line of the currently-visible tree. A cursor indexes a
// []Row, so it can land on folder headers — that's what makes them
// togglable. For a file row, EntryIdx points back into the entry slice
// the rows were built from; for a folder row it's -1 and Dir is the
// folder's slash-separated path.
type Row struct {
	Kind     RowKind
	Dir      string // folder path (RowFolder) or the file's containing Dir (RowFile)
	EntryIdx int    // index into the entries slice for RowFile; -1 for RowFolder
	Depth    int    // nesting level, 0 at the root — drives indentation
}

// FolderDirs returns every folder path implied by entries, including
// intermediate ancestors that hold no direct files (e.g. "internal"
// when only "internal/tui/app.go" exists). Sorted lexically.
func FolderDirs(entries []Entry) []string {
	set := map[string]bool{}
	for _, e := range entries {
		for d := e.Dir; d != ""; d = ParentDir(d) {
			set[d] = true
		}
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// DefaultFolds returns the initial fold state for entries: every
// folder collapsed, or — when expand is true — every folder expanded,
// intermediate ancestors included.
//
// Collapsed is the right default for a whole-project tree: expanding
// everything in a repo of any size buries the root-level files the
// user came for.
func DefaultFolds(entries []Entry, expand bool) map[string]bool {
	folds := make(map[string]bool)
	if !expand {
		return folds
	}
	for _, d := range FolderDirs(entries) {
		folds[d] = true
	}
	return folds
}

// VisibleRows flattens entries into the rows currently on screen:
// root-level files first (always visible), then a depth-first walk of
// the folder tree that descends into a folder only when expanded[dir]
// is set. Within a folder, its own files come before its sub-folders,
// matching the Dir-then-Rel order List returns.
//
// entries must be sorted as List returns them; VisibleRows does not
// re-sort, it only groups.
func VisibleRows(entries []Entry, expanded map[string]bool) []Row {
	filesByDir := map[string][]int{}
	folderSet := map[string]bool{}
	for i, e := range entries {
		filesByDir[e.Dir] = append(filesByDir[e.Dir], i)
		for d := e.Dir; d != ""; d = ParentDir(d) {
			folderSet[d] = true
		}
	}
	childFolders := map[string][]string{}
	for d := range folderSet {
		p := ParentDir(d)
		childFolders[p] = append(childFolders[p], d)
	}
	for p := range childFolders {
		sort.Strings(childFolders[p])
	}

	var rows []Row
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		for _, idx := range filesByDir[dir] {
			rows = append(rows, Row{Kind: RowFile, Dir: dir, EntryIdx: idx, Depth: depth})
		}
		for _, child := range childFolders[dir] {
			rows = append(rows, Row{Kind: RowFolder, Dir: child, EntryIdx: -1, Depth: depth})
			if expanded[child] {
				walk(child, depth+1)
			}
		}
	}
	walk("", 0) // the root is always expanded; its files are always visible
	return rows
}

// ParentDir returns the parent folder of a slash-separated folder
// path, or "" (the root) for a top-level folder.
func ParentDir(dir string) string {
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		return dir[:i]
	}
	return ""
}

// DescendantOf reports whether row r lives inside folder dir's subtree
// — a nested folder or file, or a file sitting directly in dir. The
// folder's own header row is not its own descendant, which is what
// lets a collapse move the cursor up to that header.
func DescendantOf(r Row, dir string) bool {
	if dir == "" {
		return false
	}
	if strings.HasPrefix(r.Dir, dir+"/") {
		return true
	}
	return r.Kind == RowFile && r.Dir == dir
}
