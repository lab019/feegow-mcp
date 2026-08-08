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

// adminOnlyToolNames is the explicit, hand-maintained set of tools that
// must exist ONLY on the admin profile — declared independently of both
// profiles' actual tools/list results, unlike an earlier version of
// TestAtendimentoToolList_NeverContainsAdminOnlyTools below that computed
// "admin-only" as adminNames-minus-atendimentoNames: that computation is
// impossible to fail by construction, since any tool that leaked into
// atendimento would, by the same subtraction, stop counting as
// "admin-only" and silently drop out of the very set being checked against
// (confirmed by the adversarial review: injecting
// registerGerarSenhaAtendimento into registerAtendimentoTools left that
// version of the test green). This fixed list has no such blind spot — a
// tool named here that also shows up in atendimento's tools/list is
// unambiguously a leak, regardless of what else atendimento does or does
// not expose. Kept in sync with registerAdminOnlyTools
// (internal/mcpserver/tools_admin.go) and the admin-only half of
// TestAdminToolList_HasTheFase4aThrough4cAdminOnlyTools's `want` below.
var adminOnlyToolNames = map[string]bool{
	// Fase 4a
	"buscar_pacientes":             true,
	"obter_paciente":               true,
	"consultar_paciente_clinico":   true,
	"atualizar_paciente":           true,
	"anexar_ao_prontuario":         true,
	"atualizar_status_agendamento": true,
	"gerar_senha_atendimento":      true,
	// Fase 4b
	"consultar_financeiro":        true,
	"gerenciar_conta":             true,
	"gerenciar_voucher":           true,
	"remover_registro_financeiro": true,
	"consultar_estoque":           true,
	"movimentar_estoque":          true,
	// Fase 4c
	"gerenciar_propostas":   true,
	"consultar_laudos":      true,
	"registrar_laudo":       true,
	"gerenciar_faturamento": true,
	"listar_relatorios":     true,
	"gerar_relatorio":       true,
	"listar_funcionarios":   true,
}

// TestAtendimentoToolList_NeverContainsAdminOnlyTools is acceptance
// criterion #9: whatever the atendimento profile's tools/list contains, it
// must never include a tool from adminOnlyToolNames — the fixed,
// independently-declared set of tools that may only exist on the admin
// profile (see its doc comment for why this must NOT be computed as
// "whatever admin has that atendimento doesn't").
func TestAtendimentoToolList_NeverContainsAdminOnlyTools(t *testing.T) {
	atendimentoSrv := httptest.NewServer(Handler())
	defer atendimentoSrv.Close()

	atendimentoNames := listToolNames(t, atendimentoSrv.URL, "fake-token")

	for _, n := range atendimentoNames {
		if adminOnlyToolNames[n] {
			t.Fatalf("atendimento tools/list exposes admin-only tool %q", n)
		}
	}
}

// TestAdminToolList_HasTheFase4aThrough4cAdminOnlyTools is the direct
// acceptance criterion for Fase 4a+4b+4c: the admin profile's tools/list
// contains exactly the nine atendimento tools, plus the seven Fase 4a
// paciente/agenda admin-only tools, plus the six Fase 4b financeiro/estoque
// admin-only tools, plus the seven Fase 4c propostas/laudos/faturamento/
// relatórios/funcionários admin-only tools — no more. Superseded from
// TestAdminToolList_HasTheFase4aAndFase4bAdminOnlyTools, whose own doc
// comment already flagged this exact expansion as coming in Fase 4c.
func TestAdminToolList_HasTheFase4aThrough4cAdminOnlyTools(t *testing.T) {
	srv := httptest.NewServer(AdminHandler())
	defer srv.Close()

	names := listToolNames(t, srv.URL, "fake-admin-token")
	want := map[string]bool{
		// Fase 2/3 atendimento tools, inherited structurally.
		"listar_catalogo":        true,
		"buscar_horarios_livres": true,
		"identificar_paciente":   true,
		"consultar_agenda":       true,
		"agendar":                true,
		"cancelar":               true,
		"remarcar":               true,
		"confirmar":              true,
		"criar_paciente":         true,
		// Fase 4a admin-only tools.
		"buscar_pacientes":             true,
		"obter_paciente":               true,
		"consultar_paciente_clinico":   true,
		"atualizar_paciente":           true,
		"anexar_ao_prontuario":         true,
		"atualizar_status_agendamento": true,
		"gerar_senha_atendimento":      true,
		// Fase 4b admin-only tools.
		"consultar_financeiro":        true,
		"gerenciar_conta":             true,
		"gerenciar_voucher":           true,
		"remover_registro_financeiro": true,
		"consultar_estoque":           true,
		"movimentar_estoque":          true,
		// Fase 4c admin-only tools.
		"gerenciar_propostas":   true,
		"consultar_laudos":      true,
		"registrar_laudo":       true,
		"gerenciar_faturamento": true,
		"listar_relatorios":     true,
		"gerar_relatorio":       true,
		"listar_funcionarios":   true,
	}
	if len(names) != len(want) {
		t.Fatalf("admin tools/list = %v (%d tools), want exactly %d tools", names, len(names), len(want))
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("admin tools/list contains unexpected tool %q (full list: %v)", n, names)
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
