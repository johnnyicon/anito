// Package setup provides repo introspection for the anito_setup MCP tool.
// It inspects a directory, detects the language and framework, checks whether
// the Anito service contract is met, and returns a suggested config plus
// step-by-step instructions.
package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Language identifies the primary language of a repo.
type Language string

const (
	Go      Language = "go"
	Node    Language = "node"
	Python  Language = "python"
	Rust    Language = "rust"
	Unknown Language = "unknown"
)

// Issue describes a gap between the repo's current state and the Anito service contract.
type Issue struct {
	Severity string // "required" | "recommended"
	What     string
	Fix      string
}

// Result is the full inspection result returned by Inspect.
type Result struct {
	RepoPath        string
	ServiceName     string
	Language        Language
	HasAnitoConfig  bool
	HasPORT         bool   // service reads PORT from environment
	HasHealthRoute  bool   // service exposes GET /health
	SuggestedConfig string // ready-to-use .anito/config.yaml content
	Issues          []Issue
	Instructions    []string // ordered action items
}

// Inspect walks path and returns everything needed to make it Anito-compatible.
func Inspect(path string) (*Result, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("path %q does not exist", abs)
	}

	r := &Result{
		RepoPath:    abs,
		ServiceName: filepath.Base(abs),
	}

	r.Language = detectLanguage(abs)

	configPath := filepath.Join(abs, ".anito", "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		r.HasAnitoConfig = true
	}

	r.HasPORT = detectPORTUsage(abs, r.Language)
	r.HasHealthRoute = detectHealthRoute(abs, r.Language)

	buildCmd, outputPath := inferBuildAndOutput(abs, r.ServiceName, r.Language)

	if !r.HasPORT {
		r.Issues = append(r.Issues, Issue{
			Severity: "required",
			What:     "Service does not read PORT from the environment",
			Fix:      portFix(r.Language),
		})
	}
	if !r.HasHealthRoute {
		r.Issues = append(r.Issues, Issue{
			Severity: "required",
			What:     "No GET /health endpoint detected",
			Fix:      healthFix(r.Language),
		})
	}
	if !r.HasAnitoConfig {
		r.Issues = append(r.Issues, Issue{
			Severity: "required",
			What:     "No .anito/config.yaml found",
			Fix:      "Create .anito/config.yaml using the suggested config in this response",
		})
	}

	r.SuggestedConfig = generateConfig(r.ServiceName, buildCmd, outputPath)
	r.Instructions = generateInstructions(r, buildCmd)
	return r, nil
}

// --- language detection ---

func detectLanguage(root string) Language {
	checks := []struct {
		file string
		lang Language
	}{
		{"go.mod", Go},
		{"package.json", Node},
		{"pyproject.toml", Python},
		{"setup.py", Python},
		{"Cargo.toml", Rust},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(root, c.file)); err == nil {
			return c.lang
		}
	}
	return Unknown
}

// --- PORT detection ---

func detectPORTUsage(root string, lang Language) bool {
	var patterns []string
	var exts []string
	switch lang {
	case Go:
		patterns = []string{`os.Getenv("PORT")`, `os.LookupEnv("PORT")`}
		exts = []string{".go"}
	case Node:
		patterns = []string{"process.env.PORT"}
		exts = []string{".js", ".ts", ".mjs", ".cjs"}
	case Python:
		patterns = []string{`environ.get("PORT")`, `environ["PORT"]`, `os.getenv("PORT")`}
		exts = []string{".py"}
	case Rust:
		patterns = []string{`env::var("PORT")`}
		exts = []string{".rs"}
	default:
		patterns = []string{"PORT"}
		exts = []string{".go", ".js", ".ts", ".py", ".rs"}
	}
	return searchFiles(root, exts, patterns)
}

// --- /health detection ---

func detectHealthRoute(root string, lang Language) bool {
	var exts []string
	switch lang {
	case Go:
		exts = []string{".go"}
	case Node:
		exts = []string{".js", ".ts", ".mjs"}
	case Python:
		exts = []string{".py"}
	case Rust:
		exts = []string{".rs"}
	default:
		exts = []string{".go", ".js", ".ts", ".py", ".rs"}
	}
	return searchFiles(root, exts, []string{`"/health"`, `'/health'`})
}

// --- build + output inference ---

