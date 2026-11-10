package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/10mTech/local-control-panel/internal/actions"
	"github.com/10mTech/local-control-panel/internal/herdr"
	"github.com/10mTech/local-control-panel/internal/safety"
	"github.com/10mTech/local-control-panel/internal/scan"
)

type (
	herdrMsg struct {
		version string
		err     error
	}
	actionMsg struct{ res actions.Result }
	runsMsg   struct{ runs []runView }
	tailMsg   struct {
		id    string
		lines []string
	}
	runsTickMsg struct{}
)

// runView is a run record plus whether its process is alive right now.
type runView struct {
	actions.Run
	Alive bool
}

func (r runView) state() string {
	switch {
	case r.Alive:
		return "running"
	case !r.Stopped.IsZero():
		return "stopped"
	default:
		return "exited"
	}
}

const maxRunsShown = 50

// listRuns reads run records and checks which are alive. Runs already
// stopped by the panel are never probed.
func (m *Model) listRuns() []runView {
	all := m.acts.Runs.List()
	if len(all) > maxRunsShown {
		all = all[:maxRunsShown]
	}
	out := make([]runView, len(all))
	for i, r := range all {
		out[i] = runView{Run: r, Alive: r.Stopped.IsZero() && r.Alive()}
	}
	return out
}

func (m *Model) loadRuns() tea.Cmd {
	return func() tea.Msg { return runsMsg{runs: m.listRuns()} }
}

// activeRun is the live panel-started run for a project, if any.
func (m *Model) activeRun(rel string) *runView {
	for i := range m.runs {
		if m.runs[i].Project == rel && m.runs[i].Alive {
			return &m.runs[i]
		}
	}
	return nil
}

// panelListener reports whether a listening process belongs to a run the
// panel started, by process group.
func (m *Model) panelListener(l scan.Listener) bool {
	pg, ok := m.listenerPG[l.PID]
	if !ok {
		return false
	}
	for _, r := range m.runs {
		if r.Alive && r.PGID == pg {
			return true
		}
	}
	return false
}

func (m *Model) checkHerdr() tea.Cmd {
	return func() tea.Msg {
		v, err := m.herdr.Check(context.Background())
		return herdrMsg{version: v, err: err}
	}
}

func errorText(err error) string {
	if herdr.IsNotRunning(err) {
		return "herdr is not running: start herdr, then try again"
	}
	return err.Error()
}

// do runs an action in the background; its result comes back as actionMsg.
func (m *Model) do(what string, f func(ctx context.Context) actions.Result) tea.Cmd {
	if m.busy != "" {
		m.setError("wait: " + m.busy + " is still running")
		return nil
	}
	m.busy = what
	m.setFlash(what + "…")
	return func() tea.Msg { return actionMsg{res: f(context.Background())} }
}

// finder shows a project, or a group, in Finder.
func (m *Model) finder(p *scan.Project) tea.Cmd {
	pp := *p
	return m.do("opening Finder", func(ctx context.Context) actions.Result { return m.acts.Finder(ctx, &pp, "key:F") })
}

// projectAction handles o, e, c, r and s for a project.
func (m *Model) projectAction(key string, p *scan.Project) tea.Cmd {
	if p.Kind == scan.KindGroup {
		m.setError(p.Rel + " is a group; pick a project inside it")
		return nil
	}
	pp := *p
	trigger := "key:" + key
	switch key {
	case "o":
		return m.do("opening "+p.Rel, func(ctx context.Context) actions.Result { return m.acts.Open(ctx, &pp, trigger) })
	case "e":
		return m.do("opening editor", func(ctx context.Context) actions.Result {
			return m.acts.Tool(ctx, &pp, "editor", "editor", m.cfg.Editor, trigger)
		})
	case "c":
		return m.do("opening Claude Code", func(ctx context.Context) actions.Result {
			return m.acts.Tool(ctx, &pp, "claude", "claude", "claude", trigger)
		})
	case "s":
		r := m.activeRun(p.Rel)
		if r == nil {
			if p.IsRunning() {
				m.setError("not started by the panel, so it can't be stopped here; stop it where you started it")
			} else {
				m.setFlash("nothing running in " + p.Rel)
			}
			return nil
		}
		run := r.Run
		return m.do("stopping "+p.Rel, func(ctx context.Context) actions.Result { return m.acts.StopRun(ctx, run, trigger) })
	case "r":
		if r := m.activeRun(p.Rel); r != nil {
			m.setError(fmt.Sprintf("already running (pid %d): s stops it, L shows its log", r.PID))
			return nil
		}
		dev := m.acts.DevCommand(p)
		if dev.Confirmed() {
			return m.startRun(pp, dev, trigger)
		}
		m.askDevCommand(pp, dev, trigger)
	}
	return nil
}

