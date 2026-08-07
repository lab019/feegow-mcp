package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthz is acceptance criterion #1: GET /healthz responds 200.
func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(newMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want %q", body, "ok")
	}
}

// TestMux_BothMCPMountsRequireAuth confirms /mcp and /mcp/admin are both
// actually wired to the fail-closed auth middleware, not just present in
// the routing table.
func TestMux_BothMCPMountsRequireAuth(t *testing.T) {
	srv := httptest.NewServer(newMux())
	defer srv.Close()

	for _, path := range []string{"/mcp", "/mcp/admin"} {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Post(srv.URL+path, "application/json", nil)
			if err != nil {
				t.Fatalf("POST %s: %v", path, err)
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s: status = %d, want %d", path, resp.StatusCode, http.StatusUnauthorized)
			}
		})
	}
}
