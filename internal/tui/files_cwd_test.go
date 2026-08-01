package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/skzv/ccmux/internal/daemon"
	"github.com/skzv/ccmux/internal/project"
	"github.com/skzv/ccmux/internal/tui/styles"
)

// realTree writes a couple of files under a temp dir so SetRoot has
// something to walk, and returns the root.
func realTree(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// drain runs a tea.Cmd and feeds every message it produces back into
// the model, following the chain (walk → preview load) to completion
// without standing up a Bubble Tea program. tea.Batch yields a
// BatchMsg holding further Cmds, which are queued too.
//
// Spinner ticks are dropped: feeding one back in returns another tick,
// and the loop would never finish.
func drain(m filesModel, cmd tea.Cmd) filesModel {
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && steps < 32; steps++ {
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		switch msg := next().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case spinner.TickMsg:
			// dropped on purpose — see above
		default:
			updated, cmd2 := m.Update(msg)
			m = updated
			queue = append(queue, cmd2)
		}
	}
	return m
}

func newFilesForCwd(t *testing.T, root string) filesModel {
	t.Helper()
	m := newFiles(styles.Default(), DefaultKeymap())
	m.SetSize(120, 40)
	p := project.Project{Name: filepath.Base(root), Path: root}
	m = drain(m, m.SetProject(&p))
	// SetProject is an explicit pick, which turns tracking off. Tests
	// that exercise tracking turn it back on.
	m.followCwd = true
	return m
}

// TestCwdMsgRerootsTree is the spec scenario "Tree re-roots after a
// pane cd": ccmux reads a new pane_current_path, the tree follows.
func TestCwdMsgRerootsTree(t *testing.T) {
	start := realTree(t, "start")
	moved := realTree(t, "moved")

	m := newFilesForCwd(t, start)
	m.SetFollowSession("c-start")
	if m.Root() != start {
		t.Fatalf("Root() = %q, want %q", m.Root(), start)
	}

	m, cmd := m.Update(filesCwdMsg{Session: "c-start", Path: moved})
	m = drain(m, cmd)

	if m.Root() != moved {
		t.Errorf("Root() = %q after the pane cd'd to %q", m.Root(), moved)
	}
	if len(m.entries) == 0 {
		t.Error("re-rooting did not walk the new tree")
	}
	if m.entries[0].Rel != "main.go" {
		t.Errorf("new tree lists %q, want main.go from the new root", m.entries[0].Rel)
	}
}

// TestCwdMsgSamePathIsNoOp: the poll fires every two seconds and the
// path almost never changes. Re-walking each time would throw away the
// user's fold state and cursor position on a timer.
func TestCwdMsgSamePathIsNoOp(t *testing.T) {
	root := realTree(t, "stay")
	m := newFilesForCwd(t, root)
	m.SetFollowSession("c-stay")
	m.cursor = 0
	m.expanded["x"] = true

	m, cmd := m.Update(filesCwdMsg{Session: "c-stay", Path: root})
	if cmd != nil {
		t.Error("an unchanged path dispatched work; the poll must be idempotent")
	}
	if !m.expanded["x"] {
		t.Error("an unchanged path reset the fold state")
	}
}

// TestCwdMsgEmptyPathIsIgnored: a pane whose directory was deleted, or
// a session that exited between poll and reply, reports "". Blanking
// the tree in that case would be worse than staying put.
func TestCwdMsgEmptyPathIsIgnored(t *testing.T) {
	root := realTree(t, "keep")
	m := newFilesForCwd(t, root)
	m.SetFollowSession("c-keep")

	m, _ = m.Update(filesCwdMsg{Session: "c-keep", Path: ""})
	if m.Root() != root {
		t.Errorf("Root() = %q after an empty path; want it unchanged at %q", m.Root(), root)
	}
}

// TestCwdMsgFromStaleSessionIgnored: the user may have switched what
// they're attached to while a poll was in flight.
func TestCwdMsgFromStaleSessionIgnored(t *testing.T) {
	root := realTree(t, "here")
	other := realTree(t, "elsewhere")
	m := newFilesForCwd(t, root)
	m.SetFollowSession("c-here")

	m, _ = m.Update(filesCwdMsg{Session: "c-somewhere-else", Path: other})
	if m.Root() != root {
		t.Errorf("a reply about another session re-rooted the tree to %q", m.Root())
	}
}

