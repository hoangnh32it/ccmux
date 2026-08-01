## Context

ccmux's TUI is a router of independent Bubble Tea screen models
(`internal/tui/app.go`'s `Screen` enum + `allScreens()`), each screen
implementing its own `Update`/`View`. The Notes screen
(`internal/tui/notes.go`, ~1675 lines, backed by `internal/notes`) is
the closest existing analog to what's being asked for here: it already
has a collapsible folder tree (`applyDefaultFolds`, `collapseFolder`,
`visibleRows`), a tree/preview split pane (`previewPaneSize`,
`renderPreview`), search (ripgrep-backed), and cross-device support —
but it is deliberately scoped to `.md` files only ("Plain markdown on
disk is the source of truth," per `internal/notes`'s package doc).

`ccmux-master` (a separate, unrelated Rust project) has the feature
being asked for — general file tree + syntax-highlighted preview +
mouse resize — but as a self-contained terminal multiplexer with its
own PTY/vt100 layer. That layer is out of scope here (decided with the
project owner 2026-08-01): tmux already owns pane splitting in ccmux's
architecture, and duplicating it would fight the "tmux = session
store, ccmux = lens" design instead of extending it.

## Goals / Non-Goals

**Goals:**

- Browse every file in a project, not just markdown.
- Preview any text file with syntax highlighting (fallback to plain
  text for unrecognized/binary files).
- Keep the file tree's root in sync with the attached pane's current
  directory.
- Mouse click-to-focus and drag-to-resize between tree and preview.
- Reach the feature from both the TUI (`ScreenFiles`) and the CLI
  (`ccmux files ...`), per the repo's feature-surface policy.

**Non-Goals:**

- A self-drawn split-pane/PTY layer inside ccmux's own TUI (tmux
  already does this).
- Editing files from the Files screen (Notes already offers `e` to
  open `$EDITOR`; Files can adopt the same affordance later, but
  isn't required for this change).
- Replacing or merging the Notes screen. Notes stays markdown-scoped
  and untouched; Files is a new, separate screen.

## Decisions

1. **New screen, not a Notes extension.** Add `ScreenFiles` as its own
   screen/model rather than generalizing `notesModel`. Notes' identity
   ("this project's markdown vault") is explicit in its package doc;
   folding whole-project file browsing into it would blur that scope
   and bloat an already-1675-line file. A new `filesModel` in a new
   file reuses Notes' *patterns* (fold/unfold, split preview) without
   inheriting its markdown-only assumptions.

2. **Screen enum position: append at the end.** Insert `ScreenFiles`
   after `ScreenNetwork` in the `Screen` const block (`app.go`), i.e.
   key `8`. `screenKey()` derives the digit from enum position
   (`int(s) + 1`), so inserting Files earlier would silently renumber
   every screen after it and break existing user muscle memory (the
   same reasoning the `add-grok-agent` change used: append, don't
   reorder). Appending is the only additive option.

3. **New package `internal/filebrowser`, not extending
   `internal/notes`.** Whole-project walk + binary detection + chroma
   rendering are a different responsibility from the markdown vault.
   A separate package keeps `internal/notes`'s contract ("every `.md`
   file") unchanged and testable in isolation, mirroring how
   `internal/notes` itself is separate from `internal/project`.

4. **Syntax highlighting via `chroma` directly, Glamour stays
   Notes-only.** Glamour (already a direct dep) is a markdown renderer
   built on top of chroma; it's the wrong tool for highlighting a
   `.go` or `.rs` file with no markdown structure. Use
   `github.com/alecthomas/chroma/v2` directly for Files' preview,
   promoting it from indirect (currently pulled in transitively via
   Glamour) to a direct `go.mod` entry. Extension → lexer via chroma's
   own `lexers.Match`; unrecognized extensions or files failing a
   UTF-8/binary sniff fall back to a plain, unstyled preview (same
   binary-file guard rail `internal/notes` doesn't need today since it
   only ever sees `.md`).

5. **cwd tracking via tmux, not OSC7.** ccmux-master parses OSC7
   sequences itself because it owns the PTY. ccmux does not — tmux
   does — and tmux already exposes the pane's current working
   directory as the format variable `#{pane_current_path}`. Read it
   through `internal/tmux` (the existing wrapper all session
   operations go through) instead of adding a terminal-emulation/OSC
   parsing layer. This is strictly cheaper than the ccmux-master
   approach and fits the "no direct shell-outs outside `internal/tmux`"
   convention.

6. **Mouse handling extends the existing router-level `tea.MouseMsg`
   case in `app.go`**, plus a screen-local handler in `filesModel`
   analogous to the one already in `notes.go`. Drag-resize recomputes
   a tree/preview width ratio from the mouse X position, stored on
   `filesModel` the same way `notesModel.previewPaneSize()` derives
   its split today (but user-adjustable via drag instead of fixed).

7. **CLI shape mirrors `notes`.** `ccmux files list <project>` and
   `ccmux files read <project> <path> [--host <name>]`, following the
   existing `cmd/ccmux/cmd` notes subcommand's flag/output
   conventions, so scripting muscle memory transfers directly.

## Risks / Trade-offs

- **Large projects / deep trees.** A whole-project walk is more
  expensive than a markdown-only walk. *Mitigation:* prune the same
  VCS/dependency/build directories `internal/notes` already prunes;
  apply default folding (collapsed by default, same as Notes) so the
  initial render stays cheap.
- **Binary or huge files in preview.** Loading a large binary into the
  preview pane could hang or garble the terminal. *Mitigation:* binary
  sniff + a preview size cap before attempting to render or
  highlight, matching the "Dirty-flag rendering for minimal CPU usage"
  performance bar the rest of the TUI holds itself to.
- **Screen count growing (8 tabs).** More tabs strain the narrow
  phone-width tab bar. *Mitigation:* `allScreens()`/`screenLabels`
  already drive both the wide and narrow tab bar layouts
  automatically (per `adaptive-screen-layout`), so this is a rendering
  concern already handled generically, not a new one.
- **Duplicate mental model with Notes.** Users may wonder why Files
  and Notes are separate tabs. *Mitigation:* label and help-bar copy
  make the distinction explicit ("Notes: markdown only" /
  "Files: whole project"); revisit merging only if user feedback says
  otherwise — not a blocking concern for this change.
