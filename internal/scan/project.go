// Package scan finds projects under the root and reads their state: stack,
// git, running processes, disk use and activity. It only reads; it never
// changes anything on disk except its own size cache.
package scan

import (
	"strings"
	"time"
)

type Kind int

const (
	KindRepo  Kind = iota // a git repo
	KindGroup             // a folder that only holds repos, e.g. acme
	KindPlain             // a top-level folder with no git anywhere inside
)

type Project struct {
	Rel      string // path relative to root, e.g. "acme/web"
	Abs      string
	Name     string
	Depth    int // 1 for top-level folders
	Kind     Kind
	Archived bool // inside the archive folder

	Stack        []string
	Docker       bool
	NotInstalled bool // package.json but no node_modules
	Port         int  // expected dev port, 0 if unknown
	PortSource   string

	Git        GitState
	LastChange time.Time
	Activity   string // active | warm | dormant | abandoned | ""

	Running    []Listener
	Containers []string

	Size *Sizes // nil until measured
}

type GitState struct {
	Branch     string
	Upstream   string
	Ahead      int
	Behind     int
	Dirty      int
	Untracked  int
	LastCommit time.Time
	Subject    string
	Remote     string
	Host       string
	Owner      string
	Cloning    bool
	NoCommits  bool
	Err        string
}

type Listener struct {
	PID     int
	Command string
	Port    int
	Cwd     string
}

type Sizes struct {
	Total     int64            `json:"total"`
	Build     int64            `json:"build"`
	BuildDirs map[string]int64 `json:"build_dirs"`
	ScannedAt time.Time        `json:"scanned_at"`
}

// StackLabel is the primary stack plus any secondary ones, e.g. "Tauri".
func (p *Project) StackLabel() string {
	if len(p.Stack) == 0 {
		return ""
	}
	return strings.Join(p.Stack, "+")
}

// IsRunning reports whether any listener or container belongs to the project.
func (p *Project) IsRunning() bool {
	return len(p.Running) > 0 || len(p.Containers) > 0
}

// Unpushed reports commits ahead of the upstream, or a branch with no upstream.
func (p *Project) Unpushed() bool {
	g := p.Git
	if p.Kind != KindRepo || g.Cloning || g.NoCommits {
		return false
	}
	return g.Ahead > 0 || (g.Upstream == "" && g.Remote != "")
}
