package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"local-control-panel/internal/scan"
)

func (m *Model) detailSize() (int, int) {
	return max(10, m.width-2), max(3, m.height-4)
}

func (m *Model) openDetail(p scan.Project) tea.Cmd {
	m.back, m.screen = screenDashboard, screenDetail
	m.detailRel = p.Rel
	m.detail = nil
	w, h := m.detailSize()
	m.detailView = viewport.New(w, h)
	m.refreshDetailView()
	return tea.Batch(loadDetail(p), m.loadAudit())
}

func loadDetail(p scan.Project) tea.Cmd {
	return func() tea.Msg {
		return detailMsg{rel: p.Rel, d: scan.LoadDetail(context.Background(), p)}
	}
}

func (m *Model) detailProject() *scan.Project {
	for i := range m.projects {
		if m.projects[i].Rel == m.detailRel {
			return &m.projects[i]
		}
	}
	return nil
}

func (m *Model) refreshDetailView() {
	p := m.detailProject()
	if p == nil {
		m.detailView.SetContent(sDim.Render("  this project is no longer under the root"))
		return
	}
	w := m.detailView.Width
	lines := m.summaryLines(p, w)

	section := func(title string, body []string, empty string) {
		lines = append(lines, "", " "+sAccent.Render(title))
		if len(body) == 0 {
			lines = append(lines, "   "+sDim.Render(empty))
			return
		}
		for _, b := range body {
			lines = append(lines, fit("   "+b, w))
		}
	}

	if m.detail == nil {
		lines = append(lines, "", sDim.Render("  loading…"))
	} else {
		d := m.detail
		if p.Kind == scan.KindRepo {
			section("Uncommitted changes", colorStatus(d.Changes), "clean")
			section("Recent commits", d.Commits, "no commits")
		}
		section("README", d.Readme, "no README")
		if len(d.Scripts) > 0 {
			section("package.json scripts", d.Scripts, "")
		}
	}

	var entries []string
	for i := len(m.auditRecs) - 1; i >= 0 && len(entries) < 10; i-- {
		if r := m.auditRecs[i]; r.Project == p.Rel {
			entries = append(entries, auditLine(r, w-4))
		}
	}
	section("Audit (last 10 for this project)", entries, "no actions recorded yet")

	m.detailView.SetContent(strings.Join(lines, "\n"))
}

func colorStatus(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "??"):
			out[i] = sDim.Render(l)
		case strings.Contains(l[:min(2, len(l))], "D"):
			out[i] = sRed.Render(l)
		default:
			out[i] = sYellow.Render(l)
		}
	}
	return out
}

func (m *Model) detailScreen() string {
	p := m.detailProject()
	title := m.detailRel
	if p != nil && p.Kind == scan.KindGroup {
		title += "/"
	}
	w, h := m.detailSize()
	note := ""
	if m.detailView.TotalLineCount() > h {
		note = fmt.Sprintf("%d%%", int(m.detailView.ScrollPercent()*100))
	}
	body := box(title, note, strings.Split(m.detailView.View(), "\n"), w+2, h+2, true)
	keys := " " + sKey.Render("j/k") + " " + sBarText.Render("scroll") + "  "
	for _, k := range []string{"o open", "F finder", "e edit", "c claude", "r run", "s stop", "p pull", "x clean", "A archive", "D trash", "L runs", "R reload", "esc back"} {
		key, label, _ := strings.Cut(k, " ")
		keys += sKey.Render(key) + " " + sBarText.Render(label) + "  "
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.topBar(), body, fit(keys, m.width))
}

func (m *Model) onDetailKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "q", "backspace":
		m.screen = screenDashboard
		return m, nil
	case "?":
		m.back, m.screen = screenDetail, screenHelp
		return m, nil
	case "R":
		if p := m.detailProject(); p != nil {
			m.detail = nil
			m.refreshDetailView()
			return m, tea.Batch(loadDetail(*p), m.loadAudit())
		}
		return m, nil
	case "o", "e", "c", "r", "s":
		if p := m.detailProject(); p != nil {
			return m, m.projectAction(k.String(), p)
		}
		return m, nil
	case "p", "i", "x", "A", "D":
		if p := m.detailProject(); p != nil {
			return m, m.hygieneAction(k.String(), p)
		}
		return m, nil
	case "F":
		if p := m.detailProject(); p != nil {
			return m, m.finder(p)
		}
		return m, nil
	case "L":
		return m, m.openRuns(screenDetail)
	case "g", "home":
		m.detailView.GotoTop()
		return m, nil
	case "G", "end":
		m.detailView.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.detailView, cmd = m.detailView.Update(k)
	return m, cmd
}
