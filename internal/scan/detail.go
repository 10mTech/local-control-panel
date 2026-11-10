package scan

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Detail is what the project detail view shows beyond the dashboard row.
type Detail struct {
	Changes []string // `git status --short` lines
	Commits []string // recent commits, one line each
	Readme  []string // first non-empty README lines
	Scripts []string // package.json scripts as "name: command"
}

func LoadDetail(ctx context.Context, p Project) Detail {
	var d Detail
	if p.Kind == KindRepo && !p.Git.Cloning {
		if out, err := git(ctx, p.Abs, "status", "--short", "--untracked-files=normal"); err == nil {
			d.Changes = firstLines(out, 15)
		}
		if out, err := git(ctx, p.Abs, "log", "-8", "--format=%h  %cr  %s"); err == nil {
			d.Commits = firstLines(out, 8)
		}
	}
	d.Readme = readme(p.Abs, 6)
	if pkg, ok := readPackageJSON(p.Abs); ok {
		names := make([]string, 0, len(pkg.Scripts))
		for k := range pkg.Scripts {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			d.Scripts = append(d.Scripts, k+": "+pkg.Scripts[k])
		}
	}
	return d
}

func firstLines(s string, n int) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if l == "" {
			continue
		}
		out = append(out, l)
		if len(out) == n {
			break
		}
	}
	return out
}

func readme(abs string, n int) []string {
	for _, name := range []string{"README.md", "readme.md", "README", "README.txt"} {
		f, err := os.Open(filepath.Join(abs, name))
		if err != nil {
			continue
		}
		defer f.Close()
		var out []string
		sc := bufio.NewScanner(f)
		inCode := false
		for sc.Scan() && len(out) < n {
			l := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(l, "```") {
				inCode = !inCode
				continue
			}
			if l == "" || inCode || strings.HasPrefix(l, "<") || strings.HasPrefix(l, "![") || strings.HasPrefix(l, "[!") {
				continue
			}
			out = append(out, l)
		}
		return out
	}
	return nil
}
