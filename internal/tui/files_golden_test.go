package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/skzv/ccmux/internal/filebrowser"
	"github.com/skzv/ccmux/internal/project"
	"github.com/skzv/ccmux/internal/tui/styles"
)

// fakeFileEntries is a hand-built listing shaped like what
// filebrowser.Tree.List returns for a small Go project: root-level
// files first, then folders in lexical order, paths inside each folder
// sorted. Built in memory rather than walked off disk so the golden
// doesn't depend on a temp dir's name, its mtimes, or the machine's
// filesystem ordering.
func fakeFileEntries() []filebrowser.Entry {
	mk := func(rel string, size int64) filebrowser.Entry {
		dir := ""
		name := rel
		for i := len(rel) - 1; i >= 0; i-- {
			if rel[i] == '/' {
				dir, name = rel[:i], rel[i+1:]
				break
			}
		}
		return filebrowser.Entry{
			Path: "/tmp/demo/" + rel,
			Rel:  rel,
			Dir:  dir,
			Name: name,
			Size: size,
		}
	}
	return []filebrowser.Entry{
		mk("README.md", 2048),
		mk("go.mod", 310),
		mk("main.go", 1180),
		mk("cmd/serve/main.go", 4096),
		mk("docs/architecture.md", 8192),
		mk("internal/store/store.go", 15360),
		mk("internal/store/store_test.go", 9216),
		mk("web/assets/logo.png", 40960),
	}
}

// newFilesForGolden builds a Files screen loaded with the fake tree at
// a fixed size, with the given fold state applied.
func newFilesForGolden(st styles.Styles, width, height int, expand bool) filesModel {
	m := newFiles(st, DefaultKeymap())
	p := project.Project{Name: "demo", Path: "/tmp/demo"}
	m.project = &p
	m.root = p.Path
	m.entries = fakeFileEntries()
	m.expanded = filebrowser.DefaultFolds(m.entries, expand)
	m.SetSize(width, height)
	return m
}

// TestFilesGolden snapshots the Files screen in its default state:
// wide layout, tree collapsed to root files plus top-level folder
// headers, cursor on the first row.
//
// Note what the preview pane shows here — "No file selected." The
// cursor starts on README.md, but previews load asynchronously
// (filesPreviewLoadedMsg), and this test renders without running the
// Bubble Tea loop. That is the honest first frame: the pane before its
// content arrives. TestFilesPreviewGolden covers the loaded state.
func TestFilesGolden(t *testing.T) {
	const width, height = 120, 40
	st := styles.Default()

	m := newFilesForGolden(st, width, height, false)

	helpLine := renderHelpBarFor(st, m.HelpBarProps(width), width)
	body := m.View(width, height-lipgloss.Height(helpLine))
	goldenAssert(t, "files.txt", composeScreen(body, helpLine, height))
}

// TestFilesPreviewGolden snapshots the loaded state: every folder
// expanded, the cursor on a .go file, and its highlighted contents in
// the preview pane. The preview is injected directly rather than read
// off disk so the snapshot has no filesystem dependency.
//
// The formatter is chroma's "noop", which walks the same token stream
// and writes no escapes. Real 256-colour output would put hundreds of
// escape sequences in the golden, making it unreadable in review and
// prone to churn on any chroma style tweak — while proving nothing the
// package's own highlight tests don't already prove.
func TestFilesPreviewGolden(t *testing.T) {
	const width, height = 120, 40
	st := styles.Default()

	m := newFilesForGolden(st, width, height, true)

	// Park the cursor on internal/store/store.go.
	target := "internal/store/store.go"
	for i, r := range m.visibleRows() {
		if r.Kind == filebrowser.RowFile && m.entries[r.EntryIdx].Rel == target {
			m.cursor = i
			break
		}
	}
	if e := m.selectedEntry(); e == nil || e.Rel != target {
		t.Fatalf("cursor did not land on %s", target)
	}

	const src = `package store

import "errors"

// ErrNotFound is returned when a key is absent.
var ErrNotFound = errors.New("not found")

func Get(key string) (string, error) {
	return "", ErrNotFound
}
`
	m.previewRel = target
	m.previewData = filebrowser.Preview{
		Rel:           target,
		Size:          int64(len(src)),
		Content:       src,
		Lang:          "Go",
		Highlightable: true,
	}
	pw, _ := m.previewPaneSize()
	m.preview.SetContent(filebrowser.HighlightWith(target, src, filebrowser.DefaultStyle, "noop"))
	_ = pw

	helpLine := renderHelpBarFor(st, m.HelpBarProps(width), width)
	body := m.View(width, height-lipgloss.Height(helpLine))
	goldenAssert(t, "files_preview.txt", composeScreen(body, helpLine, height))
}

// TestFilesBinaryGolden snapshots the binary-file placeholder — the
// spec scenario that says ccmux must never spray a PNG at the
// terminal. The badge on the footer says "binary" and carries the size
// so the pane is still informative.
func TestFilesBinaryGolden(t *testing.T) {
	const width, height = 120, 40
	st := styles.Default()

	m := newFilesForGolden(st, width, height, true)

	target := "web/assets/logo.png"
	for i, r := range m.visibleRows() {
		if r.Kind == filebrowser.RowFile && m.entries[r.EntryIdx].Rel == target {
			m.cursor = i
			break
		}
	}
	if e := m.selectedEntry(); e == nil || e.Rel != target {
		t.Fatalf("cursor did not land on %s", target)
	}

	m.previewRel = target
	m.previewData = filebrowser.Preview{Rel: target, Size: 40960, Binary: true}
	pw, _ := m.previewPaneSize()
	m.preview.SetContent(m.renderPreviewBody(pw))

	helpLine := renderHelpBarFor(st, m.HelpBarProps(width), width)
	body := m.View(width, height-lipgloss.Height(helpLine))
	goldenAssert(t, "files_binary.txt", composeScreen(body, helpLine, height))
}

// TestFilesEmptyGolden snapshots the no-project state, including the
// line that tells the user what distinguishes Files from Notes — the
// mitigation design.md promised for the two tabs looking alike.
func TestFilesEmptyGolden(t *testing.T) {
	const width, height = 120, 40
	st := styles.Default()

	m := newFiles(st, DefaultKeymap())
	m.SetSize(width, height)

	helpLine := renderHelpBarFor(st, m.HelpBarProps(width), width)
	body := m.View(width, height-lipgloss.Height(helpLine))
	goldenAssert(t, "files_empty.txt", composeScreen(body, helpLine, height))
}

// TestFilesNarrowGolden snapshots the sub-100-column layout, where the
// preview pane is dropped and the tree takes the full width.
func TestFilesNarrowGolden(t *testing.T) {
	const width, height = 80, 30
	st := styles.Default()

	m := newFilesForGolden(st, width, height, true)

	helpLine := renderHelpBarFor(st, m.HelpBarProps(width), width)
	body := m.View(width, height-lipgloss.Height(helpLine))
	goldenAssert(t, "files_narrow.txt", composeScreen(body, helpLine, height))
}
