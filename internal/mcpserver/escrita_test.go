package mcpserver

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestAgendar_ToolCall_RejectsUnconfirmedWithoutCallingFeegow is Fase 3's
// acceptance criterion 5 (confirmação explícita), exercised through the
// real MCP call path: an agendar call omitting confirmacao_paciente never
// reaches the fake Feegow (which fails the test if hit). The SDK's schema
// validation already rejects this before the tool handler even runs
// (confirmacao_paciente has no "omitempty", so jsonschema-go marks it
// required) — an even stronger form of the same guard agendar() itself
// also enforces (see TestAgendar_RequiresConfirmacaoPaciente_BeforeAnyFeegowCall
// in internal/tools), so this test accepts either a transport-level error
// or a tool-level one, as long as the fake Feegow is never reached.
func TestAgendar_ToolCall_RejectsUnconfirmedWithoutCallingFeegow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unconfirmed agendar: %s %s", r.Method, r.URL)
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAtendimento(t, client, "fake-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "agendar",
		Arguments: map[string]any{
			"cpf": "11111111111", "data_nascimento": "2000-01-01",
			"local_id": 0, "profissional_id": 1, "especialidade_id": 1,
			"data": "2099-01-01", "horario": "09:00:00", "valor_centavos": 0, "plano": 2,
			// confirmacao_paciente intentionally omitted
		},
	})
	if err == nil && !res.IsError {
		t.Fatalf("CallTool(agendar) without confirmacao_paciente: want an error (transport or tool-level), got success %+v", res)
	}
}

// TestCancelar_ToolCall_CannotCancelAnotherPatientsAgendamento is the
// end-to-end version of the posse (ownership) check: through the real MCP
// call path, an agendamento_id that belongs to a DIFFERENT patient than the
// one the two facts identify must fail, and /appoints/cancel-appoint must
// never be reached.
func TestCancelar_ToolCall_CannotCancelAnotherPatientsAgendamento(t *testing.T) {
	const ownerPatientID = 777
	const callerPatientID = 1
	const agendamentoID = 55

	var cancelHit bool
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"content":[{"patient_id":` + strconv.Itoa(callerPatientID) +
			`,"nome":"X","nascimento":"2000-01-01"}],"total":1}`))
	})
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("agendamento_id") != strconv.Itoa(agendamentoID) {
			w.Write([]byte(`{"success":true,"content":[]}`))
			return
		}
		w.Write([]byte(`{"success":true,"content":[{"agendamento_id":` + strconv.Itoa(agendamentoID) +
			`,"paciente_id":` + strconv.Itoa(ownerPatientID) +
			`,"data":"07-08-2026","horario":"09:00:00","profissional_id":1,"especialidade_id":1,` +
			`"procedimento_id":1,"unidade_id":1,"status_id":1}]}`))
	})
	mux.HandleFunc("/appoints/cancel-appoint", func(w http.ResponseWriter, r *http.Request) {
		cancelHit = true
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"content":"Agendamento cancelado"}`))
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAtendimento(t, client, "fake-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "cancelar",
		Arguments: map[string]any{
			"cpf": "11111111111", "data_nascimento": "2000-01-01",
			"agendamento_id": agendamentoID, "confirmacao_paciente": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(cancelar): %v", err)
	}
	if !res.IsError {
		t.Fatalf("CallTool(cancelar) for an agendamento_id belonging to a different patient: want a tool error, got %+v", res)
	}
	if cancelHit {
		t.Fatal("cancelar reached /appoints/cancel-appoint for an agendamento_id that does not belong to the identified patient")
	}
}

// TestCriarPaciente_ToolCall_NeverReturnsPII mirrors
// TestIdentificarPaciente_ToolCall_NeverReturnsPII for the create path,
// through the real MCP call path (structured content is what actually
// reaches an agent's context).
func TestCriarPaciente_ToolCall_NeverReturnsPII(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"content":[],"total":0}`))
	})
	mux.HandleFunc("/patient/create", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"content":321}`))
	})
	client := fakeFeegow(t, mux)
	session, ctx := connectAtendimento(t, client, "fake-token")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "criar_paciente",
		Arguments: map[string]any{
			"nome_completo": "Fulano de Tal", "cpf": "11122233344", "data_nascimento": "1990-05-10",
			"confirmacao_paciente": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(criar_paciente): %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool(criar_paciente) returned a tool error: %+v", res.Content)
	}
	structured, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling StructuredContent: %v", err)
	}
	s := string(structured)
	for _, needle := range []string{"Fulano", "Tal", "11122233344", "1990-05-10"} {
		if strings.Contains(s, needle) {
			t.Fatalf("CallTool(criar_paciente) StructuredContent leaked PII %q: %s", needle, s)
		}
	}
}
