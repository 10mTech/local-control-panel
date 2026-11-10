package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// git runs a read-only git command in dir. GIT_OPTIONAL_LOCKS=0 stops
// `git status` from taking the index lock, so the panel never blocks a git
// command you are running yourself.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	out, err := cmd.Output()
	return string(out), err
}

func readGit(ctx context.Context, abs string) GitState {
	var g GitState

	// A clone in progress has a .git folder but no HEAD yet.
	gitPath := filepath.Join(abs, ".git")
	if st, err := os.Stat(gitPath); err == nil && st.IsDir() && !exists(filepath.Join(gitPath, "HEAD")) {
		g.Cloning = true
		return g
	}

	out, err := git(ctx, abs, "status", "--porcelain=v2", "--branch", "--untracked-files=normal")
	if err != nil {
		g.Err = gitErr(err)
		return g
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			g.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.upstream "):
			g.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			f := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(f) == 2 {
				g.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[0], "+"))
				g.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[1], "-"))
			}
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "), strings.HasPrefix(line, "u "):
			g.Dirty++
		case strings.HasPrefix(line, "? "):
			g.Untracked++
		}
	}

	if out, err := git(ctx, abs, "log", "-1", "--format=%ct%x1f%s"); err == nil && strings.TrimSpace(out) != "" {
		parts := strings.SplitN(strings.TrimSpace(out), "\x1f", 2)
		if ts, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			g.LastCommit = time.Unix(ts, 0)
		}
		if len(parts) == 2 {
			g.Subject = parts[1]
		}
	} else {
		g.NoCommits = true
	}

	if out, err := git(ctx, abs, "remote", "get-url", "origin"); err == nil {
		g.Remote = strings.TrimSpace(out)
		g.Host, g.Owner = parseRemote(g.Remote)
	}
	return g
}

func gitErr(err error) string {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		return strings.TrimSpace(strings.SplitN(string(ee.Stderr), "\n", 2)[0])
	}
	return err.Error()
}

var (
	reSSHRemote   = regexp.MustCompile(`^(?:ssh://)?[^@]+@([^:/]+)[:/]([^/]+)/`)
	reHTTPSRemote = regexp.MustCompile(`^https?://(?:[^@/]+@)?([^/]+)/([^/]+)/`)
)

// parseRemote returns the host and owner of a remote URL, e.g.
// git@github.com:acme/x.git -> github.com, acme. CodeCommit has no
// owner in the URL, so its owner is "codecommit".
func parseRemote(url string) (host, owner string) {
	if strings.Contains(url, "git-codecommit.") {
		return "codecommit", "codecommit"
	}
	if m := reSSHRemote.FindStringSubmatch(url); m != nil {
		return m[1], m[2]
	}
	if m := reHTTPSRemote.FindStringSubmatch(url); m != nil {
		return m[1], m[2]
	}
	return "", ""
}
