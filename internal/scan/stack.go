package scan

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type packageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func readPackageJSON(abs string) (*packageJSON, bool) {
	b, err := os.ReadFile(filepath.Join(abs, "package.json"))
	if err != nil {
		return nil, false
	}
	var p packageJSON
	if json.Unmarshal(b, &p) != nil {
		return &packageJSON{}, true
	}
	return &p, true
}

// PackageScripts returns the scripts in a folder's package.json, or nil.
func PackageScripts(abs string) map[string]string {
	if p, ok := readPackageJSON(abs); ok {
		return p.Scripts
	}
	return nil
}

func (p *packageJSON) has(dep string) bool {
	_, a := p.Dependencies[dep]
	_, b := p.DevDependencies[dep]
	return a || b
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func anyExists(abs string, names ...string) bool {
	for _, n := range names {
		if exists(filepath.Join(abs, n)) {
			return true
		}
	}
	return false
}

func fileContains(path, needle string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), needle)
}

// detectStack fills Stack, Docker and NotInstalled from the files in a folder.
func detectStack(p *Project) {
	abs := p.Abs
	var stack []string
	add := func(s string) { stack = append(stack, s) }

	pkg, hasPkg := readPackageJSON(abs)
	switch {
	case exists(filepath.Join(abs, "src-tauri")):
		add("Tauri")
	case anyExists(abs, "next.config.js", "next.config.mjs", "next.config.ts", "next.config.cjs"):
		add("Next.js")
	case hasPkg && (pkg.has("react-native") || pkg.has("expo")):
		add("React Native")
	case hasPkg && pkg.has("vite"):
		add("Vite")
	case hasPkg:
		add("Node")
	}

	for _, f := range []string{"pom.xml", "build.gradle", "build.gradle.kts"} {
		path := filepath.Join(abs, f)
		if exists(path) {
			if fileContains(path, "spring-boot") || fileContains(path, "org.springframework") {
				add("Spring")
			} else {
				add("Java")
			}
			break
		}
	}
	if exists(filepath.Join(abs, "Cargo.toml")) && !exists(filepath.Join(abs, "src-tauri")) {
		add("Rust")
	}
	if exists(filepath.Join(abs, "go.mod")) {
		add("Go")
	}
	if anyExists(abs, "pyproject.toml", "requirements.txt", "setup.py") {
		add("Python")
	}

	p.Stack = stack
	p.Docker = anyExists(abs, "Dockerfile", "docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml")
	p.NotInstalled = hasPkg && len(pkg.Dependencies)+len(pkg.DevDependencies) > 0 &&
		!exists(filepath.Join(abs, "node_modules"))
}

var (
	reURLPort    = regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0)?:(\d{2,5})`)
	reVitePort   = regexp.MustCompile(`port\s*:\s*(\d{2,5})`)
	reScriptPort = regexp.MustCompile(`(?:-p|--port)[ =](\d{2,5})`)
)

// detectPort finds the expected dev port: config first, then Tauri's devUrl,
// .env PORT, Vite config, the dev script, then the framework default.
func detectPort(p *Project, configured int) {
	if configured > 0 {
		p.Port, p.PortSource = configured, "config"
		return
	}
	abs := p.Abs

	if b, err := os.ReadFile(filepath.Join(abs, "src-tauri", "tauri.conf.json")); err == nil {
		var conf struct {
			Build struct {
				DevURL  string `json:"devUrl"`
				DevPath string `json:"devPath"`
			} `json:"build"`
		}
		if json.Unmarshal(b, &conf) == nil {
			for _, u := range []string{conf.Build.DevURL, conf.Build.DevPath} {
				if m := reURLPort.FindStringSubmatch(u); m != nil {
					p.Port, p.PortSource = atoi(m[1]), "tauri.conf.json"
					return
				}
			}
		}
	}

	for _, env := range []string{".env.local", ".env"} {
		if port := envPort(filepath.Join(abs, env)); port > 0 {
			p.Port, p.PortSource = port, env
			return
		}
	}

	for _, f := range []string{"vite.config.ts", "vite.config.js", "vite.config.mts", "vite.config.mjs"} {
		if b, err := os.ReadFile(filepath.Join(abs, f)); err == nil {
			if m := reVitePort.FindSubmatch(b); m != nil {
				p.Port, p.PortSource = atoi(string(m[1])), f
				return
			}
		}
	}

	if pkg, ok := readPackageJSON(abs); ok {
		if m := reScriptPort.FindStringSubmatch(pkg.Scripts["dev"]); m != nil {
			p.Port, p.PortSource = atoi(m[1]), "dev script"
			return
		}
	}

	for _, s := range p.Stack {
		switch s {
		case "Next.js":
			p.Port, p.PortSource = 3000, "Next.js default"
			return
		case "Vite":
			p.Port, p.PortSource = 5173, "Vite default"
			return
		}
	}
}

func envPort(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "PORT="); ok {
			return atoi(strings.Trim(v, `"' `))
		}
	}
	return 0
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
