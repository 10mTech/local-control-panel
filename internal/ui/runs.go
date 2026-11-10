package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"local-control-panel/internal/actions"
	"local-control-panel/internal/safety"
)

func runsTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return runsTickMsg{} })
}

func (m *Model) openRuns(from screen) tea.Cmd {
	m.back, m.screen = from, screenRuns
	return tea.Batch(m.loadRuns(), runsTick())
}

func (m *Model) runsListHeight() int {
	return min(max(3, len(m.runs)), max(3, (m.height-4)/3))
}

func (m *Model) clampRuns() {
	h := m.runsListHeight()
	m.runsCursor = min(m.runsCursor, len(m.runs)-1)
	m.runsCursor = max(m.runsCursor, 0)
	if m.runsCursor < m.runsOffset {
		m.runsOffset = m.runsCursor
	}
	if m.runsCursor >= m.runsOffset+h {
		m.runsOffset = m.runsCursor - h + 1
	}
}

func (m *Model) selectedRun() *runView {
	if m.runsCursor < 0 || m.runsCursor >= len(m.runs) {
		return nil
	}
	return &m.runs[m.runsCursor]
}

// loadTail reads the end of the selected run's log.
func (m *Model) loadTail() tea.Cmd {
	r := m.selectedRun()
	if r == nil {
		m.runsTailID, m.runsTail = "", nil
		return nil
	}
	if r.ID != m.runsTailID {
		m.runsTailID, m.runsTail = r.ID, nil
	}
	id, path, n := r.ID, r.Log, max(10, m.height)
	return func() tea.Msg {
		lines, err := actions.Tail(path, n)
		if err != nil {
			lines = []string{"cannot read the log: " + err.Error()}
		}
		return tailMsg{id: id, lines: lines}
	}
}

func runMark(r runView) string {
	switch r.state() {
	case "running":
		return sGreen.Render("●")
	case "stopped":
		return sDim.Render("■")
	default:
		return sYellow.Render("○")
	}
}

func (m *Model) runLine(r runView, w int) string {
	when := r.Started.Format("01-02 15:04")
	status := ""
	switch r.state() {
	case "running":
		status = sGreen.Render("up " + age(r.Started))
	case "stopped":
		status = sDim.Render(r.StopNote)
	default:
		status = sYellow.Render("exited on its own")
	}
	pid := ""
	if r.PID > 0 {
		pid = fmt.Sprintf("pid %d", r.PID)
	}
	line := " " + runMark(r) + " " + sDim.Render(when) + " " + sText.Render(fit(r.Project, 26)) + " " +
		sSub.Render(fit(safety.ShellJoin(r.Argv), 24)) + " " + sDim.Render(fit(pid, 10)) + " " +
		sDim.Render(fit(r.PaneID, 8)) + " " + status
	return fit(line, w)
}

func (m *Model) runsScreen() string {
	w := m.width
	listH := m.runsListHeight()
	var lines []string
	for i := m.runsOffset; i < len(m.runs) && i < m.runsOffset+listH; i++ {
		line := m.runLine(m.runs[i], w-2)
		if i == m.runsCursor {
			line = sSelected.Render(fit(ansi.Strip(line), w-2))
		}
		lines = append(lines, line)
	}
	if len(m.runs) == 0 {
		lines = append(lines, sDim.Render(" no runs yet: select a project on the dashboard and press r"))
	}
	alive := 0
	for _, r := range m.runs {
		if r.Alive {
			alive++
		}
	}
	list := box("Runs", fmt.Sprintf("%d running · %d recorded", alive, len(m.runs)), lines, w, listH+2, true)

	tailH := m.height - 2 - (listH + 2) - 2
	var tail []string
	title, note := "Log", ""
	if r := m.selectedRun(); r != nil {
		title = "Log · " + r.Project
		note = r.Log
		for _, l := range m.runsTail {
			tail = append(tail, " "+sSub.Render(cleanLogLine(l)))
		}
		if len(tail) > tailH {
			tail = tail[len(tail)-tailH:]
		}
	}
	logBox := box(title, note, tail, w, tailH+2, false)

	top := sBadge.Render("runs") + " " + sDim.Render("dev servers started from the panel · live log, refreshed every second")
	keys := " " + sKey.Render("j/k") + " " + sBarText.Render("move") + "  " +
		sKey.Render("s") + " " + sBarText.Render("stop") + "  " +
		sKey.Render("o") + " " + sBarText.Render("go to its workspace") + "  " +
		sKey.Render("esc") + " " + sBarText.Render("back")
	if m.flash != "" && time.Now().Before(m.flashUntil) {
		keys += "   " + m.flashStyle().Render(m.flash)
	}
	return lipgloss.JoinVertical(lipgloss.Left, fit(top, w), list, logBox, fit(keys, w))
}

// cleanLogLine turns raw terminal output into one plain line: escape codes
// removed, and only the last frame of a carriage-return progress line kept.
func cleanLogLine(s string) string {
	s = strings.TrimRight(s, "\r")
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		s = s[i+1:]
	}
	return strings.ReplaceAll(ansi.Strip(s), "\t", "    ")
}

func (m *Model) onRunsKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "q", "backspace", "L":
		m.screen = m.back
	case "?":
		m.back, m.screen = screenRuns, screenHelp
	case "j", "down":
		m.runsCursor++
		m.clampRuns()
		return m, m.loadTail()
	case "k", "up":
		m.runsCursor--
		m.clampRuns()
		return m, m.loadTail()
	case "s":
		r := m.selectedRun()
		if r == nil || !r.Alive {
			m.setFlash("that run is not running")
			return m, nil
		}
		run := r.Run
		return m, m.do("stopping "+run.Project, func(ctx context.Context) actions.Result {
			return m.acts.StopRun(ctx, run, "key:s runs")
		})
	case "o":
		r := m.selectedRun()
		if r == nil {
			return m, nil
		}
		run := r.Run
		return m, m.do("opening "+run.Project, func(ctx context.Context) actions.Result {
			return m.acts.FocusRun(ctx, run, "key:o runs")
		})
	}
	return m, nil
}
