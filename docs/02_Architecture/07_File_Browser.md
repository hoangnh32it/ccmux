# File Browser

## Design Stance

The Files tab is a read-only lens on a project's whole working tree —
every file, not just markdown — with a syntax-highlighted preview. It
exists so you can read the code an agent just wrote without leaving
ccmux, on the same SSH/Mosh/Tailscale path that already carries
everything else.

It is a *browser*, not an editor. `enter` / `e` hand the selected file
to `$EDITOR`; ccmux itself never writes.

## Why a Separate Tab from Notes

The obvious alternative was to widen the Notes tab's scope. We didn't,
and the reasons are worth keeping written down:

| Reason | Detail |
|---|---|
| Notes' identity is "this project's markdown vault" | Stated in `internal/notes`' package doc. Whole-project file browsing blurs a scope the package deliberately draws. |
| Notes labels rows by each file's leading H1 | Reading the first 4 KiB of every file to find a heading is fine for a vault of `.md`; it is nonsense for a `.png` and expensive for a repo. Files labels rows with the filename. |
| Notes' preview is always Glamour | Glamour is a markdown renderer. Handed a bare `.go` file it renders an undifferentiated paragraph. |
| `notes.go` was already 1675 lines | Folding a second, differently-shaped feature into it would not have improved anyone's day. |

What the two *do* share is the interaction shape — `tab` toggles focus,
`j`/`k` navigate within the focused pane, `→`/`←` fold and unfold — so
muscle memory transfers between the tabs. The label copy makes the
distinction explicit: **Notes is markdown only; Files is the whole
project.**

## Components

```
internal/filebrowser/          ← operations layer, no Bubble Tea
├── filebrowser.go   Tree.List / Read / Resolve, skipDir
├── tree.go          VisibleRows, FolderDirs, DefaultFolds, ParentDir, DescendantOf
├── binary.go        IsBinary / IsBinaryContent
├── highlight.go     Lang, Highlight, HighlightWith
└── preview.go       Tree.Preview, Preview.Render, IsMarkdown

internal/tui/
├── files.go         filesModel — the screen
└── files_mouse.go   click-to-focus, drag-to-resize

cmd/ccmuxd/main.go   handleFiles → GET /v1/files
cmd/ccmux/cmd/files.go   `ccmux files list|read`
```

The fold/unfold logic lives in `internal/filebrowser/tree.go` as **pure
functions** rather than as methods on the screen model. That is the one
structural difference from Notes worth copying elsewhere: it is
directly testable without a terminal, and it left `filesModel` as
wiring.

## The Walk

`Tree.List()` walks the browsed root and returns every **regular** file,
sorted by containing directory then by path.

- **Pruning** is byte-for-byte the same rule as `internal/notes.skipDir`
  — hidden directories plus `node_modules`, `vendor`, `dist`, `build`,
  `target`, `__pycache__` — so Notes and Files agree on what counts as
  part of the project. Hidden *files* (`.gitignore`, `.goreleaser.yaml`)
  are kept: they are project files you expect to browse, and unlike a
  hidden directory they can't drag in thousands of entries.
- **Symlinks, sockets, devices, and fifos are skipped.** Following a
  symlinked directory risks walking out of the tree or into a cycle.
- **An unreadable directory is skipped, not fatal.** A whole-project
  walk meets permission-denied often enough that losing the entire
  listing over one would make the screen useless. (Notes returns the
  error for the whole vault; it can afford to, only ever seeing `.md`.)
- **Folders open collapsed.** Expanding a repo on entry buries the
  root-level files you came for.

## Preview

`Tree.Preview(rel)` does the I/O and the classification, and returns
**raw text**. Choosing a renderer is the caller's job — the TUI hands
`.md` to Glamour and everything else to chroma, while `ccmux files
read` prints the content as-is. Folding that fork into the package
would force the CLI to strip ANSI back out of its own output.

