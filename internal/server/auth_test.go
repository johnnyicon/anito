package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/johnnyicon/anito/internal/auth"
)

func TestCapabilityMiddlewareProtectsPrivilegedRoutes(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		path     string
		token    string
		bearer   string
		wantCode int
	}{
		{
			name:     "deploy requires token",
			method:   http.MethodPost,
			path:     "/deploy",
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "remove requires token",
			method:   http.MethodPost,
			path:     "/remove/demo",
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "teardown requires token",
			method:   http.MethodPost,
			path:     "/teardown",
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "wrong token rejected",
			method:   http.MethodPost,
			path:     "/deploy",
			token:    "wrong",
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "dedicated header accepted",
			method:   http.MethodPost,
			path:     "/deploy",
			token:    "secret",
			wantCode: http.StatusNoContent,
		},
		{
			name:     "bearer token accepted",
			method:   http.MethodPost,
			path:     "/teardown",
			bearer:   "secret",
			wantCode: http.StatusNoContent,
		},
		{
			name:     "read route stays open",
			method:   http.MethodGet,
			path:     "/services",
			wantCode: http.StatusNoContent,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &Server{capabilityToken: "secret"}
			e := echo.New()
			e.Use(srv.capabilityMiddleware())
			e.Any("/*", func(c echo.Context) error {
				return c.NoContent(http.StatusNoContent)
			})

			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.token != "" {
				req.Header.Set(auth.HeaderName, tc.token)
			}
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantCode, rec.Body.String())
			}
		})
	}
}

func TestRequiresCapability(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{method: http.MethodPost, path: "/deploy", want: true},
		{method: http.MethodPost, path: "/stop/demo", want: true},
		{method: http.MethodPost, path: "/restart/demo", want: true},
		{method: http.MethodPost, path: "/rollback/demo", want: true},
		{method: http.MethodPost, path: "/remove/demo", want: true},
		{method: http.MethodPost, path: "/issues", want: true},
		{method: http.MethodDelete, path: "/issues", want: true},
		{method: http.MethodPost, path: "/teardown", want: true},
		{method: http.MethodGet, path: "/services", want: false},
		{method: http.MethodGet, path: "/health", want: false},
	}

	for _, tc := range cases {
		if got := requiresCapability(tc.method, tc.path); got != tc.want {
			t.Fatalf("requiresCapability(%q, %q) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
