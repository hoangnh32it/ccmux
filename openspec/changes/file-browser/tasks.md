## 1. OpenSpec

- [x] 1.1 Complete OpenSpec proposal, design, capability spec, and this checklist for the Files screen, and run `openspec validate file-browser` (OpenSpec CLI not installed locally — validated by hand against the `add-grok-agent` template instead).

## 2. File Walk & Syntax Highlight Package

- [ ] 2.1 Add `internal/filebrowser` package: `Walk(projectRoot)` returns every file (not just `.md`), pruning the same VCS/dependency/build dirs `internal/notes.skipDir` already prunes.
- [ ] 2.2 Add binary-file detection (content sniff) so binary files are flagged and never fed to the highlighter or loaded fully into memory.
- [ ] 2.3 Promote `github.com/alecthomas/chroma/v2` from indirect to direct in `go.mod`; add a `Highlight(path, content) string` helper using `lexers.Match` by extension, falling back to plain text.
- [ ] 2.4 Add a preview size cap (skip/truncate highlighting above a threshold) mirroring the TUI's existing performance bar.
- [ ] 2.5 Table-driven unit tests: walk pruning, binary detection, highlight fallback for unknown extensions.

## 3. TUI Screen

- [ ] 3.1 Add `ScreenFiles` to the `Screen` enum in `app.go`, appended after `ScreenNetwork` (key `8`); add its `screenLabels` entry ("Files").
- [ ] 3.2 Add `filesModel` (new file `internal/tui/files.go`), structured after `notesModel`: tree pane + preview pane, focus switching, scrolling, project switching.
- [ ] 3.3 Wire `filesModel` into `app.go`'s screen router (`Update`/`View` dispatch, help bar, key handling) the same way `ScreenNotes` is wired.
- [ ] 3.4 Style exclusively via `internal/tui/styles/` tokens (no literal hex colors or bare padding/margin ints — `styles_lint_test.go` enforces this).
- [ ] 3.5 Golden test for the new screen (`teatest`), following `screens_golden_test.go`'s pattern.

## 4. Mouse Support

- [ ] 4.1 Extend the router-level `tea.MouseMsg` handling in `app.go` to route clicks to the Files screen's tree/preview panes (click-to-focus).
- [ ] 4.2 Add drag-to-resize between tree and preview in `filesModel`, tracking mouse X during `MouseMotion`/`MouseDrag` and recomputing the split ratio (analogous to `notesModel.previewPaneSize()`, but user-adjustable).
- [ ] 4.3 Unit test the split-ratio-from-mouse-X computation directly (pure function, no terminal needed).

## 5. cwd Tracking

- [ ] 5.1 Add a `CurrentPath(session string) (string, error)` helper in `internal/tmux` wrapping `tmux display-message -p -t <session> '#{pane_current_path}'`.
- [ ] 5.2 Hook the Files screen (or the existing daemon poll cycle) to re-root the tree when the attached pane's cwd changes.
- [ ] 5.3 Test with a fake `tmux` output changing `pane_current_path`; assert the tree root updates.

## 6. CLI Parity

- [ ] 6.1 Add `cmd/ccmux/cmd/files.go`: `ccmux files list <project>` and `ccmux files read <project> <path> [--host <name>]`, mirroring the `notes` subcommand's flags and output shape.
- [ ] 6.2 CLI tests mirroring the existing `notes` CLI tests.

## 7. Verification

- [ ] 7.1 `go test ./...` clean, `gofmt -l` clean, `go vet ./...` clean.
- [ ] 7.2 Cross-compile `GOOS=windows` and `GOOS=linux` (mouse handling and path joins touch OS-specific code).
- [ ] 7.3 e2e CUJ test in `internal/e2e/`: open Files screen, navigate tree, select a non-markdown file, confirm highlighted preview renders, resize via mouse.

## 8. Docs

- [ ] 8.1 Update `README.md` feature list.
- [ ] 8.2 Add `docs/02_Architecture/07_File_Browser.md`, modeled on `01_Notes_System.md`.
- [ ] 8.3 Update `CLAUDE.md`'s component list with `internal/filebrowser`.
