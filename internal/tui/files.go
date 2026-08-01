package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/skzv/ccmux/internal/filebrowser"
	"github.com/skzv/ccmux/internal/project"
	"github.com/skzv/ccmux/internal/tmux"
	"github.com/skzv/ccmux/internal/tui/components"
	"github.com/skzv/ccmux/internal/tui/styles"
)

// filesModel is the Files tab — a whole-project file browser with a
// syntax-highlighted preview pane. It is deliberately *not* a
// generalized notesModel:
//
//   - Notes is a markdown vault. Its rows are labelled by each file's
//     leading H1 and its preview is always Glamour. Neither assumption
//     survives a .go or a .png, and bending notesModel to cover both
//     would blur a scope its own package doc states explicitly.
//
//   - Files is the whole tree. Rows are filenames as written on disk,
//     the preview picks a renderer per file type, and binary files get
//     a placeholder instead of bytes sprayed at the terminal.
//
// What it *does* reuse is Notes' interaction shape — tab toggles focus
// between tree and preview, j/k navigate within the focused pane,
// →/← fold and unfold — so muscle memory transfers between the two
// tabs. The tree logic itself lives in internal/filebrowser as pure
// functions, which is why this file is a fraction of notes.go's size.
type filesModel struct {
	st styles.Styles
	km Keymap

	project  *project.Project
	projects []project.Project

	// root is the directory being browsed. It starts as the project
	// path and is re-pointed by SetRoot when cwd tracking follows the
	// attached pane somewhere else.
	root string

	entries []filebrowser.Entry
	loading bool
	loadErr string
	cursor  int
	focus   filesFocus

	// expanded is the folder-fold state; a folder's children render
	// only when expanded[dir] is true. Collapsed is the default —
	// expanding a whole repo on open buries the root-level files the
	// user came for.
	expanded map[string]bool

	preview     viewport.Model
	previewRel  string // rel path backing the viewport, so we don't re-read on every render
	previewData filebrowser.Preview
	previewErr  string

	termWidth  int
	termHeight int

	// splitRatio is the tree pane's share of the total width, moved by
	// dragging the border between the panes. See files_mouse.go.
	splitRatio float64

	// draggingSplit is true between a press on the split border and
	// the matching release, so intermediate motion events are only
	// treated as a resize when a drag actually started there.
	draggingSplit bool

	pickingProject bool
	projCursor     int

	// followSession is the tmux session whose pane cwd the tree
	// tracks, pushed from the App as sessions refresh. Empty when
	// nothing is attached.
	followSession string

	// followCwd is the tracking toggle, bound to `f`. On by default,
	// because a file browser that silently ignores where you actually
	// are is the wrong default. Turned off the moment the user picks a
	// project explicitly — they asked for that tree, and yanking it
	// away on the next poll would be hostile.
	followCwd bool

	loadingSpinner spinner.Model
	editor         string
}

// filesFocus tracks which pane receives navigation keys.
type filesFocus int

const (
	filesFocusTree filesFocus = iota
	filesFocusPreview
)

const (
	// defaultSplitRatio gives the tree a third of the width, matching
	// the Notes screen so the two tabs don't jump around when you
	// switch between them.
	defaultSplitRatio = 1.0 / 3.0

	// minSplitRatio / maxSplitRatio bound the drag in Phase 4 and
	// clamp any ratio restored from elsewhere. Below the minimum the
	// tree can't show a filename; above the maximum the preview stops
	// being a preview.
	minSplitRatio = 0.15
	maxSplitRatio = 0.75

	// filesNarrowWidth is the width below which the preview pane is
	// dropped and the tree gets the full terminal. Matches Notes'
	// tighter-than-global threshold for the same reason: at 100 cols
	// a 1/3 + 2/3 split still leaves the preview readable.
	filesNarrowWidth = 100
)

// filesEntriesLoadedMsg carries the result of a background tree walk.
// Root is echoed so a stale result (the user switched projects while
// the walk ran) can be discarded.
type filesEntriesLoadedMsg struct {
	Root    string
	Entries []filebrowser.Entry
	Err     string
}

