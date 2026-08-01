package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/skzv/ccmux/internal/filebrowser"
	"github.com/skzv/ccmux/internal/tui/styles"
)

// press/motion/release build the three mouse events the drag state
// machine cares about.
func press(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}
func motion(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion}
}
func release(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}
}

// TestRatioForX is task 4.3: the split-ratio-from-mouse-X computation
// as a pure function, no terminal involved.
func TestRatioForX(t *testing.T) {
	const total = 120
	cases := []struct {
		name string
		x    int
		want float64
	}{
		{"one third across", 40, 40.0 / 120.0},
		{"halfway", 60, 0.5},
		{"at the max bound", 90, maxSplitRatio},
		{"past the right edge clamps to max", 200, maxSplitRatio},
		{"past the left edge clamps to min", 0, minSplitRatio},
		{"negative clamps to min", -30, minSplitRatio},
		{"just below the min bound clamps", 17, minSplitRatio},        // 17/120 = 0.142
		{"just above the min bound passes through", 19, 19.0 / 120.0}, // 0.158
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ratioForX(tc.x, total)
			if got != tc.want {
				t.Errorf("ratioForX(%d, %d) = %v, want %v", tc.x, total, got, tc.want)
			}
			if got < minSplitRatio || got > maxSplitRatio {
				t.Errorf("ratioForX escaped the [%v, %v] band: %v", minSplitRatio, maxSplitRatio, got)
			}
		})
	}
}

// TestRatioForX_ZeroWidth guards the division: a zero-width terminal
// (which does happen before the first WindowSizeMsg) must not produce
// NaN or a panic.
func TestRatioForX_ZeroWidth(t *testing.T) {
	if got := ratioForX(10, 0); got != defaultSplitRatio {
		t.Errorf("ratioForX(10, 0) = %v, want the default %v", got, defaultSplitRatio)
	}
}

func TestTreeWidth(t *testing.T) {
	cases := []struct {
		total int
		ratio float64
		want  int
	}{
		{120, 1.0 / 3.0, 40},
		{120, 0.5, 60},
		{120, 0.9, 90},  // clamped to maxSplitRatio (0.75)
		{120, 0.01, 18}, // clamped to minSplitRatio (0.15)
		{0, 0.5, 0},
		{1, 0.5, 0}, // total-1 leaves nothing for the tree
	}
	for _, tc := range cases {
		if got := treeWidth(tc.total, tc.ratio); got != tc.want {
			t.Errorf("treeWidth(%d, %v) = %d, want %d", tc.total, tc.ratio, got, tc.want)
		}
	}
}

func TestOnSplitBorder(t *testing.T) {
	const border = 40
	for _, x := range []int{39, 40, 41} {
		if !onSplitBorder(x, border) {
			t.Errorf("onSplitBorder(%d, %d) = false; the grab target must be wider than one cell", x, border)
		}
	}
	for _, x := range []int{0, 38, 42, 100} {
		if onSplitBorder(x, border) {
			t.Errorf("onSplitBorder(%d, %d) = true, want false", x, border)
		}
	}
}

// newFilesForMouse is a loaded Files screen at a known size.
func newFilesForMouse(t *testing.T, width, height int) filesModel {
	t.Helper()
	m := newFilesForGolden(styles.Default(), width, height, false)
	return m
}

// TestClickFocusesPane covers the spec scenario "Click focuses a
// pane": clicking in the preview column moves focus there, clicking
// back in the tree returns it.
func TestClickFocusesPane(t *testing.T) {
	m := newFilesForMouse(t, 120, 40)
	if m.focus != filesFocusTree {
		t.Fatalf("expected the tree to start focused, got %v", m.focus)
	}

	// A click well inside the preview column.
	m, _ = m.Update(press(100, 10))
	if m.focus != filesFocusPreview {
		t.Error("clicking the preview pane did not move focus there")
	}

	// And back into the tree column.
	m, _ = m.Update(press(10, 10))
	if m.focus != filesFocusTree {
		t.Error("clicking the tree pane did not move focus back")
	}
}

