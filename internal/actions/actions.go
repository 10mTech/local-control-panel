// Package actions changes things: it opens projects in herdr and starts and
// stops dev servers. Every action writes its intent to the audit log before
// it runs and its result after, and passes the safety checks first.
package actions

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/oklog/ulid/v2"

	"local-control-panel/internal/audit"
	"local-control-panel/internal/config"
	"local-control-panel/internal/herdr"
	"local-control-panel/internal/safety"
	"local-control-panel/internal/scan"
)

type Actions struct {
	cfg       *config.Config
	herdr     *herdr.Client
	audit     *audit.Logger
	log       *log.Logger
	Runs      *Runs
	Approvals *Approvals
}

func New(cfg *config.Config, h *herdr.Client, a *audit.Logger, debug *log.Logger, stateDir string) (*Actions, error) {
	runs, err := OpenRuns(stateDir)
	if err != nil {
		return nil, err
	}
	return &Actions{cfg: cfg, herdr: h, audit: a, log: debug, Runs: runs, Approvals: OpenApprovals(stateDir)}, nil
}

// Result is what the panel shows after an action: a one-line message, or the error.
type Result struct {
	Msg       string
	Err       error
	Remeasure string // a project folder whose size changed
}

func fail(op *audit.Op, details map[string]any, err error) Result {
	if op != nil {
		_ = op.End("failed", nil, details, err)
	}
	return Result{Err: err}
}

func (a *Actions) begin(action, trigger string, p *scan.Project, argv []string, details map[string]any) (*audit.Op, error) {
	return a.audit.Begin(audit.Entry{
		Action: action, Project: p.Rel, Remote: p.Git.Remote, Cwd: p.Abs,
		Argv: argv, Trigger: trigger, Details: details,
	})
}

// Cancelled records an action you declined at the confirmation prompt.
func (a *Actions) Cancelled(action, trigger string, p *scan.Project, argv []string, details map[string]any) {
	op, err := a.begin(action, trigger, p, argv, details)
	if err == nil {
		err = op.End("cancelled", nil, nil, nil)
	}
	if err != nil {
		a.log.Printf("audit: %v", err)
	}
}

// DevCommand returns the dev command for a project: config.toml first, then
// one you approved before, then a guess that must be confirmed.
func (a *Actions) DevCommand(p *scan.Project) DevCommand {
	if argv := a.cfg.Project(p.Rel).Dev; len(argv) > 0 {
		return DevCommand{argv, "config"}
	}
	if argv := a.Approvals.Get(p.Rel); len(argv) > 0 {
		return DevCommand{argv, "approved"}
	}
	if argv := Propose(p); argv != nil {
		return DevCommand{argv, "proposed"}
	}
	return DevCommand{nil, "none"}
}

// ensureWorkspace finds the herdr workspace labelled with the project path,
// or creates one. rootPane is set only when it was just created, because
// only then is that pane known to be an idle shell.
func (a *Actions) ensureWorkspace(ctx context.Context, h *herdr.Client, p *scan.Project, focus bool) (ws herdr.Workspace, rootPane string, created bool, err error) {
	list, err := h.Workspaces(ctx)
	if err != nil {
		return ws, "", false, err
	}
	for _, w := range list {
		if w.Label == p.Rel {
			if focus {
				err = h.FocusWorkspace(ctx, w.ID)
			}
			return w, "", false, err
		}
	}
	ws, root, err := h.CreateWorkspace(ctx, p.Abs, p.Rel, focus)
	return ws, root.ID, true, err
}

