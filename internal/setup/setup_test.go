package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- helpers ---

// mkDir creates a temp directory and returns its path. Cleanup is automatic.
func mkDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// writeFile creates a file inside dir with the given relative path and content.
// Parent directories are created automatically.
func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ============================================================
// 1. detectLanguage
// ============================================================

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name   string
		marker string // file to create
		want   Language
	}{
		{"Go", "go.mod", Go},
		{"Node", "package.json", Node},
		{"Python_pyproject", "pyproject.toml", Python},
		{"Python_setup", "setup.py", Python},
		{"Rust", "Cargo.toml", Rust},
		{"Unknown_empty_dir", "", Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := mkDir(t)
			if tt.marker != "" {
				writeFile(t, dir, tt.marker, "")
			}
			got := detectLanguage(dir)
			if got != tt.want {
				t.Errorf("detectLanguage(%q) = %q, want %q", tt.marker, got, tt.want)
			}
		})
	}
}

func TestDetectLanguage_Priority(t *testing.T) {
	// When both go.mod and package.json exist, Go wins (checked first).
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "")
	writeFile(t, dir, "package.json", "{}")
	got := detectLanguage(dir)
	if got != Go {
		t.Errorf("expected Go when both go.mod and package.json exist, got %q", got)
	}
}

// ============================================================
// 2. detectPORTUsage
// ============================================================

func TestDetectPORTUsage_Go(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")
	writeFile(t, dir, "main.go", `package main

import "os"

func main() {
	port := os.Getenv("PORT")
	_ = port
}
`)
	if !detectPORTUsage(dir, Go) {
		t.Error("expected true for Go file with os.Getenv(\"PORT\")")
	}
}

func TestDetectPORTUsage_Go_LookupEnv(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "main.go", `package main
import "os"
func main() {
	port, _ := os.LookupEnv("PORT")
	_ = port
}`)
	if !detectPORTUsage(dir, Go) {
		t.Error("expected true for Go file with os.LookupEnv(\"PORT\")")
	}
}

func TestDetectPORTUsage_Node(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "index.js", "const port = process.env.PORT || 3000;")
	if !detectPORTUsage(dir, Node) {
		t.Error("expected true for Node file with process.env.PORT")
	}
}

func TestDetectPORTUsage_Missing(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "main.go", `package main
func main() {}`)
	if detectPORTUsage(dir, Go) {
		t.Error("expected false when PORT env not used")
	}
}

func TestDetectPORTUsage_Python(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "app.py", `import os
port = os.getenv("PORT")`)
	if !detectPORTUsage(dir, Python) {
		t.Error("expected true for Python file with os.getenv(\"PORT\")")
	}
}

func TestDetectPORTUsage_Rust(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "main.rs", `fn main() { let port = env::var("PORT"); }`)
	if !detectPORTUsage(dir, Rust) {
		t.Error("expected true for Rust file with env::var(\"PORT\")")
	}
}

// ============================================================
// 3. detectHealthRoute
// ============================================================

func TestDetectHealthRoute_Go(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "server.go", `package main

import "net/http"

func main() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
}
`)
	if !detectHealthRoute(dir, Go) {
		t.Error("expected true for Go file with \"/health\"")
	}
}

func TestDetectHealthRoute_Node(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "app.ts", `app.get('/health', (req, res) => res.sendStatus(200));`)
	if !detectHealthRoute(dir, Node) {
		t.Error("expected true for Node file with '/health'")
	}
}

func TestDetectHealthRoute_Missing(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "main.go", `package main
func main() {}`)
	if detectHealthRoute(dir, Go) {
		t.Error("expected false when no /health route")
	}
}

// ============================================================
// 4. inferBuildAndOutput
// ============================================================

func TestInferBuildAndOutput_Go_CmdName(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")
	writeFile(t, dir, "cmd/mysvc/main.go", "package main")

	build, output := inferBuildAndOutput(dir, "mysvc", Go)
	if build != "go build -o ./dist/mysvc ./cmd/mysvc/" {
		t.Errorf("build = %q, want cmd/<name>/ pattern", build)
	}
	if output != "./dist/mysvc" {
		t.Errorf("output = %q, want ./dist/mysvc", output)
	}
}

