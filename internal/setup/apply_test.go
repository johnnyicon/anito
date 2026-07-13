package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakePorts struct {
	used     map[int]bool
	existing map[string]int
	failName string
	removed  []string
}

func (f *fakePorts) UsedPorts() map[int]bool {
	out := make(map[int]bool, len(f.used))
	for port, used := range f.used {
		out[port] = used
	}
	return out
}

func (f *fakePorts) StablePort(name string) (int, bool) {
	port, ok := f.existing[name]
	return port, ok
}

func (f *fakePorts) Reserve(name string, preferredPort int) (int, error) {
	if name == f.failName {
		return 0, os.ErrPermission
	}
	if port, ok := f.existing[name]; ok {
		return port, nil
	}
	port := preferredPort
	if port == 0 {
		port = portRangeMin
		for f.used[port] {
			port++
		}
	}
	f.existing[name] = port
	f.used[port] = true
	return port, nil
}

func (f *fakePorts) Remove(name string) error {
	f.removed = append(f.removed, name)
	if port, ok := f.existing[name]; ok {
		delete(f.used, port)
	}
	delete(f.existing, name)
	return nil
}

func TestDryRunCompositeDeterministicAndPreservesExistingPort(t *testing.T) {
	repo := t.TempDir()
	api := filepath.Join(repo, "api")
	web := filepath.Join(repo, "web")
	writeFile(t, api, "go.mod", "module example.com/api")
	writeFile(t, api, "main.go", "package main")
	writeFile(t, web, "package.json", `{"name":"web","scripts":{"build":"vite build"}}`)
	writeFile(t, web, "vite.config.ts", "export default defineConfig({})\n")

	ports := &fakePorts{
		used:     map[int]bool{8123: true},
		existing: map[string]int{"api": 8123},
	}
	req := PlanRequest{
		Path: repo,
		Services: []ServiceSpec{
			{Name: "api", Path: api},
			{Name: "web", Path: web},
		},
		Relationships: []Relationship{{From: "web", To: "api", ProxyPath: "/api"}},
	}

	first, err := DryRun(req, ports)
	if err != nil {
		t.Fatalf("DryRun first: %v", err)
	}
	second, err := DryRun(req, ports)
	if err != nil {
		t.Fatalf("DryRun second: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("plans differ\nfirst=%#v\nsecond=%#v", first, second)
	}
	if got := first.Allocations["api"]; got != 8123 {
		t.Fatalf("api allocation = %d, want existing 8123", got)
	}
}

func TestApplyRejectsConflictingGeneratedPathsWithoutWriting(t *testing.T) {
	root := t.TempDir()
	plan := &Plan{
		Mode:     ModeComposite,
		RepoPath: root,
		GeneratedFiles: []GeneratedFile{
			{RelPath: ".anito/config.yaml", Content: "name: one\n"},
			{RelPath: ".anito/../.anito/config.yaml", Content: "name: two\n"},
		},
	}

	_, err := Apply(plan, nil)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("error = %q, want duplicate path conflict", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(root, ".anito", "config.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("config was written on conflict, stat err = %v", statErr)
	}
}

func TestApplyRejectsPathEscapeWithoutWriting(t *testing.T) {
	root := t.TempDir()
	plan := &Plan{
		Mode:           ModeSingle,
		RepoPath:       root,
		ServiceName:    "svc",
		GeneratedFiles: []GeneratedFile{{RelPath: "../outside", Content: "bad"}},
	}

	_, err := Apply(plan, nil)
	if err == nil {
		t.Fatal("expected path escape error")
	}
	if !strings.Contains(err.Error(), "escapes repo root") {
		t.Fatalf("error = %q, want escapes repo root", err.Error())
	}
}

func TestApplySingleIsIdempotentAndDoesNotMutatePlan(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/svc")
	writeFile(t, root, "main.go", `package main
import (
	"net/http"
	"os"
)
func main() {
	_ = os.Getenv("PORT")
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
}`)
	plan, err := DryRun(PlanRequest{Path: root}, nil)
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	original := plan.Clone()
	ports := &fakePorts{used: map[int]bool{}, existing: map[string]int{}}

	first, err := Apply(plan, ports)
	if err != nil {
		t.Fatalf("Apply first: %v", err)
	}
	second, err := Apply(plan, ports)
	if err != nil {
		t.Fatalf("Apply second: %v", err)
	}
	if !reflect.DeepEqual(plan, original) {
		t.Fatalf("dry-run plan mutated\nplan=%#v\noriginal=%#v", plan, original)
	}
	if !reflect.DeepEqual(first.Plan.GeneratedFiles, second.Plan.GeneratedFiles) {
		t.Fatalf("effective generated files differ after idempotent apply")
	}
	data, err := os.ReadFile(filepath.Join(root, ".anito", "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile config: %v", err)
	}
	if !strings.Contains(string(data), "port: 8100") {
		t.Fatalf("config missing reserved port:\n%s", string(data))
	}
}

func TestApplyRollsBackNewReservationsOnReserveFailure(t *testing.T) {
	root := t.TempDir()
	plan := &Plan{
		Mode:     ModeComposite,
		RepoPath: root,
		Allocations: PortAllocation{
			"api": 8100,
			"web": 8101,
		},
		GeneratedFiles: []GeneratedFile{{RelPath: ".anito/api.yaml", Content: "name: api\n"}},
	}
	ports := &fakePorts{used: map[int]bool{}, existing: map[string]int{}, failName: "web"}

	_, err := Apply(plan, ports)
	if err == nil {
		t.Fatal("expected reserve failure")
	}
	if !reflect.DeepEqual(ports.removed, []string{"api"}) {
		t.Fatalf("removed = %v, want api rollback", ports.removed)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".anito", "api.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("file was written after reserve failure, stat err = %v", statErr)
	}
}

func TestApplicationCommitRollsBackGeneratedFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "first.txt")
	if err := os.WriteFile(target, []byte("old\n"), 0600); err != nil {
		t.Fatalf("WriteFile target: %v", err)
	}
	tmp, err := os.CreateTemp(root, ".tmp-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := tmp.WriteString("new\n"); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp: %v", err)
	}

	app := &application{ops: []fileOp{
		{
			relPath:    "first.txt",
			path:       target,
			tmpPath:    tmp.Name(),
			mode:       0644,
			existed:    true,
			before:     []byte("old\n"),
			beforeMode: 0600,
		},
		{
			relPath: "missing.txt",
			path:    filepath.Join(root, "missing.txt"),
			tmpPath: filepath.Join(root, "does-not-exist.tmp"),
			mode:    0644,
		},
	}}
	err = app.commit(&ApplyResult{Plan: &Plan{}})
	if err == nil {
		t.Fatal("expected commit failure")
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("ReadFile target: %v", readErr)
	}
	if string(data) != "old\n" {
		t.Fatalf("target content = %q, want rollback to old", string(data))
	}
	info, statErr := os.Stat(target)
	if statErr != nil {
		t.Fatalf("Stat target: %v", statErr)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("target mode = %v, want original 0600", info.Mode().Perm())
	}
}