// filesPreviewLoadedMsg carries a file loaded for the preview pane.
// Root+Rel are echoed for the same staleness check.
type filesPreviewLoadedMsg struct {
	Root    string
	Rel     string
	Preview filebrowser.Preview
	Err     string
}

// filesReloadMsg asks the screen to re-walk the tree — dispatched when
// $EDITOR exits, since the user may have created or deleted files.
type filesReloadMsg struct{}

// filesCwdMsg carries the tracked session's pane working directory
// back from a poll. Session is echoed so a reply about a session the
// user has since stopped following can be dropped.
type filesCwdMsg struct {
	Session string
	Path    string
}

func newFiles(st styles.Styles, km Keymap) filesModel {
	vp := viewport.New(80, 20)
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(st.Semantic.Accent)
	return filesModel{
		st:             st,
		km:             km,
		preview:        vp,
		editor:         pickEditor(),
		expanded:       make(map[string]bool),
		splitRatio:     defaultSplitRatio,
		followCwd:      true,
		loadingSpinner: sp,
	}
}

// SetProject points the browser at a project. The walk runs off the UI
// goroutine; the returned Cmd delivers filesEntriesLoadedMsg. Returns
// nil when nothing changed or there is nothing to load.
func (m *filesModel) SetProject(p *project.Project) tea.Cmd {
	if p == nil {
		m.project = nil
		m.clearTree()
		return nil
	}
	if m.project != nil && m.project.Path == p.Path {
		return nil
	}
	m.project = p
	return m.SetRoot(p.Path)
}

// SetFollowSession names the tmux session whose pane cwd the tree
// should track. Pushed by the App from the session list; empty when
// nothing is attached.
func (m *filesModel) SetFollowSession(name string) {
	m.followSession = name
}

// FollowActive reports whether a cwd poll is worth dispatching: the
// toggle is on and there is a session to ask about.
func (m filesModel) FollowActive() bool {
	return m.followCwd && m.followSession != ""
}

// PollCwdCmd asks tmux for the tracked session's pane directory off
// the UI goroutine. Returns nil when there is nothing to track, so the
// App can call it unconditionally on its tick.
//
// The App only calls this while the Files screen is the visible one.
// Shelling out to tmux every two seconds for a tree nobody is looking
// at would burn a process per tick for no observable effect, which is
// the opposite of the dirty-flag rendering bar the rest of the TUI
// holds itself to.
func (m filesModel) PollCwdCmd() tea.Cmd {
	if !m.FollowActive() {
		return nil
	}
	session := m.followSession
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		path, err := tmux.CurrentPath(ctx, session)
		if err != nil {
			return filesCwdMsg{Session: session}
		}
		return filesCwdMsg{Session: session, Path: path}
	}
}

// SetRoot re-points the tree at a directory and starts a fresh walk.
// Separate from SetProject because cwd tracking moves the root within
// a project without changing which project is selected.
//
// Unlike Notes there is no per-project entry cache: the whole point of
// the Files tree is to show what is on disk right now, and a session-
// long cache would hide every file the user's agent just wrote.
func (m *filesModel) SetRoot(dir string) tea.Cmd {
	if dir == "" {
		m.clearTree()
		return nil
	}
	if dir == m.root && (m.loading || len(m.entries) > 0) {
		return nil
	}
	m.root = dir
	m.entries = nil
	m.expanded = make(map[string]bool)
	m.cursor = 0
	m.focus = filesFocusTree
	m.loading = true
	m.loadErr = ""
	m.clearPreview()
	return tea.Batch(loadFilesCmd(dir), m.loadingSpinner.Tick)
}

// Root returns the directory currently being browsed.
func (m filesModel) Root() string { return m.root }

func (m *filesModel) clearTree() {
	m.root = ""
	m.entries = nil
	m.expanded = make(map[string]bool)
	m.cursor = 0
	m.loading = false
	m.loadErr = ""
	m.clearPreview()
}