func TestInferBuildAndOutput_Go_CmdMain(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")
	writeFile(t, dir, "cmd/main.go", "package main")

	build, output := inferBuildAndOutput(dir, "mysvc", Go)
	if build != "go build -o ./dist/mysvc ./cmd/" {
		t.Errorf("build = %q, want cmd/ pattern", build)
	}
	if output != "./dist/mysvc" {
		t.Errorf("output = %q", output)
	}
}

func TestInferBuildAndOutput_Go_RootMain(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")
	writeFile(t, dir, "main.go", "package main")

	build, output := inferBuildAndOutput(dir, "mysvc", Go)
	if build != "go build -o ./dist/mysvc ." {
		t.Errorf("build = %q, want root pattern", build)
	}
	if output != "./dist/mysvc" {
		t.Errorf("output = %q", output)
	}
}

func TestInferBuildAndOutput_Go_Fallback(t *testing.T) {
	// No main.go anywhere — falls through all checks.
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")

	build, output := inferBuildAndOutput(dir, "mysvc", Go)
	if build != "go build -o ./dist/mysvc ." {
		t.Errorf("build = %q, want fallback pattern", build)
	}
	if output != "./dist/mysvc" {
		t.Errorf("output = %q", output)
	}
}

func TestInferBuildAndOutput_Node_WithBuildScript(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "package.json", `{"scripts":{"build":"tsc"}}`)

	build, output := inferBuildAndOutput(dir, "mysvc", Node)
	if build != "npm run build" {
		t.Errorf("build = %q", build)
	}
	if output != "./dist" {
		t.Errorf("output = %q", output)
	}
}

func TestInferBuildAndOutput_Python(t *testing.T) {
	dir := mkDir(t)
	build, output := inferBuildAndOutput(dir, "mysvc", Python)
	if build != "" {
		t.Errorf("build = %q, want empty for Python", build)
	}
	if output != "./mysvc.py" {
		t.Errorf("output = %q", output)
	}
}

func TestInferBuildAndOutput_Unknown(t *testing.T) {
	dir := mkDir(t)
	build, output := inferBuildAndOutput(dir, "mysvc", Unknown)
	if build != "" {
		t.Errorf("build = %q, want empty for Unknown", build)
	}
	if output != "./mysvc" {
		t.Errorf("output = %q", output)
	}
}

// ============================================================
// 5. generateConfig
// ============================================================

func TestGenerateConfig(t *testing.T) {
	cfg := generateConfig("my-api", "go build -o ./dist/my-api .", "./dist/my-api")

	if !strings.Contains(cfg, "name: my-api") {
		t.Error("config missing name")
	}
	if !strings.Contains(cfg, "type: binary") {
		t.Error("config missing type")
	}
	if !strings.Contains(cfg, "build: go build -o ./dist/my-api .") {
		t.Error("config missing build")
	}
	if !strings.Contains(cfg, "output: ./dist/my-api") {
		t.Error("config missing output")
	}
	if !strings.Contains(cfg, "health_check: /health") {
		t.Error("config missing health_check")
	}
}

func TestGenerateConfig_NoBuildCmd(t *testing.T) {
	cfg := generateConfig("my-api", "", "./my-api.py")
	if strings.Contains(cfg, "build:") {
		t.Error("config should omit build when empty")
	}
	if !strings.Contains(cfg, "output: ./my-api.py") {
		t.Error("config missing output")
	}
}

func TestGenerateConfig_NoOutput(t *testing.T) {
	cfg := generateConfig("my-api", "make build", "")
	if strings.Contains(cfg, "output:") {
		t.Error("config should omit output when empty")
	}
}

// ============================================================
// 6. Inspect (integration)
// ============================================================