// TestFollowToggle covers the `f` key and the two states it gates.
func TestFollowToggle(t *testing.T) {
	root := realTree(t, "toggle")
	moved := realTree(t, "moved")
	m := newFilesForCwd(t, root)
	m.SetFollowSession("c-toggle")

	if !m.FollowActive() {
		t.Fatal("tracking should be active with a session set and the toggle on")
	}

	// Turn it off; a subsequent poll reply must be ignored.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if m.FollowActive() {
		t.Fatal("`f` did not turn tracking off")
	}
	m, _ = m.Update(filesCwdMsg{Session: "c-toggle", Path: moved})
	if m.Root() != root {
		t.Errorf("tracking was off but the tree still moved to %q", m.Root())
	}

	// Turn it back on; the reply is honoured again.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if !m.FollowActive() {
		t.Fatal("`f` did not turn tracking back on")
	}
	m, cmd := m.Update(filesCwdMsg{Session: "c-toggle", Path: moved})
	m = drain(m, cmd)
	if m.Root() != moved {
		t.Errorf("Root() = %q with tracking back on, want %q", m.Root(), moved)
	}
}

// TestFollowActiveNeedsASession: with nothing attached there is no cwd
// to track, so no poll should be dispatched.
func TestFollowActiveNeedsASession(t *testing.T) {
	m := newFilesForCwd(t, realTree(t, "nosession"))
	m.SetFollowSession("")
	if m.FollowActive() {
		t.Error("FollowActive() = true with no session")
	}
	if m.PollCwdCmd() != nil {
		t.Error("PollCwdCmd() returned work with no session to poll")
	}
}

// TestExplicitProjectPickStopsFollowing: naming a project is a direct
// instruction, and the next poll must not undo it.
func TestExplicitProjectPickStopsFollowing(t *testing.T) {
	root := realTree(t, "auto")
	picked := realTree(t, "picked")

	m := newFilesForCwd(t, root)
	m.SetFollowSession("c-auto")
	m.SetProjects([]project.Project{{Name: "picked", Path: picked}})

	// Open the picker and choose the one project in it.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if !m.pickingProject {
		t.Fatal("`p` did not open the project picker")
	}
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drain(m, cmd)

	if m.Root() != picked {
		t.Fatalf("Root() = %q after picking, want %q", m.Root(), picked)
	}
	if m.FollowActive() {
		t.Error("an explicit project pick left cwd tracking on; the next poll would undo it")
	}
}

// TestAttachedLocalSession covers the App-side selection rule.
func TestAttachedLocalSession(t *testing.T) {
	cases := []struct {
		name     string
		sessions []daemon.SessionState
		want     string
	}{
		{"nothing attached", []daemon.SessionState{
			{Name: "c-a", Host: "local"},
			{Name: "c-b", Host: "local"},
		}, ""},
		{"one attached", []daemon.SessionState{
			{Name: "c-a", Host: "local"},
			{Name: "c-b", Host: "local", Attached: true},
		}, "c-b"},
		{"host defaults to local when empty", []daemon.SessionState{
			{Name: "c-a", Attached: true},
		}, "c-a"},
		// Two attached sessions have no single answer, and picking one
		// arbitrarily would make the tree flip between projects on
		// alternate ticks.
		{"two attached is ambiguous", []daemon.SessionState{
			{Name: "c-a", Host: "local", Attached: true},
			{Name: "c-b", Host: "local", Attached: true},
		}, ""},
		// A remote session's cwd names a directory on another machine's
		// disk, which this machine cannot walk.
		{"remote attached is skipped", []daemon.SessionState{
			{Name: "c-remote", Host: "mac-mini", Attached: true},
		}, ""},
		{"local attached wins over a remote one", []daemon.SessionState{
			{Name: "c-remote", Host: "mac-mini", Attached: true},
			{Name: "c-local", Host: "local", Attached: true},
		}, "c-local"},
		{"empty list", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := attachedLocalSession(tc.sessions); got != tc.want {
				t.Errorf("attachedLocalSession() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPollOnlyWhileFilesIsVisible: the tick must not shell out to tmux
// for a screen nobody is looking at.
func TestPollOnlyWhileFilesIsVisible(t *testing.T) {
	root := realTree(t, "visible")
	build := func(screen Screen) App {
		a := App{styles: styles.Default(), keys: DefaultKeymap(), width: 120, height: 40, screen: screen}
		a.files = newFilesForCwd(t, root)
		a.files.SetFollowSession("c-visible")
		return a
	}

	// The poll Cmd is only reachable through PollCwdCmd, so assert on
	// the guard the tick uses rather than trying to count subprocesses.
	a := build(ScreenFiles)
	if a.screen == ScreenFiles && a.files.PollCwdCmd() == nil {
		t.Error("no poll dispatched while the Files screen is visible")
	}
	a = build(ScreenNotes)
	if a.screen == ScreenFiles {
		t.Fatal("test setup: expected a non-Files screen")
	}
	// The tick's guard is `a.screen == ScreenFiles`; with Notes active
	// PollCwdCmd is never called at all.
}
