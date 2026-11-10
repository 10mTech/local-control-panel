package actions

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/10mTech/local-control-panel/internal/audit"
	"github.com/10mTech/local-control-panel/internal/scan"
)

// gitRun runs git without a terminal. It starts in its own session, so
// neither git nor ssh can open /dev/tty and draw a password prompt over the
// panel; anything that needs a password fails instead. On timeout the whole
// session (git and its ssh) is killed.
func gitRun(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		code = ee.ExitCode()
	case err != nil:
		code = -1
	}
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("timed out after %s", timeout)
	}
	return strings.TrimSpace(out.String()), code, err
}

func head(ctx context.Context, dir string) string {
	out, _, err := gitRun(ctx, dir, 5*time.Second, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// lastLines keeps the end of command output for the audit log.
func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// errLine is the most useful single line of git's output for a failure.
func errLine(out string, err error) string {
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "fatal:") || strings.HasPrefix(l, "error:") || strings.Contains(l, "Permission denied") {
			return l
		}
	}
	if out != "" {
		return lastLines(out, 1)
	}
	return err.Error()
}

// Fetchable reports whether fetch-all includes a project: a repo with a
// remote, not cloning, not archived.
func Fetchable(p *scan.Project) bool {
	return p.Kind == scan.KindRepo && !p.Git.Cloning && p.Git.Remote != "" && !p.Archived
}

// FetchAll runs `git fetch --all` in every fetchable repo, 6 at a time. The
// full output of each goes to one log in the runs folder; the audit result
// lists every repo with its outcome.
func (a *Actions) FetchAll(ctx context.Context, ps []scan.Project, trigger string, progress func(done, total int)) Result {
	var targets []scan.Project
	for _, p := range ps {
		if Fetchable(&p) {
			targets = append(targets, p)
		}
	}
	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
	logPath := a.Runs.path(id, ".log")
	argv := []string{"git", "fetch", "--all"}
	op, err := a.audit.Begin(audit.Entry{Action: "git.fetch-all", Cwd: a.cfg.Root, Argv: argv, Trigger: trigger,
		Details: map[string]any{"repos": len(targets), "log": logPath}})
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}

	type res struct {
		Project string `json:"project"`
		OK      bool   `json:"ok"`
		Updated int    `json:"updated_refs,omitempty"`
		Error   string `json:"error,omitempty"`
		MS      int64  `json:"ms"`
		out     string
	}
	results := make([]res, len(targets))
	var mu sync.Mutex
	done := 0
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i := range targets {
		p := targets[i]
		if err := a.checkPath(&p); err != nil {
			results[i] = res{Project: p.Rel, Error: err.Error()}
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			start := time.Now()
			out, _, err := gitRun(ctx, p.Abs, 90*time.Second, "fetch", "--all")
			r := res{Project: p.Rel, OK: err == nil, MS: time.Since(start).Milliseconds(), out: out}
			if err != nil {
				r.Error = errLine(out, err)
			}
			r.Updated = strings.Count(out, " -> ")
			results[i] = r
			mu.Lock()
			done++
			if progress != nil {
				progress(done, len(targets))
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	var log strings.Builder
	fmt.Fprintf(&log, "# local-control-panel fetch-all %s · %s · %d repos\n", id, time.Now().Format(time.RFC3339), len(targets))
	var failed []string
	var list []res
	for _, r := range results {
		status := "ok"
		if !r.OK {
			status = "FAILED: " + r.Error
			failed = append(failed, r.Project)
		}
		fmt.Fprintf(&log, "\n## %s · %s · %dms\n%s\n", r.Project, status, r.MS, r.out)
		list = append(list, r)
	}
	if err := os.WriteFile(logPath, []byte(log.String()), 0o600); err != nil {
		a.log.Printf("write fetch log: %v", err)
	}

	details := map[string]any{"repos": len(targets), "failed": len(failed), "results": list, "log": logPath}
	if len(failed) > 0 {
		err := fmt.Errorf("%d of %d failed: %s", len(failed), len(targets), strings.Join(failed, ", "))
		_ = op.End("failed", nil, details, err)
		return Result{Err: fmt.Errorf("fetched %d repos; %w (a: audit, log in runs)", len(targets)-len(failed), err)}
	}
	_ = op.End("ok", nil, details, nil)
	return Result{Msg: fmt.Sprintf("fetched %d repos", len(targets))}
}

// Pull runs `git pull --ff-only`: it only ever fast-forwards, never merges
// or rebases, and git refuses if local changes would be overwritten.
func (a *Actions) Pull(ctx context.Context, p *scan.Project, trigger string) Result {
	argv := []string{"git", "pull", "--ff-only"}
	g := p.Git
	op, err := a.begin("git.pull", trigger, p, argv, map[string]any{"branch": g.Branch, "upstream": g.Upstream})
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}
	if err := a.checkPath(p); err != nil {
		return fail(op, nil, err)
	}
	switch {
	case p.Kind != scan.KindRepo:
		return fail(op, nil, errors.New("not a git repo"))
	case g.Cloning:
		return fail(op, nil, errors.New("clone still in progress"))
	case g.Upstream == "":
		return fail(op, nil, fmt.Errorf("branch %s has no upstream to pull from", g.Branch))
	}

	before := head(ctx, p.Abs)
	out, code, err := gitRun(ctx, p.Abs, 2*time.Minute, "pull", "--ff-only")
	after := head(ctx, p.Abs)
	details := map[string]any{"branch": g.Branch, "upstream": g.Upstream, "remote": g.Remote,
		"before": before, "after": after, "output": lastLines(out, 20)}
	if err != nil {
		_ = op.End("failed", &code, details, errors.New(errLine(out, err)))
		return Result{Err: fmt.Errorf("pull %s: %s", p.Rel, errLine(out, err))}
	}
	_ = op.End("ok", &code, details, nil)
	if before == after {
		return Result{Msg: p.Rel + " already up to date"}
	}
	n := "?"
	if c, _, err := gitRun(ctx, p.Abs, 5*time.Second, "rev-list", "--count", before+".."+after); err == nil {
		n = c
	}
	return Result{Msg: fmt.Sprintf("%s fast-forwarded %s commits (%.7s → %.7s)", p.Rel, n, before, after)}
}

// Init runs `git init` in a top-level folder that has no git anywhere inside.
// Nothing is committed and no remote is added.
func (a *Actions) Init(ctx context.Context, p *scan.Project, trigger string) Result {
	argv := []string{"git", "init"}
	op, err := a.begin("git.init", trigger, p, argv, map[string]any{"path": p.Abs})
	if err != nil {
		return Result{Err: fmt.Errorf("audit: %w", err)}
	}
	if err := a.checkPath(p); err != nil {
		return fail(op, nil, err)
	}
	if p.Kind != scan.KindPlain {
		return fail(op, nil, errors.New("only folders with no git anywhere inside can be initialised"))
	}
	if _, err := os.Lstat(filepath.Join(p.Abs, ".git")); err == nil {
		return fail(op, nil, errors.New(".git already exists"))
	}
	out, code, err := gitRun(ctx, p.Abs, 10*time.Second, "init")
	details := map[string]any{"path": p.Abs, "output": out}
	if err != nil {
		_ = op.End("failed", &code, details, errors.New(errLine(out, err)))
		return Result{Err: fmt.Errorf("git init %s: %s", p.Rel, errLine(out, err))}
	}
	_ = op.End("ok", &code, details, nil)
	return Result{Msg: "git init done in " + p.Rel + " (no commit yet)"}
}