func TestInspect_FullyCompliant(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")
	writeFile(t, dir, "main.go", `package main

import (
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	http.ListenAndServe(":"+port, nil)
}
`)
	writeFile(t, dir, ".anito/config.yaml", "name: svc\nport: 3000\n")

	r, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Language != Go {
		t.Errorf("language = %q, want go", r.Language)
	}
	if !r.HasPORT {
		t.Error("HasPORT should be true")
	}
	if !r.HasHealthRoute {
		t.Error("HasHealthRoute should be true")
	}
	if !r.HasAnitoConfig {
		t.Error("HasAnitoConfig should be true")
	}
	// Fully compliant: no issues.
	if len(r.Issues) != 0 {
		t.Errorf("expected 0 issues, got %d: %+v", len(r.Issues), r.Issues)
	}
}

func TestInspect_MissingPORT(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")
	writeFile(t, dir, "main.go", `package main

import "net/http"

func main() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	http.ListenAndServe(":8080", nil)
}
`)

	r, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.HasPORT {
		t.Error("HasPORT should be false")
	}
	// Should have a "required" issue for missing PORT.
	foundPORTIssue := false
	for _, iss := range r.Issues {
		if iss.Severity == "required" && strings.Contains(iss.What, "PORT") {
			foundPORTIssue = true
		}
	}
	if !foundPORTIssue {
		t.Error("expected a required issue about missing PORT")
	}
}

func TestInspect_MissingHealth(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")
	writeFile(t, dir, "main.go", `package main

import "os"

func main() {
	_ = os.Getenv("PORT")
}
`)

	r, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.HasHealthRoute {
		t.Error("HasHealthRoute should be false")
	}
	foundHealthIssue := false
	for _, iss := range r.Issues {
		if iss.Severity == "required" && strings.Contains(iss.What, "/health") {
			foundHealthIssue = true
		}
	}
	if !foundHealthIssue {
		t.Error("expected a required issue about missing /health")
	}
}

func TestInspect_MissingConfig(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")
	writeFile(t, dir, "main.go", `package main
import (
	"net/http"
	"os"
)
func main() {
	port := os.Getenv("PORT")
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	http.ListenAndServe(":"+port, nil)
}`)

	r, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.HasAnitoConfig {
		t.Error("HasAnitoConfig should be false")
	}
	foundConfigIssue := false
	for _, iss := range r.Issues {
		if iss.Severity == "required" && strings.Contains(iss.What, ".anito/config.yaml") {
			foundConfigIssue = true
		}
	}
	if !foundConfigIssue {
		t.Error("expected a required issue about missing config")
	}
}

func TestInspect_NonExistentPath(t *testing.T) {
	_, err := Inspect("/this/path/does/not/exist")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestInspect_ServiceNameFromDir(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")

	r, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.ServiceName != filepath.Base(dir) {
		t.Errorf("ServiceName = %q, want %q", r.ServiceName, filepath.Base(dir))
	}
}

func TestInspect_SuggestedConfigNotEmpty(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")

	r, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.SuggestedConfig == "" {
		t.Error("SuggestedConfig should not be empty")
	}
}

func TestInspect_InstructionsNotEmpty(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "go.mod", "module example.com/svc")

	r, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Instructions) == 0 {
		t.Error("Instructions should not be empty")
	}
}

// ============================================================
// 7. AllocatePorts
// ============================================================

func TestAllocatePorts_PreferredRespected(t *testing.T) {
	services := []ServiceSpec{
		{Name: "api", PreferredPort: 9000},
		{Name: "web", PreferredPort: 9001},
	}
	alloc, err := AllocatePorts(services, nil)
	if err != nil {
		t.Fatal(err)
	}
	if alloc["api"] != 9000 {
		t.Errorf("api port = %d, want 9000", alloc["api"])
	}
	if alloc["web"] != 9001 {
		t.Errorf("web port = %d, want 9001", alloc["web"])
	}
}

