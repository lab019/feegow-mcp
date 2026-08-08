package tools

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

func TestConsultarLaudos_Listar_AlwaysUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for acao=listar: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarLaudos(ctxWithToken("tok"), client, ConsultarLaudosArgs{Acao: "listar"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarLaudos error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestConsultarLaudos_UnknownAcao_Rejects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarLaudos(ctxWithToken("tok"), client, ConsultarLaudosArgs{Acao: "apagar"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarLaudos error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestConsultarLaudos_ObterArquivo_RequiresLabReportID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarLaudos(ctxWithToken("tok"), client, ConsultarLaudosArgs{Acao: "obter_arquivo"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarLaudos error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestConsultarLaudos_ObterArquivo_Success(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/medical-reports/get-labs-report-file", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"status":3,"msg":"Arquivo não existe"},"total":2}`)
	})
	client := newTestClient(t, mux)

	result, err := ConsultarLaudos(ctxWithToken("tok"), client, ConsultarLaudosArgs{
		Acao: "obter_arquivo", LabReportID: intPtr(1),
	})
	if err != nil {
		t.Fatalf("ConsultarLaudos: %v", err)
	}
	if gotQuery.Get("lab_report_id") != "1" {
		t.Fatalf("query = %v, want lab_report_id=1", gotQuery)
	}
	if result.Itens == nil {
		t.Fatalf("result.Itens is nil, want the decoded content")
	}
}

func TestConsultarLaudos_Visualizar_RequiresAgendamentoID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarLaudos(ctxWithToken("tok"), client, ConsultarLaudosArgs{Acao: "visualizar"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarLaudos error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestConsultarLaudos_Visualizar_NestedValidation422_SurfacesFieldNames
// proves the THIRD 422 format (nested under "message") this fase's
// classify422 fix handles reaches consultar_laudos as an ordinary
// *ArgumentError-shaped experience: SanitizeFeegowError still folds a
// *ValidationError into the generic sentinel — this test locks in that the
// call fails cleanly (no crash, no leaked body) rather than falling into
// UnexpectedStatusError the way it would have before the classify422 fix.
func TestConsultarLaudos_Visualizar_NestedValidation422_Sanitized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/medical-reports/search", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnprocessableEntity,
			`{"success":false,"message":{"agendamento_id":["O campo agendamento id é obrigatório."]}}`)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarLaudos(ctxWithToken("tok"), client, ConsultarLaudosArgs{
		Acao: "visualizar", AgendamentoID: intPtr(1),
	})
	if !errors.Is(err, ErrEntradaInvalidaFeegow) {
		t.Fatalf("ConsultarLaudos error = %v, want ErrEntradaInvalidaFeegow (sanitized ValidationError)", err)
	}
}

// TestConsultarLaudos_Visualizar_409NaoEncontrado_Sanitized proves the real
// 409 the Fase 4c smoke test measured (agendamento sem laudo associado,
// with the unusual capitalized "Message" field) is surfaced as the generic
// sanitized conflict, never the raw Feegow body.
func TestConsultarLaudos_Visualizar_409NaoEncontrado_Sanitized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/medical-reports/search", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusConflict, `{"success":false,"Message":"Não foi encontrado resultado para este laudo"}`)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarLaudos(ctxWithToken("tok"), client, ConsultarLaudosArgs{
		Acao: "visualizar", AgendamentoID: intPtr(1),
	})
	if !errors.Is(err, ErrConflitoFeegow) {
		t.Fatalf("ConsultarLaudos error = %v, want ErrConflitoFeegow (sanitized ConflictError)", err)
	}
}

// TestConsultarLaudos_NoPIIInLogs proves neither acao logs laudo content —
// "laudo é dado clínico", per this fase's own instruction.
func TestConsultarLaudos_NoPIIInLogs(t *testing.T) {
	marker := "CONTEUDO-CLINICO-SECRETO"
	mux := http.NewServeMux()
	mux.HandleFunc("/medical-reports/get-labs-report-file", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"laudo":"`+marker+`"},"total":1}`)
	})
	assertNoPIIInLog(t, mux, []string{marker}, func(client *feegow.Client) error {
		_, err := ConsultarLaudos(ctxWithToken("tok"), client, ConsultarLaudosArgs{
			Acao: "obter_arquivo", LabReportID: intPtr(1),
		})
		return err
	})
}