| Field | Meaning |
|---|---|
| `Binary` | The content sniff rejected this as text. `Content` is empty; the pane shows a placeholder. |
| `Truncated` | The file exceeded `PreviewLimit`; `Content` holds only its head. `Size` still reports the real length. |
| `Lang` | chroma's lexer name, `""` when unrecognized. |
| `Highlightable` | Whether `Highlight` would actually colour this — false for binary, unknown extensions, and anything over `HighlightLimit`. |

The preview footer states all of this plainly (`Go · 15.0 KiB`,
`binary · 40.0 KiB`, `plain text · 3.2 KiB · truncated`) rather than
implying colour it isn't showing.

### Binary detection

A NUL byte, or invalid UTF-8, within the first **8000 bytes** — git's
own threshold. The sniff is **lazy**: it runs on the file about to be
previewed, never per listing row. Opening 600 files to render one
screen is not affordable, which is why `Entry` carries no `Binary`
field.

A legacy single-byte-encoded file (Latin-1, Shift-JIS) is classified
binary. That is intended: the preview pane writes to a UTF-8 terminal,
so such a file would render as mojibake anyway, and the placeholder is
the more honest outcome.

### Syntax highlighting

`github.com/alecthomas/chroma/v2`, directly — not through Glamour,
which is a markdown renderer that happens to call chroma for fenced
code blocks.

- **Style `catppuccin-mocha`**, the same palette `styles.DefaultPalette`
  is built from, so highlighted code sits in the TUI's own colour world.
- **Formatter `terminal256`**, not `terminal16m`: chroma writes escapes
  straight into the string and never passes through lipgloss's
  colour-profile downsampling, so the widely-supported depth is the
  right default. `"noop"` emits no escapes at all — that is the seam
  golden tests use.
- **An unrecognized extension returns the content untouched.** Falling
  through to chroma's plaintext lexer would re-emit the whole file
  wrapped in escapes that colour nothing.

### Size caps

Both live in the package, so every caller inherits them:

| Constant | Value | Effect |
|---|---|---|
| `HighlightLimit` | 256 KiB | Above this, plain text. |
| `PreviewLimit` | 1 MiB | Above this, only the head is read. |

Selecting a 2 GB file costs what selecting a 2 KB file does.

## cwd Tracking

The tree follows the working directory of the tmux pane you are
attached to. ccmux does not own the PTY — tmux does — so there is
nothing to parse OSC 7 out of and no reason to: tmux already tracks the
directory and hands it over through `#{pane_current_path}` for the cost
of one `display-message`. (`ccmux-master`, the Rust project this
feature was ported from, parses OSC 7 itself because it *is* the
terminal emulator.)

- Polled on the App's existing 2-second tick, and **only while the
  Files screen is visible**. One `display-message` per tick is cheap,
  not free, and a tree nobody is looking at doesn't need to be current.
- `attachedLocalSession` picks the target: exactly one attached local
  session, or nothing. Two attached sessions have no single answer, and
  choosing arbitrarily would make the tree flip between projects on
  alternate ticks. Remote sessions are skipped — their cwd names a
  directory on another machine's disk.
- **On by default** (a file browser that ignores where you actually are
  is the wrong default), marked with a `⇢` on the path line so "why did
  my tree move" has a visible answer. `f` toggles it.
- **Picking a project explicitly turns it off.** You named the tree you
  wanted; having the next poll undo it would be hostile.
- An unchanged path is a no-op rather than a re-walk, so the poll
  doesn't discard your fold state on a timer. An empty path — pane
  directory deleted, or session gone between poll and reply — leaves
  the tree where it is instead of blanking it.

## Mouse

The router forwards non-wheel mouse events to Files and **only** Files;
everywhere else they stay absorbed, so a stray click can't move a
selection you weren't looking at. Coordinates arrive in absolute
terminal space and are translated to body space by `bodyMouse`.

