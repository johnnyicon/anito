package domain

import (
	"errors"
	"testing"
)

func TestWireErrorRedactsSecrets(t *testing.T) {
	err := InvalidConfigf("env parse failed: API_KEY=supersecret token: abc123 Bearer live-token")
	wire := ToWire(err)
	for _, leaked := range []string{"supersecret", "abc123", "live-token"} {
		if contains(wire.Error, leaked) {
			t.Fatalf("wire error leaked %q: %q", leaked, wire.Error)
		}
	}
	if !contains(wire.Error, "[redacted]") {
		t.Fatalf("wire error was not redacted: %q", wire.Error)
	}
}

func TestCodeOf(t *testing.T) {
	err := Conflictf("port conflict")
	code, ok := CodeOf(err)
	if !ok || code != CodeConflict {
		t.Fatalf("CodeOf = %q/%v, want conflict/true", code, ok)
	}
	if _, ok := CodeOf(errors.New("plain")); ok {
		t.Fatal("plain error unexpectedly had a domain code")
	}
}

func contains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
