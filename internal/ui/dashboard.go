package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"local-control-panel/internal/safety"
	"local-control-panel/internal/scan"
)

const sideMinWidth = 120 // below this the side panel is hidden

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	var s string
	switch m.screen {
	case screenHelp:
		s = m.helpView()
	case screenDetail:
		s = m.detailScreen()
	case screenAudit:
		s = m.auditScreen()
	case screenRuns:
		s = m.runsScreen()
	default:
		s = m.dashboardView()
	}
	if m.prompt != nil {
		s = m.overlay(s)
	}
	return s
}

func (m *Model) flashStyle() lipgloss.Style {
	if m.flashErr {
		return sRed.Bold(true)
	}
	return sAccent
}

// listHeight is the number of project rows that fit in the list window.
func (m *Model) listHeight() int {
	// top bar, tab bar, footer, two border lines, column header
	return max(1, m.height-3-2-1)
}

func (m *Model) listWidth() int {
	if m.width >= sideMinWidth {
		return m.width - m.sideWidth()
	}
	return m.width
}

func (m *Model) sideWidth() int {
	return min(56, max(44, m.width/3))
}

func (m *Model) dashboardView() string {
	bodyH := m.height - 3
	list := m.listWindow(m.listWidth(), bodyH)
	body := list
	if m.width >= sideMinWidth {
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, m.sideWindow(m.sideWidth(), bodyH))
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.topBar(), m.tabBar(), body, m.footer())
}

