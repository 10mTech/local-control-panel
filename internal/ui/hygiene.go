package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"local-control-panel/internal/actions"
	"local-control-panel/internal/scan"
)

type (
	fetchProgressMsg struct{ done, total int }
	cleanPlanMsg     struct {
		p    scan.Project
		plan actions.CleanPlan
		err  error
	}
)

// runningUnder lists projects at or below rel that have something running.
func (m *Model) runningUnder(rel string) []string {
	var out []string
	for i := range m.projects {
		p := &m.projects[i]
		if (p.Rel == rel || strings.HasPrefix(p.Rel, rel+"/")) && (p.IsRunning() || m.activeRun(p.Rel) != nil) {
			out = append(out, p.Rel)
		}
	}
	return out
}

// hygieneAction handles p, i, x, A and D for a project.
func (m *Model) hygieneAction(key string, p *scan.Project) tea.Cmd {
	pp := *p
	trigger := "key:" + key
	switch key {
	case "p":
		switch {
		case p.Kind != scan.KindRepo:
			m.setError(p.Rel + " is not a git repo")
		case p.Git.Upstream == "":
			m.setError(p.Rel + ": branch has no upstream to pull from")
		default:
			return m.do("pulling "+p.Rel, func(ctx context.Context) actions.Result { return m.acts.Pull(ctx, &pp, trigger) })
		}
	case "i":
		if p.Kind != scan.KindPlain {
			m.setError("git init is only for top-level folders with no git anywhere inside")
			return nil
		}
		m.confirm("git init · "+p.Rel, []string{
			"Run " + sText.Render("git init") + " in",
			"  " + sText.Render(p.Abs),
			"",
			sDim.Render("Creates .git only. Nothing is committed and no remote is added."),
		}, "y init · n cancel", func() tea.Cmd {
			return m.do("git init "+p.Rel, func(ctx context.Context) actions.Result { return m.acts.Init(ctx, &pp, trigger) })
		}, func() {
			m.acts.Cancelled("git.init", trigger, &pp, []string{"git", "init"}, nil)
		})
	case "x":
		if p.Kind == scan.KindGroup {
			m.setError("a group holds other projects; clean them one by one")
			return nil
		}
		if r := m.runningUnder(p.Rel); len(r) > 0 {
			m.setError(p.Rel + " is running; stop it before cleaning its build folders")
			return nil
		}
		if m.busy != "" {
			m.setError("wait: " + m.busy + " is still running")
			return nil
		}
		m.busy = "measuring build folders in " + p.Rel
		return func() tea.Msg {
			plan, err := m.acts.PlanClean(context.Background(), &pp)
			return cleanPlanMsg{p: pp, plan: plan, err: err}
		}
	case "A":
		return m.askArchive(pp, trigger)
	case "D":
		m.askTrash(pp, trigger)
	}
	return nil
}

func (m *Model) onCleanPlan(msg cleanPlanMsg) {
	m.busy = ""
	if msg.err != nil {
		m.setError(msg.err.Error())
		return
	}
	p, plan := msg.p, msg.plan
	if len(plan.Items) == 0 {
		s := "nothing to clean in " + p.Rel
		if len(plan.Skipped) > 0 {
			s += fmt.Sprintf(" (%s left alone: %s)", plan.Skipped[0].Dir, plan.Skipped[0].Reason)
		}
		m.setFlash(s)
		return
	}
	lines := []string{"Move these folders to the Trash:"}
	for _, it := range plan.Items {
		lines = append(lines, "  "+sText.Render(fit(it.Dir, 20))+" "+buildStyle(it.Size).Render(fitLeft(humanBytes(it.Size), 6)))
	}
	if len(plan.Skipped) > 0 {
		lines = append(lines, "", "Left alone:")
		for _, s := range plan.Skipped {
			lines = append(lines, "  "+sSub.Render(fit(s.Dir, 20))+" "+sDim.Render(s.Reason))
		}
	}
	lines = append(lines, "",
		"Total "+sPeach.Render(humanBytes(plan.Total))+sDim.Render(". Reinstall or rebuild to get them back."),
		sDim.Render("The space is freed when you empty the Trash."))
	trigger := "key:x"
	m.confirm("Clean · "+p.Rel, lines, "y move to Trash · n cancel", func() tea.Cmd {
		return m.do("cleaning "+p.Rel, func(ctx context.Context) actions.Result { return m.acts.Clean(ctx, &p, plan, trigger) })
	}, func() {
		m.acts.Cancelled("clean", trigger, &p, nil, map[string]any{"preview": plan})
	})
}

func (m *Model) askArchive(p scan.Project, trigger string) tea.Cmd {
	if r := m.runningUnder(p.Rel); len(r) > 0 {
		m.setError(strings.Join(r, ", ") + " is running; stop it first")
		return nil
	}
	mv, err := m.acts.PlanArchive(&p)
	if err != nil {
		m.setError(err.Error())
		return nil
	}
	verb, action := "Archive", "archive"
	if mv.Restore {
		verb, action = "Restore", "restore"
	}
	lines := []string{
		"Move", "  " + sText.Render(mv.From), "to", "  " + sText.Render(mv.To), "",
		sDim.Render("A rename on the same disk; nothing is copied or deleted."),
	}
	if !mv.Restore {
		var warn []string
		g := p.Git
		switch {
		case p.Kind == scan.KindPlain:
			warn = append(warn, "no git: the folder is the only copy (it stays on disk)")
		case p.Kind == scan.KindRepo && g.Remote == "":
			warn = append(warn, "no remote: this repo exists only here (it stays on disk)")
		}
		if p.Kind == scan.KindRepo && changes(&p) > 0 {
			warn = append(warn, fmt.Sprintf("%d uncommitted changes", changes(&p)))
		}
		if p.Unpushed() {
			warn = append(warn, "commits not pushed")
		}
		for _, w := range warn {
			lines = append(lines, sYellow.Render("! "+w))
		}
		if p.Kind == scan.KindGroup {
			lines = append(lines, sYellow.Render("! a group: everything inside moves with it"))
		}
	}
	m.confirm(verb+" · "+p.Rel, lines, "y "+action+" · n cancel", func() tea.Cmd {
		return m.do(action+" "+p.Rel, func(ctx context.Context) actions.Result { return m.acts.Archive(ctx, &p, mv, trigger) })
	}, func() {
		m.acts.Cancelled(action, trigger, &p, nil, map[string]any{"from": mv.From, "to": mv.To})
	})
	return nil
}

