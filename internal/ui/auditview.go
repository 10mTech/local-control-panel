package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/10mTech/local-control-panel/internal/audit"
)

const auditDetailH = 14

func (m *Model) loadAudit() tea.Cmd {
	dir := m.audit.Dir()
	return func() tea.Msg {
		recs, chain, err := audit.ReadAll(dir)
		return auditMsg{recs: recs, chain: chain, err: err}
	}
}

func (m *Model) auditListHeight() int {
	// The header line, both boxes' borders, the detail box and the key line take the rest.
	return max(1, m.height-auditDetailH-6)
}

func (m *Model) rebuildAudit() {
	q := strings.ToLower(m.auditSearch.Value())
	m.auditVisible = m.auditVisible[:0]
	for i := len(m.auditRecs) - 1; i >= 0; i-- {
		r := m.auditRecs[i]
		if m.auditFailed && r.Outcome != "failed" && r.Error == "" {
			continue
		}
		if q != "" {
			hay := strings.ToLower(r.Action + " " + r.Project + " " + strings.Join(r.Argv, " ") + " " + r.Trigger + " " + r.Outcome)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		m.auditVisible = append(m.auditVisible, i)
	}
	m.auditCursor = min(m.auditCursor, max(0, len(m.auditVisible)-1))
	m.clampAudit()
}

func (m *Model) clampAudit() {
	h := m.auditListHeight()
	if m.auditCursor < 0 {
		m.auditCursor = 0
	}
	if m.auditCursor >= len(m.auditVisible) {
		m.auditCursor = max(0, len(m.auditVisible)-1)
	}
	if m.auditCursor < m.auditOffset {
		m.auditOffset = m.auditCursor
	}
	if m.auditCursor >= m.auditOffset+h {
		m.auditOffset = m.auditCursor - h + 1
	}
}

func phaseMark(r audit.Record) string {
	switch {
	case r.Phase == "intent":
		return sSky.Render("→")
	case r.Phase == "event":
		return sDim.Render("·")
	case r.Outcome == "ok":
		return sGreen.Render("✓")
	case r.Outcome == "cancelled":
		return sYellow.Render("⊘")
	default:
		return sRed.Render("✗")
	}
}

// auditLine is one record as a single row: time, mark, action, project, result.
func auditLine(r audit.Record, w int) string {
	ts := r.TS
	if t, err := time.Parse("2006-01-02T15:04:05.000Z07:00", r.TS); err == nil {
		ts = t.Format("01-02 15:04:05")
	}
	result := ""
	if r.Phase == "result" {
		result = r.Outcome
		if r.DurationMS != nil {
			result += fmt.Sprintf(" %dms", *r.DurationMS)
		}
	}
	what := strings.Join(r.Argv, " ")
	if what == "" {
		what = r.Trigger
	}
	line := sDim.Render(ts) + " " + phaseMark(r) + " " + sText.Render(fit(r.Action, 15)) + " " +
		sSub.Render(fit(r.Project, 24)) + " " + sDim.Render(fit(result, 14)) + " " + sSub.Render(what)
	return fit(line, w)
}

func (m *Model) auditScreen() string {
	w := m.width
	listH := m.auditListHeight()

	var lines []string
	for i := m.auditOffset; i < len(m.auditVisible) && i < m.auditOffset+listH; i++ {
		r := m.auditRecs[m.auditVisible[i]]
		line := auditLine(r, w-2)
		if i == m.auditCursor {
			line = sSelected.Render(fit(ansi.Strip(line), w-2))
		}
		lines = append(lines, line)
	}
	if m.auditErr != "" {
		lines = append(lines, sRed.Render(" "+m.auditErr))
	} else if len(m.auditVisible) == 0 {
		lines = append(lines, sDim.Render(" no entries"))
	}

	title := "Audit log"
	if m.auditFailed {
		title += " · failures only"
	}
	chain := sGreen.Render(fmt.Sprintf("chain ok · %d lines", m.auditChain.Lines))
	if !m.auditChain.OK {
		chain = sRed.Render("chain broken at " + m.auditChain.BrokenAt + " (this line or the one before was changed or removed)")
	}
	note := fmt.Sprintf("%d/%d", min(m.auditCursor+1, len(m.auditVisible)), len(m.auditVisible))
	list := box(title, note, lines, w, listH+2, true)

	var detail []string
	if len(m.auditVisible) > 0 {
		r := m.auditRecs[m.auditVisible[m.auditCursor]]
		b, _ := json.MarshalIndent(r.Entry, "", "  ")
		for _, l := range strings.Split(string(b), "\n") {
			detail = append(detail, " "+sSub.Render(l))
		}
		detail = append([]string{" " + sDim.Render(fmt.Sprintf("%s line %d", r.File, r.Line))}, detail...)
	}
	det := box("Entry", "", detail, w, auditDetailH+2, false)

	top := sBadge.Render("audit") + " " + chain + sDim.Render("  ·  "+m.audit.Dir())
	keys := " " + sKey.Render("j/k") + " " + sBarText.Render("move") + "  " +
		sKey.Render("f") + " " + sBarText.Render("failures only") + "  " +
		sKey.Render("/") + " " + sBarText.Render("search") + "  " +
		sKey.Render("r") + " " + sBarText.Render("reload") + "  " +
		sKey.Render("esc") + " " + sBarText.Render("back")
	if m.auditTyping || m.auditSearch.Value() != "" {
		keys = " " + m.auditSearch.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, fit(top, w), list, det, fit(keys, w))
}

func (m *Model) onAuditKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.auditTyping {
		switch k.String() {
		case "esc":
			m.auditTyping = false
			m.auditSearch.SetValue("")
			m.auditSearch.Blur()
		case "enter":
			m.auditTyping = false
			m.auditSearch.Blur()
		default:
			var cmd tea.Cmd
			m.auditSearch, cmd = m.auditSearch.Update(k)
			m.rebuildAudit()
			return m, cmd
		}
		m.rebuildAudit()
		return m, nil
	}
	switch k.String() {
	case "esc", "q", "backspace":
		if m.auditSearch.Value() != "" {
			m.auditSearch.SetValue("")
			m.rebuildAudit()
			return m, nil
		}
		m.screen = m.back
	case "?":
		m.back, m.screen = screenAudit, screenHelp
	case "j", "down":
		m.auditCursor++
		m.clampAudit()
	case "k", "up":
		m.auditCursor--
		m.clampAudit()
	case "ctrl+d", "pgdown":
		m.auditCursor += m.auditListHeight() / 2
		m.clampAudit()
	case "ctrl+u", "pgup":
		m.auditCursor -= m.auditListHeight() / 2
		m.clampAudit()
	case "g", "home":
		m.auditCursor = 0
		m.clampAudit()
	case "G", "end":
		m.auditCursor = len(m.auditVisible) - 1
		m.clampAudit()
	case "f":
		m.auditFailed = !m.auditFailed
		m.rebuildAudit()
	case "/":
		m.auditTyping = true
		m.auditSearch.Focus()
		return m, textinput.Blink
	case "r":
		return m, m.loadAudit()
	}
	return m, nil
}