func (m *Model) topBar() string {
	repos, noGit, dirty, running, unpushed := 0, 0, 0, 0, 0
	for i := range m.projects {
		p := &m.projects[i]
		switch p.Kind {
		case scan.KindRepo:
			repos++
		case scan.KindPlain:
			noGit++
		}
		if p.Kind == scan.KindRepo && changes(p) > 0 {
			dirty++
		}
		if p.IsRunning() {
			running++
		}
		if p.Unpushed() {
			unpushed++
		}
	}
	left := sBadge.Render("local-control-panel") + " " +
		sText.Render(fmt.Sprintf("%d repos", repos)) + sDim.Render(" · ") +
		sPeach.Render(fmt.Sprintf("%d no git", noGit)) + sDim.Render(" · ") +
		sYellow.Render(fmt.Sprintf("%d dirty", dirty)) + sDim.Render(" · ") +
		sSky.Render(fmt.Sprintf("%d unpushed", unpushed)) + sDim.Render(" · ") +
		sGreen.Render(fmt.Sprintf("%d running", running))
	if n := m.liveRuns(); n > 0 {
		left += sDim.Render(" · ") + sAccent.Render(fmt.Sprintf("▶ %d panel", n))
	}
	switch {
	case m.herdrErr != nil:
		left += sDim.Render(" · ") + sRed.Render("herdr off")
	case m.herdrVersion != "":
		left += sDim.Render(" · ") + sDim.Render(m.herdrVersion)
	}

	var status string
	switch {
	case m.flash != "" && time.Now().Before(m.flashUntil):
		status = m.flashStyle().Render(m.flash)
	case m.busy != "":
		status = sAccent.Render(m.busy + "…")
	case m.scanning && m.lastScan.IsZero():
		status = sDim.Render("scanning…")
	case m.scanning:
		status = sDim.Render("refreshing…")
	default:
		status = sDim.Render(fmt.Sprintf("scanned %s in %s", m.lastScan.Format("15:04:05"), m.scanTook.Round(time.Millisecond)))
	}
	if m.measuring {
		status = sDim.Render("measuring sizes · ") + status
	}
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(status) - 1
	if gap < 1 {
		if m.flash != "" && time.Now().Before(m.flashUntil) {
			return fit(status, m.width) // a message beats the counters on a narrow screen
		}
		return fit(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + status + " "
}

func (m *Model) liveRuns() int {
	n := 0
	for _, r := range m.runs {
		if r.Alive {
			n++
		}
	}
	return n
}

func (m *Model) tabBar() string {
	counts := make([]int, len(filters))
	for i := range m.projects {
		p := &m.projects[i]
		for f := range filters {
			if f > 0 && p.Kind == scan.KindGroup {
				continue
			}
			if filters[f].match(p, m.cfg) {
				counts[f]++
			}
		}
	}
	var parts []string
	for i, f := range filters {
		label := fmt.Sprintf("%d %s %d", i+1, f.name, counts[i])
		if i == 0 {
			label = "1 All"
		}
		if i == m.filter {
			parts = append(parts, sTabOn.Render(label))
		} else {
			parts = append(parts, sTabOff.Render(label))
		}
	}
	bar := strings.Join(parts, " ")
	right := sDim.Render("sort: ") + sSub.Render(sortNames[m.sort])
	gap := m.width - ansi.StringWidth(bar) - ansi.StringWidth(right) - 1
	if gap < 1 {
		return fit(bar, m.width)
	}
	return bar + strings.Repeat(" ", gap) + right + " "
}

func (m *Model) footer() string {
	if m.searching {
		return " " + m.search.View()
	}
	keys := []string{"? help", "enter detail", "o open", "F finder", "e edit", "c claude", "r run", "s stop", "L runs", "f fetch", "p pull", "x clean", "A archive", "D trash", "/ search", "1-8 filter", "S sort", "a audit", "q quit"}
	var parts []string
	for _, k := range keys {
		key, label, _ := strings.Cut(k, " ")
		parts = append(parts, sKey.Render(key)+" "+sBarText.Render(label))
	}
	s := " " + strings.Join(parts, sDim.Render("  "))
	if q := m.search.Value(); q != "" {
		s = " " + sAccent.Render("/"+q) + sDim.Render("  esc clears ·") + s
	}
	return fit(s, m.width)
}

type columns struct {
	name, stack, git, run, disk, build, age int
}

func (m *Model) columns(inner int) columns {
	c := columns{stack: 15, git: 24, run: 12, disk: 6, build: 6, age: 5}
	fixed := c.stack + c.git + c.run + c.disk + c.build + c.age + 7 // one space between each
	c.name = inner - fixed - 1
	if c.name < 22 {
		// Narrow terminal: drop stack and build first.
		c.name += c.stack + c.build + 2
		c.stack, c.build = 0, 0
	}
	return c
}

func (m *Model) listWindow(w, h int) string {
	inner := w - 2
	c := m.columns(inner)
	hdr := " " + fit("PROJECT", c.name) + " "
	if c.stack > 0 {
		hdr += fit("STACK", c.stack) + " "
	}
	hdr += fit("GIT", c.git) + " " + fit("RUNNING", c.run) + " " + fitLeft("DISK", c.disk) + " "
	if c.build > 0 {
		hdr += fitLeft("BUILD", c.build) + " "
	}
	hdr += fitLeft("AGE", c.age)
	lines := []string{sHeader.Render(fit(hdr, inner))}

	rows := h - 3
	for i := m.offset; i < len(m.view) && i < m.offset+rows; i++ {
		idx := m.view[i]
		line := m.row(idx, c, inner)
		if i == m.cursor {
			line = sSelected.Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	if len(m.view) == 0 && !m.scanning {
		lines = append(lines, sDim.Render("  nothing matches"))
	}

	title := "Projects"
	if f := filters[m.filter]; m.filter != 0 {
		title += " · " + f.name
	}
	note := ""
	if len(m.view) > 0 {
		note = fmt.Sprintf("%d/%d", m.cursor+1, len(m.view))
	}
	return box(title, note, lines, w, h, true)
}

func (m *Model) row(idx int, c columns, inner int) string {
	p := &m.projects[idx]

	var name string
	if m.flat() {
		name = p.Rel
	} else {
		indent := strings.Repeat("  ", p.Depth-1)
		marker := "  "
		if m.hasChildren(idx) {
			marker = "▾ "
			if m.collapsed[p.Rel] {
				marker = "▸ "
			}
		}
		name = indent + marker + p.Name
	}
	nameStyle := sText
	switch {
	case p.Kind == scan.KindGroup:
		nameStyle = sSub.Bold(true)
		name += "/"
	case p.Archived:
		nameStyle = sDim
	}
	tags := ""
	if p.NotInstalled {
		tags += sDim.Render(" no deps")
	}
	if p.Kind == scan.KindRepo && p.Git.Owner != "" && !m.cfg.IsMine(p.Git.Owner) {
		tags += sDim.Render(" @" + p.Git.Owner)
	}
	nameCell := nameStyle.Render(name) + tags
	line := " " + fit(nameCell, c.name) + " "
	if c.stack > 0 {
		line += fit(sSub.Render(stackCell(p)), c.stack) + " "
	}
	line += fit(m.gitCell(idx), c.git) + " " + fit(m.runCell(p), c.run) + " "
	line += fitLeft(diskCell(p), c.disk) + " "
	if c.build > 0 {
		line += fitLeft(buildCell(p), c.build) + " "
	}
	line += fitLeft(ageCell(p), c.age)
	return fit(line, inner)
}

func stackCell(p *scan.Project) string {
	s := p.StackLabel()
	if p.Docker {
		if s == "" {
			return "Docker"
		}
		s += "+Docker"
	}
	return s
}

func (m *Model) gitCell(idx int) string {
	p := &m.projects[idx]
	g := p.Git
	switch p.Kind {
	case scan.KindPlain:
		return sPeach.Render("no git")
	case scan.KindGroup:
		n := 0
		for j := idx + 1; j < len(m.projects) && strings.HasPrefix(m.projects[j].Rel, p.Rel+"/"); j++ {
			if m.projects[j].Kind == scan.KindRepo {
				n++
			}
		}
		if n == 1 {
			return sDim.Render("1 repo")
		}
		return sDim.Render(fmt.Sprintf("%d repos", n))
	}
	switch {
	case g.Cloning:
		return sSky.Render("cloning…")
	case g.Err != "":
		return sRed.Render("git error")
	case g.NoCommits:
		return sYellow.Render("no commits")
	}
	s := sSub.Render(ansi.Truncate(g.Branch, 12, "…"))
	if g.Dirty > 0 {
		s += sYellow.Render(fmt.Sprintf(" ✚%d", g.Dirty))
	}
	if g.Untracked > 0 {
		s += sDim.Render(fmt.Sprintf(" ?%d", g.Untracked))
	}
	if g.Ahead > 0 {
		s += sPeach.Render(fmt.Sprintf(" ↑%d", g.Ahead))
	}
	if g.Behind > 0 {
		s += sSky.Render(fmt.Sprintf(" ↓%d", g.Behind))
	}
	switch {
	case g.Remote == "":
		s += sDim.Render(" local")
	case g.Upstream == "":
		s += sPeach.Render(" no upstream")
	}
	return s
}

func (m *Model) runCell(p *scan.Project) string {
	var parts []string
	seen := map[int]bool{}
	for _, l := range p.Running {
		if !seen[l.Port] {
			seen[l.Port] = true
			parts = append(parts, fmt.Sprintf(":%d", l.Port))
		}
	}
	s := ""
	if len(parts) > 0 {
		s = sGreen.Render("● " + strings.Join(parts, " "))
	}
	if m.activeRun(p.Rel) != nil {
		if s == "" {
			s = sAccent.Render("▶ starting")
		} else {
			s = sAccent.Render("▶") + s[strings.Index(s, "●")+len("●"):]
		}
	}
	if n := len(p.Containers); n > 0 {
		if s != "" {
			s += " "
		}
		s += sTeal.Render(fmt.Sprintf("◆%d", n))
	}
	return s
}

func diskCell(p *scan.Project) string {
	if p.Kind == scan.KindGroup {
		return ""
	}
	if p.Size == nil {
		return sDim.Render("…")
	}
	return sSub.Render(humanBytes(p.Size.Total))
}

func buildCell(p *scan.Project) string {
	if p.Size == nil || p.Size.Build == 0 {
		return ""
	}
	st := sSub
	if p.Size.Build >= 500<<20 {
		st = sPeach
	}
	return st.Render(humanBytes(p.Size.Build))
}

func ageCell(p *scan.Project) string {
	st, ok := activityStyle[p.Activity]
	if !ok {
		st = sDim
	}
	return st.Render(age(p.LastChange))
}

// sideWindow is the summary of the selected project, next to the list.
func (m *Model) sideWindow(w, h int) string {
	p := m.selected()
	if p == nil {
		return box("Details", "", nil, w, h, false)
	}
	lines := m.summaryLines(p, w-2)
	return box(p.Name, "enter for more", lines, w, h, false)
}

// summaryLines is shared by the side panel and the detail screen.
func (m *Model) summaryLines(p *scan.Project, width int) []string {
	cfg := m.cfg
	kv := func(k, v string) string {
		return " " + sDim.Render(fit(k, 9)) + " " + v
	}
	var out []string
	add := func(s ...string) { out = append(out, s...) }

	kind := map[scan.Kind]string{scan.KindRepo: "git repo", scan.KindGroup: "group of repos", scan.KindPlain: "folder, no git"}[p.Kind]
	if p.Archived {
		kind += ", archived"
	}
	add(kv("path", sText.Render(p.Rel)), kv("kind", sSub.Render(kind)))
	if s := stackCell(p); s != "" {
		add(kv("stack", sSub.Render(s)))
	}
	if p.NotInstalled {
		add(kv("deps", sYellow.Render("not installed (no node_modules)")))
	}
	add("")

	if p.Kind == scan.KindRepo {
		g := p.Git
		switch {
		case g.Cloning:
			add(kv("git", sSky.Render("clone in progress")))
		case g.Err != "":
			add(kv("git", sRed.Render(g.Err)))
		default:
			add(kv("branch", sText.Render(g.Branch)))
			up := g.Upstream
			if up == "" {
				up = sPeach.Render("none")
			} else {
				up = sSub.Render(up) + sDim.Render(fmt.Sprintf("  ↑%d ↓%d", g.Ahead, g.Behind))
			}
			add(kv("upstream", up))
			add(kv("changes", sSub.Render(fmt.Sprintf("%d modified, %d untracked", g.Dirty, g.Untracked))))
			if g.NoCommits {
				add(kv("commit", sYellow.Render("no commits yet")))
			} else {
				add(kv("commit", sSub.Render(ago(g.LastCommit))+sDim.Render(" · ")+sText.Render(g.Subject)))
			}
			remote := g.Remote
			if remote == "" {
				remote = sDim.Render("none (local only)")
			} else {
				remote = sSub.Render(remote)
			}
			add(kv("remote", remote))
			if g.Owner != "" {
				who := sGreen.Render("yours")
				if !cfg.IsMine(g.Owner) {
					who = sPeach.Render("not yours")
				}
				add(kv("owner", sText.Render(g.Owner)+sDim.Render(" · ")+who))
			}
		}
		add("")
	}

	if p.Kind != scan.KindGroup {
		run := m.activeRun(p.Rel)
		if run != nil {
			add(kv("panel run", sAccent.Render("▶ "+safety.ShellJoin(run.Argv))+sDim.Render(fmt.Sprintf(" pid %d · up %s · L for log", run.PID, age(run.Started)))))
		}
		if len(p.Running) == 0 && len(p.Containers) == 0 && run == nil {
			add(kv("running", sDim.Render("nothing")))
		}
		for i, l := range p.Running {
			k := ""
			if i == 0 {
				k = "running"
			}
			who := sDim.Render(" external")
			if m.panelListener(l) {
				who = sAccent.Render(" panel")
			}
			add(kv(k, sGreen.Render(fmt.Sprintf(":%d", l.Port))+sSub.Render(fmt.Sprintf(" %s pid %d", l.Command, l.PID))+who))
		}
		for i, c := range p.Containers {
			k := ""
			if i == 0 && len(p.Running) == 0 {
				k = "running"
			}
			add(kv(k, sTeal.Render("◆ "+c)))
		}
		if p.Port > 0 {
			add(kv("dev port", sText.Render(fmt.Sprintf("%d", p.Port))+sDim.Render(" from "+p.PortSource)))
		}
		add("")

		if p.Size == nil {
			add(kv("disk", sDim.Render("measuring…")))
		} else {
			add(kv("disk", sText.Render(humanBytes(p.Size.Total))+sDim.Render(" measured "+ago(p.Size.ScannedAt))))
			for _, b := range scan.BuildDirs {
				if n, ok := p.Size.BuildDirs[b]; ok {
					add(kv("", sSub.Render(fit(b, 18))+" "+buildStyle(n).Render(humanBytes(n))))
				}
			}
		}
		act := p.Activity
		if st, ok := activityStyle[act]; ok {
			act = st.Render(act)
		}
		if !p.LastChange.IsZero() {
			add(kv("activity", act+sDim.Render(" · last change "+p.LastChange.Format("2006-01-02"))))
		}
	}
	for i := range out {
		out[i] = fit(out[i], width)
	}
	return out
}

func buildStyle(n int64) lipgloss.Style {
	if n >= 500<<20 {
		return sPeach
	}
	return sSub
}
