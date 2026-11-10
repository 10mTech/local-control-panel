package scan

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// skipDirs are never descended into while looking for repos or measuring
// activity: build output, dependencies and tool caches.
var skipDirs = map[string]bool{
	"node_modules": true, "target": true, "dist": true, "build": true,
	"vendor": true, "venv": true, "__pycache__": true, "Pods": true,
	"out": true, "coverage": true, "DerivedData": true,
}

func skippable(name string) bool {
	return skipDirs[name] || strings.HasPrefix(name, ".")
}

// Discover lists every git repo up to depth levels under root, the folders
// that only hold repos (groups), and top-level folders with no git at all.
// The result is in tree order: each group or repo precedes what it contains.
//
// The archive folder is a group whenever it holds anything, its direct
// children count as top-level (so an archived folder without git still
// shows and can be restored), and it gets one extra level of depth because
// archived projects keep their path, e.g. archive/acme/web.
func Discover(root, archive string, depth int, ignore func(rel string) bool) []Project {
	archRel, err := filepath.Rel(root, archive)
	inRoot := err == nil && !strings.HasPrefix(archRel, "..")
	if !inRoot {
		archRel = ""
	}
	w := walker{root: root, archRel: archRel, max: depth, ignore: ignore}
	items, _ := w.visit("", 1)
	for i := range items {
		if inRoot && (items[i].Rel == archRel || strings.HasPrefix(items[i].Rel, archRel+"/")) {
			items[i].Archived = true
		}
	}
	return items
}

type walker struct {
	root, archRel string
	max           int
	ignore        func(string) bool
}

func (w *walker) inArchive(rel string) bool {
	return w.archRel != "" && (rel == w.archRel || strings.HasPrefix(rel, w.archRel+"/"))
}

func (w *walker) visit(rel string, d int) ([]Project, bool) {
	root := w.root
	entries, err := os.ReadDir(filepath.Join(root, rel))
	if err != nil {
		return nil, false
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	var items []Project
	hasRepo := false
	for _, e := range entries {
		// DirEntry.IsDir is false for symlinks, so links are never followed.
		if !e.IsDir() || skippable(e.Name()) {
			continue
		}
		childRel := filepath.Join(rel, e.Name())
		if w.ignore(childRel) {
			continue
		}
		abs := filepath.Join(root, childRel)
		_, gitErr := os.Lstat(filepath.Join(abs, ".git"))
		isRepo := gitErr == nil

		var sub []Project
		subHas := false
		limit := w.max
		if w.inArchive(childRel) {
			limit++
		}
		if d < limit {
			sub, subHas = w.visit(childRel, d+1)
		}
		isArchive := w.archRel != "" && childRel == w.archRel

		p := Project{Rel: childRel, Abs: abs, Name: e.Name(), Depth: d}
		switch {
		case isArchive && !isRepo && len(sub) == 0:
			continue // an empty archive folder is not worth a row
		case isRepo:
			p.Kind = KindRepo
		case subHas || (isArchive && len(sub) > 0):
			p.Kind = KindGroup
		case d == 1 || (w.archRel != "" && rel == w.archRel):
			p.Kind = KindPlain
		default:
			continue // a plain subfolder of a project; not shown
		}
		items = append(items, p)
		if isRepo || subHas {
			hasRepo = true
		}
		if p.Kind != KindPlain {
			items = append(items, sub...)
		}
	}
	return items, hasRepo
}