func (m *filesModel) clearPreview() {
	m.previewRel = ""
	m.previewData = filebrowser.Preview{}
	m.previewErr = ""
	m.preview.SetContent("")
	m.preview.GotoTop()
}

// SetProjects pushes the discovered-projects list so the `p` picker can
// offer all of them.
func (m *filesModel) SetProjects(ps []project.Project) {
	m.projects = ps
	if m.projCursor >= len(ps) {
		m.projCursor = 0
	}
}

// SetSize records the terminal size and resizes the preview viewport.
func (m *filesModel) SetSize(w, h int) {
	if w == m.termWidth && h == m.termHeight {
		return
	}
	m.termWidth = w
	m.termHeight = h
	pw, ph := m.previewPaneSize()
	m.preview.Width = pw
	m.preview.Height = ph
	if m.previewRel != "" {
		m.preview.SetContent(m.renderPreviewBody(pw))
	}
}

// treeWidth returns the tree pane's column count for a given total
// width, derived from splitRatio. Pure so the drag-resize arithmetic
// in Phase 4 is testable without a terminal.
func treeWidth(total int, ratio float64) int {
	if total <= 0 {
		return 0
	}
	ratio = clampRatio(ratio)
	w := int(float64(total) * ratio)
	if w < 1 {
		w = 1
	}
	if w > total-1 {
		w = total - 1
	}
	return w
}

// clampRatio keeps a split ratio inside the usable band.
func clampRatio(r float64) float64 {
	if r < minSplitRatio {
		return minSplitRatio
	}
	if r > maxSplitRatio {
		return maxSplitRatio
	}
	return r
}

// previewPaneSize returns (viewportWidth, viewportHeight) for the
// preview column at the cached terminal size. Mirrors the arithmetic
// in View/renderPreview: the viewport interior is the column minus
// border and padding, and reserves inner lines for the header, the
// rule, and the scroll hint.
func (m filesModel) previewPaneSize() (int, int) {
	tw, th := m.termWidth, m.termHeight
	if tw < 20 {
		tw = 20
	}
	if th < 10 {
		th = 10
	}
	leftW := treeWidth(tw, m.splitRatio)
	pw := tw - leftW - 1 - 4
	if pw < 10 {
		pw = 10
	}
	ph := th - 6
	if ph < 3 {
		ph = 3
	}
	return pw, ph
}

// loadFilesCmd walks the tree off the UI goroutine.
func loadFilesCmd(root string) tea.Cmd {
	return func() tea.Msg {
		entries, err := filebrowser.Open(root).List()
		if err != nil {
			return filesEntriesLoadedMsg{Root: root, Err: err.Error()}
		}
		return filesEntriesLoadedMsg{Root: root, Entries: entries}
	}
}

// loadFilePreviewCmd reads and classifies one file off the UI
// goroutine. Reading on the UI goroutine would stall the render loop
// on every cursor move over a slow or network-mounted disk.
func loadFilePreviewCmd(root, rel string) tea.Cmd {
	return func() tea.Msg {
		p, err := filebrowser.Open(root).Preview(rel)
		if err != nil {
			return filesPreviewLoadedMsg{Root: root, Rel: rel, Err: err.Error()}
		}
		return filesPreviewLoadedMsg{Root: root, Rel: rel, Preview: p}
	}
}

// refreshPreview kicks off a load for the file under the cursor, or
// clears the pane when the cursor sits on a folder header.
func (m *filesModel) refreshPreview() tea.Cmd {
	e := m.selectedEntry()
	if e == nil {
		m.clearPreview()
		return nil
	}
	if e.Rel == m.previewRel {
		return nil // selection didn't move
	}
	m.previewRel = e.Rel
	m.previewData = filebrowser.Preview{}
	m.previewErr = ""
	m.preview.SetContent(m.st.Muted.Render("Loading…"))
	m.preview.GotoTop()
	return loadFilePreviewCmd(m.root, e.Rel)
}

