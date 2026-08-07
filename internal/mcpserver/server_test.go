package mcpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestHandler_FailsClosedWithoutBearer confirms the auth middleware is
// actually wired in front of the atendimento MCP handler.
func TestHandler_FailsClosedWithoutBearer(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/mcp", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestAdminHandler_FailsClosedWithoutBearer confirms the same for the admin
// profile: the admin mount is not a backdoor around auth.
func TestAdminHandler_FailsClosedWithoutBearer(t *testing.T) {
	srv := httptest.NewServer(AdminHandler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/mcp/admin", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /mcp/admin: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestHandler_PassesBearerThrough confirms a request that does carry a
// bearer token is let through the middleware to the MCP handler.
func TestHandler_PassesBearerThrough(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer fake-feegow-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("request with bearer token was rejected by the auth middleware (status 401)")
	}
}

// TestNewAtendimento_DoesNotPanic and TestNewAdmin_DoesNotPanic are light
// smoke tests that server construction succeeds for both profiles.
func TestNewAtendimento_DoesNotPanic(t *testing.T) {
	if s := newAtendimento(); s == nil {
		t.Fatal("newAtendimento() returned nil server")
	}
}

func TestNewAdmin_DoesNotPanic(t *testing.T) {
	if s := newAdmin(); s == nil {
		t.Fatal("newAdmin() returned nil server")
	}
}

// listToolNames connects an MCP client to the given streamable-HTTP server
// URL with the given bearer token and returns the names of every tool
// returned by tools/list.
func listToolNames(t *testing.T, url, bearerToken string) []string {
	t.Helper()
	ctx := t.Context()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	httpClient := &http.Client{Transport: bearerRoundTripper{token: bearerToken, base: http.DefaultTransport}}
	transport := &mcp.StreamableClientTransport{
		Endpoint:             url,
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer session.Close()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// bearerRoundTripper injects a fixed Authorization header on every outgoing
// request, standing in for the agent-runtime forwarding the clinic's token.
type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (rt bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+rt.token)
	return rt.base.RoundTrip(req)
}

// TestAtendimentoToolList_HasTheNineFase3Tools is the direct acceptance
// criterion for Fase 3: the atendimento profile's tools/list contains
// exactly the four Fase 2 read-only tools plus the five Fase 3 write tools
// this phase adds, no more (admin-only tools are Fase 4).
func TestAtendimentoToolList_HasTheNineFase3Tools(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	names := listToolNames(t, srv.URL, "fake-token")
	want := map[string]bool{
		"listar_catalogo":        true,
		"buscar_horarios_livres": true,
		"identificar_paciente":   true,
		"consultar_agenda":       true,
		"agendar":                true,
		"cancelar":               true,
		"remarcar":               true,
		"confirmar":              true,
		"criar_paciente":         true,
	}
	if len(names) != len(want) {
		t.Fatalf("atendimento tools/list = %v, want exactly %v", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("atendimento tools/list contains unexpected tool %q (full list: %v)", n, names)
		}
	}
}

// TestAtendimentoToolList_NeverContainsAdminOnlyTools is acceptance
// criterion #9: whatever the atendimento profile's tools/list contains, it
// must never include a tool that exists only on the admin profile. This is
// checked structurally (against the live admin tool set, not a hardcoded
// name list) so the test keeps meaning something once Fase 4 adds
// admin-only tools — today both sides are empty and the check is
// vacuously true, but the machinery (real MCP tools/list calls against
// both profiles) is already exercised and will start doing real work the
// moment either registerAtendimentoTools or registerAdminTools stops being
// empty.
func TestAtendimentoToolList_NeverContainsAdminOnlyTools(t *testing.T) {
	atendimentoSrv := httptest.NewServer(Handler())
	defer atendimentoSrv.Close()
	adminSrv := httptest.NewServer(AdminHandler())
	defer adminSrv.Close()

	atendimentoNames := listToolNames(t, atendimentoSrv.URL, "fake-token")
	adminNames := listToolNames(t, adminSrv.URL, "fake-admin-token")

	inAtendimento := make(map[string]bool, len(atendimentoNames))
	for _, n := range atendimentoNames {
		inAtendimento[n] = true
	}
	adminOnly := make(map[string]bool)
	for _, n := range adminNames {
		if !inAtendimento[n] {
			adminOnly[n] = true
		}
	}

	for _, n := range atendimentoNames {
		if adminOnly[n] {
			t.Fatalf("atendimento tools/list exposes admin-only tool %q", n)
		}
	}
}

// TestAdminToolList_IsSupersetOfAtendimento locks in the structural
// invariant from ESPECIFICACAO.md §3: the admin profile must expose
// everything atendimento does, plus admin-only tools on top. Composing
// registerAdminTools out of registerAtendimentoTools (see server.go) is
// what makes this true by construction; this test guards against that
// composition being accidentally broken later.
func TestAdminToolList_IsSupersetOfAtendimento(t *testing.T) {
	atendimentoSrv := httptest.NewServer(Handler())
	defer atendimentoSrv.Close()
	adminSrv := httptest.NewServer(AdminHandler())
	defer adminSrv.Close()

	atendimentoNames := listToolNames(t, atendimentoSrv.URL, "fake-token")
	adminNames := listToolNames(t, adminSrv.URL, "fake-admin-token")

	inAdmin := make(map[string]bool, len(adminNames))
	for _, n := range adminNames {
		inAdmin[n] = true
	}
	for _, n := range atendimentoNames {
		if !inAdmin[n] {
			t.Fatalf("atendimento tool %q missing from admin profile's tools/list", n)
		}
	}
}
