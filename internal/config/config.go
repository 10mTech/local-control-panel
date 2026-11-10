// Package config loads ~/.config/local-control-panel/config.toml and
// resolves the panel's state folder.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const AppName = "local-control-panel"

type Activity struct {
	Active  int `toml:"active"`
	Warm    int `toml:"warm"`
	Dormant int `toml:"dormant"`
}

type RunLogs struct {
	Keep     int `toml:"keep"`
	MaxLogMB int `toml:"max_log_mb"`
}

type Project struct {
	Dev    []string `toml:"dev"`
	Port   int      `toml:"port"`
	Ignore bool     `toml:"ignore"`
}

type Config struct {
	Root            string             `toml:"root"`
	ScanDepth       int                `toml:"scan_depth"`
	Archive         string             `toml:"archive"`
	Editor          string             `toml:"editor"`
	RefreshSeconds  int                `toml:"refresh_seconds"`
	GitHubUsers     []string           `toml:"github_users"`
	Activity        Activity           `toml:"activity"`
	Runs            RunLogs            `toml:"runs"`
	StateDir        string             `toml:"state_dir"`
	Projects        map[string]Project `toml:"projects"`
	Path            string             `toml:"-"` // file this was loaded from
	CreatedDefaults bool               `toml:"-"` // true when the file was written on this run
	Unknown         []string           `toml:"-"` // keys in the file the panel does not know, e.g. typos
}

const defaultFile = `# local-control-panel config. Everything here is optional.
root = "~/Projects"                 # the folder that holds your projects
scan_depth = 3                      # how deep to look for git repos
archive = "~/Projects/archive"      # must be inside root
editor = "nvim"
refresh_seconds = 10                # git + port refresh while the panel is open
github_users = []                   # e.g. ["your-github-name"]; remotes owned by anyone else show as "not yours"

[activity]                          # days since last change
active = 14
warm = 90
dormant = 365                       # older = abandoned

[runs]                              # dev-server and fetch logs in the state folder
keep = 50                           # finished runs kept; older ones are removed at startup
max_log_mb = 20                     # finished logs are cut to their last 20 MB

# Per-project overrides, keyed by path relative to root:
# [projects."acme/web"]
# dev = ["pnpm", "tauri", "dev"]
# port = 1420
#
# [projects."scratch"]
# ignore = true
`

// Dir returns $XDG_CONFIG_HOME/local-control-panel, by default
// ~/.config/local-control-panel.
func Dir() string {
	return xdgDir("XDG_CONFIG_HOME", ".config")
}

// StateDir returns $XDG_STATE_HOME/local-control-panel, by default
// ~/.local/state/local-control-panel.
func StateDir() string {
	return xdgDir("XDG_STATE_HOME", filepath.Join(".local", "state"))
}

// xdgDir follows the XDG base directory spec: the variable is used only when
// it holds an absolute path, otherwise the folder under home.
func xdgDir(env, fallback string) string {
	if d := os.Getenv(env); filepath.IsAbs(d) {
		return filepath.Join(d, AppName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback, AppName)
}

// Load reads the config file, writing the defaults first if none exists.
// path overrides the default location; that file must already exist.
func Load(path string) (*Config, error) {
	created := false
	if path != "" {
		path = expand(path)
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
	} else {
		path = filepath.Join(Dir(), "config.toml")
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(Dir(), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(defaultFile), 0o644); err != nil {
			return nil, err
		}
		created = true
	}

	var c Config
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, k := range md.Undecoded() {
		c.Unknown = append(c.Unknown, k.String())
	}
	c.Path = path
	c.CreatedDefaults = created
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// validate rejects settings the safety checks rely on not having: a relative
// root, and an archive outside root, which the scan would never show, so
// nothing archived there could be restored.
func (c *Config) validate() error {
	if !filepath.IsAbs(c.Root) {
		return fmt.Errorf("root %q must be an absolute path or start with ~/", c.Root)
	}
	rel, err := filepath.Rel(c.Root, c.Archive)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("archive %q must be a folder inside root %q", c.Archive, c.Root)
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.Root == "" {
		c.Root = "~/Projects"
	}
	c.Root = expand(c.Root)
	if c.Archive == "" {
		c.Archive = filepath.Join(c.Root, "archive")
	}
	c.Archive = expand(c.Archive)
	if c.ScanDepth <= 0 {
		c.ScanDepth = 3
	}
	if c.Editor == "" {
		c.Editor = "nvim"
	}
	if c.RefreshSeconds <= 0 {
		c.RefreshSeconds = 10
	}
	if c.Activity.Active <= 0 {
		c.Activity.Active = 14
	}
	if c.Activity.Warm <= 0 {
		c.Activity.Warm = 90
	}
	if c.Activity.Dormant <= 0 {
		c.Activity.Dormant = 365
	}
	if c.StateDir == "" {
		c.StateDir = StateDir()
	}
	c.StateDir = expand(c.StateDir)
	if c.Runs.Keep <= 0 {
		c.Runs.Keep = 50
	}
	if c.Runs.MaxLogMB <= 0 {
		c.Runs.MaxLogMB = 20
	}
	if c.Projects == nil {
		c.Projects = map[string]Project{}
	}
}

// Project returns the override for a path relative to root.
func (c *Config) Project(rel string) Project {
	return c.Projects[rel]
}

// IsMine reports whether a remote owner is one of the configured accounts.
func (c *Config) IsMine(owner string) bool {
	for _, u := range c.GitHubUsers {
		if strings.EqualFold(u, owner) {
			return true
		}
	}
	return false
}

func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return filepath.Clean(p)
}