// visibleRows is the flattened, currently-visible tree.
func (m filesModel) visibleRows() []filebrowser.Row {
	return filebrowser.VisibleRows(m.entries, m.expanded)
}

func (m filesModel) listLen() int { return len(m.visibleRows()) }

// selectedRow returns the tree row under the cursor.
func (m filesModel) selectedRow() (filebrowser.Row, bool) {
	vis := m.visibleRows()
	if m.cursor < 0 || m.cursor >= len(vis) {
		return filebrowser.Row{}, false
	}
	return vis[m.cursor], true
}

// selectedEntry returns the file under the cursor, or nil on a folder
// header or an out-of-bounds cursor.
func (m filesModel) selectedEntry() *filebrowser.Entry {
	r, ok := m.selectedRow()
	if !ok || r.Kind != filebrowser.RowFile || r.EntryIdx < 0 || r.EntryIdx >= len(m.entries) {
		return nil
	}
	e := m.entries[r.EntryIdx]
	return &e
}

// clampCursor keeps the cursor inside the visible-row bounds after any
// change that can shrink the list.
func (m *filesModel) clampCursor() {
	n := m.listLen()
	if n == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// handleRight expands the folder under the cursor, or drills into an
// already-expanded one. On a file row it does nothing.
//
// That last part is deliberate and is where this diverges from
// notes.go, which moves focus to the preview instead. In Notes most
// rows sit inside folders, so the behaviour rarely fires. In a source
// repo the first dozen rows are root-level files (.gitignore, go.mod,
// README.md), so → — the first key anyone presses on a tree — silently
// handed the keyboard to the preview pane. From there ↑↓, →, and enter
// all went to the viewport and the tree looked broken: no folder would
// open, no file would launch $EDITOR, and nothing said why. Only tab
// or ← got you back, and nothing advertised either.
//
// The help bar already promises "→/← expand/collapse". This makes the
// code keep that promise. Focus moves on tab or a click, which are the
// two affordances that say so.
func (m *filesModel) handleRight() tea.Cmd {
	r, ok := m.selectedRow()
	if !ok || r.Kind != filebrowser.RowFolder {
		return nil
	}
	if !m.expanded[r.Dir] {
		m.expanded[r.Dir] = true
		return nil
	}
	// Already open — step onto its first child, which VisibleRows
	// emits immediately after the header.
	if m.cursor+1 < m.listLen() {
		m.cursor++
		return m.refreshPreview()
	}
	return nil
}

// handleLeft collapses the expanded folder under the cursor, or jumps
// out to the enclosing parent's header.
func (m *filesModel) handleLeft() tea.Cmd {
	r, ok := m.selectedRow()
	if !ok {
		return nil
	}
	if r.Kind == filebrowser.RowFolder && m.expanded[r.Dir] {
		m.collapseFolder(r.Dir)
		return m.refreshPreview()
	}
	target := r.Dir
	if r.Kind == filebrowser.RowFolder {
		target = filebrowser.ParentDir(r.Dir)
	}
	if target == "" {
		return nil
	}
	for i, row := range m.visibleRows() {
		if row.Kind == filebrowser.RowFolder && row.Dir == target {
			m.cursor = i
			return m.refreshPreview()
		}
	}
	return nil
}

// collapseFolder hides a folder's children, moving the cursor up to
// the folder header first when it was sitting on a row about to
// disappear.
func (m *filesModel) collapseFolder(dir string) {
	r, ok := m.selectedRow()
	moveToHeader := ok && filebrowser.DescendantOf(r, dir)
	m.expanded[dir] = false
	if moveToHeader {
		for i, row := range m.visibleRows() {
			if row.Kind == filebrowser.RowFolder && row.Dir == dir {
				m.cursor = i
				return
			}
		}
	}
	m.clampCursor()
}

func (m filesModel) Update(msg tea.Msg) (filesModel, tea.Cmd) {
	// Project picker modal owns input while open.
	if m.pickingProject {
		var cmd tea.Cmd
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc":
				m.pickingProject = false
			case "up", "k":
				if n := len(m.projects); n > 0 {
					if m.projCursor <= 0 {
						m.projCursor = n - 1
					} else {
						m.projCursor--
					}
				}
			case "down", "j":
				if n := len(m.projects); n > 0 {
					if m.projCursor >= n-1 {
						m.projCursor = 0
					} else {
						m.projCursor++
					}
				}
			case "enter":
				if m.projCursor >= 0 && m.projCursor < len(m.projects) {
					p := m.projects[m.projCursor]
					// An explicit pick wins over cwd tracking: the user
					// named the tree they want, and re-rooting it on the
					// next poll would undo the thing they just did.
					m.followCwd = false
					cmd = m.SetProject(&p)
				}
				m.pickingProject = false
			}
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		return m.updateMouse(msg)

	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.loadingSpinner, cmd = m.loadingSpinner.Update(msg)
		return m, cmd

	case filesReloadMsg:
		if m.root == "" {
			return m, nil
		}
		m.loading = true
		return m, tea.Batch(loadFilesCmd(m.root), m.loadingSpinner.Tick)

	case filesCwdMsg:
		// Drop a reply about a session we're no longer following, or
		// one that arrived after the user turned tracking off.
		if !m.FollowActive() || msg.Session != m.followSession {
			return m, nil
		}
		// An empty path means the pane's directory is gone (deleted out
		// from under it) or the session exited between the poll and the
		// reply. Keep the tree where it is rather than blanking it.
		if msg.Path == "" || msg.Path == m.root {
			return m, nil
		}
		return m, m.SetRoot(msg.Path)

	case filesEntriesLoadedMsg:
		// Discard a walk the user has already navigated away from.
		if msg.Root != m.root {
			return m, nil
		}
		m.loading = false
		if msg.Err != "" {
			m.loadErr = msg.Err
			m.entries = nil
			m.cursor = 0
			return m, nil
		}
		m.loadErr = ""
		m.entries = msg.Entries
		m.expanded = filebrowser.DefaultFolds(msg.Entries, false)
		m.clampCursor()
		return m, m.refreshPreview()

	case filesPreviewLoadedMsg:
		// Drop a stale read: the cursor may have moved on, or the root
		// changed, since this was dispatched.
		if msg.Root != m.root || msg.Rel != m.previewRel {
			return m, nil
		}
		if msg.Err != "" {
			m.previewErr = msg.Err
			m.preview.SetContent(m.st.StatusError.Render(msg.Err))
			m.preview.GotoTop()
			return m, nil
		}
		m.previewData = msg.Preview
		pw, _ := m.previewPaneSize()
		m.preview.SetContent(m.renderPreviewBody(pw))
		m.preview.GotoTop()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "f":
			m.followCwd = !m.followCwd
			if !m.followCwd {
				return m, func() tea.Msg {
					return toastMsg{
						Text:  "cwd tracking off — tree stays put",
						Kind:  toastInfo,
						Until: time.Now().Add(3 * time.Second),
					}
				}
			}
			// Turning it back on should catch up immediately rather
			// than waiting out the poll interval.
			return m, m.PollCwdCmd()
		case "p", " ":
			if len(m.projects) > 0 {
				m.pickingProject = true
				m.projCursor = 0
				if m.project != nil {
					for i, p := range m.projects {
						if p.Path == m.project.Path {
							m.projCursor = i
							break
						}
					}
				}
			}
			return m, nil
		case "tab":
			if m.focus == filesFocusTree {
				m.focus = filesFocusPreview
			} else {
				m.focus = filesFocusTree
			}
			return m, nil
		case "right", "l":
			if m.focus == filesFocusTree {
				return m, m.handleRight()
			}
		case "left", "h":
			if m.focus == filesFocusPreview {
				m.focus = filesFocusTree
				return m, nil
			}
			return m, m.handleLeft()
		}

		if m.focus == filesFocusTree {
			n := m.listLen()
			switch {
			case keyMatches(msg, m.km.Up):
				if n > 0 {
					if m.cursor <= 0 {
						m.cursor = n - 1
					} else {
						m.cursor--
					}
					return m, m.refreshPreview()
				}
				return m, nil
			case keyMatches(msg, m.km.Down):
				if n > 0 {
					if m.cursor >= n-1 {
						m.cursor = 0
					} else {
						m.cursor++
					}
					return m, m.refreshPreview()
				}
				return m, nil
			case keyMatches(msg, m.km.EditInEd), keyMatches(msg, m.km.Enter):
				// enter and e both open the selection in $EDITOR. A
				// folder row toggles instead — there is nothing to
				// edit, and Enter meaning "open" should still do
				// something sensible there.
				r, ok := m.selectedRow()
				if ok && r.Kind == filebrowser.RowFolder {
					return m, m.handleRight()
				}
				if e := m.selectedEntry(); e != nil {
					if m.previewData.Binary {
						return m, func() tea.Msg {
							return toastMsg{
								Text:  "binary file — not opening in $EDITOR",
								Kind:  toastInfo,
								Until: time.Now().Add(3 * time.Second),
							}
						}
					}
					return m, openEditorCmd(m.editor, e.Path, filesReloadMsg{})
				}
				return m, nil
			}
			return m, nil
		}

		// Preview focused → the viewport handles scroll keys.
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd
	}
	return m, nil
}

