package tools

import (
	"errors"
	"net/http"
	"testing"
)

func TestListarRelatorios_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/reports/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		writeJSON(t, w, http.StatusOK, `[{"id":53,"Ct":"Agenda","Relatorio":"Agendamentos","Arquivo":"schedule-appointments","sysActive":1}]`)
	})
	client := newTestClient(t, mux)

	result, err := ListarRelatorios(ctxWithToken("tok"), client)
	if err != nil {
		t.Fatalf("ListarRelatorios: %v", err)
	}
	list, ok := result.Relatorios.([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("result.Relatorios = %#v, want a one-element array (bare array, EnvelopeNone)", result.Relatorios)
	}
}

func TestGerarRelatorio_RequiresReport(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerarRelatorio(ctxWithToken("tok"), client, GerarRelatorioArgs{})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerarRelatorio error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestGerarRelatorio_NoConfirmacaoField proves gerar_relatorio needs no
// confirmação at all — it has no such field, unlike every write tool in
// this package — since it never mutates a clinic record (see its doc
// comment in relatorios.go).
func TestGerarRelatorio_Success(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/reports/generate", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"reportId":53,"route":"schedule-appointments","reportName":"Agendamentos","columns":false,"filters":false,"data":false}`)
	})
	client := newTestClient(t, mux)

	result, err := GerarRelatorio(ctxWithToken("tok"), client, GerarRelatorioArgs{Report: "schedule-appointments"})
	if err != nil {
		t.Fatalf("GerarRelatorio: %v", err)
	}
	if gotBody["report"] != "schedule-appointments" {
		t.Fatalf("wire body = %+v, want report=schedule-appointments", gotBody)
	}
	obj, ok := result.Relatorio.(map[string]any)
	if !ok || obj["reportId"] != float64(53) {
		t.Fatalf("result.Relatorio = %#v, want the decoded object with reportId", result.Relatorio)
	}
}

// TestGerarRelatorio_UnknownReport_DecodesEmptyArray proves the OTHER real
// success shape the Fase 4c smoke test measured: an unrecognized "report"
// slug answers 200 with a bare EMPTY ARRAY ([]), not the {success,...}
// object — a completely different JSON type for the same 2xx status. A
// fixed-struct decode would break here; this tool decodes into `any`
// specifically so this shape never becomes a decoding error.
func TestGerarRelatorio_UnknownReport_DecodesEmptyArray(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/reports/generate", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `[]`)
	})
	client := newTestClient(t, mux)

	result, err := GerarRelatorio(ctxWithToken("tok"), client, GerarRelatorioArgs{Report: "nao-existe"})
	if err != nil {
		t.Fatalf("GerarRelatorio: %v", err)
	}
	list, ok := result.Relatorio.([]any)
	if !ok || len(list) != 0 {
		t.Fatalf("result.Relatorio = %#v, want an empty array", result.Relatorio)
	}
}

func TestGerarRelatorio_Filtros_ForwardedAlongsideReport(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/reports/generate", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"reportId":1,"route":"x","reportName":"x","columns":false,"filters":false,"data":false}`)
	})
	client := newTestClient(t, mux)

	if _, err := GerarRelatorio(ctxWithToken("tok"), client, GerarRelatorioArgs{
		Report:  "schedule-appointments",
		Filtros: map[string]any{"unidade_id": 1},
	}); err != nil {
		t.Fatalf("GerarRelatorio: %v", err)
	}
	if gotBody["unidade_id"] != float64(1) {
		t.Fatalf("wire body = %+v, want filtros forwarded alongside report", gotBody)
	}
}