func TestAllocatePorts_AutoAllocate(t *testing.T) {
	services := []ServiceSpec{
		{Name: "svc-a"},
		{Name: "svc-b"},
	}
	alloc, err := AllocatePorts(services, nil)
	if err != nil {
		t.Fatal(err)
	}
	if alloc["svc-a"] < portRangeMin || alloc["svc-a"] > portRangeMax {
		t.Errorf("svc-a port %d outside range %d-%d", alloc["svc-a"], portRangeMin, portRangeMax)
	}
	if alloc["svc-b"] < portRangeMin || alloc["svc-b"] > portRangeMax {
		t.Errorf("svc-b port %d outside range %d-%d", alloc["svc-b"], portRangeMin, portRangeMax)
	}
	if alloc["svc-a"] == alloc["svc-b"] {
		t.Error("two services got the same port")
	}
}

func TestAllocatePorts_SkipsReservedPorts(t *testing.T) {
	services := []ServiceSpec{
		{Name: "api", PreferredPort: 7700}, // Anito management API
		{Name: "mcp", PreferredPort: 7701}, // Anito MCP server
	}
	alloc, err := AllocatePorts(services, nil)
	if err != nil {
		t.Fatal(err)
	}
	if alloc["api"] == 7700 {
		t.Error("should not allocate Anito reserved port 7700")
	}
	if alloc["mcp"] == 7701 {
		t.Error("should not allocate Anito reserved port 7701")
	}
}

func TestAllocatePorts_SkipsUsedPorts(t *testing.T) {
	used := map[int]bool{8100: true, 8101: true}
	services := []ServiceSpec{
		{Name: "svc-a"},
		{Name: "svc-b"},
	}
	alloc, err := AllocatePorts(services, used)
	if err != nil {
		t.Fatal(err)
	}
	if alloc["svc-a"] == 8100 || alloc["svc-a"] == 8101 {
		t.Errorf("svc-a got already-used port %d", alloc["svc-a"])
	}
	if alloc["svc-b"] == 8100 || alloc["svc-b"] == 8101 {
		t.Errorf("svc-b got already-used port %d", alloc["svc-b"])
	}
}

func TestAllocatePorts_PreferredConflictFallsToAuto(t *testing.T) {
	used := map[int]bool{9000: true}
	services := []ServiceSpec{
		{Name: "api", PreferredPort: 9000}, // conflict
	}
	alloc, err := AllocatePorts(services, used)
	if err != nil {
		t.Fatal(err)
	}
	if alloc["api"] == 9000 {
		t.Error("should not allocate conflicting preferred port")
	}
	if alloc["api"] < portRangeMin || alloc["api"] > portRangeMax {
		t.Errorf("auto-allocated port %d outside range", alloc["api"])
	}
}

func TestAllocatePorts_RangeExhausted(t *testing.T) {
	// Fill the entire auto-allocation range.
	used := make(map[int]bool)
	for p := portRangeMin; p <= portRangeMax; p++ {
		used[p] = true
	}
	services := []ServiceSpec{
		{Name: "overflow"},
	}
	_, err := AllocatePorts(services, used)
	if err == nil {
		t.Error("expected error when port range is exhausted")
	}
	if !strings.Contains(err.Error(), "no available ports") {
		t.Errorf("error = %q, want 'no available ports' message", err.Error())
	}
}

func TestAllocatePorts_MixedPreferredAndAuto(t *testing.T) {
	services := []ServiceSpec{
		{Name: "fixed", PreferredPort: 5000},
		{Name: "auto-a"},
		{Name: "auto-b"},
	}
	alloc, err := AllocatePorts(services, nil)
	if err != nil {
		t.Fatal(err)
	}
	if alloc["fixed"] != 5000 {
		t.Errorf("fixed port = %d, want 5000", alloc["fixed"])
	}
	if alloc["auto-a"] < portRangeMin || alloc["auto-a"] > portRangeMax {
		t.Errorf("auto-a = %d, outside range", alloc["auto-a"])
	}
	if alloc["auto-b"] < portRangeMin || alloc["auto-b"] > portRangeMax {
		t.Errorf("auto-b = %d, outside range", alloc["auto-b"])
	}
}

// ============================================================
// 8. envPrefix
// ============================================================