// updateMouse routes a mouse event by the column it landed in. Wheel
// events over the tree move the cursor; over the preview they scroll
// the viewport. Press, drag, and release go to files_mouse.go, which
// owns click-to-focus and drag-to-resize.
func (m filesModel) updateMouse(msg tea.MouseMsg) (filesModel, tea.Cmd) {
	if !isWheelMsg(msg) {
		return m.updateMousePointer(msg)
	}
	if msg.X < treeWidth(m.termWidth, m.splitRatio) {
		n := m.listLen()
		if n == 0 {
			return m, nil
		}
		var cmd tea.Cmd
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
				cmd = m.refreshPreview()
			}
		case tea.MouseButtonWheelDown:
			if m.cursor < n-1 {
				m.cursor++
				cmd = m.refreshPreview()
			}
		}
		return m, cmd
	}
	var cmd tea.Cmd
	m.preview, cmd = m.preview.Update(msg)
	return m, cmd
}

// renderPreviewBody turns the loaded Preview into display text at
// `wrap` columns. Markdown goes through Glamour — the same renderer
// the Notes tab uses, so a .md file looks identical in both places —
// and everything else falls to filebrowser's own chroma/plain/binary
// fork.
func (m filesModel) renderPreviewBody(wrap int) string {
	p := m.previewData
	if m.previewErr != "" {
		return m.st.StatusError.Render(m.previewErr)
	}
	if p.Binary || p.Content == "" {
		// The placeholder copy lives in the package so the CLI and the
		// TUI describe an unreadable file the same way.
		return m.st.Muted.Render(p.Render())
	}
	if filebrowser.IsMarkdown(p.Rel) {
		if wrap < 10 {
			wrap = 10
		}
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(styles.GlamourStyle(m.st)),
			glamour.WithWordWrap(wrap),
		)
		if err != nil {
			return p.Content
		}
		out, rerr := r.Render(p.Content)
		if rerr != nil {
			return p.Content
		}
		return out
	}
	return p.Render()
}

