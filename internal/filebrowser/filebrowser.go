// Package filebrowser is the read-only operations layer for the TUI's
// Files tab. Where internal/notes scopes a project to its markdown
// vault, filebrowser sees the whole tree: every regular file under the
// browsed root, grouped by the folder it lives in, with the same
// version-control, dependency, and build-output directories pruned.
//
// The two packages stay separate on purpose. internal/notes' contract
// is "every .md file", and its row labels lean on markdown structure
// (the leading H1) — neither assumption survives contact with a .go or
// a .png. filebrowser keeps a simpler contract: filenames as written
// on disk, no interpretation of file contents at list time.
package filebrowser

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Tree is the browsable file listing for one directory — normally a
// project root, but the Files screen re-roots it to the attached tmux
// pane's working directory once cwd tracking lands.
type Tree struct {
	Root string // absolute path to the directory being browsed
}

// Open returns a Tree rooted at dir.
func Open(dir string) Tree {
	return Tree{Root: dir}
}

// Entry is one regular file in the tree listing.
type Entry struct {
	Path     string    // absolute path on disk
	Rel      string    // slash-separated path relative to Root ("internal/tui/app.go")
	Dir      string    // slash-separated directory portion of Rel ("" for a root-level file)
	Name     string    // base filename, verbatim — no prefix-stripping or prettifying
	Size     int64     // bytes on disk, for the preview size cap
	Modified time.Time // mtime, for sorting and display
}

// List returns every regular file under Root, sorted by containing
// directory (root-level files first, then folders lexically) and then
// by path. Version-control, dependency, and build-output directories
// are pruned so the listing reflects the project's own files rather
// than vendored trees.
//
// Unlike internal/notes.Vault.List, a per-entry walk error does not
// abort the listing: a whole-project walk routinely meets a directory
// the user can't read, and losing the entire tree over one of them
// would make the screen useless. Such a directory is skipped and the
// rest of the tree is still returned.
func (t Tree) List() ([]Entry, error) {
	if _, err := os.Stat(t.Root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	err := filepath.WalkDir(t.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != t.Root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		// Symlinks, sockets, devices, and fifos are not browsable
		// content — and following a symlinked directory risks walking
		// out of the tree or into a cycle. Only regular files list.
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(t.Root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		var (
			mod  time.Time
			size int64
		)
		if info, infoErr := d.Info(); infoErr == nil {
			mod = info.ModTime()
			size = info.Size()
		}
		out = append(out, Entry{
			Path:     path,
			Rel:      rel,
			Dir:      dirOf(rel),
			Name:     filepath.Base(rel),
			Size:     size,
			Modified: mod,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir < out[j].Dir
		}
		return out[i].Rel < out[j].Rel
	})
	return out, nil
}

// Read returns the bytes of the file at rel, a slash-separated path
// relative to Root.
func (t Tree) Read(rel string) ([]byte, error) {
	full, err := t.Resolve(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

// Resolve turns a tree-relative slash path into an absolute path on
// disk, rejecting anything that escapes Root.
//
// The containment check is not decoration: `ccmux files read <project>
// <path>` takes rel straight from the command line, and with `--host`
// it arrives over the network from a peer ccmuxd. Without this, a rel
// of "../../.ssh/id_rsa" would read outside the browsed project.
// A rel naming a file that does not exist resolves fine — reporting
// ENOENT is the caller's job, not this function's.
func (t Tree) Resolve(rel string) (string, error) {
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("filebrowser: %q: path must be relative to the tree root", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("filebrowser: %q escapes the tree root", rel)
	}
	full := filepath.Join(t.Root, clean)

	// Lexical cleaning can't see a symlink inside the tree pointing
	// out of it. Both sides are resolved before comparing because the
	// root itself is often a symlink (macOS /tmp → /private/tmp).
	realRoot, err := filepath.EvalSymlinks(t.Root)
	if err != nil {
		return "", fmt.Errorf("filebrowser: resolve root: %w", err)
	}
	realFull, err := filepath.EvalSymlinks(full)
	if err != nil {
		if os.IsNotExist(err) {
			return full, nil
		}
		return "", fmt.Errorf("filebrowser: resolve %q: %w", rel, err)
	}
	if realFull != realRoot && !strings.HasPrefix(realFull, realRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("filebrowser: %q escapes the tree root", rel)
	}
	return full, nil
}

// skipDir reports whether a directory should be pruned from the walk.
// The rule is deliberately identical to internal/notes.skipDir —
// hidden directories (.git, .obsidian, .ccmux) plus the usual
// dependency and build-output trees — so Notes and Files agree on what
// counts as part of the project.
//
// Hidden *files* are kept: .gitignore, .goreleaser.yaml and friends
// are project files a user browsing the tree expects to see, and
// unlike a hidden directory they carry no risk of dragging in
// thousands of entries.
func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "dist", "build", "target", "__pycache__":
		return true
	}
	return false
}

// dirOf returns the slash-separated directory portion of a tree-
// relative path, or "" when the file sits at the root.
func dirOf(rel string) string {
	d := filepath.ToSlash(filepath.Dir(rel))
	if d == "." {
		return ""
	}
	return d
}
