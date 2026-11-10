package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/10mTech/local-control-panel/internal/actions"
	"github.com/10mTech/local-control-panel/internal/audit"
	"github.com/10mTech/local-control-panel/internal/config"
	"github.com/10mTech/local-control-panel/internal/debuglog"
	"github.com/10mTech/local-control-panel/internal/herdr"
	"github.com/10mTech/local-control-panel/internal/scan"
	"github.com/10mTech/local-control-panel/internal/ui"
)

// version is set for releases with -ldflags "-X main.version=v1.2.3".
// Without it, appVersion falls back to what the Go toolchain recorded.
var version string

// appVersion is the release version, the module version from go install, or
// the git commit the binary was built from.
func appVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func main() {
	list := flag.Bool("list", false, "print the scan as plain text and exit")
	snapshot := flag.String("snapshot", "", `render one screen to stdout and exit: dashboard, help, audit, runs, or detail:<path>`)
	size := flag.String("size", "160x45", "terminal size for -snapshot, WIDTHxHEIGHT")
	showVersion := flag.Bool("version", false, "print the version and exit")
	configPath := flag.String("config", "", "use this config file instead of config.toml in $XDG_CONFIG_HOME/local-control-panel (default ~/.config/local-control-panel)")
	flag.Parse()

	if *showVersion {
		fmt.Println(config.AppName, appVersion())
		return
	}
	if err := run(*configPath, *list, *snapshot, *size); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(configPath string, list bool, snapshot, size string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if len(cfg.Unknown) > 0 {
		fmt.Fprintf(os.Stderr, "warning: %s: unknown keys ignored (typo?): %s\n", cfg.Path, strings.Join(cfg.Unknown, ", "))
	}
	state := cfg.StateDir
	debug, closer, err := debuglog.Open(state)
	if err != nil {
		return err
	}
	defer closer.Close()

	scanner := scan.NewScanner(cfg, debug, state)
	if list {
		printList(scanner.Fast(context.Background()))
		return nil
	}

	auditLog, err := audit.New(state, appVersion())
	if err != nil {
		return err
	}
	h := herdr.New()
	acts, err := actions.New(cfg, h, auditLog, debug, state)
	if err != nil {
		return err
	}
	m := ui.New(cfg, scanner, auditLog, acts, h, debug)
	if snapshot != "" {
		var w, h int
		if _, err := fmt.Sscanf(size, "%dx%d", &w, &h); err != nil {
			return fmt.Errorf("-size %q: want WIDTHxHEIGHT", size)
		}
		fmt.Println(ui.Snapshot(m, w, h, snapshot))
		return nil
	}
	acts.PruneRuns(cfg.Runs.Keep, int64(cfg.Runs.MaxLogMB)<<20)
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func printList(ps []scan.Project) {
	for _, p := range ps {
		kind := map[scan.Kind]string{scan.KindRepo: "repo", scan.KindGroup: "group", scan.KindPlain: "no-git"}[p.Kind]
		g := p.Git
		gitInfo := ""
		switch {
		case p.Kind != scan.KindRepo:
		case g.Cloning:
			gitInfo = "cloning"
		case g.Err != "":
			gitInfo = "error: " + g.Err
		default:
			gitInfo = fmt.Sprintf("%s dirty=%d untracked=%d ahead=%d behind=%d owner=%s", g.Branch, g.Dirty, g.Untracked, g.Ahead, g.Behind, g.Owner)
		}
		var running []string
		for _, l := range p.Running {
			running = append(running, fmt.Sprintf("%s:%d", l.Command, l.Port))
		}
		running = append(running, p.Containers...)
		fmt.Printf("%-48s %-6s %-14s %-10s port=%d(%s) running=[%s] %s\n",
			strings.Repeat("  ", p.Depth-1)+p.Rel, kind, p.StackLabel(), p.Activity,
			p.Port, p.PortSource, strings.Join(running, ","), gitInfo)
	}
}
