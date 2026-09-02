package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// connectAdmin is connectAtendimento's admin-profile counterpart: wires
// client into the real admin MCP server (registerAdminTools, same as
// production), serving it over streamable-HTTP behind the real
// fail-closed auth middleware, and returns a connected client session.
func connectAdmin(t *testing.T, client *feegow.Client, bearerToken string) (*mcp.ClientSession, context.Context) {
	t.Helper()
	s := mcp.NewServer(&mcp.Implementation{Name: adminServerName, Version: serverVersion()}, nil)
	registerAdminTools(s, client)
	srv := httptest.NewServer(newStreamableHandler(s))
	t.Cleanup(srv.Close)

	ctx := t.Context()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	httpClient := &http.Client{Transport: bearerRoundTripper{token: bearerToken, base: http.DefaultTransport}}
	transport := &mcp.StreamableClientTransport{
		Endpoint:             srv.URL,
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session, ctx
}

// TestBuscarPacientes_ToolCall_RoundTrips is an end-to-end sanity check: a
// real tools/call for buscar_pacientes, through the SDK, the auth
// middleware and the tool wiring in tools_admin.go, returns the fake
// Feegow's content — WITHOUT requiring the atendimento profile's two-fact
// identification.
func TestBuscarPacientes_ToolCall_RoundTrips(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"content":[{"patient_id":42,"nome":"Fulano de Tal","nascimento":"1990-05-10"}],"total":1}`))
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAdmin(t, client, "fake-admin-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "buscar_pacientes",
		Arguments: map[string]any{"cpf": "11122233344"},
	})
	if err != nil {
		t.Fatalf("CallTool(buscar_pacientes): %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool(buscar_pacientes) returned a tool error: %+v", res.Content)
	}
	structured, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling StructuredContent: %v", err)
	}
	if !strings.Contains(string(structured), "Fulano de Tal") {
		t.Fatalf("StructuredContent = %s, want it to contain the fake Feegow's PII — the admin profile does not minimize patient data", structured)
	}
}

// TestAtualizarPaciente_ToolCall_RejectsUnconfirmedWithoutCallingFeegow
// proves atualizar_paciente's confirmacao guard is enforced through the
// real MCP call path: an unconfirmed call never reaches the fake Feegow.
func TestAtualizarPaciente_ToolCall_RejectsUnconfirmedWithoutCallingFeegow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unconfirmed atualizar_paciente: %s %s", r.Method, r.URL)
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAdmin(t, client, "fake-admin-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "atualizar_paciente",
		Arguments: map[string]any{
			"paciente_id": 1, "nome_completo": "Novo Nome",
			// confirmacao intentionally omitted
		},
	})
	if err == nil && !res.IsError {
		t.Fatalf("CallTool(atualizar_paciente) without confirmacao: want an error (transport or tool-level), got success %+v", res)
	}
}

// TestAtualizarStatusAgendamento_ToolCall_RejectsUnconfirmedWithoutCallingFeegow
// mirrors the above for atualizar_status_agendamento.
func TestAtualizarStatusAgendamento_ToolCall_RejectsUnconfirmedWithoutCallingFeegow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unconfirmed atualizar_status_agendamento: %s %s", r.Method, r.URL)
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAdmin(t, client, "fake-admin-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "atualizar_status_agendamento",
		Arguments: map[string]any{
			"agendamento_id": 100, "status_id": 7,
			// confirmacao intentionally omitted
		},
	})
	if err == nil && !res.IsError {
		t.Fatalf("CallTool(atualizar_status_agendamento) without confirmacao: want an error (transport or tool-level), got success %+v", res)
	}
}

// TestGerarSenhaAtendimento_ToolCall_RoundTrips proves the tool decodes the
// real API's "sucess"-typo body through the full MCP call path.
func TestGerarSenhaAtendimento_ToolCall_RoundTrips(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/queue-position", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"sucess":true,"content":{"posicao":1,"tipoSenha":1,"tipoFormatado":"P"}}`))
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAdmin(t, client, "fake-admin-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "gerar_senha_atendimento",
		Arguments: map[string]any{"unidade_id": 0, "tipo_senha": 1},
	})
	if err != nil {
		t.Fatalf("CallTool(gerar_senha_atendimento): %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool(gerar_senha_atendimento) returned a tool error: %+v", res.Content)
	}
	structured, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling StructuredContent: %v", err)
	}
	if !strings.Contains(string(structured), `"posicao":1`) {
		t.Fatalf("StructuredContent = %s, want the decoded posicao", structured)
	}
}

// TestConsultarFinanceiro_ToolCall_RoundTrips is an end-to-end sanity check
// for the Fase 4b financeiro/estoque surface: a real tools/call for
// consultar_financeiro, through the SDK, the auth middleware and the tool
// wiring in tools_admin.go, returns the fake Feegow's content.
func TestConsultarFinanceiro_ToolCall_RoundTrips(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/credit-card-flags", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"content":[{"id":1,"Bandeira":"Visa"}]}`))
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAdmin(t, client, "fake-admin-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "consultar_financeiro",
		Arguments: map[string]any{"tipo": "bandeiras"},
	})
	if err != nil {
		t.Fatalf("CallTool(consultar_financeiro): %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool(consultar_financeiro) returned a tool error: %+v", res.Content)
	}
	structured, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling StructuredContent: %v", err)
	}
	if !strings.Contains(string(structured), "Visa") {
		t.Fatalf("StructuredContent = %s, want it to contain the fake Feegow's content", structured)
	}
}

// TestRemoverRegistroFinanceiro_ToolCall_RejectsSingleConfirmacaoWithoutCallingFeegow
// proves the reinforced-confirmation guard is enforced through the real MCP
// call path: confirmacao=true ALONE (without ciente_irreversivel=true) must
// never reach the fake Feegow — this tool's whole reason for existing
// separately from gerenciar_conta.
func TestRemoverRegistroFinanceiro_ToolCall_RejectsSingleConfirmacaoWithoutCallingFeegow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a single-confirmed remover_registro_financeiro: %s %s", r.Method, r.URL)
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAdmin(t, client, "fake-admin-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "remover_registro_financeiro",
		Arguments: map[string]any{
			"tipo": "fatura", "invoice_id": 1, "confirmacao": true,
			// ciente_irreversivel intentionally omitted
		},
	})
	if err == nil && !res.IsError {
		t.Fatalf("CallTool(remover_registro_financeiro) with only confirmacao=true: want an error (transport or tool-level), got success %+v", res)
	}
}

// TestMovimentarEstoque_ToolCall_DeadHostAcao_RejectsWithoutCallingFeegow
// proves acao=entrada (and its siblings saida/movimentacao) never reach the
// fake Feegow through the real MCP call path — the dead-host refusal is
// wired end-to-end, not just at the internal/tools layer.
func TestMovimentarEstoque_ToolCall_DeadHostAcao_RejectsWithoutCallingFeegow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for acao=entrada: %s %s", r.Method, r.URL)
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAdmin(t, client, "fake-admin-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "movimentar_estoque",
		Arguments: map[string]any{"acao": "entrada", "confirmacao": true},
	})
	if err == nil && !res.IsError {
		t.Fatalf("CallTool(movimentar_estoque, acao=entrada): want an error, got success %+v", res)
	}
}