func TestEnvPrefix(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"tahua-www", "TAHUA_WWW"},
		{"my-api", "MY_API"},
		{"simple", "SIMPLE"},
		{"a-b-c", "A_B_C"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := envPrefix(tt.name)
			if got != tt.want {
				t.Errorf("envPrefix(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// ============================================================
// 9. detectFramework
// ============================================================

func TestDetectFramework_Go(t *testing.T) {
	dir := mkDir(t)
	got := detectFramework(dir, Go)
	if got != "go" {
		t.Errorf("detectFramework(Go) = %q, want \"go\"", got)
	}
}

func TestDetectFramework_Vite(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "vite.config.ts", "export default {}")
	got := detectFramework(dir, Node)
	if got != "vite" {
		t.Errorf("detectFramework(Node+vite.config.ts) = %q, want \"vite\"", got)
	}
}

func TestDetectFramework_ViteJS(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "vite.config.js", "export default {}")
	got := detectFramework(dir, Node)
	if got != "vite" {
		t.Errorf("detectFramework(Node+vite.config.js) = %q, want \"vite\"", got)
	}
}

func TestDetectFramework_Next(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "next.config.js", "module.exports = {}")
	got := detectFramework(dir, Node)
	if got != "next" {
		t.Errorf("detectFramework(Node+next.config.js) = %q, want \"next\"", got)
	}
}

func TestDetectFramework_PlainNode(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "package.json", "{}")
	got := detectFramework(dir, Node)
	if got != "node" {
		t.Errorf("detectFramework(Node no framework) = %q, want \"node\"", got)
	}
}

func TestDetectFramework_Python(t *testing.T) {
	dir := mkDir(t)
	got := detectFramework(dir, Python)
	if got != "python" {
		t.Errorf("detectFramework(Python) = %q, want \"python\"", got)
	}
}

func TestDetectFramework_Rust(t *testing.T) {
	dir := mkDir(t)
	got := detectFramework(dir, Rust)
	if got != "rust" {
		t.Errorf("detectFramework(Rust) = %q, want \"rust\"", got)
	}
}

// ============================================================
// 10. readPackageJSONName
// ============================================================

func TestReadPackageJSONName_Valid(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "package.json", `{
  "name": "@myorg/web",
  "version": "1.0.0"
}`)
	got := readPackageJSONName(dir)
	if got != "@myorg/web" {
		t.Errorf("readPackageJSONName = %q, want \"@myorg/web\"", got)
	}
}

func TestReadPackageJSONName_Simple(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "package.json", `{"name": "my-app"}`)
	got := readPackageJSONName(dir)
	if got != "my-app" {
		t.Errorf("readPackageJSONName = %q, want \"my-app\"", got)
	}
}

func TestReadPackageJSONName_Missing(t *testing.T) {
	dir := mkDir(t)
	got := readPackageJSONName(dir)
	if got != "" {
		t.Errorf("readPackageJSONName = %q, want empty for missing file", got)
	}
}

func TestReadPackageJSONName_NoNameField(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "package.json", `{"version": "1.0.0"}`)
	got := readPackageJSONName(dir)
	if got != "" {
		t.Errorf("readPackageJSONName = %q, want empty when no name field", got)
	}
}

// ============================================================
// 11. searchFiles
// ============================================================

func TestSearchFiles_FindsPattern(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "src/handler.go", `http.HandleFunc("/health", h)`)

	if !searchFiles(dir, []string{".go"}, []string{`"/health"`}) {
		t.Error("expected to find pattern in .go file")
	}
}

func TestSearchFiles_WrongExtension(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "handler.py", `"/health"`)

	if searchFiles(dir, []string{".go"}, []string{`"/health"`}) {
		t.Error("should not find pattern in .py when searching .go only")
	}
}

func TestSearchFiles_SkipsVendor(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "vendor/lib/match.go", `os.Getenv("PORT")`)

	if searchFiles(dir, []string{".go"}, []string{`os.Getenv("PORT")`}) {
		t.Error("should skip vendor/ directory")
	}
}