// freshPane returns an idle shell pane in the project's workspace: the root
// pane of a new workspace, or a new split in an existing one.
func (a *Actions) freshPane(ctx context.Context, h *herdr.Client, p *scan.Project, direction string, focus bool) (herdr.Workspace, string, error) {
	ws, root, created, err := a.ensureWorkspace(ctx, h, p, focus)
	if err != nil || created {
		return ws, root, err
	}
	panes, err := h.Panes(ctx, ws.ID)
	if err != nil {
		return ws, "", err
	}
	if len(panes) == 0 {
		return ws, "", fmt.Errorf("workspace %s has no panes", ws.ID)
	}
	base := panes[0]
	for _, pn := range panes {
		if pn.Focused {
			base = pn
		}
	}
	pn, err := h.Split(ctx, base.ID, direction, p.Abs, focus)
	return ws, pn.ID, err
}

// devPane reuses the project's "dev" pane from an earlier run when its shell
// is idle, so repeated runs do not keep splitting the workspace.
func (a *Actions) devPane(ctx context.Context, h *herdr.Client, p *scan.Project) (herdr.Workspace, string, error) {
	ws, root, created, err := a.ensureWorkspace(ctx, h, p, false)
	if err != nil || created {
		return ws, root, err
	}
	if panes, err := h.Panes(ctx, ws.ID); err == nil {
		for _, pn := range panes {
			if pn.Label != "dev" {
				continue
			}
			if info, err := h.ProcessInfo(ctx, pn.ID); err == nil && shellIdle(info) {
				return ws, pn.ID, nil
			}
		}
	}
	return a.freshPane(ctx, h, p, "down", false)
}

// shellIdle reports whether only the pane's shell is in the foreground.
func shellIdle(info herdr.ProcessInfo) bool {
	for _, fp := range info.Foreground {
		if fp.PID != info.ShellPID {
			return false
		}
	}
	return info.ShellPID > 0
}

func (a *Actions) checkPath(p *scan.Project) error {
	return safety.Inside(p.Abs, a.cfg.Root, a.cfg.Archive)
}

// Open focuses the project's herdr workspace, creating it if needed.
func (a *Actions) Open(ctx context.Context, p *scan.Project, trigger string) Result {
	var calls []string
	h := a.herdr.WithTrace(&calls)
	op, err := a.begin("open", trigger, p, nil, nil)
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}
	if err := a.checkPath(p); err != nil {
		return fail(op, nil, err)
	}
	ws, _, created, err := a.ensureWorkspace(ctx, h, p, true)
	details := map[string]any{"workspace_id": ws.ID, "created": created, "herdr": calls}
	if err != nil {
		return fail(op, details, err)
	}
	_ = op.End("ok", nil, details, nil)
	if created {
		return Result{Msg: "opened new workspace " + p.Rel}
	}
	return Result{Msg: "switched to workspace " + p.Rel}
}

// FocusRun switches herdr to the workspace a run was started in.
func (a *Actions) FocusRun(ctx context.Context, r Run, trigger string) Result {
	var calls []string
	h := a.herdr.WithTrace(&calls)
	op, err := a.audit.Begin(audit.Entry{Action: "open", Project: r.Project, Cwd: r.Cwd, Trigger: trigger,
		Details: map[string]any{"run_id": r.ID, "workspace_id": r.WorkspaceID}})
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}
	err = h.FocusWorkspace(ctx, r.WorkspaceID)
	details := map[string]any{"run_id": r.ID, "workspace_id": r.WorkspaceID, "herdr": calls}
	if err != nil {
		return fail(op, details, err)
	}
	_ = op.End("ok", nil, details, nil)
	return Result{Msg: "switched to workspace " + r.Project}
}

// Finder shows the project folder in a Finder window. It only opens the
// folder and needs no herdr.
func (a *Actions) Finder(ctx context.Context, p *scan.Project, trigger string) Result {
	argv := []string{"open", p.Abs}
	op, err := a.begin("finder", trigger, p, argv, nil)
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}
	if err := a.checkPath(p); err != nil {
		return fail(op, nil, err)
	}
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return fail(op, nil, fmt.Errorf("%v %s", err, strings.TrimSpace(string(out))))
	}
	_ = op.End("ok", nil, nil, nil)
	return Result{Msg: "opened " + p.Rel + " in Finder"}
}