// HelpBarProps returns the Files screen's key hints.
func (m filesModel) HelpBarProps(width int) components.HelpBarProps {
	return components.HelpBarProps{
		Hints: []components.KeyHint{
			{Key: "?", Label: "help", Priority: 10},
			{Key: "q", Label: "quit", Priority: 10},
			{Key: "enter", Label: "open", Priority: 8},
			{Key: "→/←", Label: "expand/collapse", Priority: 8},
			{Key: "e", Label: "edit", Priority: 7},
			{Key: "p / space", Label: "switch project", Priority: 5},
			{Key: "f", Label: "follow cwd", Priority: 5},
			{Key: "tab", Label: "focus preview", Priority: 4},
			{Key: "drag", Label: "resize split", Priority: 3},
			{Key: screenKeyRange(), Label: "screens", Priority: 2},
		},
		Width: width,
	}
}

func (m filesModel) View(width, height int) string {
	if m.root == "" {
		bodyLines := []string{
			m.st.Emphasis.Render("Files"),
			"",
			m.st.Muted.Render("No project selected."),
			"",
			"Press " + m.st.Key.Render("p") + " here to pick one, or " +
				m.st.Key.Render(screenKey(ScreenProjects)) + " to go to the Projects tab.",
			"",
			m.st.Muted.Render("Files browses every file in a project. For markdown only, use the Notes tab."),
		}
		return m.st.Pane.Width(width - 2).Height(height - 2).Render(strings.Join(bodyLines, "\n"))
	}
	if m.pickingProject {
		return m.renderProjectPicker(width, height)
	}
	if width < filesNarrowWidth {
		return m.renderTree(width, height)
	}
	leftW := treeWidth(width, m.splitRatio)
	rightW := width - leftW - 1
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderTree(leftW, height),
		" ",
		m.renderPreview(rightW, height),
	)
}

