// Package ui is the Bubble Tea front end: dashboard, project detail, runs and
// audit screens. It reads state from scan and changes things only through actions.
package ui

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"

	"github.com/10mTech/local-control-panel/internal/actions"
	"github.com/10mTech/local-control-panel/internal/audit"
	"github.com/10mTech/local-control-panel/internal/config"
	"github.com/10mTech/local-control-panel/internal/herdr"
	"github.com/10mTech/local-control-panel/internal/scan"
)

type screen int

const (
	screenDashboard screen = iota
	screenDetail
	screenAudit
	screenRuns
	screenHelp
)

type sortMode int

const (
	sortTree sortMode = iota
	sortRecent
	sortSize
	sortBuild
	sortChanges
)

var sortNames = []string{"tree", "recent", "size", "build size", "changes"}

type Model struct {
	cfg     *config.Config
	scanner *scan.Scanner
	audit   *audit.Logger
	acts    *actions.Actions
	herdr   *herdr.Client
	log     *log.Logger
	events  chan tea.Msg
	watcher *fsnotify.Watcher
	watched map[string]bool

	width, height int
	screen, back  screen

	projects  []scan.Project
	view      []int // indices into projects, after filter, sort and collapse
	cursor    int
	offset    int
	collapsed map[string]bool
	sort      sortMode
	filter    int
	search    textinput.Model
	searching bool

	scanning   bool
	rescan     bool // another scan was asked for while one ran
	measuring  bool
	lastScan   time.Time
	scanTook   time.Duration
	refreshOp  *audit.Op // the audited manual refresh in flight
	pendingFS  bool
	flash      string
	flashErr   bool
	flashUntil time.Time

	herdrVersion string
	herdrErr     error
	busy         string  // action in flight; one at a time
	prompt       *prompt // confirmation or command input, drawn over the screen

	runs       []runView   // newest first
	listenerPG map[int]int // listener PID → process group, to tell panel runs from external ones
	runsCursor int
	runsOffset int
	runsTail   []string
	runsTailID string

	detail     *scan.Detail
	detailRel  string
	detailView viewport.Model

	auditRecs    []audit.Record
	auditChain   audit.Chain
	auditCursor  int
	auditOffset  int
	auditFailed  bool
	auditSearch  textinput.Model
	auditTyping  bool
	auditErr     string
	auditVisible []int
}

// Messages.
type (
	scanDoneMsg struct {
		projects   []scan.Project
		took       time.Duration
		runs       []runView
		listenerPG map[int]int
	}
	sizeMsg struct {
		abs string
		sz  scan.Sizes
	}
	sizesDoneMsg struct{}
	tickMsg      struct{}
	fsMsg        struct{}
	fsFireMsg    struct{}
	detailMsg    struct {
		rel string
		d   scan.Detail
	}
	auditMsg struct {
		recs  []audit.Record
		chain audit.Chain
		err   error
	}
)

