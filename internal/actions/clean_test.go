package actions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/10mTech/local-control-panel/internal/scan"
)

func newRepo(t *testing.T, root, name string) *scan.Project {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	return &scan.Project{Rel: name, Abs: dir, Name: name, Depth: 1, Kind: scan.KindRepo}
}

func skippedFor(plan CleanPlan, dir string) string {
	for _, s := range plan.Skipped {
		if s.Dir == dir {
			return s.Reason
		}
	}
	return ""
}

func itemDirs(plan CleanPlan) []string {
	var d []string
	for _, it := range plan.Items {
		d = append(d, it.Dir)
	}
	return d
}

func TestPlanCleanRepo(t *testing.T) {
	a, root := testActions(t)
	p := newRepo(t, root, "app")
	writeFile(t, filepath.Join(p.Abs, "node_modules", "pkg", "index.js"), strings.Repeat("x", 8192))
	writeFile(t, filepath.Join(p.Abs, "src-tauri", "target", "debug", "bin"), "bin")
	writeFile(t, filepath.Join(p.Abs, "dist", "bundle.js"), "tracked")
	writeFile(t, filepath.Join(p.Abs, "build", "out.js"), "generated")
	writeFile(t, filepath.Join(p.Abs, ".gitignore"), "build/\nnode_modules/\nsrc-tauri/target/\n")
	git(t, p.Abs, "add", "dist/bundle.js", ".gitignore")

	plan, err := a.PlanClean(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(itemDirs(plan), ",")
	if got != "node_modules,build,src-tauri/target" {
		t.Errorf("items = %s", got)
	}
	if why := skippedFor(plan, "dist"); !strings.Contains(why, "tracked") {
		t.Errorf("dist skip reason = %q, want a tracked-files reason", why)
	}
	var sum int64
	for _, it := range plan.Items {
		if it.Path != filepath.Join(p.Abs, it.Dir) || it.Size <= 0 {
			t.Errorf("item %+v", it)
		}
		sum += it.Size
	}
	if plan.Total != sum {
		t.Errorf("Total = %d, items add up to %d", plan.Total, sum)
	}
}

func TestPlanCleanSkips(t *testing.T) {
	a, root := testActions(t)
	p := newRepo(t, root, "app")
	elsewhere := t.TempDir()
	writeFile(t, filepath.Join(elsewhere, "keep.txt"), "precious")
	if err := os.Symlink(elsewhere, filepath.Join(p.Abs, "node_modules")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(p.Abs, "target", "vendored", ".git", "HEAD"), "ref: refs/heads/main")
	if err := os.MkdirAll(filepath.Join(p.Abs, "target", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A symlink in the middle of a nested path is refused as well.
	if err := os.Symlink(elsewhere, filepath.Join(p.Abs, "src-tauri")); err != nil {
		t.Fatal(err)
	}

	plan, err := a.PlanClean(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 0 {
		t.Errorf("items = %v, want none", itemDirs(plan))
	}
	if why := skippedFor(plan, "node_modules"); !strings.Contains(why, "symlink") {
		t.Errorf("node_modules reason = %q", why)
	}
	if why := skippedFor(plan, "target"); !strings.Contains(why, "git repo") {
		t.Errorf("target reason = %q", why)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "keep.txt")); err != nil {
		t.Errorf("the link's target was touched: %v", err)
	}
}

func TestPlanCleanNoGit(t *testing.T) {
	a, root := testActions(t)
	dir := filepath.Join(root, "plain")
	writeFile(t, filepath.Join(dir, "node_modules", "a.js"), "x")
	writeFile(t, filepath.Join(dir, "dist", "a.js"), "x")
	writeFile(t, filepath.Join(dir, "build", "a.js"), "x")
	writeFile(t, filepath.Join(dir, ".next", "a.js"), "x")
	p := &scan.Project{Rel: "plain", Abs: dir, Name: "plain", Depth: 1, Kind: scan.KindPlain}

	plan, err := a.PlanClean(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(itemDirs(plan), ","); got != "node_modules,.next" {
		t.Errorf("items = %s, want node_modules,.next", got)
	}
	for _, d := range []string{"dist", "build"} {
		if why := skippedFor(plan, d); !strings.Contains(why, "no git") {
			t.Errorf("%s reason = %q, want the no-git reason", d, why)
		}
	}
}

func TestPlanCleanNothingThere(t *testing.T) {
	a, root := testActions(t)
	p := newRepo(t, root, "app")
	plan, err := a.PlanClean(context.Background(), p)
	if err != nil || len(plan.Items) != 0 || len(plan.Skipped) != 0 || plan.Total != 0 {
		t.Errorf("plan = %+v, err = %v; want empty", plan, err)
	}
}

func TestPlanCleanRefusals(t *testing.T) {
	a, root := testActions(t)
	group := filepath.Join(root, "acme")
	writeFile(t, filepath.Join(group, "node_modules", "a.js"), "x")
	g := &scan.Project{Rel: "acme", Abs: group, Name: "acme", Depth: 1, Kind: scan.KindGroup}
	if _, err := a.PlanClean(context.Background(), g); err == nil || !strings.Contains(err.Error(), "group") {
		t.Errorf("group: err = %v, want a group refusal", err)
	}

	out := t.TempDir()
	writeFile(t, filepath.Join(out, "node_modules", "a.js"), "x")
	o := &scan.Project{Rel: "out", Abs: out, Name: "out", Depth: 1, Kind: scan.KindPlain}
	if _, err := a.PlanClean(context.Background(), o); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("outside the root: err = %v, want a refusal", err)
	}
}
