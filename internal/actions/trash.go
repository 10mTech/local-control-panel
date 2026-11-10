package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"local-control-panel/internal/safety"
	"local-control-panel/internal/scan"
)

// CanTrash reports why a project can't be moved to the Trash, or nil.
func (a *Actions) CanTrash(p *scan.Project) error {
	if err := a.checkPath(p); err != nil {
		return err
	}
	switch {
	case p.Abs == a.cfg.Root:
		return errors.New("that is the projects folder itself")
	case inside(a.cfg.Archive, p.Abs):
		return errors.New("this folder holds the archive; trash the projects inside it instead")
	}
	return nil
}

// Trash moves a whole project folder to the macOS Trash, where Finder's
// Put Back restores it. The UI asks for the project's path to be typed out
// first; warnings shown at that prompt are recorded with the action.
func (a *Actions) Trash(ctx context.Context, p *scan.Project, warnings []string, trigger string) Result {
	argv := []string{trashBin, p.Abs}
	details := map[string]any{"path": p.Abs, "warnings": warnings}
	if p.Size != nil {
		details["size"] = p.Size.Total
	}
	op, err := a.begin("trash", trigger, p, argv, details)
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}
	if err := a.CanTrash(p); err != nil {
		return fail(op, details, err)
	}
	if err := safety.Inside(filepath.Dir(p.Abs), a.cfg.Root, a.cfg.Archive); err != nil {
		return fail(op, details, err)
	}
	if st, err := os.Lstat(p.Abs); err != nil || st.Mode()&os.ModeSymlink != 0 {
		return fail(op, details, errors.New("not a real folder any more"))
	}
	for _, r := range a.Runs.List() {
		if (r.Project == p.Rel || strings.HasPrefix(r.Project, p.Rel+"/")) && r.Stopped.IsZero() && r.Alive() {
			return fail(op, details, fmt.Errorf("%s has a panel run (pid %d); stop it first", r.Project, r.PID))
		}
	}
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return fail(op, details, fmt.Errorf("%v %s", err, strings.TrimSpace(string(out))))
	}
	_ = op.End("ok", nil, details, nil)
	return Result{Msg: "moved " + p.Rel + " to the Trash (Finder's Put Back restores it)"}
}
