package shellcmd

import (
	"os/exec"
	"path/filepath"
)

const shellPath = "/bin/sh"

// Command returns an exec.Cmd that runs command through the user's POSIX shell
// surface. Anito config build fields are shell commands, not argv vectors: they
// may contain operators such as &&, variable expansion, redirects, and quoting.
func Command(command string, dir string) *exec.Cmd {
	cmd := exec.Command(shellPath, "-c", command)
	cmd.Dir = dir
	return cmd
}

// BuildDir returns the working directory for a build command loaded from
// configPath. Repo-local Anito configs usually live in .anito/, while their
// build commands are written relative to the repo root.
func BuildDir(configPath string) string {
	dir := filepath.Dir(configPath)
	if filepath.Base(dir) == ".anito" {
		return filepath.Dir(dir)
	}
	return dir
}