// renderTree draws the left column: the folded file tree.
func (m filesModel) renderTree(width, height int) string {
	focusMark := ""
	if m.focus == filesFocusTree {
		focusMark = m.st.Emphasis.Render(" ◀")
	}
	title := "Files"
	if m.project != nil {
		title = m.project.Name
	}
	// The path line doubles as the tracking indicator: a leading ⇢
	// when the tree is following a session's cwd, so "why did my tree
	// just move" has a visible answer.
	pathLine := compactPath(m.root, width-4)
	if m.FollowActive() {
		pathLine = "⇢ " + compactPath(m.root, width-6)
	}
	lines := []string{
		m.st.Emphasis.Render(title) + focusMark,
		m.st.Muted.Render(pathLine),
	}
	if m.loadErr != "" {
		lines = append(lines, m.st.StatusError.Render("⚠ "+m.loadErr))
	}
	lines = append(lines, "")

	rows, cursorRow := m.treeRows(width)
	if len(rows) == 0 {
		var empty string
		if m.loading {
			empty = m.loadingSpinner.View() + m.st.Muted.Render(" scanning project…")
		} else {
			empty = m.st.Muted.Render("(no files)")
		}
		lines = append(lines, empty)
		return m.treePaneStyle().Width(width - 2).Height(height - 2).Render(strings.Join(lines, "\n"))
	}

	budget := height - 2 - len(lines) - 1
	if budget < 1 {
		budget = 1
	}
	visible, above, below := windowLines(rows, cursorRow, budget)
	lines = append(lines, visible...)
	if above > 0 || below > 0 {
		lines = append(lines, m.st.Muted.Render(scrollHintText(above, below)))
	}
	return m.treePaneStyle().Width(width - 2).Height(height - 2).Render(strings.Join(lines, "\n"))
}

func (m filesModel) treePaneStyle() lipgloss.Style {
	if m.focus == filesFocusTree {
		return m.st.PaneFocused
	}
	return m.st.Pane
}

// treeRows renders the visible tree rows and reports which one holds
// the cursor, so renderTree can window around it. Indentation uses the
// tight SM spacing step — the left pane is narrow and depth can run
// several levels in a real repo.
func (m filesModel) treeRows(width int) (rows []string, cursorRow int) {
	cursorRow = -1
	rowW := width - 4
	step := m.st.Spacing.SM
	for i, r := range m.visibleRows() {
		sel := i == m.cursor
		if sel {
			cursorRow = len(rows)
		}
		indent := strings.Repeat(" ", r.Depth*step)
		bodyW := rowW - r.Depth*step
		if bodyW < 1 {
			bodyW = 1
		}
		var content string
		if r.Kind == filebrowser.RowFolder {
			glyph := "▸ "
			if m.expanded[r.Dir] {
				glyph = "▾ "
			}
			content = m.st.Subtitle.Render(glyph + folderHeader(r.Dir))
		} else {
			content = m.entries[r.EntryIdx].Name
		}
		rows = append(rows, indent+components.RenderListRow(m.st, content, sel, bodyW))
	}
	return rows, cursorRow
}

