package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateTokenCreatesPrivateTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	t.Setenv(EnvToken, "")
	t.Setenv(EnvTokenFile, path)

	token, source, err := LoadOrCreateToken()
	if err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	if source != path {
		t.Fatalf("source = %q, want %q", source, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("mode = %o, want 0600", got)
	}

	again, againSource, err := LoadOrCreateToken()
	if err != nil {
		t.Fatalf("LoadOrCreateToken again: %v", err)
	}
	if again != token || againSource != path {
		t.Fatalf("second load = (%q, %q), want (%q, %q)", again, againSource, token, path)
	}
}

func TestLoadOrCreateTokenUsesEnvToken(t *testing.T) {
	t.Setenv(EnvToken, "from-env")
	t.Setenv(EnvTokenFile, filepath.Join(t.TempDir(), "token"))

	token, source, err := LoadOrCreateToken()
	if err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	if token != "from-env" || source != EnvToken {
		t.Fatalf("got (%q, %q), want env token", token, source)
	}
}

func TestLoadClientTokenDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-token")
	t.Setenv(EnvToken, "")
	t.Setenv(EnvTokenFile, path)

	token, source, err := LoadClientToken()
	if err != nil {
		t.Fatalf("LoadClientToken: %v", err)
	}
	if token != "" || source != path {
		t.Fatalf("got (%q, %q), want empty token with source %q", token, source, path)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("token file created unexpectedly: %v", err)
	}
}

func TestAuthorizedAcceptsHeaderAndBearerToken(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*http.Request)
		want  bool
	}{
		{
			name: "dedicated header",
			setup: func(req *http.Request) {
				req.Header.Set(HeaderName, "secret")
			},
			want: true,
		},
		{
			name: "bearer",
			setup: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer secret")
			},
			want: true,
		},
		{
			name: "wrong token",
			setup: func(req *http.Request) {
				req.Header.Set(HeaderName, "nope")
			},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			tc.setup(req)
			if got := Authorized(req, "secret"); got != tc.want {
				t.Fatalf("Authorized = %v, want %v", got, tc.want)
			}
		})
	}
}
