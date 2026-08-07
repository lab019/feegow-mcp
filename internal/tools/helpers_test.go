package tools

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lab019/feegow-mcp/internal/auth"
	"github.com/lab019/feegow-mcp/internal/feegow"
)

// ctxWithToken returns a context carrying tok, the way auth.Middleware
// would set it up for a real request.
func ctxWithToken(tok string) context.Context {
	return auth.WithToken(context.Background(), tok)
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("writing test response: %v", err)
	}
}

// newTestClient stands up mux as a fake Feegow (every host this package
// calls — api.feegow.com — lives under HostAPI, so one httptest.Server
// with a hostOverride covers every endpoint a single tool call needs).
func newTestClient(t *testing.T, mux *http.ServeMux) *feegow.Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return feegow.New(srv.Client(), srv.URL)
}

// captureLog redirects the standard "log" package's output to a buffer for
// the duration of the test, restoring the previous output on cleanup, and
// forces LOG_LEVEL=DEBUG so internal/feegow's per-request log line
// actually fires — otherwise a log-leak test would trivially pass by
// having nothing logged at all.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	t.Setenv("LOG_LEVEL", "DEBUG")
	var buf bytes.Buffer
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	})
	return &buf
}

func intPtr(n int) *int { return &n }
