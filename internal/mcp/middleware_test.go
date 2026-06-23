package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johnnyicon/anito/internal/auth"
	"github.com/johnnyicon/anito/internal/sessions"
)

func newSessStore(t *testing.T) *sessions.Store {
	t.Helper()
	return sessions.New(t.TempDir())
}

// fakeHandler is a downstream handler that optionally sets Mcp-Session-Id on
// the response (simulating the SDK during an initialize call).
type fakeHandler struct {
	responseSessionID string
}

func (f *fakeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.responseSessionID != "" {
		w.Header().Set("Mcp-Session-Id", f.responseSessionID)
	}
	w.WriteHeader(http.StatusOK)
}

// TestSessionMiddleware_CapturesNewSessionOnInitialize verifies that when the
// downstream handler (SDK) assigns a session ID in the response header, the
// middleware creates a new session record.
func TestSessionMiddleware_CapturesNewSessionOnInitialize(t *testing.T) {
	sess := newSessStore(t)
	inner := &fakeHandler{responseSessionID: "new-session-123"}
	handler := sessionMiddleware(sess, inner)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"method":"initialize"}`))
	// No Mcp-Session-Id header — this is an initialize request.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	list, _ := sess.List()
	if len(list) != 1 {
		t.Fatalf("want 1 session created, got %d", len(list))
	}
	if list[0].ID != "new-session-123" {
		t.Errorf("want ID new-session-123, got %s", list[0].ID)
	}
}

// TestSessionMiddleware_NoCreateWhenResponseLacksID covers the case where the
// downstream handler doesn't assign a session ID (e.g. error response).
func TestSessionMiddleware_NoCreateWhenResponseLacksID(t *testing.T) {
	sess := newSessStore(t)
	inner := &fakeHandler{} // no responseSessionID
	handler := sessionMiddleware(sess, inner)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"method":"initialize"}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	list, _ := sess.List()
	if len(list) != 0 {
		t.Errorf("want 0 sessions, got %d", len(list))
	}
}

// TestSessionMiddleware_TouchesExistingSession verifies that requests with a
// known Mcp-Session-Id header update the session (LastSeenAt + CallCount).
func TestSessionMiddleware_TouchesExistingSession(t *testing.T) {
	sess := newSessStore(t)
	_ = sess.Create("existing-456")

	inner := &fakeHandler{}
	handler := sessionMiddleware(sess, inner)

	body := `{"method":"tools/call","params":{"name":"anito_status"}}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Mcp-Session-Id", "existing-456")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	list, _ := sess.List()
	if len(list) != 1 {
		t.Fatalf("want 1 session, got %d", len(list))
	}
	if list[0].CallCount != 1 {
		t.Errorf("want CallCount 1, got %d", list[0].CallCount)
	}
	if list[0].LastTool != "anito_status" {
		t.Errorf("want LastTool anito_status, got %s", list[0].LastTool)
	}
}

// TestSessionMiddleware_TouchCreatesUnknownSession verifies that a session ID
// the middleware has never seen (e.g. after a daemon restart) is auto-created.
func TestSessionMiddleware_TouchCreatesUnknownSession(t *testing.T) {
	sess := newSessStore(t)
	inner := &fakeHandler{}
	handler := sessionMiddleware(sess, inner)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"method":"tools/call","params":{"name":"anito_services"}}`))
	req.Header.Set("Mcp-Session-Id", "post-restart-789")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	list, _ := sess.List()
	if len(list) != 1 {
		t.Fatalf("want 1 session created, got %d", len(list))
	}
	if list[0].ID != "post-restart-789" {
		t.Errorf("want ID post-restart-789, got %s", list[0].ID)
	}
}

// TestExtractToolName covers the JSON-RPC body parser.
func TestExtractToolName(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "tools/call with name",
			body: `{"method":"tools/call","params":{"name":"anito_deploy"}}`,
			want: "anito_deploy",
		},
		{
			name: "non-tool method",
			body: `{"method":"initialize","params":{}}`,
			want: "",
		},
		{
			name: "tools/call without name",
			body: `{"method":"tools/call","params":{}}`,
			want: "",
		},
		{
			name: "invalid json",
			body: `{not json`,
			want: "",
		},
		{
			name: "empty body",
			body: ``,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			got := extractToolName(req)
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
			// Body must be readable again after extraction.
			if req.Body != nil {
				buf := make([]byte, len(tc.body))
				n, _ := req.Body.Read(buf)
				if string(buf[:n]) != tc.body {
					t.Errorf("body not restored: got %q, want %q", string(buf[:n]), tc.body)
				}
			}
		})
	}
}

func TestCapabilityMiddlewareProtectsPrivilegedTools(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		token    string
		bearer   string
		wantCode int
		wantCall bool
	}{
		{
			name:     "initialize remains open",
			body:     `{"method":"initialize","params":{}}`,
			wantCode: http.StatusNoContent,
			wantCall: true,
		},
		{
			name:     "read tool remains open",
			body:     `{"method":"tools/call","params":{"name":"anito_status"}}`,
			wantCode: http.StatusNoContent,
			wantCall: true,
		},
		{
			name:     "privileged deploy requires token",
			body:     `{"method":"tools/call","params":{"name":"anito_deploy"}}`,
			wantCode: http.StatusUnauthorized,
			wantCall: false,
		},
		{
			name:     "privileged teardown accepts header token",
			body:     `{"method":"tools/call","params":{"name":"anito_teardown"}}`,
			token:    "secret",
			wantCode: http.StatusNoContent,
			wantCall: true,
		},
		{
			name:     "privileged remove accepts bearer token",
			body:     `{"method":"tools/call","params":{"name":"anito_remove"}}`,
			bearer:   "secret",
			wantCode: http.StatusNoContent,
			wantCall: true,
		},
		{
			name:     "batch with privileged tool requires token",
			body:     `[{"method":"tools/call","params":{"name":"anito_status"}},{"method":"tools/call","params":{"name":"anito_deploy"}}]`,
			wantCode: http.StatusUnauthorized,
			wantCall: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			handler := capabilityMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				gotBody, _ := io.ReadAll(r.Body)
				if string(gotBody) != tc.body {
					t.Fatalf("body = %q, want %q", string(gotBody), tc.body)
				}
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			if tc.token != "" {
				req.Header.Set(auth.HeaderName, tc.token)
			}
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, tc.wantCode, rr.Body.String())
			}
			if called != tc.wantCall {
				t.Fatalf("downstream called = %v, want %v", called, tc.wantCall)
			}
		})
	}
}

func TestRPCBodyNeedsCapability(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{name: "empty", body: "", want: false},
		{name: "initialize", body: `{"method":"initialize"}`, want: false},
		{name: "read tool", body: `{"method":"tools/call","params":{"name":"anito_services"}}`, want: false},
		{name: "deploy tool", body: `{"method":"tools/call","params":{"name":"anito_deploy"}}`, want: true},
		{name: "batch read only", body: `[{"method":"tools/call","params":{"name":"anito_services"}}]`, want: false},
		{name: "batch privileged", body: `[{"method":"tools/call","params":{"name":"anito_status"}},{"method":"tools/call","params":{"name":"anito_remove"}}]`, want: true},
		{name: "invalid json", body: `{not-json`, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rpcBodyNeedsCapability([]byte(tc.body)); got != tc.want {
				t.Fatalf("rpcBodyNeedsCapability = %v, want %v", got, tc.want)
			}
		})
	}
}
