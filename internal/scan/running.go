package scan

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

// listeners returns every TCP port in LISTEN state owned by this user, with
// the owning process's working directory. A process belongs to a project when
// its cwd is inside the project folder, so no port guessing is needed.
func listeners(ctx context.Context) ([]Listener, error) {
	out, err := run(ctx, 4*time.Second, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpcn")
	// lsof exits 1 when nothing matches.
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, nil
	}

	var ls []Listener
	seen := map[string]bool{}
	pid, cmd := 0, ""
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'c':
			cmd = line[1:]
		case 'n':
			i := strings.LastIndex(line, ":")
			if i < 0 {
				continue
			}
			port, err := strconv.Atoi(line[i+1:])
			if err != nil {
				continue
			}
			key := fmt.Sprintf("%d:%d", pid, port) // IPv4 and IPv6 show twice
			if !seen[key] {
				seen[key] = true
				ls = append(ls, Listener{PID: pid, Command: cmd, Port: port})
			}
		}
	}
	if len(ls) == 0 {
		return nil, nil
	}

	pids := map[int]bool{}
	var list []string
	for _, l := range ls {
		if !pids[l.PID] {
			pids[l.PID] = true
			list = append(list, strconv.Itoa(l.PID))
		}
	}
	out, _ = run(ctx, 4*time.Second, "lsof", "-a", "-d", "cwd", "-p", strings.Join(list, ","), "-Fpn")
	cwds := map[int]string{}
	pid = 0
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			cwds[pid] = line[1:]
		}
	}
	for i := range ls {
		ls[i].Cwd = cwds[ls[i].PID]
	}
	return ls, nil
}

type container struct {
	Name       string
	WorkingDir string
}

// containers returns running Docker Compose containers with their project
// folder. Missing docker or a stopped daemon just means no containers.
func containers(ctx context.Context) []container {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}
	out, err := run(ctx, 3*time.Second, "docker", "ps", "--format",
		`{{.Names}}`+"\t"+`{{.Label "com.docker.compose.project.working_dir"}}`)
	if err != nil {
		return nil
	}
	var cs []container
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		name, dir, ok := strings.Cut(line, "\t")
		if ok && dir != "" {
			cs = append(cs, container{Name: name, WorkingDir: filepath.Clean(dir)})
		}
	}
	return cs
}

// owner returns the index of the deepest project containing path, or -1.
func owner(projects []Project, path string) int {
	best, bestLen := -1, 0
	for i, p := range projects {
		if p.Kind == KindGroup {
			continue
		}
		if (path == p.Abs || strings.HasPrefix(path, p.Abs+"/")) && len(p.Abs) > bestLen {
			best, bestLen = i, len(p.Abs)
		}
	}
	return best
}
