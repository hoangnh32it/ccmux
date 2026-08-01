## Why

ccmux's Notes tab only browses `.md` files — by design, it's a notes
vault, not a general project browser. There is currently no way to see
the *whole* project tree, preview a non-markdown file (`.go`, `.rs`,
`.ts`, config files, …) with syntax highlighting, or have that view
follow the cwd of the agent pane you're attached to. A sibling project
(`ccmux-master`, a Rust TUI) has exactly this — file-tree sidebar +
syntax-highlighted preview + mouse resize — but is a separate,
unrelated codebase (different language, no daemon/remote/multi-agent
model). Rather than adopting that codebase, ccmux gets an equivalent,
native Go implementation of the same user-facing capability, built to
fit ccmux's existing architecture (tmux as the session store, ccmux as
the lens) instead of duplicating a PTY/vt100 layer tmux already owns.

## What Changes

- Add a new **Files** screen: a whole-project file tree (every file,
  not just `.md`) with a syntax-highlighted preview pane, reusing the
  fold/unfold and split-pane-preview patterns already proven in the
  Notes screen (`internal/tui/notes.go`).
- Add a new `internal/filebrowser` package: walks the full project
  tree (pruning `.git`/`node_modules`/build dirs the same way
  `internal/notes` already does), detects binary files, and renders
  syntax-highlighted previews via `chroma` (promoted from an indirect
  to a direct dependency; already vendored transitively through
  Glamour).
- Wire mouse support for the new screen: click-to-focus between
  tree/preview, drag-to-resize the split (extends the existing
  `tea.MouseMsg` handling already present in `app.go` and
  `notes.go`).
- Add automatic cwd tracking: the Files tree re-roots to the attached
  pane's current directory using tmux's own
  `#{pane_current_path}` (via `internal/tmux`) — no OSC7 parsing
  needed, unlike ccmux-master's approach, since tmux already tracks
  this.
- Add CLI parity per the repo's feature-surface policy:
  `ccmux files list|read <project> [--host <name>]`, mirroring the
  existing `ccmux notes list|read|search` commands.
- **Out of scope:** a self-drawn split-pane terminal multiplexer
  (PTY/vt100 layer inside ccmux's own TUI). tmux already provides pane
  splitting; ccmux's role stays "the lens," not a second session
  store. This was explicitly decided against with the project owner
  before scoping this change.

## Capabilities

### New Capabilities

- `file-browser-support`: ccmux can browse every file in a project
  (not just markdown), preview any file with syntax highlighting, keep
  that view synced to the attached pane's current directory, and
  reach all of this from both the TUI and the CLI.

### Modified Capabilities

- None. This is additive: the Notes screen, its markdown-only scope,
  and `internal/notes` are untouched. The new `internal/filebrowser`
  package and `ScreenFiles` are new surfaces.

## Impact

- `internal/filebrowser` — new package: full-tree walk, binary
  detection, chroma-based syntax highlighting, cwd re-rooting via
  tmux.
- `internal/tui` — new `ScreenFiles` entry in the `Screen` enum
  (`app.go`), new `filesModel` (new file, structured after
  `notesModel` in `notes.go`), mouse click-focus/drag-resize wiring,
  style tokens from `internal/tui/styles/` (no hardcoded colors/ints —
  enforced by `styles_lint_test.go`).
- `internal/tmux` — read `#{pane_current_path}` for the attached
  pane.
- `cmd/ccmux/cmd` — new `files` subcommand (`list`, `read`) mirroring
  the `notes` subcommand shape.
- `go.mod` — `github.com/alecthomas/chroma/v2` moves from indirect to
  direct.
- Tests — unit tests for `internal/filebrowser` (table-driven, fake
  trees), golden test for the new screen (`teatest`), CLI tests
  mirroring the `notes` CLI tests, an e2e CUJ (open Files screen,
  select a file, see highlighted preview, resize).
- Docs — README feature list, new
  `docs/02_Architecture/07_File_Browser.md` (modeled on
  `01_Notes_System.md`), `CLAUDE.md` component list.
