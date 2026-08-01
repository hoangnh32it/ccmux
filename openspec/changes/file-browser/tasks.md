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
