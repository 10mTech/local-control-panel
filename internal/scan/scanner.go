package scan

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/10mTech/local-control-panel/internal/config"
)

type Scanner struct {
	cfg   *config.Config
	log   *log.Logger
	Sizes *SizeCache
}

func NewScanner(cfg *config.Config, debug *log.Logger, stateDir string) *Scanner {
	return &Scanner{cfg: cfg, log: debug, Sizes: OpenSizeCache(stateDir)}
}

// Fast finds projects and reads everything except folder sizes, which come
// from the cache. It runs git in up to 8 projects at once.
func (s *Scanner) Fast(ctx context.Context) []Project {
	start := time.Now()
	ignore := func(rel string) bool { return s.cfg.Project(rel).Ignore }
	ps := Discover(s.cfg.Root, s.cfg.Archive, s.cfg.ScanDepth, ignore)

	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := range ps {
		p := &ps[i]
		if p.Kind == KindGroup {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			detectStack(p)
			detectPort(p, s.cfg.Project(p.Rel).Port)
			if p.Kind == KindRepo {
				p.Git = readGit(ctx, p.Abs)
				p.LastChange = p.Git.LastCommit
			}
			if p.LastChange.IsZero() {
				p.LastChange = newestChange(p.Abs, 20000)
			}
			p.Activity = s.activity(p.LastChange)
			if sz, ok := s.Sizes.Get(p.Abs); ok {
				p.Size = &sz
			}
		}()
	}

	var ls []Listener
	var cs []container
	wg.Add(1)
	go func() {
		defer wg.Done()
		var err error
		if ls, err = listeners(ctx); err != nil {
			s.log.Printf("listeners: %v", err)
		}
		cs = containers(ctx)
	}()
	wg.Wait()

	for _, l := range ls {
		if i := owner(ps, l.Cwd); i >= 0 {
			ps[i].Running = append(ps[i].Running, l)
		}
	}
	for _, c := range cs {
		if i := owner(ps, c.WorkingDir); i >= 0 {
			ps[i].Containers = append(ps[i].Containers, c.Name)
		}
	}
	s.log.Printf("fast scan: %d entries in %s", len(ps), time.Since(start).Round(time.Millisecond))
	return ps
}

func (s *Scanner) activity(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	days := int(time.Since(t).Hours() / 24)
	a := s.cfg.Activity
	switch {
	case days <= a.Active:
		return "active"
	case days <= a.Warm:
		return "warm"
	case days <= a.Dormant:
		return "dormant"
	default:
		return "abandoned"
	}
}

// MeasureAll measures folder sizes in the background, 3 at a time, calling
// done for each project as it finishes. With force, cached sizes are ignored.
func (s *Scanner) MeasureAll(ctx context.Context, ps []Project, force bool, done func(abs string, sz Sizes)) {
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for _, p := range ps {
		if p.Kind == KindGroup || (!force && s.Sizes.Fresh(p.Abs)) {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(abs string) {
			defer wg.Done()
			defer func() { <-sem }()
			start := time.Now()
			sz := Measure(abs)
			s.Sizes.Put(abs, sz)
			s.log.Printf("measured %s in %s", abs, time.Since(start).Round(time.Millisecond))
			done(abs, sz)
		}(p.Abs)
	}
	wg.Wait()
	if err := s.Sizes.Save(); err != nil {
		s.log.Printf("save size cache: %v", err)
	}
}
