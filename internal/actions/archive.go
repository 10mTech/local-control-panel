package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"local-control-panel/internal/safety"
	"local-control-panel/internal/scan"
)

// ArchiveMove is where archive or restore would move a project.
type ArchiveMove struct {
	From, To string
	Restore  bool // moving out of the archive back under the root
}

// PlanArchive works out the move. A project keeps its path: acme/web
// goes to archive/acme/web, and restore moves it back.
func (a *Actions) PlanArchive(p *scan.Project) (ArchiveMove, error) {
	mv := ArchiveMove{From: p.Abs}
	if err := a.checkPath(p); err != nil {
		return mv, err
	}
	if p.Archived {
		rel, err := filepath.Rel(a.cfg.Archive, p.Abs)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return mv, errors.New("the archive folder itself can't be restored")
		}
		mv.To, mv.Restore = filepath.Join(a.cfg.Root, rel), true
	} else {
		if inside(a.cfg.Archive, p.Abs) {
			return mv, errors.New("this folder contains the archive; archive the projects inside it instead")
		}
		mv.To = filepath.Join(a.cfg.Archive, p.Rel)
	}
	if _, err := os.Lstat(mv.To); err == nil {
		return mv, fmt.Errorf("%s already exists; move or rename it first", mv.To)
	}
	return mv, nil
}

// inside reports whether path is dir or below it.
func inside(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// existingAncestor returns path, or its deepest parent that exists.
func existingAncestor(path string) string {
	for {
		if _, err := os.Lstat(path); err == nil {
			return path
		}
		up := filepath.Dir(path)
		if up == path {
			return path
		}
		path = up
	}
}

// Archive moves a project into the archive folder, or back out of it. It is a
// rename on the same disk; nothing is copied or deleted.
func (a *Actions) Archive(ctx context.Context, p *scan.Project, mv ArchiveMove, trigger string) Result {
	action := "archive"
	if mv.Restore {
		action = "restore"
	}
	details := map[string]any{"from": mv.From, "to": mv.To}
	op, err := a.begin(action, trigger, p, nil, details)
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}
	if err := safety.Inside(mv.From, a.cfg.Root, a.cfg.Archive); err != nil {
		return fail(op, details, err)
	}
	for _, r := range a.Runs.List() {
		if (r.Project == p.Rel || strings.HasPrefix(r.Project, p.Rel+"/")) && r.Stopped.IsZero() && r.Alive() {
			return fail(op, details, fmt.Errorf("%s has a panel run (pid %d); stop it first", r.Project, r.PID))
		}
	}
	if _, err := os.Lstat(mv.To); err == nil {
		return fail(op, details, fmt.Errorf("%s already exists", mv.To))
	}
	// Check before creating anything: MkdirAll follows symlinks, so a linked
	// folder on the way could otherwise create folders outside the roots.
	parent := filepath.Dir(mv.To)
	if err := safety.Inside(existingAncestor(parent), a.cfg.Root, a.cfg.Archive); err != nil {
		return fail(op, details, err)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fail(op, details, err)
	}
	if err := safety.Inside(parent, a.cfg.Root, a.cfg.Archive); err != nil {
		return fail(op, details, err)
	}
	if err := os.Rename(mv.From, mv.To); err != nil {
		return fail(op, details, err)
	}
	_ = op.End("ok", nil, details, nil)
	if mv.Restore {
		return Result{Remeasure: mv.To, Msg: "restored " + p.Rel + " to " + mv.To}
	}
	return Result{Remeasure: mv.To, Msg: "archived " + p.Rel + " (A on it in archive/ restores it)"}
}
