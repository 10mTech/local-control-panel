package actions

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/10mTech/local-control-panel/internal/scan"
)

func TestTrimLog(t *testing.T) {
	const max = 1000
	lines := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteString("line of log output ........................ " + string(rune('A'+i%26)) + "\n")
		}
		return b.String()
	}
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("missing file", func(t *testing.T) {
		if n, err := trimLog(filepath.Join(dir, "nope.log"), max); n != 0 || err != nil {
			t.Errorf("= %d, %v", n, err)
		}
	})
	t.Run("under the limit", func(t *testing.T) {
		body := lines(5)
		p := write("small.log", body)
		if n, err := trimLog(p, max); n != 0 || err != nil {
			t.Errorf("= %d, %v", n, err)
		}
		if b, _ := os.ReadFile(p); string(b) != body {
			t.Error("file changed")
		}
	})
	t.Run("exactly the limit", func(t *testing.T) {
		body := strings.Repeat("x", max)
		p := write("exact.log", body)
		if n, _ := trimLog(p, max); n != 0 {
			t.Errorf("= %d", n)
		}
		if b, _ := os.ReadFile(p); string(b) != body {
			t.Error("file changed")
		}
	})
	t.Run("over the limit", func(t *testing.T) {
		body := lines(200)
		p := write("big.log", body)
		n, err := trimLog(p, max)
		if err != nil || n <= 0 {
			t.Fatalf("= %d, %v", n, err)
		}
		b, _ := os.ReadFile(p)
		if int64(len(body)-len(b)) != n {
			t.Errorf("reported %d freed, file shrank by %d", n, len(body)-len(b))
		}
		if !strings.HasPrefix(string(b), "# local-control-panel: trimmed") {
			t.Errorf("no note at the top: %q", b[:40])
		}
		rest := string(b[strings.IndexByte(string(b), '\n')+1:])
		if !strings.HasSuffix(body, rest) || !strings.HasPrefix(rest, "line of log output") {
			t.Error("the kept part is not a whole-line tail of the original")
		}
		if _, err := os.Stat(p + ".tmp"); err == nil {
			t.Error("temp file left behind")
		}
	})
	t.Run("barely over", func(t *testing.T) {
		// Ten bytes over; the tail starts on a line break, so only 11 bytes
		// would go, fewer than the note that replaces them.
		body := "aaaaaaaaa\n" + "\n" + strings.Repeat("y", max-1)
		p := write("edge.log", body)
		if n, err := trimLog(p, max); n != 0 || err != nil {
			t.Errorf("= %d, %v; want 0 when the note would not make it smaller", n, err)
		}
		if b, _ := os.ReadFile(p); string(b) != body {
			t.Error("file changed")
		}
	})
}

func TestExistingAncestor(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}
	tests := []struct{ in, want string }{
		{filepath.Join(dir, "a", "b"), filepath.Join(dir, "a", "b")},
		{filepath.Join(dir, "a", "b", "c", "d"), filepath.Join(dir, "a", "b")},
		{filepath.Join(dir, "x", "y"), dir},
		{filepath.Join(dir, "dangling"), filepath.Join(dir, "dangling")}, // Lstat sees the link itself
		{"/", "/"},
	}
	for _, tc := range tests {
		if got := existingAncestor(tc.in); got != tc.want {
			t.Errorf("existingAncestor(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPropose(t *testing.T) {
	pkg := func(scripts string) map[string]string {
		return map[string]string{"package.json": `{"scripts":{` + scripts + `}}`}
	}
	merge := func(ms ...map[string]string) map[string]string {
		out := map[string]string{}
		for _, m := range ms {
			for k, v := range m {
				out[k] = v
			}
		}
		return out
	}
	tests := []struct {
		name  string
		files map[string]string
		stack []string
		dock  bool
		want  []string
	}{
		{"nothing", nil, nil, false, nil},
		{"npm dev", pkg(`"dev":"vite"`), nil, false, []string{"npm", "run", "dev"}},
		{"pnpm dev", merge(pkg(`"dev":"vite"`), map[string]string{"pnpm-lock.yaml": ""}), nil, false, []string{"pnpm", "run", "dev"}},
		{"yarn start", merge(pkg(`"start":"node ."`), map[string]string{"yarn.lock": ""}), nil, false, []string{"yarn", "run", "start"}},
		{"bun dev", merge(pkg(`"dev":"x"`), map[string]string{"bun.lock": ""}), nil, false, []string{"bun", "run", "dev"}},
		{"dev beats start", pkg(`"start":"a","dev":"b"`), nil, false, []string{"npm", "run", "dev"}},
		{"tauri", merge(pkg(`"tauri":"tauri","dev":"vite"`), map[string]string{"src-tauri/Cargo.toml": ""}), nil, false, []string{"npm", "run", "tauri", "dev"}},
		{"tauri script without src-tauri", pkg(`"tauri":"tauri","dev":"vite"`), nil, false, []string{"npm", "run", "dev"}},
		{"package.json without dev or start", pkg(`"build":"x"`), nil, false, nil},
		{"mvnw", map[string]string{"mvnw": "", "pom.xml": ""}, []string{"Spring"}, false, []string{"./mvnw", "spring-boot:run"}},
		{"mvn", map[string]string{"pom.xml": ""}, []string{"Spring"}, false, []string{"mvn", "spring-boot:run"}},
		{"gradlew", map[string]string{"gradlew": ""}, []string{"Spring"}, false, []string{"./gradlew", "bootRun"}},
		{"cargo", map[string]string{"Cargo.toml": ""}, nil, false, []string{"cargo", "run"}},
		{"go", map[string]string{"go.mod": ""}, nil, false, []string{"go", "run", "."}},
		{"compose", map[string]string{"compose.yaml": ""}, nil, true, []string{"docker", "compose", "up"}},
		{"compose file but not flagged as docker", map[string]string{"compose.yaml": ""}, nil, false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range tc.files {
				writeFile(t, filepath.Join(dir, name), body)
			}
			got := Propose(&scan.Project{Abs: dir, Stack: tc.stack, Docker: tc.dock})
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Propose = %q, want %q", got, tc.want)
			}
		})
	}
}
