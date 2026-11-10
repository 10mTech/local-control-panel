package actions

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"local-control-panel/internal/audit"
)

// PruneRuns keeps the runs folder bounded. Finished runs and fetch logs past
// the newest keep are removed (record and log together), and finished logs
// over maxBytes are cut down to their last maxBytes. A run that is still alive
// is never touched, however old or large. The audit log itself is never
// pruned; this records what it removed there.
func (a *Actions) PruneRuns(keep int, maxBytes int64) {
	alive := map[string]bool{}
	for _, r := range a.Runs.List() {
		if r.Stopped.IsZero() && r.Alive() {
			alive[r.ID] = true
		}
	}

	// Every id with a record or a log; fetch-all logs have no record.
	files, _ := filepath.Glob(filepath.Join(a.Runs.dir, "*"))
	seen := map[string]bool{}
	var ids []string
	for _, f := range files {
		id := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(f), ".log"), ".json")
		if strings.Contains(id, ".") || seen[id] {
			continue // .json.tmp and the like
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids))) // ULIDs: newest first

	var removed, trimmed []string
	var freed int64
	kept := 0
	for _, id := range ids {
		if alive[id] {
			continue
		}
		logPath := a.Runs.path(id, ".log")
		if kept < keep {
			kept++
			if n, err := trimLog(logPath, maxBytes); err != nil {
				a.log.Printf("trim %s: %v", logPath, err)
			} else if n > 0 {
				trimmed = append(trimmed, id)
				freed += n
			}
			continue
		}
		for _, ext := range []string{".json", ".log"} {
			p := a.Runs.path(id, ext)
			st, err := os.Lstat(p)
			if err != nil {
				continue
			}
			if err := os.Remove(p); err != nil {
				a.log.Printf("prune %s: %v", p, err)
				continue
			}
			freed += st.Size()
		}
		removed = append(removed, id)
	}

	if len(removed) == 0 && len(trimmed) == 0 {
		return
	}
	err := a.audit.Event(audit.Entry{Action: "runs.prune", Cwd: a.Runs.dir, Trigger: "startup", Outcome: "ok",
		Details: map[string]any{"removed": removed, "trimmed": trimmed, "freed_bytes": freed,
			"keep": keep, "max_log_bytes": maxBytes}})
	if err != nil {
		a.log.Printf("audit: %v", err)
	}
}

// trimLog cuts a log down to its last max bytes, starting at a line break,
// behind a note saying how much was dropped. It returns the bytes removed.
func trimLog(path string, max int64) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() <= max {
		return 0, err
	}
	b := make([]byte, max)
	if _, err := f.ReadAt(b, st.Size()-max); err != nil && err != io.EOF {
		return 0, err
	}
	if i := strings.IndexByte(string(b), '\n'); i >= 0 {
		b = b[i+1:]
	}
	dropped := st.Size() - int64(len(b))
	note := fmt.Sprintf("# local-control-panel: trimmed, first %s of this log removed\n", HumanBytes(dropped))
	if dropped <= int64(len(note)) {
		return 0, nil // barely over: the note would make the file no smaller
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte(note), b...), 0o600); err != nil {
		return 0, err
	}
	return dropped - int64(len(note)), os.Rename(tmp, path)
}
