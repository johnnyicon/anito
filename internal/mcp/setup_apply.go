package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/johnnyicon/anito/internal/setup"
)

func (s *Server) applySetup(out *setupResult) error {
	if out == nil {
		return nil
	}

	for name, plannedPort := range out.Allocations {
		port, err := s.svc.Reserve(name, plannedPort)
		if err != nil {
			return fmt.Errorf("reserve %s: %w", name, err)
		}
		if plannedPort != 0 && port != plannedPort {
			return fmt.Errorf("reserve %s: got port %d, wanted planned port %d", name, port, plannedPort)
		}
		out.Allocations[name] = port
	}

	files, err := writeGeneratedFiles(out.RepoPath, out.GeneratedFiles)
	if err != nil {
		return err
	}
	applied, unapplied, err := applySourcePatches(out.RepoPath, out.SourcePatches)
	if err != nil {
		return err
	}
	out.Applied = true
	out.AppliedFiles = files
	out.AppliedPatches = applied
	out.UnappliedPatches = unapplied
	return nil
}

func writeGeneratedFiles(root string, files []coordinateFile) ([]string, error) {
	applied := make([]string, 0, len(files))
	for _, file := range files {
		path, err := safeJoin(root, file.RelPath)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, err
		}
		mode := os.FileMode(0644)
		if strings.HasSuffix(path, ".sh") {
			mode = 0755
		}
		if err := os.WriteFile(path, []byte(file.Content), mode); err != nil {
			return nil, err
		}
		applied = append(applied, file.RelPath)
	}
	return applied, nil
}

func applySourcePatches(root string, patches []coordinatePatch) (applied []string, unapplied []string, err error) {
	for _, patch := range patches {
		path, joinErr := safeJoin(root, patch.RelPath)
		if joinErr != nil {
			return nil, nil, joinErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				unapplied = append(unapplied, patch.RelPath)
				continue
			}
			return nil, nil, readErr
		}

		next, ok := replaceManagedBlock(string(data), patch.Block)
		if !ok {
			unapplied = append(unapplied, patch.RelPath)
			continue
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, nil, statErr
		}
		if err := os.WriteFile(path, []byte(next), info.Mode()); err != nil {
			return nil, nil, err
		}
		applied = append(applied, patch.RelPath)
	}
	return applied, unapplied, nil
}

func replaceManagedBlock(content, block string) (string, bool) {
	start := strings.Index(content, setup.ManagedBlockStart)
	if start == -1 {
		return "", false
	}
	endRel := strings.Index(content[start:], setup.ManagedBlockEnd)
	if endRel == -1 {
		return "", false
	}
	end := start + endRel + len(setup.ManagedBlockEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	replacement := block
	if !strings.HasSuffix(replacement, "\n") {
		replacement += "\n"
	}
	return content[:start] + replacement + content[end:], true
}

func safeJoin(root, rel string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("repo_path is required")
	}
	if rel == "" {
		return "", fmt.Errorf("generated file path is required")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("generated path %q must be relative", rel)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path := filepath.Clean(filepath.Join(absRoot, rel))
	prefix := absRoot + string(os.PathSeparator)
	if path != absRoot && !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("generated path %q escapes repo root", rel)
	}
	return path, nil
}
