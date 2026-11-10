package actions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"local-control-panel/internal/scan"
)

// DevCommand is the command "run" will use for a project and where it came from.
type DevCommand struct {
	Argv   []string
	Source string // config | approved | proposed | none
}

// Confirmed reports whether the command may run without asking first.
func (d DevCommand) Confirmed() bool {
	return d.Source == "config" || d.Source == "approved"
}

// Approvals remembers dev commands you confirmed once, so "run" does not ask
// again. Kept in <state>/dev-commands.json; config.toml always wins.
type Approvals struct {
	path string
	mu   sync.Mutex
	m    map[string][]string
}

func OpenApprovals(stateDir string) *Approvals {
	a := &Approvals{path: filepath.Join(stateDir, "dev-commands.json"), m: map[string][]string{}}
	if b, err := os.ReadFile(a.path); err == nil {
		_ = json.Unmarshal(b, &a.m)
	}
	return a
}

func (a *Approvals) Get(rel string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.m[rel]
}

func (a *Approvals) Set(rel string, argv []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if slices.Equal(a.m[rel], argv) {
		return nil
	}
	a.m[rel] = argv
	b, err := json.MarshalIndent(a.m, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

// Propose guesses a dev command from the project's files. It returns nil when
// nothing fits; the panel then asks you to type one.
func Propose(p *scan.Project) []string {
	abs := p.Abs
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(abs, name))
		return err == nil
	}

	if scripts := scan.PackageScripts(abs); scripts != nil {
		pm := "npm"
		switch {
		case has("pnpm-lock.yaml"):
			pm = "pnpm"
		case has("yarn.lock"):
			pm = "yarn"
		case has("bun.lockb"), has("bun.lock"):
			pm = "bun"
		}
		switch {
		case has("src-tauri") && scripts["tauri"] != "":
			return []string{pm, "run", "tauri", "dev"}
		case scripts["dev"] != "":
			return []string{pm, "run", "dev"}
		case scripts["start"] != "":
			return []string{pm, "run", "start"}
		}
	}

	spring := slices.Contains(p.Stack, "Spring")
	switch {
	case spring && has("mvnw"):
		return []string{"./mvnw", "spring-boot:run"}
	case spring && has("pom.xml"):
		return []string{"mvn", "spring-boot:run"}
	case spring && has("gradlew"):
		return []string{"./gradlew", "bootRun"}
	case has("Cargo.toml"):
		return []string{"cargo", "run"}
	case has("go.mod"):
		return []string{"go", "run", "."}
	case p.Docker && (has("docker-compose.yml") || has("docker-compose.yaml") || has("compose.yml") || has("compose.yaml")):
		return []string{"docker", "compose", "up"}
	}
	return nil
}
