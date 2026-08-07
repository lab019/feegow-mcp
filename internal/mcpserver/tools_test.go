package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// fakeFeegow stands up mux as a fake Feegow host and returns a *feegow.Client
// pointed at it.
func fakeFeegow(t *testing.T, mux *http.ServeMux) *feegow.Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return feegow.New(srv.Client(), srv.URL)
}

// connectAtendimento wires client into the real atendimento MCP server
// (registerAtendimentoTools, same as production), serves it over
// streamable-HTTP behind the real fail-closed auth middleware, and returns
// a connected client session — the full request path this service actually
// runs in production, minus the network host.
func connectAtendimento(t *testing.T, client *feegow.Client, bearerToken string) (*mcp.ClientSession, context.Context) {
	t.Helper()
	srv := httptest.NewServer(newStreamableHandler(newAtendimentoWithClient(client)))
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

// TestListarCatalogo_ToolCall_RoundTrips is an end-to-end sanity check: a
// real tools/call for listar_catalogo, through the SDK, the auth
// middleware and the tool wiring in tools.go, returns the fake Feegow's
// content.
func TestListarCatalogo_ToolCall_RoundTrips(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/specialties/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"content":[{"especialidade_id":1,"nome":"Psicólogo"}]}`))
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAtendimento(t, client, "fake-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "listar_catalogo",
		Arguments: map[string]any{"tipo": "especialidades"},
	})
	if err != nil {
		t.Fatalf("CallTool(listar_catalogo): %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool(listar_catalogo) returned a tool error: %+v", res.Content)
	}
	structured, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling StructuredContent: %v", err)
	}
	if !strings.Contains(string(structured), "Psicólogo") {
		t.Fatalf("StructuredContent = %s, want it to contain the fake Feegow's content", structured)
	}
}

// TestIdentificarPaciente_ToolCall_RejectsSingleFactWithoutCallingFeegow is
// acceptance criterion 2, exercised through the real MCP call path: a
// single-fact identificar_paciente call surfaces as a tool error, and the
// fake Feegow (which fails the test if hit) is never contacted.
func TestIdentificarPaciente_ToolCall_RejectsSingleFactWithoutCallingFeegow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a single-fact identificar_paciente: %s %s", r.Method, r.URL)
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAtendimento(t, client, "fake-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "identificar_paciente",
		Arguments: map[string]any{"cpf": "11111111111"},
	})
	if err != nil {
		t.Fatalf("CallTool(identificar_paciente): %v", err)
	}
	if !res.IsError {
		t.Fatalf("CallTool(identificar_paciente) with a single fact: want a tool error, got %+v", res)
	}
}

// TestIdentificarPaciente_ToolCall_NeverReturnsPII is acceptance criterion
// 3, exercised through the real MCP call path (structured content is what
// actually reaches an agent's context).
func TestIdentificarPaciente_ToolCall_NeverReturnsPII(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"content":[{"patient_id":42,"nome":"Fulano de Tal","nascimento":"1990-05-10","documentos":{"cpf":"11122233344"}}],"total":1}`))
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAtendimento(t, client, "fake-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "identificar_paciente",
		Arguments: map[string]any{"cpf": "11122233344", "data_nascimento": "1990-05-10"},
	})
	if err != nil {
		t.Fatalf("CallTool(identificar_paciente): %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool(identificar_paciente) returned a tool error: %+v", res.Content)
	}

	full, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshaling CallToolResult: %v", err)
	}
	for _, needle := range []string{"Fulano", "Tal", "11122233344", "1990-05-10", "10-05-1990"} {
		if strings.Contains(string(full), needle) {
			t.Fatalf("tool call result leaked PII %q: %s", needle, full)
		}
	}
}

// TestConsultarAgenda_SchemaNeverAcceptsRawPacienteID is acceptance
// criterion 5: consultar_agenda's advertised input schema must not contain
// a paciente_id property under any name a caller could pass a raw id
// through.
func TestConsultarAgenda_SchemaNeverAcceptsRawPacienteID(t *testing.T) {
	client := fakeFeegow(t, http.NewServeMux())
	session, ctx := connectAtendimento(t, client, "fake-token")

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range res.Tools {
		if tool.Name != "consultar_agenda" {
			continue
		}
		schemaJSON, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshaling consultar_agenda's input schema: %v", err)
		}
		if strings.Contains(string(schemaJSON), "paciente_id") {
			t.Fatalf("consultar_agenda input schema exposes paciente_id: %s", schemaJSON)
		}
		return
	}
	t.Fatal("consultar_agenda not found in tools/list")
}

// TestAtendimentoToolCalls_NoPatientPayloadInLogs is acceptance criterion 8,
// exercised across the whole request path (SDK dispatch + auth middleware +
// tool + feegow.Client), not just the internal/tools functions directly.
func TestAtendimentoToolCalls_NoPatientPayloadInLogs(t *testing.T) {
	t.Setenv("LOG_LEVEL", "DEBUG")
	var buf bytes.Buffer
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"content":[{"patient_id":9,"nome":"Segredo Pessoal","nascimento":"2000-02-20"}],"total":1}`))
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAtendimento(t, client, "fake-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "identificar_paciente",
		Arguments: map[string]any{"cpf": "22233344455", "data_nascimento": "2000-02-20"},
	})
	if err != nil {
		t.Fatalf("CallTool(identificar_paciente): %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool(identificar_paciente) returned a tool error: %+v", res.Content)
	}

	logged := buf.String()
	for _, needle := range []string{"Segredo Pessoal", "22233344455", "2000-02-20", "20-02-2000"} {
		if strings.Contains(logged, needle) {
			t.Fatalf("log output leaked patient data %q: %s", needle, logged)
		}
	}
}
