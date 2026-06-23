// Package auth manages Anito's local control-plane capability token.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	// HeaderName carries the Anito control-plane capability token.
	HeaderName = "X-Anito-Capability-Token"

	// EnvToken supplies a token without touching the token file. This is useful
	// for tests and launch environments that already manage secrets.
	EnvToken = "ANITO_CAPABILITY_TOKEN"

	// EnvTokenFile overrides the default token file path.
	EnvTokenFile = "ANITO_CAPABILITY_TOKEN_FILE"

	tokenFileName = "capability-token"
	tokenBytes    = 32
)

// DefaultTokenPath returns the default per-user capability token location.
func DefaultTokenPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv(EnvTokenFile)); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".anito", tokenFileName), nil
}

// LoadOrCreateToken returns the configured capability token, creating a
// per-user token file with 0600 permissions when no env token is set.
func LoadOrCreateToken() (token string, source string, err error) {
	if token := strings.TrimSpace(os.Getenv(EnvToken)); token != "" {
		return token, EnvToken, nil
	}
	path, err := DefaultTokenPath()
	if err != nil {
		return "", "", err
	}
	token, err = readTokenFile(path)
	if err == nil {
		return token, path, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	token, err = generateToken()
	if err != nil {
		return "", "", err
	}
	if err := writeTokenFile(path, token); err != nil {
		if errors.Is(err, os.ErrExist) {
			token, err = readTokenFile(path)
			if err == nil {
				return token, path, nil
			}
		}
		return "", "", err
	}
	return token, path, nil
}

// LoadClientToken returns the env or file token for clients. It never creates a
// token file; the daemon is responsible for first-run creation.
func LoadClientToken() (token string, source string, err error) {
	if token := strings.TrimSpace(os.Getenv(EnvToken)); token != "" {
		return token, EnvToken, nil
	}
	path, err := DefaultTokenPath()
	if err != nil {
		return "", "", err
	}
	token, err = readTokenFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", path, nil
	}
	if err != nil {
		return "", "", err
	}
	return token, path, nil
}

// AttachToken adds the capability token to an outbound request.
func AttachToken(req *http.Request, token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	req.Header.Set(HeaderName, token)
}

// Authorized reports whether r presents the expected capability token.
func Authorized(r *http.Request, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get(HeaderName))
	if got == "" {
		got = bearerToken(r.Header.Get("Authorization"))
	}
	if len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func bearerToken(header string) string {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func readTokenFile(path string) (string, error) {
	if err := ensurePrivateFile(path); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", fmt.Errorf("empty capability token file: %s", path)
	}
	return token, nil
}

func ensurePrivateFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("capability token path is a directory: %s", path)
	}
	if info.Mode().Perm()&0077 != 0 {
		if err := os.Chmod(path, 0600); err != nil {
			return fmt.Errorf("chmod capability token file: %w", err)
		}
	}
	return nil
}

func writeTokenFile(path string, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create capability token directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(token + "\n"); err != nil {
		return err
	}
	return f.Chmod(0600)
}

func generateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate capability token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
