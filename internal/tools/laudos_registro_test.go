package tools

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

func TestRegistrarLaudo_RequiresConfirmacao_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unconfirmed registro: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := RegistrarLaudo(ctxWithToken("tok"), client, RegistrarLaudoArgs{
		AgendamentoID: intPtr(1), LaudoBase64: "aGVsbG8=",
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("RegistrarLaudo error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestRegistrarLaudo_RequiresAgendamentoIDAndLaudoBase64(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	for _, args := range []RegistrarLaudoArgs{
		{Confirmacao: true, LaudoBase64: "aGVsbG8="},
		{Confirmacao: true, AgendamentoID: intPtr(1)},
		{Confirmacao: true, AgendamentoID: intPtr(0), LaudoBase64: "aGVsbG8="},
	} {
		_, err := RegistrarLaudo(ctxWithToken("tok"), client, args)
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("RegistrarLaudo(%+v) error = %v (%T), want *ArgumentError", args, err, err)
		}
	}
}

// TestRegistrarLaudo_Success proves a confirmed call reaches
// /medical-reports/create with agendamento_id/laudo_base64, and that
// {"success":true,...} is treated as success.
func TestRegistrarLaudo_Success(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/medical-reports/create", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"message":"Laudo registrado"}`)
	})
	client := newTestClient(t, mux)

	result, err := RegistrarLaudo(ctxWithToken("tok"), client, RegistrarLaudoArgs{
		Confirmacao: true, AgendamentoID: intPtr(3), LaudoBase64: "aGVsbG8=",
	})
	if err != nil {
		t.Fatalf("RegistrarLaudo: %v", err)
	}
	if !result.Registrado {
		t.Fatalf("result = %+v, want Registrado=true", result)
	}
	if gotBody["agendamento_id"] != float64(3) || gotBody["laudo_base64"] != "aGVsbG8=" {
		t.Fatalf("wire body = %+v, want agendamento_id/laudo_base64", gotBody)
	}
}

// TestRegistrarLaudo_ServerRejection_SurfacesError proves a
// {"success":false,...} 200 body — the pattern medical_reports.create's own
// Notes document (an internal Feegow error for a nonexistent
// agendamento_id) — is treated as a failure, not a silent success.
func TestRegistrarLaudo_ServerRejection_SurfacesError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/medical-reports/create", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":false,"message":"Trying to access array offset on value of type bool"}`)
	})
	client := newTestClient(t, mux)

	_, err := RegistrarLaudo(ctxWithToken("tok"), client, RegistrarLaudoArgs{
		Confirmacao: true, AgendamentoID: intPtr(1), LaudoBase64: "aGVsbG8=",
	})
	if !errors.Is(err, ErrOperacaoNaoConfirmadaFeegow) {
		t.Fatalf("RegistrarLaudo error = %v, want ErrOperacaoNaoConfirmadaFeegow", err)
	}
}

func TestRegistrarLaudo_AuditsWrite(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/medical-reports/create", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"message":"ok"}`)
	})
	client := newTestClient(t, mux)

	if _, err := RegistrarLaudo(ctxWithToken("tok"), client, RegistrarLaudoArgs{
		Confirmacao: true, AgendamentoID: intPtr(3), LaudoBase64: "aGVsbG8=",
	}); err != nil {
		t.Fatalf("RegistrarLaudo: %v", err)
	}
	if !strings.Contains(buf.String(), "ADMIN WRITE registrar_laudo — agendamento_id=3") {
		t.Fatalf("write not audited with agendamento_id: %s", buf.String())
	}
}

// TestRegistrarLaudo_NoLaudoContentInLogs proves neither the request's
// laudo_base64 nor Feegow's response body is ever logged — "laudo é dado
// clínico", per this fase's own instruction.
func TestRegistrarLaudo_NoLaudoContentInLogs(t *testing.T) {
	marker := "Q09OVEVVRE8tQ0xJTklDTy1TRUNSRVRP" // a distinctive base64 marker
	mux := http.NewServeMux()
	mux.HandleFunc("/medical-reports/create", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"message":"ok"}`)
	})
	assertNoPIIInLog(t, mux, []string{marker}, func(client *feegow.Client) error {
		_, err := RegistrarLaudo(ctxWithToken("tok"), client, RegistrarLaudoArgs{
			Confirmacao: true, AgendamentoID: intPtr(3), LaudoBase64: marker,
		})
		return err
	})
}
