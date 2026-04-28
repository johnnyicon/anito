package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johnnyicon/anito/internal/setup"
)

func TestWriteGeneratedFilesWritesRelativeFiles(t *testing.T) {
	root := t.TempDir()
	files := []coordinateFile{
		{RelPath: ".anito/config.yaml", Content: "name: svc\n"},
		{RelPath: ".anito/svc-dev.sh", Content: "#!/usr/bin/env bash\n"},
	}

	applied, err := writeGeneratedFiles(root, files)
	if err != nil {
		t.Fatalf("writeGeneratedFiles: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied len = %d, want 2", len(applied))
	}

	data, err := os.ReadFile(filepath.Join(root, ".anito/config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile config: %v", err)
	}
	if string(data) != "name: svc\n" {
		t.Errorf("config content = %q", string(data))
	}

	info, err := os.Stat(filepath.Join(root, ".anito/svc-dev.sh"))
	if err != nil {
		t.Fatalf("Stat script: %v", err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("script mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestWriteGeneratedFilesRejectsPathEscape(t *testing.T) {
	_, err := writeGeneratedFiles(t.TempDir(), []coordinateFile{{RelPath: "../outside", Content: "bad"}})
	if err == nil {
		t.Fatal("expected path escape error")
	}
	if !strings.Contains(err.Error(), "escapes repo root") {
		t.Errorf("error = %q, want escapes repo root", err.Error())
	}
}

func TestApplySourcePatchesReplacesExistingManagedBlock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "vite.config.ts")
	original := "export default defineConfig({\n" +
		"  " + setup.ManagedBlockStart + "\n" +
		"  old: true,\n" +
		"  " + setup.ManagedBlockEnd + "\n" +
		"})\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	block := "  " + setup.ManagedBlockStart + "\n  server: { port: 8100 },\n  " + setup.ManagedBlockEnd + "\n"
	applied, unapplied, err := applySourcePatches(root, []coordinatePatch{{RelPath: "vite.config.ts", Block: block}})
	if err != nil {
		t.Fatalf("applySourcePatches: %v", err)
	}
	if len(applied) != 1 || applied[0] != "vite.config.ts" {
		t.Fatalf("applied = %v, want vite.config.ts", applied)
	}
	if len(unapplied) != 0 {
		t.Fatalf("unapplied = %v, want none", unapplied)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "server: { port: 8100 }") {
		t.Errorf("patched file missing replacement block:\n%s", string(data))
	}
	if strings.Contains(string(data), "old: true") {
		t.Errorf("patched file still contains old block:\n%s", string(data))
	}
}

func TestApplySourcePatchesReportsUnappliedWithoutManagedBlock(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vite.config.ts"), []byte("export default {}\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	applied, unapplied, err := applySourcePatches(root, []coordinatePatch{{RelPath: "vite.config.ts", Block: "server: {}\n"}})
	if err != nil {
		t.Fatalf("applySourcePatches: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("applied = %v, want none", applied)
	}
	if len(unapplied) != 1 || unapplied[0] != "vite.config.ts" {
		t.Fatalf("unapplied = %v, want vite.config.ts", unapplied)
	}
}