// TestClickSelectsRow pins the row-hit arithmetic against what
// renderTree actually draws: find the screen row holding a known
// filename, click it, and assert the cursor lands on that entry.
// Deriving the Y from the render is the point — a hand-written offset
// would silently rot the first time the pane's header block changes.
func TestClickSelectsRow(t *testing.T) {
	const width, height = 120, 40
	m := newFilesForMouse(t, width, height)

	// The collapsed tree shows: README.md, go.mod, main.go, then the
	// folder headers. Target the third file so an off-by-one shows up.
	const target = "main.go"
	body := m.View(width, height)
	wantY := -1
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(stripANSIForTabKey(line), target) {
			wantY = i
			break
		}
	}
	if wantY < 0 {
		t.Fatalf("%q is not on screen:\n%s", target, body)
	}

	m, _ = m.Update(press(10, wantY))
	e := m.selectedEntry()
	if e == nil {
		t.Fatalf("click at y=%d selected nothing (cursor=%d)", wantY, m.cursor)
	}
	if e.Rel != target {
		t.Errorf("click at y=%d selected %q, want %q", wantY, e.Rel, target)
	}
}

// TestClickAboveListDoesNotMoveCursor: the pane header and border are
// not rows. Clicking them focuses the pane but must leave the
// selection where it was.
func TestClickAboveListDoesNotMoveCursor(t *testing.T) {
	m := newFilesForMouse(t, 120, 40)
	m.cursor = 2
	before := m.cursor
	for _, y := range []int{0, 1, 2, 3} { // border + title + path + blank
		m, _ = m.Update(press(10, y))
		if m.cursor != before {
			t.Errorf("click at y=%d moved the cursor to %d; header rows are not list rows", y, m.cursor)
		}
	}
}

// TestDragResizesSplit covers the spec scenario "Dragging the border
// resizes the split".
func TestDragResizesSplit(t *testing.T) {
	const width, height = 120, 40
	m := newFilesForMouse(t, width, height)

	border := treeWidth(width, m.splitRatio)
	if border != 40 {
		t.Fatalf("test assumes a 40-column border at the default ratio, got %d", border)
	}

	// Grab the border and drag right.
	m, _ = m.Update(press(border, 10))
	if !m.draggingSplit {
		t.Fatal("pressing on the border did not start a drag")
	}
	m, _ = m.Update(motion(72, 10))
	if got := treeWidth(width, m.splitRatio); got != 72 {
		t.Errorf("after dragging to x=72 the tree is %d columns wide, want 72", got)
	}
	m, _ = m.Update(release(72, 10))
	if m.draggingSplit {
		t.Error("release did not end the drag")
	}

	// The rendered layout must actually follow the new ratio. Count in
	// runes, not bytes — the box-drawing glyphs are three bytes each.
	body := m.View(width, height)
	firstLine := []rune(strings.Split(stripANSIForTabKey(body), "\n")[0])
	previewStart := -1
	for i := 1; i < len(firstLine); i++ {
		if firstLine[i] == '╭' {
			previewStart = i
			break
		}
	}
	if previewStart != 73 {
		t.Errorf("the preview pane starts at column %d, want 73 (tree 72 + 1 gap):\n%s",
			previewStart, string(firstLine))
	}
}

// TestDragOnlyStartsFromTheBorder: a press inside a pane must not
// arm the resize, or every click in the tree would drag the layout.
func TestDragOnlyStartsFromTheBorder(t *testing.T) {
	m := newFilesForMouse(t, 120, 40)
	before := m.splitRatio

	m, _ = m.Update(press(10, 10)) // inside the tree
	if m.draggingSplit {
		t.Fatal("a press inside the tree armed the split drag")
	}
	m, _ = m.Update(motion(90, 10))
	if m.splitRatio != before {
		t.Errorf("motion without a border grab changed the ratio: %v → %v", before, m.splitRatio)
	}
}

// TestDragClampsAtBounds: dragging past either edge must leave a
// usable pane on both sides rather than collapsing one to nothing.
func TestDragClampsAtBounds(t *testing.T) {
	const width, height = 120, 40
	m := newFilesForMouse(t, width, height)
	border := treeWidth(width, m.splitRatio)

	m, _ = m.Update(press(border, 10))
	m, _ = m.Update(motion(width+50, 10))
	if m.splitRatio != maxSplitRatio {
		t.Errorf("dragging past the right edge = %v, want the max bound %v", m.splitRatio, maxSplitRatio)
	}
	m, _ = m.Update(motion(-10, 10))
	if m.splitRatio != minSplitRatio {
		t.Errorf("dragging past the left edge = %v, want the min bound %v", m.splitRatio, minSplitRatio)
	}
	m, _ = m.Update(release(-10, 10))

	// Both panes still have room to render.
	tw := treeWidth(width, m.splitRatio)
	if tw < 1 || tw > width-2 {
		t.Errorf("after clamping, the tree is %d of %d columns — one pane collapsed", tw, width)
	}
}

