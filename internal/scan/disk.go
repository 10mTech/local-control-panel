package scan

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// BuildDirs are the only folders cleanup may remove (Safety rule 2). Sizes
// report them separately so the dashboard shows what is reclaimable.
var BuildDirs = []string{"node_modules", "target", ".next", "dist", "build", ".turbo", "src-tauri/target"}

const sizeTTL = 6 * time.Hour

// SizeCache keeps measured sizes on disk between runs.
type SizeCache struct {
	path string
	mu   sync.Mutex
	m    map[string]Sizes
}

func OpenSizeCache(stateDir string) *SizeCache {
	c := &SizeCache{path: filepath.Join(stateDir, "sizes.json"), m: map[string]Sizes{}}
	if b, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(b, &c.m)
	}
	return c
}

func (c *SizeCache) Get(abs string) (Sizes, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.m[abs]
	return s, ok
}

func (c *SizeCache) Fresh(abs string) bool {
	s, ok := c.Get(abs)
	return ok && time.Since(s.ScannedAt) < sizeTTL
}

func (c *SizeCache) Put(abs string, s Sizes) {
	c.mu.Lock()
	c.m[abs] = s
	c.mu.Unlock()
}

func (c *SizeCache) Save() error {
	c.mu.Lock()
	b, err := json.Marshal(c.m)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Measure walks a project and returns its disk use. Nested repos are skipped,
// since they are projects of their own. Symlinks are not followed.
func Measure(abs string) Sizes {
	s := Sizes{BuildDirs: map[string]int64{}, ScannedAt: time.Now()}
	for _, b := range BuildDirs {
		dir := filepath.Join(abs, b)
		if st, err := os.Lstat(dir); err == nil && st.IsDir() {
			n := du(dir, false)
			s.BuildDirs[b] = n
			s.Build += n
		}
	}
	s.Total = du(abs, true)
	return s
}

// DirSize is the allocated size of a folder, symlinks not followed.
func DirSize(dir string) int64 { return du(dir, false) }

// du sums allocated bytes under root. With skipNested, folders holding their
// own .git (other than root) are left out.
func du(root string, skipNested bool) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && skipNested && path != root {
			if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
				return filepath.SkipDir
			}
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			total += st.Blocks * 512
		} else {
			total += info.Size()
		}
		return nil
	})
	return total
}

// newestChange returns the latest modification time of any file under root,
// skipping build folders and hidden folders, looking at most at limit entries.
func newestChange(root string, limit int) time.Time {
	var newest time.Time
	seen := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		seen++
		if seen > limit {
			return filepath.SkipAll
		}
		if d.IsDir() && path != root && skippable(d.Name()) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}