// Tool opens a pane in the project's workspace running command, e.g. the
// editor or Claude Code. label names the pane in herdr.
func (a *Actions) Tool(ctx context.Context, p *scan.Project, action, label, command, trigger string) Result {
	var calls []string
	h := a.herdr.WithTrace(&calls)
	// The command is split into words and quoted again, so the pane's shell
	// runs exactly those words and expands nothing.
	argv, splitErr := safety.SplitWords(command)
	op, err := a.begin(action, trigger, p, argv, nil)
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}
	if splitErr != nil || len(argv) == 0 {
		return fail(op, nil, fmt.Errorf("%s command %q is not a command: %v", label, command, splitErr))
	}
	if err := a.checkPath(p); err != nil {
		return fail(op, nil, err)
	}
	ws, pane, err := a.freshPane(ctx, h, p, "right", true)
	details := map[string]any{"workspace_id": ws.ID, "pane_id": pane, "herdr": calls}
	if err == nil {
		_ = h.Rename(ctx, pane, label) // cosmetic; a failure is in the trace
		err = h.Run(ctx, pane, safety.ShellJoin(argv))
	}
	details["herdr"] = calls
	if err != nil {
		return fail(op, details, err)
	}
	_ = op.End("ok", nil, details, nil)
	return Result{Msg: fmt.Sprintf("%s opened in %s (pane %s)", label, p.Rel, pane)}
}

// ActiveRun returns the panel-started run for a project that is still alive.
func (a *Actions) ActiveRun(rel string) *Run {
	for _, r := range a.Runs.List() {
		if r.Project == rel && r.Stopped.IsZero() && r.Alive() {
			return &r
		}
	}
	return nil
}

// StartRun runs the dev command in a new pane of the project's workspace,
// with all output teed into the run log, and records the PID.
func (a *Actions) StartRun(ctx context.Context, p *scan.Project, dev DevCommand, trigger string) Result {
	var calls []string
	h := a.herdr.WithTrace(&calls)
	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
	logPath := a.Runs.path(id, ".log")
	op, err := a.begin("run", trigger, p, dev.Argv, map[string]any{"run_id": id, "dev_source": dev.Source, "log": logPath})
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}
	if err := a.checkPath(p); err != nil {
		return fail(op, nil, err)
	}
	if len(dev.Argv) == 0 {
		return fail(op, nil, errors.New("no dev command"))
	}
	if r := a.ActiveRun(p.Rel); r != nil {
		return fail(op, map[string]any{"existing_run": r.ID, "pid": r.PID}, fmt.Errorf("already running (pid %d); stop it first", r.PID))
	}

	// The log and the run record get the command masked; only the pane sees it whole.
	shown := audit.MaskArgv(dev.Argv)
	header := fmt.Sprintf("# local-control-panel run %s · %s · %s\n# cwd %s\n", id, time.Now().Format(time.RFC3339), safety.ShellJoin(shown), p.Abs)
	if err := os.WriteFile(logPath, []byte(header), 0o600); err != nil {
		return fail(op, nil, err)
	}

	// The wrapper writes its own PID, then execs the command, so the PID on
	// record is the dev server itself and it leads the pipeline's process group.
	pidPath := a.Runs.path(id, ".pid")
	shell := fmt.Sprintf(`sh -c 'echo $$ > "$0"; exec "$@"' %s %s 2>&1 | tee -a %s`,
		safety.ShellQuote(pidPath), safety.ShellJoin(dev.Argv), safety.ShellQuote(logPath))

	ws, pane, err := a.devPane(ctx, h, p)
	run := &Run{ID: id, Project: p.Rel, Cwd: p.Abs, Argv: shown, Shell: audit.Mask(shell),
		WorkspaceID: ws.ID, PaneID: pane, Port: p.Port, Log: logPath, Started: time.Now()}
	details := func() map[string]any {
		return map[string]any{"run_id": id, "workspace_id": run.WorkspaceID, "pane_id": run.PaneID,
			"pid": run.PID, "port": run.Port, "log": logPath, "dev_source": dev.Source, "herdr": calls}
	}
	if err == nil {
		_ = h.Rename(ctx, pane, "dev")
		err = h.Run(ctx, pane, shell)
	}
	if err != nil {
		return fail(op, details(), err)
	}

	pid := waitPID(pidPath, 10*time.Second)
	os.Remove(pidPath)
	if pid > 0 {
		run.PID = pid
		run.ProcStart, _ = procStart(pid)
		run.PGID, _ = procGroup(pid)
	}
	if err := a.Runs.Save(run); err != nil {
		a.log.Printf("save run %s: %v", id, err)
	}
	if pid == 0 {
		return fail(op, details(), errors.New("no PID after 10s; the command may not have started, check the pane"))
	}
	if run.ProcStart == "" {
		return fail(op, details(), fmt.Errorf("pid %d exited right after starting; see the run log", pid))
	}
	_ = op.End("ok", nil, details(), nil)
	return Result{Msg: fmt.Sprintf("started %s in %s (pid %d)", safety.ShellJoin(shown), p.Rel, pid)}
}

