package actions

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Run is one dev server the panel started. Records live in <state>/runs as
// <id>.json next to the full output in <id>.log.
type Run struct {
	ID          string    `json:"id"`
	Project     string    `json:"project"`
	Cwd         string    `json:"cwd"`
	Argv        []string  `json:"argv"`
	Shell       string    `json:"shell"` // the line typed into the pane, secrets masked
	WorkspaceID string    `json:"workspace_id"`
	PaneID      string    `json:"pane_id"`
	PID         int       `json:"pid"`
	PGID        int       `json:"pgid"`
	ProcStart   string    `json:"proc_start"` // ps lstart, guards against PID reuse
	Port        int       `json:"port,omitempty"`
	Log         string    `json:"log"`
	Started     time.Time `json:"started"`
	Stopped     time.Time `json:"stopped,omitzero"`
	StopNote    string    `json:"stop_note,omitempty"`
}

// Alive reports whether the recorded process is still the one the panel
// started: same PID and same start time.
func (r *Run) Alive() bool {
	if r.PID <= 0 {
		return false
	}
	if err := syscall.Kill(r.PID, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	st, err := procStart(r.PID)
	return err == nil && st == r.ProcStart
}

type Runs struct{ dir string }

func OpenRuns(stateDir string) (*Runs, error) {
	dir := filepath.Join(stateDir, "runs")
	return &Runs{dir: dir}, os.MkdirAll(dir, 0o700)
}

func (s *Runs) Dir() string { return s.dir }

func (s *Runs) path(id, ext string) string { return filepath.Join(s.dir, id+ext) }

func (s *Runs) Save(r *Run) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(r.ID, ".json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(r.ID, ".json"))
}

// List returns every recorded run, newest first.
func (s *Runs) List() []Run {
	files, _ := filepath.Glob(filepath.Join(s.dir, "*.json"))
	var out []Run
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r Run
		if json.Unmarshal(b, &r) == nil && r.ID != "" {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out
}

// procStart is the process start time as ps prints it, e.g.
// "Thu Oct 30 14:02:11 2026".
func procStart(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", errors.New("no such process")
	}
	return s, nil
}

func procGroup(pid int) (int, error) {
	out, err := exec.Command("ps", "-o", "pgid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// ProcGroups maps each PID to its process group, in one ps call.
func ProcGroups(pids []int) map[int]int {
	out := map[int]int{}
	if len(pids) == 0 {
		return out
	}
	s := make([]string, len(pids))
	for i, p := range pids {
		s[i] = strconv.Itoa(p)
	}
	b, _ := exec.Command("ps", "-o", "pid=,pgid=", "-p", strings.Join(s, ",")).Output()
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			pid, _ := strconv.Atoi(f[0])
			pg, _ := strconv.Atoi(f[1])
			out[pid] = pg
		}
	}
	return out
}

// Tail returns up to n final lines of a run log, as written.
func Tail(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const window = 256 << 10
	off := max(0, st.Size()-window)
	b := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(b, off); err != nil {
		return nil, err
	}
	if off > 0 {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