func TestApplicationCommitRollsBackSourcePatch(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "vite.config.ts")
	if err := os.WriteFile(target, []byte("old patch\n"), 0600); err != nil {
		t.Fatalf("WriteFile target: %v", err)
	}
	tmp, err := os.CreateTemp(root, ".tmp-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := tmp.WriteString("new patch\n"); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp: %v", err)
	}

	app := &application{ops: []fileOp{
		{
			relPath:    "vite.config.ts",
			path:       target,
			tmpPath:    tmp.Name(),
			mode:       0644,
			existed:    true,
			before:     []byte("old patch\n"),
			beforeMode: 0600,
		},
		{
			relPath: "missing.txt",
			path:    filepath.Join(root, "missing.txt"),
			tmpPath: filepath.Join(root, "does-not-exist.tmp"),
			mode:    0644,
		},
	}}
	err = app.commit(&ApplyResult{Plan: &Plan{
		SourcePatches: []SourcePatch{{RelPath: "vite.config.ts"}},
	}})
	if err == nil {
		t.Fatal("expected commit failure")
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("ReadFile target: %v", readErr)
	}
	if string(data) != "old patch\n" {
		t.Fatalf("target content = %q, want rollback to old patch", string(data))
	}
	info, statErr := os.Stat(target)
	if statErr != nil {
		t.Fatalf("Stat target: %v", statErr)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("target mode = %v, want original 0600", info.Mode().Perm())
	}
}