func (m *Model) startRun(p scan.Project, dev actions.DevCommand, trigger string) tea.Cmd {
	return m.do("starting "+p.Rel, func(ctx context.Context) actions.Result { return m.acts.StartRun(ctx, &p, dev, trigger) })
}

// askDevCommand shows the proposed dev command for editing before its first
// run. Enter runs it and remembers it; esc is logged as cancelled.
func (m *Model) askDevCommand(p scan.Project, dev actions.DevCommand, trigger string) {
	in := textinput.New()
	in.Prompt = "$ "
	in.SetValue(safety.ShellJoin(dev.Argv))
	in.Placeholder = "type the command that starts the dev server"
	in.Focus()

	lines := []string{"First run in this project. Check the command, edit it if needed."}
	if dev.Source == "proposed" {
		lines = append(lines, sDim.Render("Guessed from the project's files."))
	} else {
		lines = append(lines, sYellow.Render("No dev command found."))
	}
	if p.NotInstalled {
		lines = append(lines, sYellow.Render("Dependencies are not installed (no node_modules). Install them first if needed."))
	}
	lines = append(lines, sDim.Render("It runs in a new pane of the project's herdr workspace; output goes to a run log."),
		sDim.Render("Once confirmed it is remembered; config.toml can override it."))

	m.prompt = &prompt{
		title: "Run dev server · " + p.Rel,
		lines: lines,
		input: &in,
		keys:  "enter run · esc cancel",
		yes: func(value string) tea.Cmd {
			argv, err := safety.SplitWords(value)
			if err != nil || len(argv) == 0 {
				m.setError("not a command: " + value)
				return nil
			}
			if err := m.acts.Approvals.Set(p.Rel, argv); err != nil {
				m.log.Printf("save dev command: %v", err)
			}
			return m.startRun(p, actions.DevCommand{Argv: argv, Source: "confirmed"}, trigger)
		},
		no: func() {
			m.acts.Cancelled("run", trigger, &p, dev.Argv, map[string]any{"reason": "declined at the dev command prompt"})
			m.setFlash("run cancelled")
		},
	}
}

// prompt is a small window over the current screen that waits for an answer.
type prompt struct {
	title string
	lines []string
	input *textinput.Model // nil for a y/n question
	keys  string
	yes   func(value string) tea.Cmd
	no    func()
}

func (m *Model) onPromptKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	pr := m.prompt
	switch k.String() {
	case "esc", "ctrl+c":
		m.prompt = nil
		pr.no()
		return m, nil
	case "enter":
		if pr.input != nil {
			m.prompt = nil
			return m, pr.yes(pr.input.Value())
		}
	case "y", "Y":
		if pr.input == nil {
			m.prompt = nil
			return m, pr.yes("")
		}
	case "n", "N":
		if pr.input == nil {
			m.prompt = nil
			pr.no()
			return m, nil
		}
	}
	if pr.input != nil {
		var cmd tea.Cmd
		*pr.input, cmd = pr.input.Update(k)
		return m, cmd
	}
	return m, nil
}

// overlay draws the prompt window centred over base.
func (m *Model) overlay(base string) string {
	pr := m.prompt
	w := min(m.width-4, 92)
	lines := []string{""}
	for _, l := range pr.lines {
		lines = append(lines, " "+l)
	}
	if pr.input != nil {
		pr.input.Width = w - 8
		lines = append(lines, "", " "+pr.input.View())
	}
	lines = append(lines, "")
	win := strings.Split(box(pr.title, pr.keys, lines, w, len(lines)+2, true), "\n")

	rows := strings.Split(base, "\n")
	x := max(0, (m.width-w)/2)
	y := max(0, (len(rows)-len(win))/2)
	for i, wl := range win {
		if y+i >= len(rows) {
			break
		}
		row := fit(rows[y+i], m.width)
		rows[y+i] = ansi.Truncate(row, x, "") + wl + ansi.TruncateLeft(row, x+w, "")
	}
	return strings.Join(rows, "\n")
}