func waitPID(path string, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0
}

// StopRun stops a run the panel started: Ctrl-C to its pane first, then
// SIGTERM and finally SIGKILL to the recorded process group, each only after
// checking the PID still belongs to the same process. Nothing else is touched.
func (a *Actions) StopRun(ctx context.Context, r Run, trigger string) Result {
	var calls []string
	h := a.herdr.WithTrace(&calls)
	op, err := a.audit.Begin(audit.Entry{Action: "stop", Project: r.Project, Cwd: r.Cwd, Argv: r.Argv, Trigger: trigger,
		Details: map[string]any{"run_id": r.ID, "pid": r.PID, "pgid": r.PGID, "pane_id": r.PaneID}})
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}

	var steps []string
	how := ""
	switch {
	case !r.Alive():
		how = "had already exited"
	default:
		if err := h.SendKeys(ctx, r.PaneID, "ctrl+c"); err != nil {
			steps = append(steps, "ctrl+c to pane failed: "+err.Error())
		} else {
			steps = append(steps, "ctrl+c to pane "+r.PaneID)
		}
		if waitExit(&r, 5*time.Second) {
			how = "exited after ctrl+c"
			break
		}
		for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
			name := map[syscall.Signal]string{syscall.SIGTERM: "SIGTERM", syscall.SIGKILL: "SIGKILL"}[sig]
			if !r.Alive() { // re-checks PID and start time right before signalling
				how = "exited"
				break
			}
			target := r.PID
			if r.PGID == r.PID {
				target = -r.PGID // the whole group the panel started: server, its children, tee
			}
			if err := syscall.Kill(target, sig); err != nil {
				steps = append(steps, fmt.Sprintf("%s to %d failed: %v", name, target, err))
				continue
			}
			steps = append(steps, fmt.Sprintf("%s to %d", name, target))
			if waitExit(&r, 5*time.Second) {
				how = "exited after " + name
				break
			}
		}
	}

	details := map[string]any{"run_id": r.ID, "pid": r.PID, "steps": steps, "herdr": calls}
	if how == "" {
		return fail(op, details, fmt.Errorf("pid %d is still running", r.PID))
	}
	details["exit"] = how
	r.Stopped, r.StopNote = time.Now(), how
	if err := a.Runs.Save(&r); err != nil {
		a.log.Printf("save run %s: %v", r.ID, err)
	}
	_ = op.End("ok", nil, details, nil)
	return Result{Msg: fmt.Sprintf("stopped %s: %s", r.Project, how)}
}

func waitExit(r *Run, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !r.Alive() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return !r.Alive()
}
