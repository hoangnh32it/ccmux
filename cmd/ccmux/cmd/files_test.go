package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skzv/ccmux/internal/config"
	"github.com/skzv/ccmux/internal/daemon"
)

// fakeFilesDaemon stands up an httptest server speaking the /v1/files
// contract, and returns a config whose single host points at it — so
// `--host fake` exercises the real client path end to end without a
// tailnet or a live ccmuxd.
func fakeFilesDaemon(t *testing.T, h http.HandlerFunc) config.Config {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	addr := strings.TrimPrefix(srv.URL, "http://")
	host, port, ok := strings.Cut(addr, ":")
	if !ok {
		t.Fatalf("unexpected test server URL %q", srv.URL)
	}
	p := 0
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	return config.Config{Hosts: []config.Host{{Name: "fake", Address: host, Port: p}}}
}

// runFiles executes `ccmux files <args…>` against cfg, capturing
// stdout. config.Load() is what the command actually calls, so the
// host is injected by pointing HOME at a temp dir holding a config
// with our fake host in it.
func runFiles(t *testing.T, cfg config.Config, args ...string) (string, error) {
	t.Helper()
	writeTempConfig(t, cfg)

	cmd := newFilesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// writeTempConfig points $HOME (and XDG_CONFIG_HOME) at a temp dir
// holding cfg, so config.Load() inside the command picks it up.
func writeTempConfig(t *testing.T, cfg config.Config) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save test config: %v", err)
	}
}

func TestFilesList_PrintsTable(t *testing.T) {
	mod := time.Date(2026, 5, 27, 14, 30, 0, 0, time.UTC)
	cfg := fakeFilesDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/files" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("project"); got != "demo" {
			t.Errorf("project query = %q, want demo", got)
		}
		_ = json.NewEncoder(w).Encode([]daemon.FileEntry{
			{Rel: "README.md", Dir: "", Name: "README.md", Size: 2048, Modified: mod},
			{Rel: "internal/app.go", Dir: "internal", Name: "app.go", Size: 15360, Modified: mod},
		})
	})

	out, err := runFiles(t, cfg, "list", "demo", "--host", "fake")
	if err != nil {
		t.Fatalf("files list: %v (out=%s)", err, out)
	}
	for _, want := range []string{"REL", "SIZE", "MODIFIED", "README.md", "internal/app.go", "2048", "15360", "2026-05-27 14:30"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestFilesRead_PrintsContent(t *testing.T) {
	const body = "package main\n\nfunc main() {}\n"
	cfg := fakeFilesDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("file"); got != "main.go" {
			t.Errorf("file query = %q, want main.go", got)
		}
		_ = json.NewEncoder(w).Encode(daemon.FileContent{
			Rel: "main.go", Content: body, Size: int64(len(body)), Lang: "Go",
		})
	})

	out, err := runFiles(t, cfg, "read", "demo", "main.go", "--host", "fake")
	if err != nil {
		t.Fatalf("files read: %v (out=%s)", err, out)
	}
	if out != body {
		t.Errorf("output = %q, want the raw body %q", out, body)
	}
}

// TestFilesRead_RefusesBinary: dumping a PNG into a terminal corrupts
// its state, so the CLI errors instead of printing.
func TestFilesRead_RefusesBinary(t *testing.T) {
	cfg := fakeFilesDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(daemon.FileContent{
			Rel: "logo.png", Size: 40960, Binary: true,
		})
	})

	out, err := runFiles(t, cfg, "read", "demo", "logo.png", "--host", "fake")
	if err == nil {
		t.Fatalf("reading a binary file succeeded; out=%q", out)
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("error = %q, want it to mention the file is binary", err)
	}
}

// TestFilesRead_TruncatedNoteGoesToStderr: the bytes must be the only
// thing on stdout so `ccmux files read … > out` stays clean.
func TestFilesRead_TruncatedKeepsStdoutClean(t *testing.T) {
	const head = "line one\nline two\n"
	cfg := fakeFilesDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(daemon.FileContent{
			Rel: "huge.log", Content: head, Size: 99999, Truncated: true,
		})
	})

	writeTempConfig(t, cfg)
	cmd := newFilesCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"read", "demo", "huge.log", "--host", "fake"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("files read: %v", err)
	}
	if stdout.String() != head {
		t.Errorf("stdout = %q, want only the file bytes %q", stdout.String(), head)
	}
}

// TestFilesUnknownHost: a typo must not silently fall back to reading
// the local machine's files.
func TestFilesUnknownHost(t *testing.T) {
	out, err := runFiles(t, config.Config{}, "list", "demo", "--host", "typo")
	if err == nil {
		t.Fatalf("unknown host succeeded; out=%q", out)
	}
	if !strings.Contains(err.Error(), "unknown host") {
		t.Errorf("error = %q, want it to name the unknown host", err)
	}
}

// TestFilesCommandShape pins the CLI surface the feature-surface
// policy asks for, and its parity with `ccmux notes`.
func TestFilesCommandShape(t *testing.T) {
	c := newFilesCmd()
	if c.Use != "files" {
		t.Errorf("Use = %q, want files", c.Use)
	}
	if c.PersistentFlags().Lookup("host") == nil {
		t.Error("files has no --host flag; it must mirror notes")
	}

	subs := map[string]bool{}
	for _, sc := range c.Commands() {
		subs[strings.Fields(sc.Use)[0]] = true
		if sc.Args == nil {
			t.Errorf("subcommand %q has no Args validator", sc.Use)
		}
	}
	for _, want := range []string{"list", "read"} {
		if !subs[want] {
			t.Errorf("missing subcommand %q", want)
		}
	}
}