func TestApplySourcePatchReplacesManagedBlockAndReportsUnapplied(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "vite.config.ts", "export default defineConfig({\n"+
		"  "+ManagedBlockStart+"\n"+
		"  old: true,\n"+
		"  "+ManagedBlockEnd+"\n"+
		"})\n")
	writeFile(t, root, "plain.config.ts", "export default {}\n")

	block := "  " + ManagedBlockStart + "\n  server: { port: 8100 },\n  " + ManagedBlockEnd + "\n"
	result, err := Apply(&Plan{
		Mode:     ModeComposite,
		RepoPath: root,
		SourcePatches: []SourcePatch{
			{RelPath: "vite.config.ts", Block: block},
			{RelPath: "plain.config.ts", Block: block},
			{RelPath: "missing.config.ts", Block: block},
		},
	}, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !reflect.DeepEqual(result.AppliedPatches, []string{"vite.config.ts"}) {
		t.Fatalf("applied patches = %v, want vite.config.ts", result.AppliedPatches)
	}
	if !reflect.DeepEqual(result.UnappliedPatches, []string{"plain.config.ts", "missing.config.ts"}) {
		t.Fatalf("unapplied patches = %v", result.UnappliedPatches)
	}
	data, err := os.ReadFile(filepath.Join(root, "vite.config.ts"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "server: { port: 8100 }") || strings.Contains(string(data), "old: true") {
		t.Fatalf("managed block not replaced:\n%s", string(data))
	}
}

func TestApplyValidatesPlanAndRepository(t *testing.T) {
	if _, err := Apply(nil, nil); err == nil {
		t.Fatal("nil plan unexpectedly succeeded")
	}
	if _, err := Apply(&Plan{RepoPath: filepath.Join(t.TempDir(), "missing")}, nil); err == nil {
		t.Fatal("missing repository unexpectedly succeeded")
	}
	root := t.TempDir()
	for _, rel := range []string{"", filepath.Join(root, "absolute.txt")} {
		_, err := Apply(&Plan{RepoPath: root, GeneratedFiles: []GeneratedFile{{RelPath: rel}}}, nil)
		if err == nil {
			t.Fatalf("path %q unexpectedly succeeded", rel)
		}
	}
	_, err := Apply(&Plan{Mode: ModeComposite, RepoPath: root, Allocations: PortAllocation{"svc": 8125}}, nil)
	if err == nil {
		t.Fatal("composite setup without reserver unexpectedly succeeded")
	}
	fileRoot := filepath.Join(root, "file-root")
	if err := os.WriteFile(fileRoot, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(&Plan{RepoPath: fileRoot}, nil); err == nil {
		t.Fatal("file repository unexpectedly succeeded")
	}
	configured := filepath.Join(root, "configured")
	if err := os.MkdirAll(filepath.Join(configured, ".anito"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configured, ".anito", "config.yaml"), []byte("name: svc\n"), 0644); err != nil {
		t.Fatal(err)
	}
	configuredPlan, err := DryRun(PlanRequest{Path: configured}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(configuredPlan, nil); err == nil {
		t.Fatal("configured repository unexpectedly succeeded")
	}
}

func TestGeneratedShellFileUsesExecutableMode(t *testing.T) {
	if got := modeForGeneratedFile("scripts/setup.sh"); got != 0755 {
		t.Fatalf("shell mode = %o, want 0755", got)
	}
	if got := modeForGeneratedFile("config.yaml"); got != 0644 {
		t.Fatalf("config mode = %o, want 0644", got)
	}
}