func TestSearchFiles_SkipsNodeModules(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "node_modules/dep/index.js", "process.env.PORT")

	if searchFiles(dir, []string{".js"}, []string{"process.env.PORT"}) {
		t.Error("should skip node_modules/ directory")
	}
}

func TestSearchFiles_SkipsGitDir(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, ".git/objects/match.go", `os.Getenv("PORT")`)

	if searchFiles(dir, []string{".go"}, []string{`os.Getenv("PORT")`}) {
		t.Error("should skip .git/ directory")
	}
}

func TestSearchFiles_SkipsDist(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "dist/main.go", `os.Getenv("PORT")`)

	if searchFiles(dir, []string{".go"}, []string{`os.Getenv("PORT")`}) {
		t.Error("should skip dist/ directory")
	}
}

func TestSearchFiles_SearchesSubdirectories(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "internal/server/handler.go", `"/health"`)

	if !searchFiles(dir, []string{".go"}, []string{`"/health"`}) {
		t.Error("should find pattern in subdirectories")
	}
}

func TestSearchFiles_NoFiles(t *testing.T) {
	dir := mkDir(t)
	if searchFiles(dir, []string{".go"}, []string{"anything"}) {
		t.Error("should return false in empty directory")
	}
}

func TestSearchFiles_MultiplePatterns(t *testing.T) {
	dir := mkDir(t)
	writeFile(t, dir, "main.go", `os.LookupEnv("PORT")`)

	// First pattern doesn't match, second does.
	found := searchFiles(dir, []string{".go"}, []string{`os.Getenv("PORT")`, `os.LookupEnv("PORT")`})
	if !found {
		t.Error("should match on the second pattern")
	}
}

// ============================================================
// portFix / healthFix
// ============================================================

func TestPortFix_PerLanguage(t *testing.T) {
	if !strings.Contains(portFix(Go), "os.Getenv") {
		t.Error("Go portFix should mention os.Getenv")
	}
	if !strings.Contains(portFix(Node), "process.env.PORT") {
		t.Error("Node portFix should mention process.env.PORT")
	}
	if !strings.Contains(portFix(Python), "os.environ") {
		t.Error("Python portFix should mention os.environ")
	}
	if portFix(Unknown) == "" {
		t.Error("Unknown portFix should not be empty")
	}
}

func TestHealthFix_PerLanguage(t *testing.T) {
	if !strings.Contains(healthFix(Go), "/health") {
		t.Error("Go healthFix should mention /health")
	}
	if !strings.Contains(healthFix(Node), "/health") {
		t.Error("Node healthFix should mention /health")
	}
	if !strings.Contains(healthFix(Python), "/health") {
		t.Error("Python healthFix should mention /health")
	}
	if healthFix(Unknown) == "" {
		t.Error("Unknown healthFix should not be empty")
	}
}

// ============================================================
// generateInstructions
// ============================================================

func TestGenerateInstructions_CompliantRepo(t *testing.T) {
	r := &Result{
		RepoPath:       "/tmp/test",
		ServiceName:    "my-svc",
		HasPORT:        true,
		HasHealthRoute: true,
		HasAnitoConfig: true,
	}
	steps := generateInstructions(r, "go build .")
	if len(steps) == 0 {
		t.Fatal("expected at least one instruction")
	}
	// Compliant repo starts with the checkmark message.
	if !strings.Contains(steps[0], "already meets") {
		t.Errorf("first step = %q, want compliant message", steps[0])
	}
}

func TestGenerateInstructions_NoncompliantRepo(t *testing.T) {
	r := &Result{
		RepoPath:       "/tmp/test",
		ServiceName:    "my-svc",
		HasPORT:        false,
		HasHealthRoute: false,
		HasAnitoConfig: false,
	}
	steps := generateInstructions(r, "go build .")
	// Should have at least 3 fix steps plus common trailing steps.
	if len(steps) < 3 {
		t.Errorf("expected at least 3 steps, got %d", len(steps))
	}
	joinedSteps := strings.Join(steps, "\n")
	if !strings.Contains(joinedSteps, "PORT") {
		t.Error("instructions should mention PORT")
	}
	if !strings.Contains(joinedSteps, "health") {
		t.Error("instructions should mention health")
	}
	if !strings.Contains(joinedSteps, ".anito/config.yaml") {
		t.Error("instructions should mention config.yaml")
	}
}

