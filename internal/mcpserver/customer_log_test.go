package mcpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lab019/feegow-mcp/internal/customerlog"
)

// LIC-3782: each tool call leaves ONE customer line (org of the runtime's X-Org-Id, tool name, status, duration,
// conversation), never the arguments or the result. These tests go through the real handler over HTTP, with a fake
// Feegow whose answer the test sets.

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) raw() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *safeBuffer) lines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(s.raw()), "\n") {
		if raw == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("line is not JSON: %q", raw)
		}
		out = append(out, m)
	}
	return out
}

type logHarness struct {
	t      *testing.T
	url    string
	logs   *safeBuffer
	feegow func(status int, body string)
}

func newLogHarness(t *testing.T) *logHarness {
	t.Helper()
	logs := &safeBuffer{}
	t.Cleanup(customerlog.SetWriter(logs))
	var mu sync.Mutex
	status, body := http.StatusOK, `{"success":true,"content":[]}`
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		st, bd := status, body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(st)
		_, _ = w.Write([]byte(bd))
	})
	srv := httptest.NewServer(newStreamableHandler(newAtendimentoWithClient(fakeFeegow(t, mux))))
	t.Cleanup(srv.Close)
	return &logHarness{t: t, url: srv.URL, logs: logs, feegow: func(st int, bd string) {
		mu.Lock()
		status, body = st, bd
		mu.Unlock()
	}}
}

func (h *logHarness) call(headers map[string]string, tool string, args map[string]any) {
	h.t.Helper()
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req = req.Clone(req.Context())
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return http.DefaultTransport.RoundTrip(req)
	})}
	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := c.Connect(h.t.Context(), &mcp.StreamableClientTransport{Endpoint: h.url, HTTPClient: client, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return // refused at the edge: its (internal) line is what is under test
	}
	defer session.Close()
	_, _ = session.CallTool(h.t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func logBearer(org, conv string) map[string]string {
	return map[string]string{"Authorization": "Bearer feegow.live-token", customerlog.OrgHeader: org, customerlog.ConversationHeader: conv}
}

var catalogo = map[string]any{"tipo": "especialidades"}

// Criteria 1 and 3: one line per call in the org the runtime named, with tool, status, duration and conversation; a
// Feegow failure is an ERROR line carrying a code, never Feegow's text.
func TestCustomerLog_OneLinePerCallInItsOrgAndAnErrorIsALevelErrorLine(t *testing.T) {
	h := newLogHarness(t)
	h.call(logBearer("orgA", "conv-a"), "listar_catalogo", catalogo)
	h.feegow(http.StatusInternalServerError, `{"detail":"boom for maria@example.com"}`)
	h.call(logBearer("orgB", "conv-b"), "listar_catalogo", catalogo)

	lines := h.logs.lines(t)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %s", len(lines), h.logs.raw())
	}
	a, b := lines[0], lines[1]
	for k, want := range map[string]any{"audience": "org", "org_id": "orgA", "kind": "tool", "operation": "listar_catalogo", "status": "ok", "level": "INFO", "group_key": "conv-a", "origin": "feegow-mcp"} {
		if a[k] != want {
			t.Errorf("ok line %s = %v, want %v", k, a[k], want)
		}
	}
	if d, ok := a["duration_ms"].(float64); !ok || d < 0 {
		t.Errorf("duration_ms = %v, want a non-negative number", a["duration_ms"])
	}
	for k, want := range map[string]any{"audience": "org", "org_id": "orgB", "status": "erro", "level": "ERROR", "code": "tool_error", "group_key": "conv-b", "message": "tool listar_catalogo failed (tool_error)"} {
		if b[k] != want {
			t.Errorf("error line %s = %v, want %v", k, b[k], want)
		}
	}
}

// Criterion 2: Feegow's refusals — 401/403 (credential), 429 (quota) — leave auth_failed / quota_exceeded lines. The
// refusal with no credential at all is INTERNAL (its X-Org-Id is unproven: it must not write into an org's logs).
func TestCustomerLog_FeegowAuthAndQuotaRefusalsLeaveTheirOwnCode(t *testing.T) {
	h := newLogHarness(t)
	for _, st := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		h.feegow(st, `{"message":"clinic data maria@example.com"}`)
		h.call(logBearer("orgA", "conv-a"), "listar_catalogo", catalogo)
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, h.url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"listar_catalogo"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(customerlog.OrgHeader, "orgA")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST without bearer: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	lines := h.logs.lines(t)
	var codes []string
	for i, l := range lines {
		if l["status"] != "erro" {
			t.Errorf("line %v is not an error line", l)
		}
		_, labelled := l["org_id"]
		if wantOrg := i < len(lines)-1; labelled != wantOrg {
			t.Errorf("line %d (%v): org label = %v, want %v", i, l["operation"], labelled, wantOrg)
		}
		codes = append(codes, l["code"].(string))
	}
	if got, want := strings.Join(codes, ","), "auth_failed,auth_failed,quota_exceeded,auth_failed"; got != want {
		t.Fatalf("codes = %s, want %s", got, want)
	}
	if last := lines[len(lines)-1]; last["audience"] != "internal" || last["operation"] != "mcp_request" {
		t.Errorf("no-bearer line = %v, want internal mcp_request", last)
	}
}

// Criterion 4: arguments, Feegow's answer, the token and an org/conversation/tool name that is not id-shaped never
// reach a line; an unreadable org leaves an INTERNAL line with no org_id to match a selector.
func TestCustomerLog_NoArgumentResultTokenOrHostileValueReachesTheLine(t *testing.T) {
	h := newLogHarness(t)
	h.feegow(http.StatusOK, `{"success":true,"content":[{"nome":"joao@example.com +55 11 99999-0000"}]}`)
	h.call(logBearer("orgA", "conv-a"), "identificar_paciente", map[string]any{"cpf": "123.456.789-09", "data_nascimento": "1990-01-02"})
	h.call(logBearer(`orgA"} or {audience="org"`, `c"} or {x="1`), "listar_catalogo", catalogo)
	h.call(logBearer("orgA", "conv-a"), "x; rm -rf /", nil)

	out := h.logs.raw()
	for _, secret := range []string{"123.456.789-09", "1990-01-02", "joao@example.com", "99999-0000", "feegow.live-token", "rm -rf", `or {audience`, "especialidades"} {
		if strings.Contains(out, secret) {
			t.Errorf("line leaks %q: %s", secret, out)
		}
	}
	lines := h.logs.lines(t)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %s", len(lines), out)
	}
	if lines[1]["audience"] != "internal" || lines[1]["group_key"] != nil {
		t.Errorf("hostile org/conversation line = %v, want internal with no group", lines[1])
	}
	if _, has := lines[1]["org_id"]; has {
		t.Errorf("hostile org became a label: %v", lines[1])
	}
	if lines[2]["operation"] != "unknown_tool" {
		t.Errorf("unknown tool operation = %v, want unknown_tool", lines[2]["operation"])
	}
}