func (m filesModel) renderPreview(width, height int) string {
	m.preview.Width = width - 4
	m.preview.Height = height - 6
	if m.preview.Height < 3 {
		m.preview.Height = 3
	}
	e := m.selectedEntry()
	if e == nil {
		return m.st.Pane.Width(width - 2).Height(height - 2).
			Render(m.st.Muted.Render("No file selected."))
	}

	focusMark := ""
	if m.focus == filesFocusPreview {
		focusMark = " " + m.st.Emphasis.Render("◀ scrolling")
	}
	header := m.st.Muted.Render(e.Rel) + focusMark
	separator := m.st.Muted.Render(strings.Repeat("─", m.preview.Width))
	pct := int(m.preview.ScrollPercent() * 100)
	footer := m.st.Muted.Render(fmt.Sprintf(
		"%s   tab: focus tree   j/k: scroll   %d%%",
		m.previewBadge(e), pct,
	))
	body := lipgloss.JoinVertical(lipgloss.Left,
		header, separator, m.preview.View(), "", footer)

	paneStyle := m.st.Pane
	if m.focus == filesFocusPreview {
		paneStyle = m.st.PaneFocused
	}
	return paneStyle.Width(width - 2).Height(height - 2).Render(body)
}

// previewBadge summarizes what the pane is actually showing: the
// detected language, plus honest notes when the content was truncated
// or is being shown without highlighting.
func (m filesModel) previewBadge(e *filebrowser.Entry) string {
	p := m.previewData
	switch {
	case m.previewErr != "":
		return "error"
	case p.Binary:
		return "binary · " + humanBytes(e.Size)
	case p.Rel == "":
		return "loading…"
	}
	parts := []string{humanBytes(e.Size)}
	if p.Lang != "" {
		parts = append([]string{p.Lang}, parts...)
	} else {
		parts = append([]string{"plain text"}, parts...)
	}
	if p.Truncated {
		parts = append(parts, "truncated")
	} else if p.Lang != "" && !p.Highlightable {
		parts = append(parts, "too large to highlight")
	}
	return strings.Join(parts, " · ")
}

// renderProjectPicker is the project-switcher modal, mirroring Notes'
// so the two tabs behave identically.
func (m filesModel) renderProjectPicker(width, height int) string {
	lines := []string{
		m.st.Emphasis.Render("Switch project"),
		m.st.Subtitle.Render("The file tree follows your selection."),
		"",
	}
	maxVisible := height - 8
	if maxVisible < 5 {
		maxVisible = 5
	}
	start := 0
	if m.projCursor > maxVisible-3 {
		start = m.projCursor - (maxVisible - 3)
	}
	end := start + maxVisible
	if end > len(m.projects) {
		end = len(m.projects)
	}
	for i := start; i < end; i++ {
		lines = append(lines, components.RenderListRow(
			m.st, m.projects[i].Name, i == m.projCursor, minInt(70, width-4)-2))
	}
	if end < len(m.projects) {
		lines = append(lines, m.st.Muted.Render(
			fmt.Sprintf("  … %d more (scroll with j/k)", len(m.projects)-end)))
	}
	lines = append(lines, "", m.st.Muted.Render("↑↓ or j/k: navigate   enter: open   esc: cancel"))
	modal := m.st.PaneFocused.Width(minInt(70, width-4)).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

// humanBytes formats a file size for the preview badge.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// compactPath shortens a directory path from the left so the tail —
// the part that says where you actually are — survives a narrow pane.
func compactPath(path string, width int) string {
	if width < 4 {
		return ""
	}
	runes := []rune(path)
	if len(runes) <= width {
		return path
	}
	return "…" + string(runes[len(runes)-(width-1):])
}
