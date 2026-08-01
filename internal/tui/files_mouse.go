package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Pointer (non-wheel) mouse handling for the Files screen: click to
// focus a pane or select a row, and drag the border between the panes
// to change the split.
//
// Phase 3 landed the wheel routing in files.go's updateMouse; this
// file is the click/drag half, kept separate because the coordinate
// arithmetic is the part worth reading on its own.

// splitGrabSlack widens the draggable border beyond its single
// rendered column. A one-cell target is unhittable in practice — the
// user aims at a line, not at a specific character cell — so a click
// within one column either side counts as grabbing it.
const splitGrabSlack = 1

// treeHeaderLines is the number of rows renderTree draws above the
// first file row: the title, the path line, and one blank spacer. A
// click below them maps onto a tree row; a click on them just focuses
// the pane. Kept next to renderTree's prologue — the two must agree.
const treeHeaderLines = 3

// updateMousePointer handles press, release, and drag. Focus follows
// the click, and a press that lands on the split border starts a drag
// that continues until release.
func (m filesModel) updateMousePointer(msg tea.MouseMsg) (filesModel, tea.Cmd) {
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	// Narrow layout has no preview pane and therefore no border to
	// drag and no second pane to focus.
	if m.termWidth < filesNarrowWidth {
		if msg.Action == tea.MouseActionPress {
			return m.clickTree(msg.Y)
		}
		return m, nil
	}

	border := treeWidth(m.termWidth, m.splitRatio)

	switch msg.Action {
	case tea.MouseActionPress:
		if onSplitBorder(msg.X, border) {
			m.draggingSplit = true
			return m, nil
		}
		if msg.X < border {
			m.focus = filesFocusTree
			return m.clickTree(msg.Y)
		}
		m.focus = filesFocusPreview
		return m, nil

	case tea.MouseActionMotion:
		if !m.draggingSplit {
			return m, nil
		}
		m.splitRatio = ratioForX(msg.X, m.termWidth)
		m.applySplitResize()
		return m, nil

	case tea.MouseActionRelease:
		if !m.draggingSplit {
			return m, nil
		}
		m.draggingSplit = false
		m.splitRatio = ratioForX(msg.X, m.termWidth)
		m.applySplitResize()
		return m, nil
	}
	return m, nil
}

// onSplitBorder reports whether an X coordinate is close enough to the
// border column to count as grabbing it.
func onSplitBorder(x, border int) bool {
	return x >= border-splitGrabSlack && x <= border+splitGrabSlack
}

// ratioForX converts a mouse X position into a split ratio, clamped to
// the usable band. Pure, so the drag arithmetic is testable without a
// terminal — which is the whole point of task 4.3.
func ratioForX(x, total int) float64 {
	if total <= 0 {
		return defaultSplitRatio
	}
	return clampRatio(float64(x) / float64(total))
}

// applySplitResize re-lays the preview viewport after the split moved
// and re-renders its content at the new width. Without the re-render
// the viewport keeps content wrapped for the old column and the text
// either overflows or leaves a gap.
func (m *filesModel) applySplitResize() {
	pw, ph := m.previewPaneSize()
	m.preview.Width = pw
	m.preview.Height = ph
	if m.previewRel != "" {
		m.preview.SetContent(m.renderPreviewBody(pw))
	}
}

// clickTree moves the cursor to the row under a click at terminal row
// y, when one is there. Rows outside the list (the header block, the
// scroll hint, empty space below the last file) leave the cursor
// alone — the click still focused the pane, which is what the user
// asked for.
func (m filesModel) clickTree(y int) (filesModel, tea.Cmd) {
	idx, ok := m.treeRowAt(y)
	if !ok || idx == m.cursor {
		return m, nil
	}
	m.cursor = idx
	return m, m.refreshPreview()
}

// treeRowAt maps a terminal row to an index into the visible tree
// rows, accounting for the pane border, the header block, and the
// window offset applied when the list is longer than the pane.
func (m filesModel) treeRowAt(y int) (int, bool) {
	// One row for the pane's top border, then the header block.
	first := 1 + treeHeaderLines
	if m.loadErr != "" {
		first++ // the error line pushes the list down one
	}
	if y < first {
		return 0, false
	}
	offset := y - first

	n := m.listLen()
	if n == 0 {
		return 0, false
	}
	// The list is windowed around the cursor when it doesn't fit, so
	// the row at screen offset 0 is not necessarily row 0.
	idx := m.treeWindowStart() + offset
	if idx >= n {
		return 0, false
	}
	return idx, true
}

// treeWindowStart returns the index of the first visible tree row,
// mirroring the windowLines call in renderTree. Both derive from the
// same budget arithmetic; if renderTree's prologue changes, this must
// change with it (treeHeaderLines is the shared knob).
func (m filesModel) treeWindowStart() int {
	n := m.listLen()
	header := treeHeaderLines
	if m.loadErr != "" {
		header++
	}
	// renderTree: budget = height - 2 - len(lines) - 1, where lines is
	// the header block at the time of the call.
	budget := m.termHeight - 2 - header - 1
	if budget < 1 {
		budget = 1
	}
	if n <= budget {
		return 0
	}
	start := m.cursor - budget/2
	if start < 0 {
		start = 0
	}
	if start+budget > n {
		start = n - budget
	}
	return start
}
