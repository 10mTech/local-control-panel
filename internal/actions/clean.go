package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"local-control-panel/internal/safety"
	"local-control-panel/internal/scan"
)

// trashBin is macOS's own trash command (macOS 15+). Cleanup moves folders to
// the Trash, never deletes them.
const trashBin = "/usr/bin/trash"

type CleanItem struct {
	Dir  string `json:"dir"` // e.g. node_modules, src-tauri/target
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type CleanSkip struct {
	Dir    string `json:"dir"`
	Reason string `json:"reason"`
}

// CleanPlan is the dry run: what would move to the Trash, and what was
// left alone and why.
type CleanPlan struct {
	Items   []CleanItem `json:"items"`
	Skipped []CleanSkip `json:"skipped,omitempty"`
	Total   int64       `json:"total"`
}

// PlanClean looks for allowlisted build folders directly in the project
// (Safety rule 2). A folder is left out when it is a symlink, holds files
// tracked in git, holds a nested repo, or, outside a git repo, is a dist or
// build folder that cannot be confirmed as generated.
func (a *Actions) PlanClean(ctx context.Context, p *scan.Project) (CleanPlan, error) {
	var plan CleanPlan
	if err := a.checkPath(p); err != nil {
		return plan, err
	}
	if p.Kind == scan.KindGroup {
		return plan, errors.New("a group holds other projects; clean them one by one")
	}
	for _, dir := range scan.BuildDirs {
		path, why := a.cleanCandidate(ctx, p, dir)
		switch {
		case path == "" && why == "":
			continue // not there
		case why != "":
			plan.Skipped = append(plan.Skipped, CleanSkip{dir, why})
		default:
			n := scan.DirSize(path)
			plan.Items = append(plan.Items, CleanItem{Dir: dir, Path: path, Size: n})
			plan.Total += n
		}
	}
	return plan, nil
}

// cleanCandidate returns the folder's path if it may be cleaned, or a reason
// it may not. Both empty means the folder does not exist.
func (a *Actions) cleanCandidate(ctx context.Context, p *scan.Project, dir string) (string, string) {
	path := filepath.Join(p.Abs, dir)
	// Every step of the path must be a real folder, not a link.
	cur := p.Abs
	for _, part := range strings.Split(dir, "/") {
		cur = filepath.Join(cur, part)
		st, err := os.Lstat(cur)
		if err != nil {
			return "", ""
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return "", "symlink, never followed"
		}
		if !st.IsDir() {
			return "", ""
		}
	}
	if err := safety.Inside(path, p.Abs); err != nil {
		return "", err.Error()
	}
	if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
		return "", "is a git repo itself"
	}
	if p.Kind == scan.KindRepo {
		out, _, err := gitRun(ctx, p.Abs, 10*time.Second, "ls-files", "--", dir)
		if err != nil {
			return "", "could not ask git whether it holds tracked files"
		}
		if out != "" {
			return "", "holds files tracked in git"
		}
	} else if dir == "dist" || dir == "build" {
		return "", "no git here, so it can't be confirmed as generated output"
	}
	return path, ""
}

// Clean moves the planned folders to the Trash. Each one is checked again
// right before it moves; the audit result lists every path and its size.
func (a *Actions) Clean(ctx context.Context, p *scan.Project, plan CleanPlan, trigger string) Result {
	op, err := a.begin("clean", trigger, p, nil, map[string]any{"preview": plan})
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}
	if err := a.checkPath(p); err != nil {
		return fail(op, nil, err)
	}
	if r := a.ActiveRun(p.Rel); r != nil {
		return fail(op, nil, fmt.Errorf("a panel run is active (pid %d); stop it first", r.PID))
	}

	var moved []CleanItem
	var failed []string
	var total int64
	for _, it := range plan.Items {
		path, why := a.cleanCandidate(ctx, p, it.Dir)
		if path != it.Path {
			if why == "" {
				why = "gone since the preview"
			}
			failed = append(failed, it.Dir+": "+why)
			continue
		}
		out, err := exec.CommandContext(ctx, trashBin, path).CombinedOutput()
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v %s", it.Dir, err, strings.TrimSpace(string(out))))
			continue
		}
		moved = append(moved, it)
		total += it.Size
	}

	details := map[string]any{"moved": moved, "total": total, "failed": failed, "to": "Trash"}
	if len(failed) > 0 {
		err := fmt.Errorf("%d not moved: %s", len(failed), strings.Join(failed, "; "))
		_ = op.End("failed", nil, details, err)
		return Result{Err: err, Remeasure: p.Abs}
	}
	_ = op.End("ok", nil, details, nil)
	return Result{Remeasure: p.Abs, Msg: fmt.Sprintf("moved %d folders (%s) from %s to the Trash; empty the Trash to free the space",
		len(moved), HumanBytes(total), p.Rel)}
}

// HumanBytes is a short size like 812M or 1.4G.
func HumanBytes(n int64) string {
	const k = 1024
	switch {
	case n < k*k:
		return fmt.Sprintf("%dK", (n+k-1)/k)
	case n < k*k*k:
		return fmt.Sprintf("%.0fM", float64(n)/(k*k))
	default:
		return fmt.Sprintf("%.1fG", float64(n)/(k*k*k))
	}
}