- **Click** a pane to focus it; click a tree row to select it.
- **Drag** the border between the panes to resize. The grab target is
  the border column ±1 — a one-cell target is unhittable in practice,
  since you aim at a line, not a character cell. The ratio is clamped
  to `[0.15, 0.75]` so neither pane collapses.
- **Wheel** over the tree moves the cursor; over the preview it scrolls
  the viewport.

Resizing re-lays the preview viewport *and* re-renders its content at
the new width — without the second half the text stays wrapped for the
old column.

### Why `→` does nothing on a file

`notes.go` moves focus to the preview when you press `→` on a file row,
and Files copied that at first. It was a trap. In Notes most rows sit
inside folders so it rarely fires; in a source repo the first dozen
rows are root-level files, so `→` — the first key anyone presses on a
tree — silently handed the keyboard to the viewport. From there `↑↓`
scrolled the preview instead of moving the cursor, `→` did nothing, and
`enter` no longer opened `$EDITOR`. The tree was dead and nothing on
screen said why; only `tab` or `←` got you out, and neither advertised
itself.

The help bar already says `→/← expand/collapse`. The key now keeps that
promise, and focus moves only through the two affordances that announce
it: `tab` and a mouse click.
`TestRightOnFileRowDoesNotStealFocus` guards the regression.

## Keys

| Key | Action |
|---|---|
| `8` / `F8` | Open the Files tab |
| `↑` `↓` / `j` `k` | Move within the focused pane |
| `→` / `l` | Expand a folder, or drill into an open one. **Nothing on a file** — see below |
| `←` / `h` | Collapse a folder, jump out to its parent header, or return focus from the preview |
| `tab` | Toggle focus between tree and preview |
| `enter` / `e` | Open the selected file in `$EDITOR` (folders toggle) |
| `p` / `space` | Switch project |
| `f` | Toggle cwd tracking |

## Daemon API

`GET /v1/files?project=<name>` → `[]FileEntry`
`GET /v1/files?project=<name>&file=<rel>` → `FileContent`

The project is resolved through the same gate `/v1/notes` uses, so a
caller can only reference projects ccmux already lists. Unlike
`handleNotes` there are **no inline traversal checks** — containment
lives in `filebrowser.Tree.Resolve`, which rejects absolute paths, `..`
traversal, and symlinks pointing out of the tree, and `Preview` goes
through it. `notes.Vault.Read` trusts its input so its handler has to
guard; `filebrowser.Tree` does not, so the guard sits with the
primitive instead of being re-derived at each call site.

`FuzzResolve` pins that invariant, and the endpoint's traversal test is
kept anyway: it is what catches a future refactor that routes around
`Resolve`.

Binary files come back with `Binary` set and `Content` empty. The
server will not ship bytes a client would then have to guard before
printing.

## CLI

```
ccmux files list <project> [--host <name>]
ccmux files read <project> <path> [--host <name>]
```

Deliberately shaped like `ccmux notes`: same subcommand names, same
`--host` flag, same output shape (table, then raw body), so scripting
muscle memory transfers.

`read` **refuses binary files** rather than printing them — dumping a
PNG into a terminal corrupts its state. A truncation notice goes to
stderr, so `ccmux files read … > out` gets the bytes and nothing else.

## Decisions Locked In

- New screen, appended at the end of the `Screen` enum (key `8`).
  `screenKey()` derives each tab's digit from enum position, so
  inserting anywhere but the end silently renumbers every screen after
  it.
- `internal/filebrowser` is separate from `internal/notes`; neither
  imports the other. The prune rule is duplicated on purpose, pinned by
  a test in each package, rather than exported and shared — Notes'
  contract shouldn't move because Files changed its mind.
- No ripgrep search and no device switching on this screen. The spec
  asks for `--host` on the CLI, not in the TUI.
- No per-project entry cache. Notes has one; the point of this tree is
  to show what is on disk *right now*, and a session-long cache would
  hide exactly the files an agent just wrote.