func inferBuildAndOutput(root, name string, lang Language) (buildCmd, outputPath string) {
	switch lang {
	case Go:
		// Prefer cmd/<name>/main.go, then cmd/main.go, then root main.go
		if _, err := os.Stat(filepath.Join(root, "cmd", name, "main.go")); err == nil {
			return fmt.Sprintf("go build -o ./dist/%s ./cmd/%s/", name, name),
				fmt.Sprintf("./dist/%s", name)
		}
		if _, err := os.Stat(filepath.Join(root, "cmd", "main.go")); err == nil {
			return fmt.Sprintf("go build -o ./dist/%s ./cmd/", name),
				fmt.Sprintf("./dist/%s", name)
		}
		if _, err := os.Stat(filepath.Join(root, "main.go")); err == nil {
			return fmt.Sprintf("go build -o ./dist/%s .", name),
				fmt.Sprintf("./dist/%s", name)
		}
		// Fallback — there's a go.mod but no obvious main package
		return fmt.Sprintf("go build -o ./dist/%s .", name),
			fmt.Sprintf("./dist/%s", name)

	case Node:
		// Check for a build script in package.json
		data, err := os.ReadFile(filepath.Join(root, "package.json"))
		if err == nil && strings.Contains(string(data), `"build"`) {
			return "npm run build", "./dist"
		}
		return "npm run build", "./dist"

	case Python:
		return "", fmt.Sprintf("./%s.py", name)

	default:
		return "", fmt.Sprintf("./%s", name)
	}
}

// --- config generation ---

func generateConfig(name, buildCmd, outputPath string) string {
	var sb strings.Builder
	sb.WriteString("# .anito/config.yaml\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", name))
	sb.WriteString("version: v0.1.0\n")
	sb.WriteString("type: binary\n")
	if buildCmd != "" {
		sb.WriteString(fmt.Sprintf("build: %s\n", buildCmd))
	}
	if outputPath != "" {
		sb.WriteString(fmt.Sprintf("output: %s\n", outputPath))
	}
	sb.WriteString("health_check: /health\n")
	sb.WriteString("# port: 3000  # omit to auto-allocate from 8100-8200\n")
	return sb.String()
}

// --- instructions ---

func generateInstructions(r *Result, buildCmd string) []string {
	var steps []string
	n := 1

	if !r.HasPORT {
		steps = append(steps, fmt.Sprintf("%d. Add PORT env var reading: %s", n, portFix(r.Language)))
		n++
	}
	if !r.HasHealthRoute {
		steps = append(steps, fmt.Sprintf("%d. Add a health endpoint: %s", n, healthFix(r.Language)))
		n++
	}
	if !r.HasAnitoConfig {
		steps = append(steps, fmt.Sprintf("%d. Create .anito/ directory and save the suggested config as .anito/config.yaml", n))
		n++
	}
	if len(steps) == 0 {
		steps = append(steps, "✓ This repo already meets the Anito service contract.")
	}
	steps = append(steps,
		fmt.Sprintf("%d. Test locally: build the binary and confirm GET /health returns 200", n),
		fmt.Sprintf("%d. Deploy: run `anito deploy` from %s", n+1, r.RepoPath),
	)
	return steps
}

// --- language-specific fix hints ---

func portFix(lang Language) string {
	switch lang {
	case Go:
		return `Read PORT at startup: port := os.Getenv("PORT"); if port == "" { port = "8080" } — then pass it to your HTTP server`
	case Node:
		return `Use process.env.PORT: const port = process.env.PORT || 3000`
	case Python:
		return `Use os.environ: port = int(os.environ.get("PORT", 8080))`
	default:
		return `Read the PORT environment variable and bind your HTTP server to that port`
	}
}

func healthFix(lang Language) string {
	switch lang {
	case Go:
		return `Add: mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })`
	case Node:
		return `Add: app.get('/health', (req, res) => res.sendStatus(200))`
	case Python:
		return `Add a route: @app.route('/health') def health(): return '', 200`
	default:
		return `Expose GET /health returning HTTP 200 OK`
	}
}

// --- file search utility ---

var skipDirs = map[string]bool{
	"vendor": true, "node_modules": true, ".git": true,
	"dist": true, "build": true, ".anito": true,
}

func searchFiles(root string, exts, patterns []string) bool {
	found := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || found {
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		for _, ext := range exts {
			if strings.HasSuffix(path, ext) {
				data, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				content := string(data)
				for _, p := range patterns {
					if strings.Contains(content, p) {
						found = true
						return filepath.SkipAll
					}
				}
			}
		}
		return nil
	})
	return found
}
