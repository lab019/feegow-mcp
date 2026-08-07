package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lab019/feegow-mcp/internal/auth"
)

type probeArgs struct{}

type probeResult struct {
	Token string `json:"token"`
}

// newProbeServer builds a minimal *mcp.Server carrying one tool that
// reports whatever bearer token auth.TokenFromContext resolves to for the
// specific call that invoked it. It exists only in this test.
//
// Production tool registration (registerAtendimentoTools/
// registerAdminTools) is empty in this phase, but the session/context-reuse
// risk this test guards against lives entirely in newStreamableHandler,
// independent of which tools are registered on top of it — a throwaway
// probe tool exercises that risk today, without waiting for Fase 2's real
// Feegow-calling tools to land.
func newProbeServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "probe", Version: "0.0.1"}, nil)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "probe_token",
		Description: "test-only: reports the bearer token visible in this call's context",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ probeArgs) (*mcp.CallToolResult, probeResult, error) {
		tok, _ := auth.TokenFromContext(ctx)
		return nil, probeResult{Token: tok}, nil
	})
	return s
}

// tokenSwappingRoundTripper lets the test flip which bearer token rides on
// outgoing requests mid-session.
type tokenSwappingRoundTripper struct {
	mu    sync.Mutex
	token string
	base  http.RoundTripper
}

func (rt *tokenSwappingRoundTripper) setToken(tok string) {
	rt.mu.Lock()
	rt.token = tok
	rt.mu.Unlock()
}

func (rt *tokenSwappingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	tok := rt.token
	rt.mu.Unlock()
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+tok)
	return rt.base.RoundTrip(req)
}

func callProbe(t *testing.T, ctx context.Context, session *mcp.ClientSession) string {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "probe_token"})
	if err != nil {
		t.Fatalf("CallTool(probe_token): %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool(probe_token) returned a tool error: %+v", res.Content)
	}
	structured, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling structured content: %v", err)
	}
	var out probeResult
	if err := json.Unmarshal(structured, &out); err != nil {
		t.Fatalf("unmarshaling probe result: %v", err)
	}
	return out.Token
}

// TestNewStreamableHandler_ToolCallsUseTheirOwnRequestToken is the
// load-bearing regression test for the stateful/stateless session bug,
// mirroring agent-mcp-google's token_isolation_test: it proves that two
// independent tool-call HTTP requests on the *same* MCP session (same
// Mcp-Session-Id), carrying two different bearer tokens, each resolve
// auth.TokenFromContext inside the tool handler to their own request's
// token — never a token left over from a previous request on that
// session.
//
// This is the exact failure mode ESPECIFICACAO.md §2 calls the central
// security guarantee of this stateless service: "nada de uma requisição
// sobrevive para a seguinte". With the SDK's default *stateful* mode
// (Stateless: false), a session is connected once — on the first request —
// and that request's context is frozen for the session's lifetime, so a
// second call's token would be silently ignored and the tool would see the
// *first* call's token twice. newStreamableHandler forces Stateless: true
// specifically to avoid that; this test fails without it and passes with
// it.
func TestNewStreamableHandler_ToolCallsUseTheirOwnRequestToken(t *testing.T) {
	h := newStreamableHandler(newProbeServer())
	srv := httptest.NewServer(h)
	defer srv.Close()

	rt := &tokenSwappingRoundTripper{token: "clinic-AAA", base: http.DefaultTransport}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             srv.URL,
		HTTPClient:           &http.Client{Transport: rt},
		DisableStandaloneSSE: true,
	}

	ctx := t.Context()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Connect (token clinic-AAA): %v", err)
	}
	defer session.Close()

	got1 := callProbe(t, ctx, session)
	if got1 != "clinic-AAA" {
		t.Fatalf("tool call #1 saw token %q, want %q", got1, "clinic-AAA")
	}

	// Same MCP session (same Mcp-Session-Id, handled automatically by the
	// client transport), but this HTTP request carries a different bearer
	// token — e.g. the runtime rotated an expired token, or (worse) this
	// session ID collided with a different clinic's session.
	rt.setToken("clinic-BBB")
	got2 := callProbe(t, ctx, session)
	if got2 != "clinic-BBB" {
		t.Fatalf("tool call #2 saw token %q, want %q (got the previous request's token instead: cross-request/cross-tenant token leak)", got2, "clinic-BBB")
	}
}
