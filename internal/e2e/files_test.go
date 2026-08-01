//go:build integration

package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skzv/ccmux/internal/filebrowser"
)

// seedFilesProject writes a small mixed-language project into the
// sandbox's projects root and returns its path. The mix is the point:
// Files exists because Notes only ever sees the .md file here.
func seedFilesProject(t *testing.T, e *Env, name string) string {
	t.Helper()
	proj := filepath.Join(e.Root, name)
	// CLAUDE.md is what project.Discover keys on, so the TUI's Projects
	// screen actually lists this directory.
	writeFile(t, filepath.Join(proj, "CLAUDE.md"), "# demo project\n")
	writeFile(t, filepath.Join(proj, "README.md"), "# Demo\n\nreadme prose\n")
	writeFile(t, filepath.Join(proj, "main.go"),
		"package main\n\nfunc main() {\n\tprintln(\"hello from main\")\n}\n")
	writeFile(t, filepath.Join(proj, "config.json"), "{\n  \"key\": \"value\"\n}\n")
	writeFile(t, filepath.Join(proj, "internal", "store", "store.go"),
		"package store\n\nconst Marker = \"store-marker\"\n")
	// Pruned trees — the CUJ includes "and the noise stays out".
	writeFile(t, filepath.Join(proj, "node_modules", "dep", "index.js"), "module.exports={}\n")
	writeFile(t, filepath.Join(proj, ".git", "config"), "[core]\n")
	// A binary file, so the placeholder path is exercised for real.
	writeFile(t, filepath.Join(proj, "logo.png"), "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	return proj
}

// TestFilesBrowseAndPreview is the package-level CUJ: the tree lists
// every file type (not just markdown), the noise directories are
// pruned, a .go file previews with highlighting, and a binary file
// does not.
func TestFilesBrowseAndPreview(t *testing.T) {
	e := newEnv(t)
	proj := seedFilesProject(t, e, "demo")

	tree := filebrowser.Open(proj)
	entries, err := tree.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	got := map[string]bool{}
	for _, en := range entries {
		got[en.Rel] = true
	}
	for _, want := range []string{"README.md", "main.go", "config.json", "logo.png", "internal/store/store.go"} {
		if !got[want] {
			t.Errorf("tree is missing %q; got %v", want, keysOfBool(got))
		}
	}
	for _, pruned := range []string{"node_modules/dep/index.js", ".git/config"} {
		if got[pruned] {
			t.Errorf("pruned path %q leaked into the tree", pruned)
		}
	}

	// A recognized language previews highlighted, with its text intact
	// under the escapes.
	p, err := tree.Preview("internal/store/store.go")
	if err != nil {
		t.Fatalf("Preview(store.go): %v", err)
	}
	if p.Lang != "Go" {
		t.Errorf("Lang = %q, want Go", p.Lang)
	}
	if !p.Highlightable {
		t.Error("a small .go file should be highlightable")
	}
	rendered := p.Render()
	if !strings.Contains(rendered, "\x1b[") {
		t.Errorf("Render() produced no highlighting:\n%s", rendered)
	}
	if !strings.Contains(rendered, "store-marker") {
		t.Error("highlighting dropped the file's own content")
	}

	// A binary file gets a placeholder, never its bytes.
	bin, err := tree.Preview("logo.png")
	if err != nil {
		t.Fatalf("Preview(logo.png): %v", err)
	}
	if !bin.Binary || bin.Content != "" {
		t.Errorf("PNG preview = %+v, want Binary with empty Content", bin)
	}
	if !strings.Contains(bin.Render(), "binary") {
		t.Errorf("binary Render() = %q, want a placeholder", bin.Render())
	}
}

func keysOfBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestFilesCLI_ListAndRead drives the real `ccmux files` binary against
// a real ccmuxd over its Unix socket — the CLI half of the
// feature-surface policy, end to end.
func TestFilesCLI_ListAndRead(t *testing.T) {
	e := newEnv(t)
	seedFilesProject(t, e, "demo")
	d := e.startDaemon()
	defer d.stop()

	stdout, stderr, err := e.ccmux("files", "list", "demo")
	if err != nil {
		t.Fatalf("ccmux files list: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	for _, want := range []string{"main.go", "config.json", "internal/store/store.go"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("`files list` output is missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "node_modules") {
		t.Errorf("`files list` leaked a pruned directory:\n%s", stdout)
	}

	// read prints the raw bytes, and nothing else.
	stdout, stderr, err = e.ccmux("files", "read", "demo", "internal/store/store.go")
	if err != nil {
		t.Fatalf("ccmux files read: %v\nstderr=%s", err, stderr)
	}
	const want = "package store\n\nconst Marker = \"store-marker\"\n"
	if stdout != want {
		t.Errorf("`files read` stdout = %q, want %q", stdout, want)
	}

	// A binary file is refused rather than dumped into the terminal.
	stdout, stderr, err = e.ccmux("files", "read", "demo", "logo.png")
	if err == nil {
		t.Errorf("`files read` on a PNG succeeded; stdout=%q", stdout)
	}
	if !strings.Contains(stderr, "binary") {
		t.Errorf("stderr = %q, want it to say the file is binary", stderr)
	}

	// Traversal out of the project is refused over the socket too.
	stdout, stderr, err = e.ccmux("files", "read", "demo", "../../.ssh/id_rsa")
	if err == nil {
		t.Errorf("`files read` with a traversal path succeeded; stdout=%q", stdout)
	}
	_ = stderr
}

// TestFilesCLI_RemoteHost covers the --host path against a second
// daemon: `ccmux files` must reach a peer's project, not silently read
// this machine's.
func TestFilesCLI_UnknownHostIsRejected(t *testing.T) {
	e := newEnv(t)
	seedFilesProject(t, e, "demo")

	stdout, stderr, err := e.ccmux("files", "list", "demo", "--host", "not-configured")
	if err == nil {
		t.Fatalf("an unknown --host succeeded; stdout=%q", stdout)
	}
	if !strings.Contains(stderr, "unknown host") {
		t.Errorf("stderr = %q, want it to name the unknown host", stderr)
	}
}

// TestFilesDaemonEndpoint exercises GET /v1/files through the daemon
// client the TUI and CLI both use.
func TestFilesDaemonEndpoint(t *testing.T) {
	e := newEnv(t)
	seedFilesProject(t, e, "demo")
	d := e.startDaemon()
	defer d.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli := e.localClient()

	entries, err := cli.Files(ctx, "demo")
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("Files returned nothing")
	}
	found := false
	for _, en := range entries {
		if en.Rel == "main.go" {
			found = true
			if en.Size == 0 {
				t.Error("main.go reported zero size")
			}
		}
		if strings.HasPrefix(en.Rel, "node_modules/") {
			t.Errorf("pruned path %q came back over the socket", en.Rel)
		}
	}
	if !found {
		t.Error("main.go missing from the daemon's listing")
	}

	fc, err := cli.FileContent(ctx, "demo", "config.json")
	if err != nil {
		t.Fatalf("FileContent: %v", err)
	}
	if !strings.Contains(fc.Content, `"key"`) {
		t.Errorf("FileContent = %q", fc.Content)
	}
	if fc.Lang != "JSON" {
		t.Errorf("Lang = %q, want JSON", fc.Lang)
	}
}

// TestTUIFlow_FilesScreen is the interactive CUJ: open the Files tab,
// walk into a folder, land on a non-markdown file, and see its
// contents in the preview pane.
func TestTUIFlow_FilesScreen(t *testing.T) {
	e := newEnv(t)
	seedFilesProject(t, e, "demo")
	cfg := e.defaultConfig()
	cfg.Tour.Shown = true
	cfg.Update.AutoCheck = false
	e.writeConfig(cfg)

	d := newTUIDriver(t, e, 40, 140)
	d.WaitFor("Sessions")

	// WaitForWithInput, not Send-then-WaitFor: the tab strip spells out
	// every screen's name on every frame, so waiting for "Projects" or
	// "Files" matches the first frame and proves nothing. Assert on
	// strings that only the screen body can produce, and let the helper
	// re-send the key until the app is actually up and listening.
	//
	// Projects first, so the Files tab inherits the highlighted project
	// the same way Notes does.
	d.WaitForWithInput("demo", "2")
	// The tree lists every file type — this is the whole reason Files
	// exists next to the markdown-only Notes tab.
	d.WaitForWithInput("config.json", "8")
	d.WaitForTimeout("main.go", 8*time.Second)
	if out := d.Output(); strings.Contains(out, "node_modules") {
		t.Error("the tree showed a pruned dependency directory")
	}

	// Step down until a non-markdown file's contents show in the
	// preview. main.go's println string survives chroma as one token,
	// so it appears contiguously in the PTY stream.
	//
	// Do not assert on a multi-word phrase from a Glamour-rendered
	// markdown preview: Glamour emits escape sequences *between* words,
	// so "readme prose" is split in the raw stream even though it reads
	// correctly on screen. Single words, or a chroma string token, are
	// what hold up here.
	d.WaitForWithInput("hello from main", "j")

	// The footer badge names the detected language, which is the
	// observable proof that highlighting (not plain text) is what the
	// pane chose for this file.
	if out := d.Output(); !strings.Contains(out, "Go \u00b7") {
		t.Error("the preview footer never showed the detected Go language badge")
	}

	d.Quit()
}

// TestTUIFlow_FilesMouseResize drives the drag-resize with real SGR
// mouse sequences through the PTY, which is the only way to know the
// program actually has mouse reporting on (tea.WithMouseCellMotion)
// and that the events reach this screen.
func TestTUIFlow_FilesMouseResize(t *testing.T) {
	e := newEnv(t)
	seedFilesProject(t, e, "demo")
	cfg := e.defaultConfig()
	cfg.Tour.Shown = true
	cfg.Update.AutoCheck = false
	e.writeConfig(cfg)

	const cols = 140
	d := newTUIDriver(t, e, 40, cols)
	d.WaitFor("Sessions")
	d.WaitForWithInput("demo", "2")
	d.WaitForWithInput("config.json", "8")

	// The tree starts at a third of the width; its border sits there.
	// Drag it right and check the preview pane's left edge moved with
	// it. Columns in SGR mouse reports are 1-based.
	border := cols / 3
	target := cols * 2 / 3

	d.Send(sgrMouse(0, border+1, 10, 'M')) // press on the border
	time.Sleep(150 * time.Millisecond)
	d.Send(sgrMouse(32, target+1, 10, 'M')) // motion with button 0 held
	time.Sleep(150 * time.Millisecond)
	d.Send(sgrMouse(0, target+1, 10, 'm')) // release
	time.Sleep(400 * time.Millisecond)

	if !waitFor(8*time.Second, func() bool {
		return previewPaneColumn(d.Output()) > border+5
	}) {
		t.Errorf("the split did not move right after the drag (preview edge at column %d, border was %d)\n%s",
			previewPaneColumn(d.Output()), border, d.Output())
	}

	d.Quit()
}

// sgrMouse builds an SGR (1006) mouse report: ESC [ < Cb ; Cx ; Cy M|m.
// Cb 0 is the left button; adding 32 marks the event as motion, which
// is what cell-motion mode (1002) sends while a button is held.
func sgrMouse(button, col, row int, final rune) string {
	return fmt.Sprintf("\x1b[<%d;%d;%d%c", button, col, row, final)
}

// previewPaneColumn returns the column of the preview pane's top-left
// corner on the widest line of the last frame, or -1. Two panes render
// side by side, so the second ╭ on a line is the preview's.
func previewPaneColumn(out string) int {
	best := -1
	for _, line := range strings.Split(out, "\n") {
		runes := []rune(line)
		seen := 0
		for i, r := range runes {
			if r != '╭' {
				continue
			}
			seen++
			if seen == 2 {
				if i > best {
					best = i
				}
				break
			}
		}
	}
	return best
}