func New(cfg *config.Config, s *scan.Scanner, a *audit.Logger, acts *actions.Actions, h *herdr.Client, debug *log.Logger) *Model {
	si := textinput.New()
	si.Prompt = "/ "
	si.Placeholder = "search projects"
	ai := textinput.New()
	ai.Prompt = "/ "
	ai.Placeholder = "search action, project, command"

	m := &Model{
		cfg: cfg, scanner: s, audit: a, acts: acts, herdr: h, log: debug,
		events:      make(chan tea.Msg, 256),
		watched:     map[string]bool{},
		collapsed:   map[string]bool{},
		search:      si,
		auditSearch: ai,
		scanning:    true,
	}
	if rel, err := filepath.Rel(cfg.Root, cfg.Archive); err == nil && !strings.HasPrefix(rel, "..") {
		m.collapsed[rel] = true
	}

	if w, err := fsnotify.NewWatcher(); err == nil {
		m.watcher = w
		go m.forwardFS()
	} else {
		debug.Printf("fsnotify: %v", err)
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	details := map[string]any{"root": m.cfg.Root, "config": m.cfg.Path, "pid": os.Getpid()}
	if m.cfg.CreatedDefaults {
		details["config_created"] = true
	}
	if len(m.cfg.Unknown) > 0 {
		details["config_unknown_keys"] = m.cfg.Unknown
		m.setError("config: unknown keys ignored (typo?): " + strings.Join(m.cfg.Unknown, ", "))
	}
	if err := m.audit.Event(audit.Entry{Action: "session.start", Cwd: m.cfg.Root, Trigger: "launch", Details: details}); err != nil {
		m.log.Printf("audit: %v", err)
	}
	return tea.Batch(m.scanCmd(), m.waitEvent(), tickAfter(m.cfg.RefreshSeconds), m.checkHerdr())
}

// Snapshot renders one screen without a terminal: "dashboard", "help",
// "audit", or "detail:<path>". It scans and measures sizes synchronously.
func Snapshot(m *Model, w, h int, which string) string {
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.Update(m.scanCmd()())
	m.scanner.MeasureAll(context.Background(), m.projects, false, func(abs string, sz scan.Sizes) {
		m.Update(sizeMsg{abs: abs, sz: sz})
	})
	m.measuring = false
	if m.watcher != nil {
		m.watcher.Close()
	}
	switch {
	case which == "help":
		m.screen = screenHelp
	case which == "audit":
		m.screen = screenAudit
		m.Update(m.loadAudit()())
	case which == "runs":
		m.screen = screenRuns
		m.Update(m.loadRuns()())
	case strings.HasPrefix(which, "detail:"):
		rel := strings.TrimPrefix(which, "detail:")
		for _, p := range m.projects {
			if p.Rel == rel {
				m.openDetail(p)
				m.Update(loadDetail(p)())
				m.Update(m.loadAudit()())
			}
		}
	}
	return m.View()
}

func tickAfter(sec int) tea.Cmd {
	return tea.Tick(time.Duration(sec)*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) waitEvent() tea.Cmd {
	return func() tea.Msg { return <-m.events }
}

func (m *Model) scanCmd() tea.Cmd {
	m.scanning = true
	return func() tea.Msg {
		start := time.Now()
		ps := m.scanner.Fast(context.Background())
		var pids []int
		for _, p := range ps {
			for _, l := range p.Running {
				pids = append(pids, l.PID)
			}
		}
		return scanDoneMsg{projects: ps, took: time.Since(start), runs: m.listRuns(), listenerPG: actions.ProcGroups(pids)}
	}
}

// requestScan starts a scan, or queues one if a scan is already running.
func (m *Model) requestScan() tea.Cmd {
	if m.scanning {
		m.rescan = true
		return nil
	}
	return m.scanCmd()
}

func (m *Model) measureCmd(force bool) tea.Cmd {
	if m.measuring {
		return nil
	}
	m.measuring = true
	ps := append([]scan.Project(nil), m.projects...)
	return func() tea.Msg {
		m.scanner.MeasureAll(context.Background(), ps, force, func(abs string, sz scan.Sizes) {
			m.events <- sizeMsg{abs: abs, sz: sz}
		})
		return sizesDoneMsg{}
	}
}

// remeasure measures one project again, ignoring the cache, e.g. after a clean.
func (m *Model) remeasure(abs string) tea.Cmd {
	if abs == "" {
		return nil
	}
	ps := []scan.Project{{Abs: abs}}
	return func() tea.Msg {
		m.scanner.MeasureAll(context.Background(), ps, true, func(abs string, sz scan.Sizes) {
			m.events <- sizeMsg{abs: abs, sz: sz}
		})
		return nil
	}
}

// forwardFS turns create/remove/rename events in watched folders into fsMsg.
func (m *Model) forwardFS() {
	for {
		select {
		case ev, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				select {
				case m.events <- fsMsg{}:
				default:
				}
			}
		case err, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
			m.log.Printf("fsnotify: %v", err)
		}
	}
}

// watchGroups watches the root and every group folder, so a clone or a new
// folder anywhere a project can appear triggers a rescan.
func (m *Model) watchGroups() {
	if m.watcher == nil {
		return
	}
	want := map[string]bool{m.cfg.Root: true}
	for _, p := range m.projects {
		if p.Kind == scan.KindGroup {
			want[p.Abs] = true
		}
	}
	for dir := range want {
		if !m.watched[dir] {
			if err := m.watcher.Add(dir); err == nil {
				m.watched[dir] = true
			}
		}
	}
	for dir := range m.watched {
		if !want[dir] {
			_ = m.watcher.Remove(dir)
			delete(m.watched, dir)
		}
	}
}

func (m *Model) setFlash(s string) {
	m.flash, m.flashErr = s, false
	m.flashUntil = time.Now().Add(4 * time.Second)
}

func (m *Model) setError(s string) {
	m.flash, m.flashErr = s, true
	m.flashUntil = time.Now().Add(8 * time.Second)
}

func (m *Model) selected() *scan.Project {
	if m.cursor < 0 || m.cursor >= len(m.view) || m.view[m.cursor] >= len(m.projects) {
		return nil
	}
	return &m.projects[m.view[m.cursor]]
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.detailView.Width, m.detailView.Height = m.detailSize()
		m.clampCursor()
		return m, nil

	case scanDoneMsg:
		return m, m.onScanDone(msg)

	case sizeMsg:
		for i := range m.projects {
			if m.projects[i].Abs == msg.abs {
				sz := msg.sz
				m.projects[i].Size = &sz
			}
		}
		if m.sort == sortSize || m.sort == sortBuild || m.filter != 0 {
			m.rebuild()
		}
		return m, m.waitEvent()

	case sizesDoneMsg:
		m.measuring = false
		return m, nil

	case fsMsg:
		cmd := m.waitEvent()
		if !m.pendingFS {
			m.pendingFS = true
			// Let a clone or a move settle before rescanning.
			return m, tea.Batch(cmd, tea.Tick(time.Second, func(time.Time) tea.Msg { return fsFireMsg{} }))
		}
		return m, cmd

	case fsFireMsg:
		m.pendingFS = false
		m.log.Printf("folder change: rescanning")
		return m, m.requestScan()

	case tickMsg:
		return m, tea.Batch(m.requestScan(), tickAfter(m.cfg.RefreshSeconds))

	case detailMsg:
		if msg.rel == m.detailRel {
			d := msg.d
			m.detail = &d
			m.refreshDetailView()
		}
		return m, nil

	case auditMsg:
		m.auditRecs, m.auditChain = msg.recs, msg.chain
		m.auditErr = ""
		if msg.err != nil {
			m.auditErr = msg.err.Error()
		}
		m.rebuildAudit()
		if m.screen == screenDetail {
			m.refreshDetailView()
		}
		return m, nil

	case herdrMsg:
		m.herdrVersion, m.herdrErr = msg.version, msg.err
		if msg.err != nil {
			m.log.Printf("herdr check: %v", msg.err)
		}
		return m, nil

	case actionMsg:
		m.busy = ""
		if msg.res.Err != nil {
			m.setError(errorText(msg.res.Err))
			if herdr.IsNotRunning(msg.res.Err) {
				m.herdrErr = msg.res.Err
			}
		} else {
			m.setFlash(msg.res.Msg)
		}
		return m, tea.Batch(m.requestScan(), m.loadRuns(), m.checkHerdr(), m.remeasure(msg.res.Remeasure))

	case runsMsg:
		m.runs = msg.runs
		m.clampRuns()
		return m, m.loadTail()

	case tailMsg:
		if msg.id == m.runsTailID {
			m.runsTail = msg.lines
		}
		return m, nil

	case fetchProgressMsg:
		if m.busy != "" {
			m.busy = fmt.Sprintf("fetching %d/%d", msg.done, msg.total)
		}
		return m, m.waitEvent()

	case cleanPlanMsg:
		m.onCleanPlan(msg)
		return m, nil

	case runsTickMsg:
		if m.screen != screenRuns {
			return m, nil
		}
		return m, tea.Batch(m.loadRuns(), runsTick())

	case tea.KeyMsg:
		if m.prompt != nil {
			return m.onPromptKey(msg)
		}
		return m.onKey(msg)
	}
	return m, nil
}

