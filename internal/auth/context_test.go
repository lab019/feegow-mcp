package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithTokenAndFromContext(t *testing.T) {
	ctx := WithToken(t.Context(), "abc123")
	got, ok := TokenFromContext(ctx)
	if !ok {
		t.Fatalf("TokenFromContext: ok = false, want true")
	}
	if got != "abc123" {
		t.Fatalf("TokenFromContext: got %q, want %q", got, "abc123")
	}
}

func TestTokenFromContext_Absent(t *testing.T) {
	if tok, ok := TokenFromContext(t.Context()); ok {
		t.Fatalf("TokenFromContext on empty context: got (%q, true), want (_, false)", tok)
	}
}

func TestTokenFromContext_EmptyStringTreatedAsAbsent(t *testing.T) {
	ctx := WithToken(t.Context(), "")
	if tok, ok := TokenFromContext(ctx); ok {
		t.Fatalf("TokenFromContext with empty token: got (%q, true), want (_, false)", tok)
	}
}

func TestExtractBearer(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
		wantOK bool
	}{
		{"missing", "", "", false},
		{"wrong scheme", "Basic dXNlcjpwYXNz", "", false},
		{"no token after scheme", "Bearer", "", false},
		{"empty token", "Bearer   ", "", false},
		{"valid", "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhIn0.sig", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhIn0.sig", true},
		{"case insensitive scheme", "bearer opaque-feegow-token", "opaque-feegow-token", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := http.Header{}
			if c.header != "" {
				h.Set("Authorization", c.header)
			}
			tok, ok := ExtractBearer(h)
			if ok != c.wantOK || tok != c.want {
				t.Fatalf("ExtractBearer(%q) = (%q, %v), want (%q, %v)", c.header, tok, ok, c.want, c.wantOK)
			}
		})
	}
}

// TestMiddleware_NoAuthorizationHeader_FailsClosed is acceptance criterion
// #2: a request with no Authorization header at all must get 401, and the
// wrapped handler must never run.
func TestMiddleware_NoAuthorizationHeader_FailsClosed(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	srv := httptest.NewServer(Middleware(next))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/mcp", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if called {
		t.Fatalf("next handler was called despite missing Authorization header")
	}
	if got := resp.Header.Get("WWW-Authenticate"); got == "" {
		t.Fatalf("expected WWW-Authenticate header on 401")
	}
}

// TestMiddleware_MalformedAuthorization_FailsClosed is acceptance criterion
// #3: any malformed Authorization header (wrong scheme, missing token,
// empty token) must get 401 and never reach the wrapped handler.
func TestMiddleware_MalformedAuthorization_FailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		header string
	}{
		{"no bearer scheme at all", "opaque-token-without-scheme"},
		{"wrong scheme", "Basic dXNlcjpwYXNz"},
		{"bearer with no token", "Bearer"},
		{"bearer with empty token", "Bearer   "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
			})
			srv := httptest.NewServer(Middleware(next))
			defer srv.Close()

			req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("Authorization", c.header)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
			}
			if called {
				t.Fatalf("next handler was called despite malformed Authorization header %q", c.header)
			}
		})
	}
}

// TestMiddleware_ValidBearer_ReachesHandlerViaContext is acceptance
// criterion #4: a valid bearer token must reach the wrapped handler,
// readable from the request's context.
func TestMiddleware_ValidBearer_ReachesHandlerViaContext(t *testing.T) {
	var gotToken string
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken, gotOK = TokenFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(Middleware(next))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer opaque-feegow-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !gotOK {
		t.Fatalf("token was not injected into context")
	}
	if gotToken != "opaque-feegow-token" {
		t.Fatalf("gotToken = %q, want %q", gotToken, "opaque-feegow-token")
	}
}

// TestMiddleware_ExpiredJWT_RejectedWithoutReachingHandler is acceptance
// criterion #5: a JWT bearer token with "exp" in the past must be rejected
// with a readable message, and the request to Feegow (i.e. the wrapped
// handler) must never fire.
func TestMiddleware_ExpiredJWT_RejectedWithoutReachingHandler(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	srv := httptest.NewServer(Middleware(next))
	defer srv.Close()

	expired := testJWT(t, map[string]any{"exp": 1000000000, "sub": "clinica-1"}) // 2001, long past
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+expired)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if called {
		t.Fatalf("wrapped handler (stand-in for the Feegow call) was reached with an expired token")
	}
	if !strings.Contains(string(body), "expirada") {
		t.Fatalf("body = %q, want a readable message mentioning the credential is expired", body)
	}
}

// TestMiddleware_NonJWTBearer_PassesThrough is acceptance criterion #6: a
// bearer token that isn't a decodable JWT must NOT be rejected by this
// service — Feegow is the authority on the token, so we let it through.
func TestMiddleware_NonJWTBearer_PassesThrough(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(Middleware(next))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer this-is-not-a-jwt-at-all")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (non-JWT bearer should pass through to the handler)", resp.StatusCode, http.StatusOK)
	}
	if !called {
		t.Fatalf("wrapped handler was never reached for a non-JWT bearer token")
	}
}

// TestMiddleware_Statelessness_SequentialRequestsDoNotShareTokens is
// acceptance criterion #7, the central security test for this service:
// this server has no org_id and no per-tenant isolation of its own, so the
// only guarantee it can offer is that nothing from one request survives
// into the next. Two sequential requests carrying two different tokens
// against the *same* middleware-wrapped handler instance must each see
// only their own token, never the other's.
func TestMiddleware_Statelessness_SequentialRequestsDoNotShareTokens(t *testing.T) {
	seen := make(chan string, 1)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := TokenFromContext(r.Context())
		if !ok {
			t.Errorf("handler invoked without a token in context")
		}
		seen <- tok
		w.WriteHeader(http.StatusOK)
	})
	// One shared handler instance (and, in the real service, one shared
	// *mcp.Server) fields both requests below, exactly like production:
	// there is no per-request server construction to hide behind.
	handler := Middleware(next)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	doRequest := func(token string) string {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		select {
		case tok := <-seen:
			return tok
		default:
			t.Fatalf("handler did not report a token for request with %q", token)
			return ""
		}
	}

	tokens := []string{"clinic-A-token", "clinic-B-token", "clinic-A-token-again", "clinic-C-token"}
	for i, tok := range tokens {
		got := doRequest(tok)
		if got != tok {
			t.Fatalf("request #%d: handler saw token %q, want %q (cross-request token leakage)", i, got, tok)
		}
		// Every other token in the set must be absent from what this
		// request's handler invocation observed — belt and suspenders on
		// top of the direct equality check above.
		for _, other := range tokens {
			if other != tok && got == other {
				t.Fatalf("request #%d (token %q) leaked a different request's token %q", i, tok, other)
			}
		}
	}
}

// TestMiddleware_NoTokenLeakInLogs is acceptance criterion #8: the audit
// log line emitted for an authenticated request must never contain the
// bearer token, in whole or in part.
func TestMiddleware_NoTokenLeakInLogs(t *testing.T) {
	logOut := captureAuditLog(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(Middleware(next))
	defer srv.Close()

	secretToken := testJWT(t, map[string]any{"sub": "clinica-secreta", "exp": 9999999999})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+secretToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	logged := logOut.String()
	if strings.Contains(logged, secretToken) {
		t.Fatalf("log output contains the full bearer token: %q", logged)
	}
	// Also guard against partial leaks (prefix/suffix), not just the exact
	// full-string match above.
	if len(secretToken) > 12 && strings.Contains(logged, secretToken[:12]) {
		t.Fatalf("log output contains a prefix of the bearer token: %q", logged)
	}
}
