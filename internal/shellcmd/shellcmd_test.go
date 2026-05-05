package shellcmd

import "testing"

func TestCommandUsesShellForBuildString(t *testing.T) {
	cmd := Command("printf ok > out && test -f out", "/tmp/anito-build")

	if cmd.Path != shellPath {
		t.Fatalf("Path = %q, want %q", cmd.Path, shellPath)
	}
	if len(cmd.Args) != 3 {
		t.Fatalf("Args len = %d, want 3: %#v", len(cmd.Args), cmd.Args)
	}
	if cmd.Args[1] != "-c" {
		t.Fatalf("Args[1] = %q, want -c", cmd.Args[1])
	}
	if cmd.Args[2] != "printf ok > out && test -f out" {
		t.Fatalf("Args[2] = %q", cmd.Args[2])
	}
	if cmd.Dir != "/tmp/anito-build" {
		t.Fatalf("Dir = %q, want /tmp/anito-build", cmd.Dir)
	}
}

func TestBuildDirForRepoAnitoConfig(t *testing.T) {
	got := BuildDir("/repo/.anito/gomanan-mcp.yaml")
	if got != "/repo" {
		t.Fatalf("BuildDir = %q, want /repo", got)
	}
}

func TestBuildDirForNonAnitoConfig(t *testing.T) {
	got := BuildDir("/repo/configs/service.yaml")
	if got != "/repo/configs" {
		t.Fatalf("BuildDir = %q, want /repo/configs", got)
	}
}