func (m *Model) onScanDone(msg scanDoneMsg) tea.Cmd {
	m.scanning = false
	keep := m.selectedRel() // m.view indexes the old list until rebuilt
	m.projects = msg.projects
	m.runs, m.listenerPG = msg.runs, msg.listenerPG
	m.clampRuns()
	m.lastScan = time.Now()
	m.scanTook = msg.took
	m.rebuildAt(keep)
	m.watchGroups()

	var cmds []tea.Cmd
	if m.refreshOp != nil {
		repos := 0
		for _, p := range m.projects {
			if p.Kind == scan.KindRepo {
				repos++
			}
		}
		if err := m.refreshOp.End("ok", nil, map[string]any{"entries": len(m.projects), "repos": repos, "sizes": "remeasuring in background"}, nil); err != nil {
			m.log.Printf("audit: %v", err)
		}
		m.refreshOp = nil
		cmds = append(cmds, m.measureCmd(true))
	} else {
		cmds = append(cmds, m.measureCmd(false))
	}
	if m.rescan {
		m.rescan = false
		cmds = append(cmds, m.scanCmd())
	}
	return tea.Batch(cmds...)
}

func (m *Model) quit() (tea.Model, tea.Cmd) {
	if err := m.audit.Event(audit.Entry{Action: "session.end", Trigger: "key:q", Cwd: m.cfg.Root}); err != nil {
		m.log.Printf("audit: %v", err)
	}
	if m.watcher != nil {
		m.watcher.Close()
	}
	return m, tea.Quit
}

