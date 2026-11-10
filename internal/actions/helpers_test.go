package actions

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/10mTech/local-control-panel/internal/audit"
	"github.com/10mTech/local-control-panel/internal/config"
)

// testActions builds an Actions over a throwaway root and state folder. There
// is no herdr client; nothing here talks to herdr, Trash or the user's HOME.
func testActions(t *testing.T) (*Actions, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	root := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Root: root, Archive: filepath.Join(root, "archive")}
	state := t.TempDir()
	al, err := audit.New(state, "test")
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(cfg, nil, al, log.New(io.Discard, "", 0), state)
	if err != nil {
		t.Fatal(err)
	}
	return a, root
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
