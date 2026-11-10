package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points HOME and the XDG variables at a temp folder.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	return home
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		toml    string
		wantErr string
	}{
		{"defaults", ``, ""},
		{"root with ~/", `root = "~/Work"`, ""},
		{"archive inside root", "root = \"/r\"\narchive = \"/r/old/stuff\"", ""},
		{"archive name starting with dots", "root = \"/r\"\narchive = \"/r/..old\"", ""},
		{"relative root", `root = "projects"`, "must be an absolute path"},
		{"archive outside root", "root = \"/r\"\narchive = \"/elsewhere\"", "inside root"},
		{"archive is the parent of root", "root = \"/r/p\"\narchive = \"/r\"", "inside root"},
		{"archive is root", "root = \"/r\"\narchive = \"/r\"", "inside root"},
		{"look-alike sibling of root", "root = \"/r\"\narchive = \"/r2/archive\"", "inside root"},
		{"archive reaches out with ..", "root = \"/r\"\narchive = \"/r/../x\"", "inside root"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			_, err := Load(writeConfig(t, tc.toml))
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	home := isolate(t)
	c, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if c.Root != filepath.Join(home, "Projects") || c.Archive != filepath.Join(home, "Projects", "archive") {
		t.Errorf("root/archive = %q, %q", c.Root, c.Archive)
	}
	if c.ScanDepth != 3 || c.Editor != "nvim" || c.RefreshSeconds != 10 {
		t.Errorf("defaults not applied: %+v", c)
	}
	if c.Activity != (Activity{Active: 14, Warm: 90, Dormant: 365}) || c.Runs != (RunLogs{Keep: 50, MaxLogMB: 20}) {
		t.Errorf("activity/runs defaults: %+v %+v", c.Activity, c.Runs)
	}
	if want := filepath.Join(home, ".local", "state", AppName); c.StateDir != want {
		t.Errorf("StateDir = %q, want %q", c.StateDir, want)
	}
	if c.Projects == nil {
		t.Error("Projects is nil")
	}
}

func TestLoadUnknownKeys(t *testing.T) {
	isolate(t)
	c, err := Load(writeConfig(t, "root = \"/r\"\narchive = \"/r/a\"\nscan_dpeth = 5\n[activity]\nactve = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(c.Unknown, ",")
	if !strings.Contains(got, "scan_dpeth") || !strings.Contains(got, "activity.actve") || len(c.Unknown) != 2 {
		t.Errorf("Unknown = %q", c.Unknown)
	}
	if c.ScanDepth != 3 {
		t.Errorf("a misspelt key must not set the value; ScanDepth = %d", c.ScanDepth)
	}
}

func TestLoadNoUnknownKeys(t *testing.T) {
	isolate(t)
	c, err := Load(writeConfig(t, defaultFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Unknown) != 0 {
		t.Errorf("the default file has unknown keys: %q", c.Unknown)
	}
}

func TestLoadProjectOverrides(t *testing.T) {
	isolate(t)
	c, err := Load(writeConfig(t, "[projects.\"acme/web\"]\ndev = [\"pnpm\", \"dev\"]\nport = 1420\n[projects.scratch]\nignore = true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p := c.Project("acme/web"); p.Port != 1420 || len(p.Dev) != 2 || p.Ignore {
		t.Errorf("acme/web = %+v", p)
	}
	if !c.Project("scratch").Ignore || c.Project("missing").Port != 0 {
		t.Error("project overrides wrong")
	}
}

func TestLoadBadFiles(t *testing.T) {
	isolate(t)
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Error("an explicit path that does not exist must fail, not be created")
	}
	if _, err := Load(writeConfig(t, "root = ")); err == nil {
		t.Error("invalid TOML must fail")
	}
}

func TestLoadWritesDefaultsOnce(t *testing.T) {
	home := isolate(t)
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", AppName, "config.toml")
	if !c.CreatedDefaults || c.Path != want {
		t.Fatalf("CreatedDefaults=%v Path=%q, want true and %q", c.CreatedDefaults, c.Path, want)
	}
	c, err = Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.CreatedDefaults {
		t.Error("second load reported creating the file again")
	}
}

func TestXDGDirs(t *testing.T) {
	home := isolate(t)
	abs := t.TempDir()
	tests := []struct {
		name, cfg, state, wantCfg, wantState string
	}{
		{"unset", "", "", filepath.Join(home, ".config", AppName), filepath.Join(home, ".local", "state", AppName)},
		{"absolute", abs, abs, filepath.Join(abs, AppName), filepath.Join(abs, AppName)},
		{"relative is ignored", "rel/cfg", "rel/state", filepath.Join(home, ".config", AppName), filepath.Join(home, ".local", "state", AppName)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tc.cfg)
			t.Setenv("XDG_STATE_HOME", tc.state)
			if got := Dir(); got != tc.wantCfg {
				t.Errorf("Dir() = %q, want %q", got, tc.wantCfg)
			}
			if got := StateDir(); got != tc.wantState {
				t.Errorf("StateDir() = %q, want %q", got, tc.wantState)
			}
		})
	}
}

func TestIsMine(t *testing.T) {
	c := &Config{GitHubUsers: []string{"Acme", "bob"}}
	for owner, want := range map[string]bool{"acme": true, "BOB": true, "carol": false, "": false} {
		if got := c.IsMine(owner); got != want {
			t.Errorf("IsMine(%q) = %v, want %v", owner, got, want)
		}
	}
}