func (m *Model) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m.quit()
	}
	switch m.screen {
	case screenHelp:
		m.screen = m.back
		return m, nil
	case screenDetail:
		return m.onDetailKey(k)
	case screenAudit:
		return m.onAuditKey(k)
	case screenRuns:
		return m.onRunsKey(k)
	}
	return m.onDashboardKey(k)
}

func (m *Model) onDashboardKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searching {
		switch k.String() {
		case "esc":
			m.searching = false
			m.search.SetValue("")
			m.search.Blur()
			m.rebuild()
			return m, nil
		case "enter":
			m.searching = false
			m.search.Blur()
			return m, nil
		case "up", "down", "ctrl+n", "ctrl+p":
			// fall through to movement below
		default:
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(k)
			m.rebuild()
			return m, cmd
		}
	}

	page := max(1, m.listHeight()-1)
	switch k.String() {
	case "q":
		return m.quit()
	case "?":
		m.back, m.screen = screenDashboard, screenHelp
	case "j", "down", "ctrl+n":
		m.move(1)
	case "k", "up", "ctrl+p":
		m.move(-1)
	case "ctrl+d", "pgdown":
		m.move(page / 2)
	case "ctrl+u", "pgup":
		m.move(-page / 2)
	case "g", "home":
		m.cursor = 0
		m.clampCursor()
	case "G", "end":
		m.cursor = len(m.view) - 1
		m.clampCursor()
	case "/":
		m.searching = true
		m.search.Focus()
		return m, textinput.Blink
	case "esc":
		if m.search.Value() != "" || m.filter != 0 {
			m.search.SetValue("")
			m.filter = 0
			m.rebuild()
		}
	case "S":
		m.sort = (m.sort + 1) % sortMode(len(sortNames))
		m.rebuild()
		m.setFlash("sort: " + sortNames[m.sort])
	case "1", "2", "3", "4", "5", "6", "7", "8":
		i := int(k.String()[0] - '1')
		if i < len(filters) {
			m.filter = i
			m.rebuild()
		}
	case " ", "left", "right", "h", "l":
		if p := m.selected(); p != nil && m.hasChildren(m.view[m.cursor]) {
			switch k.String() {
			case "left", "h":
				m.collapsed[p.Rel] = true
			case "right", "l":
				delete(m.collapsed, p.Rel)
			default:
				m.collapsed[p.Rel] = !m.collapsed[p.Rel]
			}
			m.rebuild()
		}
	case "enter":
		p := m.selected()
		if p == nil {
			return m, nil
		}
		if p.Kind == scan.KindGroup {
			m.collapsed[p.Rel] = !m.collapsed[p.Rel]
			m.rebuild()
			return m, nil
		}
		return m, m.openDetail(*p)
	case "R":
		return m, m.manualRefresh()
	case "a":
		m.back, m.screen = screenDashboard, screenAudit
		return m, m.loadAudit()
	case "L":
		return m, m.openRuns(screenDashboard)
	case "o", "e", "c", "r", "s":
		if p := m.selected(); p != nil {
			return m, m.projectAction(k.String(), p)
		}
	case "p", "i", "x", "A", "D":
		if p := m.selected(); p != nil {
			return m, m.hygieneAction(k.String(), p)
		}
	case "F":
		if p := m.selected(); p != nil {
			return m, m.finder(p)
		}
	case "f":
		return m, m.askFetchAll()
	}
	return m, nil
}

func (m *Model) manualRefresh() tea.Cmd {
	if m.refreshOp != nil {
		return nil
	}
	op, err := m.audit.Begin(audit.Entry{Action: "refresh", Trigger: "key:R", Cwd: m.cfg.Root,
		Details: map[string]any{"scope": "rescan all projects, remeasure all sizes"}})
	if err != nil {
		m.log.Printf("audit: %v", err)
		m.setFlash("audit write failed: " + err.Error())
		return nil
	}
	m.refreshOp = op
	m.setFlash("refreshing everything")
	return m.requestScan()
}