// TestDragResizeReflowsPreview: the viewport must be re-laid at the
// new width, or its content stays wrapped for the old column.
func TestDragResizeReflowsPreview(t *testing.T) {
	const width, height = 120, 40
	m := newFilesForMouse(t, width, height)
	m.previewRel = "README.md"
	m.previewData = filebrowser.Preview{Rel: "README.md", Content: "hello\n", Lang: "markdown"}

	widthBefore := m.preview.Width
	border := treeWidth(width, m.splitRatio)
	m, _ = m.Update(press(border, 10))
	m, _ = m.Update(motion(80, 10))

	if m.preview.Width == widthBefore {
		t.Errorf("the preview viewport is still %d columns after the split moved", widthBefore)
	}
	if wantW, _ := m.previewPaneSize(); m.preview.Width != wantW {
		t.Errorf("preview viewport width = %d, want %d (previewPaneSize at the new ratio)",
			m.preview.Width, wantW)
	}
}

// TestNarrowLayoutHasNoDraggableBorder: below filesNarrowWidth there
// is only one pane, so nothing should be grabbable.
func TestNarrowLayoutHasNoDraggableBorder(t *testing.T) {
	m := newFilesForMouse(t, 80, 30)
	before := m.splitRatio

	m, _ = m.Update(press(treeWidth(80, m.splitRatio), 5))
	if m.draggingSplit {
		t.Error("the narrow layout armed a split drag; there is no second pane")
	}
	m, _ = m.Update(motion(60, 5))
	if m.splitRatio != before {
		t.Error("the narrow layout let a drag change the split")
	}
}

// TestNonLeftButtonIgnored: right-click and middle-click must not
// move the selection or the split.
func TestNonLeftButtonIgnored(t *testing.T) {
	m := newFilesForMouse(t, 120, 40)
	m.cursor = 1
	before := m.cursor

	msg := tea.MouseMsg{X: 10, Y: 6, Button: tea.MouseButtonRight, Action: tea.MouseActionPress}
	m, _ = m.Update(msg)
	if m.cursor != before {
		t.Errorf("a right-click moved the cursor to %d", m.cursor)
	}
	if m.focus != filesFocusTree {
		t.Error("a right-click changed focus")
	}
}

// TestHeaderRowsMatchesRenderedHeader pins the constant the mouse
// translation depends on. renderHeader ends in forceSingleLine, so a
// change that makes the tab strip two rows tall would silently offset
// every click on the Files screen by one row.
func TestHeaderRowsMatchesRenderedHeader(t *testing.T) {
	for _, w := range []int{60, 100, 120, 200} {
		a := App{styles: styles.Default(), keys: DefaultKeymap(), width: w, screen: ScreenFiles}
		if got := lipgloss.Height(a.renderHeader()); got != headerRows {
			t.Errorf("width %d: header is %d rows, but headerRows says %d", w, got, headerRows)
		}
	}
}

// TestBodyMouseTranslatesY documents the one thing bodyMouse does.
func TestBodyMouseTranslatesY(t *testing.T) {
	got := bodyMouse(tea.MouseMsg{X: 5, Y: 9})
	if got.Y != 9-headerRows {
		t.Errorf("bodyMouse Y = %d, want %d", got.Y, 9-headerRows)
	}
	if got.X != 5 {
		t.Errorf("bodyMouse changed X to %d; only Y has chrome above it", got.X)
	}
}

// TestAppRoutesPointerEventsOnlyToFiles: the rest of the app stays
// pointer-free, so a stray click can't move a selection elsewhere.
func TestAppRoutesPointerEventsOnlyToFiles(t *testing.T) {
	base := func(screen Screen) App {
		a := App{styles: styles.Default(), keys: DefaultKeymap(), width: 120, height: 40, screen: screen}
		a.notes = newNotes(a.styles, a.keys)
		a.files = newFilesForGolden(a.styles, 120, 40, false)
		a.agentsM = newAgents(a.styles, a.keys)
		return a
	}

	// On Notes, a click must be absorbed — focus unchanged.
	a := base(ScreenNotes)
	updated, _ := a.Update(press(100, 10))
	if got := updated.(App); got.notes.focus != focusList {
		t.Error("a click changed focus on the Notes screen; pointer events should be absorbed there")
	}

	// On Files, the same click focuses the preview.
	a = base(ScreenFiles)
	updated, _ = a.Update(press(100, 10))
	if got := updated.(App); got.files.focus != filesFocusPreview {
		t.Error("a click in the preview column did not focus it on the Files screen")
	}
}