// ============================================================
// generatePortsEnv
// ============================================================

func TestGeneratePortsEnv(t *testing.T) {
	services := []ServiceSpec{
		{Name: "my-api"},
		{Name: "my-web"},
	}
	alloc := PortAllocation{"my-api": 8100, "my-web": 8101}

	content := generatePortsEnv(services, alloc)

	if !strings.Contains(content, "MY_API_PORT=8100") {
		t.Error("missing MY_API_PORT=8100")
	}
	if !strings.Contains(content, "MY_API_URL=http://localhost:8100") {
		t.Error("missing MY_API_URL")
	}
	if !strings.Contains(content, "MY_WEB_PORT=8101") {
		t.Error("missing MY_WEB_PORT=8101")
	}
	if !strings.Contains(content, "MY_WEB_URL=http://localhost:8101") {
		t.Error("missing MY_WEB_URL")
	}
	if !strings.Contains(content, "[anito:managed]") {
		t.Error("missing [anito:managed] header")
	}
}

// ============================================================
// CoordinateApp (integration)
// ============================================================

func TestCoordinateApp_BasicComposite(t *testing.T) {
	root := mkDir(t)
	apiDir := filepath.Join(root, "api")
	webDir := filepath.Join(root, "web")
	writeFile(t, root, "api/go.mod", "module example.com/api")
	writeFile(t, root, "web/package.json", `{"name": "web"}`)
	writeFile(t, root, "web/vite.config.ts", "export default {}")

	services := []ServiceSpec{
		{Name: "my-api", Path: apiDir, PreferredPort: 8100},
		{Name: "my-web", Path: webDir, PreferredPort: 8101},
	}
	relationships := []Relationship{
		{From: "my-web", To: "my-api", ProxyPath: "/api"},
	}

	result, err := CoordinateApp(root, services, relationships, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Allocations
	if result.Allocations["my-api"] != 8100 {
		t.Errorf("my-api port = %d, want 8100", result.Allocations["my-api"])
	}
	if result.Allocations["my-web"] != 8101 {
		t.Errorf("my-web port = %d, want 8101", result.Allocations["my-web"])
	}

	// Should have generated files (at least ports.env + 2 configs).
	if len(result.GeneratedFiles) < 3 {
		t.Errorf("expected at least 3 generated files, got %d", len(result.GeneratedFiles))
	}
	var wrapper string
	for _, file := range result.GeneratedFiles {
		if file.RelPath == ".anito/my-web-dev.sh" {
			wrapper = file.Content
			break
		}
	}
	if wrapper == "" {
		t.Fatal("missing generated Vite dev wrapper")
	}
	if !strings.Contains(wrapper, "pnpm --filter web exec vite dev --force") {
		t.Errorf("wrapper = %q, want Vite dev command with --force", wrapper)
	}

	// Should have a source patch for the Vite service.
	if len(result.SourcePatches) == 0 {
		t.Error("expected at least one source patch for Vite service")
	}

	// Instructions should not be empty.
	if len(result.Instructions) == 0 {
		t.Error("expected non-empty instructions")
	}

	// PortsEnvPath is always the same.
	if result.PortsEnvPath != ".anito/ports.env" {
		t.Errorf("PortsEnvPath = %q", result.PortsEnvPath)
	}
}

func TestCoordinateApp_AutoDetectsLanguageAndFramework(t *testing.T) {
	root := mkDir(t)
	svcDir := filepath.Join(root, "svc")
	writeFile(t, root, "svc/go.mod", "module example.com/svc")

	services := []ServiceSpec{
		{Name: "my-svc", Path: svcDir},
	}

	result, err := CoordinateApp(root, services, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should auto-allocate a port.
	port := result.Allocations["my-svc"]
	if port < portRangeMin || port > portRangeMax {
		t.Errorf("auto-allocated port %d outside range", port)
	}
}