func (m *Model) move(d int) {
	m.cursor += d
	m.clampCursor()
}

func (m *Model) clampCursor() {
	if m.cursor >= len(m.view) {
		m.cursor = len(m.view) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if h > 0 && m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset > max(0, len(m.view)-h) {
		m.offset = max(0, len(m.view)-h)
	}
}

func (m *Model) hasChildren(i int) bool {
	return i+1 < len(m.projects) && strings.HasPrefix(m.projects[i+1].Rel, m.projects[i].Rel+"/")
}

// flat is true when rows are shown as a list of paths rather than a tree.
func (m *Model) flat() bool {
	return m.sort != sortTree || m.filter != 0 || m.search.Value() != ""
}

func (m *Model) selectedRel() string {
	if p := m.selected(); p != nil {
		return p.Rel
	}
	return ""
}

func (m *Model) rebuild() { m.rebuildAt(m.selectedRel()) }

// rebuildAt recomputes the visible rows, keeping the cursor on keep if it is
// still listed.
func (m *Model) rebuildAt(keep string) {
	flat := m.flat()
	q := m.search.Value()
	f := filters[m.filter]

	m.view = m.view[:0]
	for i := range m.projects {
		p := &m.projects[i]
		if flat && p.Kind == scan.KindGroup {
			continue
		}
		if !f.match(p, m.cfg) {
			continue
		}
		if q != "" && !fuzzy(q, p.Rel) {
			continue
		}
		if !flat && m.hiddenByCollapse(p.Rel) {
			continue
		}
		m.view = append(m.view, i)
	}

	if flat {
		ps := m.projects
		less := map[sortMode]func(a, b *scan.Project) bool{
			sortRecent:  func(a, b *scan.Project) bool { return a.LastChange.After(b.LastChange) },
			sortSize:    func(a, b *scan.Project) bool { return sizeOf(a, false) > sizeOf(b, false) },
			sortBuild:   func(a, b *scan.Project) bool { return sizeOf(a, true) > sizeOf(b, true) },
			sortChanges: func(a, b *scan.Project) bool { return changes(a) > changes(b) },
		}[m.sort]
		if less != nil {
			sort.SliceStable(m.view, func(i, j int) bool { return less(&ps[m.view[i]], &ps[m.view[j]]) })
		}
	}

	m.cursor = 0
	for i, idx := range m.view {
		if m.projects[idx].Rel == keep {
			m.cursor = i
			break
		}
	}
	m.clampCursor()
}

func (m *Model) hiddenByCollapse(rel string) bool {
	for c := range m.collapsed {
		if strings.HasPrefix(rel, c+"/") {
			return true
		}
	}
	return false
}

func sizeOf(p *scan.Project, build bool) int64 {
	if p.Size == nil {
		return -1
	}
	if build {
		return p.Size.Build
	}
	return p.Size.Total
}

func changes(p *scan.Project) int {
	return p.Git.Dirty + p.Git.Untracked
}

type filterDef struct {
	name  string
	match func(p *scan.Project, cfg *config.Config) bool
}

var filters = []filterDef{
	{"All", func(*scan.Project, *config.Config) bool { return true }},
	{"Dirty", func(p *scan.Project, _ *config.Config) bool {
		return p.Kind == scan.KindRepo && changes(p) > 0
	}},
	{"Unpushed", func(p *scan.Project, _ *config.Config) bool { return p.Unpushed() }},
	{"No git", func(p *scan.Project, _ *config.Config) bool { return p.Kind == scan.KindPlain }},
	{"Running", func(p *scan.Project, _ *config.Config) bool { return p.IsRunning() }},
	{"Reclaim", func(p *scan.Project, _ *config.Config) bool {
		return (p.Activity == "dormant" || p.Activity == "abandoned") && p.Size != nil && p.Size.Build > 0
	}},
	{"Not mine", func(p *scan.Project, cfg *config.Config) bool {
		return p.Kind == scan.KindRepo && p.Git.Owner != "" && !cfg.IsMine(p.Git.Owner)
	}},
	{"No deps", func(p *scan.Project, _ *config.Config) bool { return p.NotInstalled }},
}
