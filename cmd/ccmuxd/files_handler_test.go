package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skzv/ccmux/internal/config"
	"github.com/skzv/ccmux/internal/daemon"
)

// filesTestServer seeds a project tree and returns a server rooted at
// its parent, plus that parent (where an out-of-tree secret lives).
func filesTestServer(t *testing.T) (*server, string) {
	t.Helper()
	root := t.TempDir()
	projDir := filepath.Join(root, "proj")
	for rel, body := range map[string]string{
		"README.md":           "# hi\n",
		"main.go":             "package main\n",
		"Makefile":            "all:\n\tgo build\n",
		"docs/guide.md":       "# guide\n",
		"node_modules/x/i.js": "module.exports={}",
		".git/config":         "[core]\n",
	} {
		full := filepath.Join(projDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A binary file the endpoint must refuse to ship as text.
	png := append([]byte("\x89PNG\r\n\x1a\n"), 0x00, 0x00, 0x00, 0x0D)
	if err := os.WriteFile(filepath.Join(projDir, "logo.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	// Outside the project, where a successful traversal would land.
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("password"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &server{cfg: config.Config{Projects: config.ProjectsConfig{Root: root}}}, root
}

// TestHandleFiles_RejectsTraversal is the same regression guard
// /v1/notes carries, re-asserted for this endpoint. The containment
// lives in filebrowser.Tree.Resolve rather than inline here, so this
// test is what catches a refactor that routes around it and turns
// /v1/files into an arbitrary-file-read reachable over the tailnet.
func TestHandleFiles_RejectsTraversal(t *testing.T) {
	s, _ := filesTestServer(t)

	for _, file := range []string{
		"/etc/passwd",
		`\Windows\system32\config\sam`,
		"../secret.txt",
		"docs/../../secret.txt",
		"docs/..",
		"../../../../../../etc/passwd",
	} {
		t.Run(file, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.handleFiles(rec, httptest.NewRequest(http.MethodGet, "/v1/files?project=proj&file="+file, nil))
			if rec.Code == http.StatusOK {
				t.Errorf("traversal %q got 200 — containment missing! body=%s", file, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "password") {
				t.Errorf("traversal %q leaked the out-of-tree file's contents", file)
			}
		})
	}
}

// TestHandleFiles_ServesEveryFileType is the difference from
// /v1/notes: no extension filter. A Makefile and a .go file are served
// where the notes endpoint would 400 them.
func TestHandleFiles_ServesEveryFileType(t *testing.T) {
	s, _ := filesTestServer(t)

	for _, tc := range []struct{ file, want string }{
		{"main.go", "package main\n"},
		{"Makefile", "all:\n\tgo build\n"},
		{"README.md", "# hi\n"},
		{"docs/guide.md", "# guide\n"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.handleFiles(rec, httptest.NewRequest(http.MethodGet, "/v1/files?project=proj&file="+tc.file, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d, body=%s", rec.Code, rec.Body)
			}
			var fc daemon.FileContent
			if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
				t.Fatal(err)
			}
			if fc.Content != tc.want {
				t.Errorf("Content = %q, want %q", fc.Content, tc.want)
			}
			if fc.Binary {
				t.Error("a text file was reported binary")
			}
		})
	}
}

// TestHandleFiles_BinaryIsFlaggedNotShipped: the server must not put
// raw binary in a JSON body a client might print.
func TestHandleFiles_BinaryIsFlaggedNotShipped(t *testing.T) {
	s, _ := filesTestServer(t)
	rec := httptest.NewRecorder()
	s.handleFiles(rec, httptest.NewRequest(http.MethodGet, "/v1/files?project=proj&file=logo.png", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rec.Code, rec.Body)
	}
	var fc daemon.FileContent
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatal(err)
	}
	if !fc.Binary {
		t.Error("a PNG was not flagged binary")
	}
	if fc.Content != "" {
		t.Errorf("binary content was shipped anyway: %q", fc.Content)
	}
}

// TestHandleFiles_ListsWholeTree covers the listing path, including
// the pruning the TUI applies.
func TestHandleFiles_ListsWholeTree(t *testing.T) {
	s, _ := filesTestServer(t)
	rec := httptest.NewRecorder()
	s.handleFiles(rec, httptest.NewRequest(http.MethodGet, "/v1/files?project=proj", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rec.Code, rec.Body)
	}
	var entries []daemon.FileEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}

	got := map[string]daemon.FileEntry{}
	for _, e := range entries {
		got[e.Rel] = e
	}
	for _, want := range []string{"README.md", "main.go", "Makefile", "logo.png", "docs/guide.md"} {
		if _, ok := got[want]; !ok {
			t.Errorf("listing is missing %q; got %v", want, keysOf(got))
		}
	}
	for _, pruned := range []string{"node_modules/x/i.js", ".git/config"} {
		if _, ok := got[pruned]; ok {
			t.Errorf("pruned path %q leaked into the listing", pruned)
		}
	}
	if e := got["main.go"]; e.Name != "main.go" || e.Size == 0 || e.Modified.IsZero() {
		t.Errorf("entry metadata not populated: %+v", e)
	}
}

func keysOf(m map[string]daemon.FileEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestHandleFiles_MethodAndProjectGuards(t *testing.T) {
	s, _ := filesTestServer(t)

	for _, m := range []string{http.MethodPost, http.MethodDelete, http.MethodPut} {
		rec := httptest.NewRecorder()
		s.handleFiles(rec, httptest.NewRequest(m, "/v1/files?project=proj", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s got %d, want 405", m, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	s.handleFiles(rec, httptest.NewRequest(http.MethodGet, "/v1/files", nil))
	if rec.Code == http.StatusOK {
		t.Error("a request with no project got 200")
	}

	rec = httptest.NewRecorder()
	s.handleFiles(rec, httptest.NewRequest(http.MethodGet, "/v1/files?project=nothere", nil))
	if rec.Code == http.StatusOK {
		t.Error("an unknown project got 200")
	}
}

func TestHandleFiles_MissingFileIs404(t *testing.T) {
	s, _ := filesTestServer(t)
	rec := httptest.NewRecorder()
	s.handleFiles(rec, httptest.NewRequest(http.MethodGet, "/v1/files?project=proj&file=nope.go", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404 (body=%s)", rec.Code, rec.Body)
	}
}
