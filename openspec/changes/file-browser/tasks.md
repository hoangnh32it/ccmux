## 1. OpenSpec

- [x] 1.1 Complete OpenSpec proposal, design, capability spec, and this checklist for the Files screen, and run `openspec validate file-browser` (OpenSpec CLI `@fission-ai/openspec` v1.7.0 installed 2026-08-01; `openspec validate file-browser` passes).

## 2. File Walk & Syntax Highlight Package

- [x] 2.1 Add `internal/filebrowser` package: `Tree.List()` returns every regular file (not just `.md`), pruning the same VCS/dependency/build dirs `internal/notes.skipDir` already prunes. Also `Tree.Read`/`Tree.Resolve` (root-containment checked, since `ccmux files read --host` will take the path off the network) and the fold/unfold tree logic (`VisibleRows`, `FolderDirs`, `DefaultFolds`, `ParentDir`, `DescendantOf`) lifted out of `notes.go` as pure functions so `filesModel` in 3.2 is wiring only.
- [x] 2.2 Add binary-file detection (`IsBinary` / `IsBinaryContent`, NUL-or-invalid-UTF-8 over a bounded 8000-byte sniff) so binary files are flagged and never fed to the highlighter or loaded fully into memory. Sniffed lazily on the previewed file, not per row — a whole-project walk cannot afford to open every file.
- [x] 2.3 Promote `github.com/alecthomas/chroma/v2` from indirect to direct in `go.mod`; `Highlight(path, content) string` + `HighlightWith(…, style, formatter)` using `lexers.Match` by extension, falling back to unmodified plain text. Style `catppuccin-mocha` matches `styles.DefaultPalette`; formatter `terminal256`. Also `Lang(path)` for the preview header, and `"noop"` as the escape-free formatter golden tests use.
- [x] 2.4 Preview size caps: `HighlightLimit` (256 KiB — above it, plain text) and `PreviewLimit` (1 MiB — above it, only the head is read). `Tree.Preview(rel)` applies both and reports `Truncated`/`Highlightable`.
- [x] 2.5 Table-driven unit tests: walk pruning, binary detection, fold/unfold row flattening.
- [x] 2.6 Table-driven unit tests: highlight fallback for unknown extensions, both size caps, markdown staying raw for Glamour.

## 3. TUI Screen

- [x] 3.1 Add `ScreenFiles` to the `Screen` enum in `app.go`, appended after `ScreenNetwork` (key `8`); `screenLabels` entry "Files"; `Files` binding (`8`/`f8`) in `keys.go`.
- [x] 3.2 Add `filesModel` (`internal/tui/files.go`): tree pane + preview pane, focus switching, folding, scrolling, project picker, async walk + async preview load. Renderer forks per file type — Glamour for `.md` (same as Notes), chroma for source, placeholder for binary.
- [x] 3.3 Wire `filesModel` into `app.go`'s router: `New`, `SetSize`, `SetProjects`, key handler, `Update`/`View` dispatch, help bar, mouse-wheel forwarding.
- [x] 3.4 Style exclusively via `internal/tui/styles/` tokens — `styles_lint_test.go` passes.
- [x] 3.5 Five goldens (`files`, `files_preview`, `files_binary`, `files_empty`, `files_narrow`) following `screens_golden_test.go`. The preview golden uses chroma's `noop` formatter so the snapshot stays escape-free and reviewable.
- [x] 3.6 Replace the hand-written `"1-7"` "screens" hint — present in eight separate help bars and made wrong by this change — with `screenKeyRange()`, derived from `screenCount`. New `TestNoLiteralTabKeyRange` lint closes the bug class, mirroring the existing `TestNoLiteralTabKeyDigits`.

## 4. Mouse Support

- [x] 4.1 Router-level `tea.MouseMsg` handling in `app.go` now forwards non-wheel events to Files (only Files — every other screen keeps absorbing them, so a stray click can't move a selection the user wasn't looking at). Coordinates are translated from absolute terminal space to body space by `bodyMouse`, using the `headerRows` constant that `TestHeaderRowsMatchesRenderedHeader` pins to the real tab strip. Click focuses a pane; click on a row selects it.
- [x] 4.2 Drag-to-resize in `internal/tui/files_mouse.go`: press on the border (with ±1 column of slack — a one-cell grab target is unhittable) arms `draggingSplit`, motion recomputes `splitRatio`, release ends it. The preview viewport is re-laid and its content re-rendered at the new width, or it would stay wrapped for the old column. Narrow layout has no second pane, so nothing is grabbable there.
- [x] 4.3 `TestRatioForX` covers the pure conversion directly, including both clamp bounds, either side of the min bound, and a zero-width terminal (which happens before the first `WindowSizeMsg`). `TestClickSelectsRow` derives the click's Y from the actual render rather than hard-coding an offset, so the row arithmetic can't rot when the pane header changes.

## 5. cwd Tracking

- [x] 5.1 `tmux.CurrentPath(ctx, session)` wrapping `display-message -p -t <session> '#{pane_current_path}'`, plus a pure `parseCurrentPath` so the output handling is testable without a live server. Returns `""` with no error on a missing session, mirroring `PaneTitle` — a poll loop must not start erroring because the session it followed exited.
- [x] 5.2 The App's existing 2-second tick polls, but only while the Files screen is visible; `attachedLocalSession` picks the session to follow (exactly one attached local session, or none — two attached sessions have no single answer and would make the tree flip between projects on alternate ticks). `filesCwdMsg` re-roots. Tracking is on by default, shown by a `⇢` on the path line, toggled with `f`, and switched off automatically when the user picks a project explicitly.
- [x] 5.3 `TestParseCurrentPath` table-drives eight shapes of fake tmux output (trailing newline, CRLF, spaces, non-ASCII, empty, whitespace-only). `TestCwdMsgRerootsTree` asserts the tree root updates and the new tree is actually walked; siblings cover the same-path no-op, empty path, stale session, the `f` toggle, and the explicit-pick override.

## 6. CLI Parity

- [x] 6.1 `cmd/ccmux/cmd/files.go`: `ccmux files list <project>` and `ccmux files read <project> <path> [--host <name>]`, mirroring `notes`' subcommand names, `--host` flag, and output shape (table, then raw body). Backed by a new `GET /v1/files` daemon endpoint (`handleFiles`) plus `daemon.Client.Files` / `.FileContent` and the `FileEntry` / `FileContent` protocol types — the CLI reaches local and remote through the same client the TUI uses, as `notes` does.
- [x] 6.2 CLI tests drive the real client against an `httptest` daemon: list formatting, raw read, binary refusal, truncation notice going to stderr so a redirected stdout stays clean, unknown-host rejection, and the command shape itself. Daemon-side tests cover traversal rejection, every-file-type serving, binary flagging, tree pruning, method/project guards, and 404.

## 7. Verification

- [ ] 7.1 `go test ./...` clean, `gofmt -l` clean, `go vet ./...` clean.
- [ ] 7.2 Cross-compile `GOOS=windows` and `GOOS=linux` (mouse handling and path joins touch OS-specific code).
- [ ] 7.3 e2e CUJ test in `internal/e2e/`: open Files screen, navigate tree, select a non-markdown file, confirm highlighted preview renders, resize via mouse.

## 8. Docs

- [ ] 8.1 Update `README.md` feature list.
- [ ] 8.2 Add `docs/02_Architecture/07_File_Browser.md`, modeled on `01_Notes_System.md`.
- [ ] 8.3 Update `CLAUDE.md`'s component list with `internal/filebrowser`.