// askTrash asks for the project's path to be typed out before moving the
// whole folder to the Trash.
func (m *Model) askTrash(p scan.Project, trigger string) {
	if r := m.runningUnder(p.Rel); len(r) > 0 {
		m.setError(strings.Join(r, ", ") + " is running; stop it first")
		return
	}
	if err := m.acts.CanTrash(&p); err != nil {
		m.setError(err.Error())
		return
	}
	var warn []string
	g := p.Git
	switch {
	case p.Kind == scan.KindPlain:
		warn = append(warn, "no git: the Trash will hold the only copy")
	case p.Kind == scan.KindRepo && g.Remote == "":
		warn = append(warn, "no remote: the Trash will hold the only copy")
	}
	if p.Kind == scan.KindRepo && changes(&p) > 0 {
		warn = append(warn, fmt.Sprintf("%d uncommitted changes", changes(&p)))
	}
	if p.Unpushed() {
		warn = append(warn, "commits not pushed")
	}
	if p.Kind == scan.KindGroup {
		n := 0
		for i := range m.projects {
			if strings.HasPrefix(m.projects[i].Rel, p.Rel+"/") {
				n++
			}
		}
		warn = append(warn, fmt.Sprintf("a group: all %d projects inside go with it", n))
	}

	size := "size not measured yet"
	if p.Size != nil {
		size = humanBytes(p.Size.Total)
	}
	lines := []string{
		"Move the whole folder to the Trash:", "  " + sText.Render(p.Abs), "  " + sDim.Render(size), "",
	}
	for _, w := range warn {
		lines = append(lines, sYellow.Render("! "+w))
	}
	if len(warn) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines,
		sDim.Render("Finder's Put Back restores it until you empty the Trash."),
		"Type "+sText.Render(p.Rel)+" to confirm.")

	in := textinput.New()
	in.Prompt = "> "
	in.Focus()
	m.prompt = &prompt{
		title: "Move to Trash · " + p.Rel,
		lines: lines,
		input: &in,
		keys:  "enter move to Trash · esc cancel",
		yes: func(value string) tea.Cmd {
			if strings.TrimSpace(value) != p.Rel {
				m.acts.Cancelled("trash", trigger, &p, nil, map[string]any{"path": p.Abs, "reason": "typed name did not match"})
				m.setError("name did not match; nothing was moved")
				return nil
			}
			return m.do("moving "+p.Rel+" to the Trash", func(ctx context.Context) actions.Result {
				return m.acts.Trash(ctx, &p, warn, trigger)
			})
		},
		no: func() {
			m.acts.Cancelled("trash", trigger, &p, nil, map[string]any{"path": p.Abs})
			m.setFlash("cancelled")
		},
	}
}

// askFetchAll confirms the bulk fetch, listing how many repos it covers.
func (m *Model) askFetchAll() tea.Cmd {
	n, skipped := 0, 0
	for i := range m.projects {
		p := &m.projects[i]
		switch {
		case actions.Fetchable(p):
			n++
		case p.Kind == scan.KindRepo:
			skipped++
		}
	}
	root := scan.Project{Abs: m.cfg.Root}
	ps := append([]scan.Project(nil), m.projects...)
	m.confirm("Fetch all", []string{
		fmt.Sprintf("Run %s in %d repos, 6 at a time.", sText.Render("git fetch --all"), n),
		sDim.Render(fmt.Sprintf("Skipped: %d without a remote, cloning, or archived.", skipped)),
		"",
		sDim.Render("Only remote-tracking branches change; your branches and files do not."),
		sDim.Render("Repos that need a password fail instead of prompting. Output goes to a log."),
	}, "y fetch · n cancel", func() tea.Cmd {
		return m.do("fetching", func(ctx context.Context) actions.Result {
			return m.acts.FetchAll(ctx, ps, "key:f", func(done, total int) {
				select {
				case m.events <- fetchProgressMsg{done, total}:
				default:
				}
			})
		})
	}, func() {
		m.acts.Cancelled("git.fetch-all", "key:f", &root, []string{"git", "fetch", "--all"}, map[string]any{"repos": n})
	})
	return nil
}

// confirm shows a y/n prompt.
func (m *Model) confirm(title string, lines []string, keys string, yes func() tea.Cmd, no func()) {
	if limit := m.height - 8; len(lines) > limit && limit > 3 {
		lines = append(lines[:limit-1], sDim.Render(fmt.Sprintf("… %d more lines", len(lines)-limit+1)))
	}
	m.prompt = &prompt{title: title, lines: lines, keys: keys,
		yes: func(string) tea.Cmd { return yes() },
		no:  func() { no(); m.setFlash("cancelled") }}
}