func (m *Model) helpView() string {
	type kv struct{ k, v string }
	groups := []struct {
		title string
		keys  []kv
	}{
		{"Dashboard", []kv{
			{"j / k, ↓ / ↑", "move"}, {"ctrl+d / ctrl+u", "half page"}, {"g / G", "top / bottom"},
			{"enter", "project detail, or fold a group"}, {"space, h / l", "fold / unfold"},
			{"/", "fuzzy search by path"}, {"1 – 8", "filters, as in the tab bar"},
			{"S", "sort: tree, recent, size, build, changes"}, {"esc", "clear search and filter"},
			{"R", "refresh everything and remeasure sizes (audited)"}, {"a", "audit log"}, {"q", "quit"},
		}},
		{"Project actions (audited; need herdr)", []kv{
			{"o", "switch to the project's herdr workspace"},
			{"F", "show the folder in Finder (no herdr needed)"},
			{"e", "editor in a new pane there"},
			{"c", "Claude Code in a new pane there"},
			{"r", "run the dev server (asks the first time)"},
			{"s", "stop a panel run: ctrl+c, then its recorded PID"},
			{"L", "runs, with a live log"},
		}},
		{"Hygiene (audited; asks before moving anything)", []kv{
			{"f", "fetch all repos with a remote, 6 at a time"},
			{"p", "pull --ff-only: fast-forward, never a merge"},
			{"i", "git init a folder with no git"},
			{"x", "clean: preview, then build folders to the Trash"},
			{"A", "archive, or restore an archived project"},
			{"D", "move the whole project to the Trash (type its name)"},
		}},
		{"Detail", []kv{{"j / k", "scroll"}, {"R", "reload"}, {"esc", "back"}}},
		{"Runs", []kv{{"s", "stop selected"}, {"o", "go to its workspace"}, {"esc", "back"}}},
		{"Audit", []kv{{"f", "failures only"}, {"/", "search"}, {"r", "reload"}, {"esc", "back"}}},
	}
	render := func(gs []struct {
		title string
		keys  []kv
	}) []string {
		var out []string
		for _, g := range gs {
			out = append(out, " "+sAccent.Render(g.title))
			for _, e := range g.keys {
				out = append(out, "   "+sKey.Render(fit(e.k, 16))+" "+sSub.Render(e.v))
			}
			out = append(out, "")
		}
		return out
	}
	legend := []string{
		" " + sAccent.Render("Legend"),
		"   " + sYellow.Render("✚n") + sSub.Render(" modified  ") + sDim.Render("?n") + sSub.Render(" untracked  ") +
			sPeach.Render("↑n") + sSub.Render(" ahead  ") + sSky.Render("↓n") + sSub.Render(" behind"),
		"   " + sGreen.Render("● :port") + sSub.Render(" listening  ") + sTeal.Render("◆n") + sSub.Render(" containers  ") +
			sAccent.Render("▶") + sSub.Render(" started by the panel"),
		"   " + sSub.Render("age colour: ") + sGreen.Render("active ") + sBlue.Render("warm ") + sYellow.Render("dormant ") + sRed.Render("abandoned"),
		"",
		" " + sDim.Render("config: "+m.cfg.Path),
	}

	var lines []string
	if m.width >= 140 {
		// Two columns: dashboard and project actions left, the rest right.
		colW := (m.width - 2) / 2
		left := render(groups[:2])
		right := append(render(groups[2:]), legend...)
		for i := 0; i < max(len(left), len(right)); i++ {
			var l, r string
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			lines = append(lines, fit(l, colW)+r)
		}
	} else {
		lines = append(render(groups), legend...)
	}
	return box("Help", "any key closes", lines, m.width, m.height, true)
}
