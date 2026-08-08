package tools

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestRemoverRegistroFinanceiro_RequiresBothConfirmacoes_BeforeAnyFeegowCall
// is the direct proof of this tool's reinforced-confirmation guard: a
// single confirmacao=true, WITHOUT ciente_irreversivel=true, must never
// reach Feegow — unlike every ordinary admin write tool in this package,
// which only requires the one boolean.
func TestRemoverRegistroFinanceiro_RequiresBothConfirmacoes_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	cases := []RemoverRegistroFinanceiroArgs{
		{Tipo: "fatura", InvoiceID: intPtr(1)},                           // neither confirmation
		{Tipo: "fatura", InvoiceID: intPtr(1), Confirmacao: true},        // only the first
		{Tipo: "fatura", InvoiceID: intPtr(1), CienteIrreversivel: true}, // only the second
	}
	for i, args := range cases {
		_, err := RemoverRegistroFinanceiro(ctxWithToken("tok"), client, args)
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("case %d: RemoverRegistroFinanceiro error = %v (%T), want *ArgumentError", i, err, err)
		}
	}
}

func TestRemoverRegistroFinanceiro_UnknownTipo_Rejects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unrecognized tipo: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := RemoverRegistroFinanceiro(ctxWithToken("tok"), client, RemoverRegistroFinanceiroArgs{
		Tipo: "nao_existe", Confirmacao: true, CienteIrreversivel: true,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("RemoverRegistroFinanceiro error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestRemoverRegistroFinanceiro_Fatura_RequiresPositiveInvoiceID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a missing/invalid invoice_id: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	for _, args := range []RemoverRegistroFinanceiroArgs{
		{Tipo: "fatura", Confirmacao: true, CienteIrreversivel: true},
		{Tipo: "fatura", Confirmacao: true, CienteIrreversivel: true, InvoiceID: intPtr(0)},
		{Tipo: "fatura", Confirmacao: true, CienteIrreversivel: true, InvoiceID: intPtr(-1)},
	} {
		_, err := RemoverRegistroFinanceiro(ctxWithToken("tok"), client, args)
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("RemoverRegistroFinanceiro(%+v) error = %v (%T), want *ArgumentError", args, err, err)
		}
	}
}

// TestRemoverRegistroFinanceiro_Fatura_Success proves a fully-confirmed
// call reaches /core/financial/invoice/remove via DELETE with the confirmed
// camelCase field name, and decodes success correctly.
func TestRemoverRegistroFinanceiro_Fatura_Success(t *testing.T) {
	var gotMethod string
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/invoice/remove", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"message":"Fatura removida"}`)
	})
	client := newTestClient(t, mux)

	result, err := RemoverRegistroFinanceiro(ctxWithToken("tok"), client, RemoverRegistroFinanceiroArgs{
		Tipo: "fatura", Confirmacao: true, CienteIrreversivel: true, InvoiceID: intPtr(99),
	})
	if err != nil {
		t.Fatalf("RemoverRegistroFinanceiro: %v", err)
	}
	if !result.Removido {
		t.Fatalf("result = %+v, want Removido=true", result)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %q, want DELETE", gotMethod)
	}
	if gotBody["invoiceId"] != float64(99) {
		t.Fatalf("wire body = %+v, want invoiceId=99", gotBody)
	}
}

// TestRemoverRegistroFinanceiro_Fatura_ServerRejection_SurfacesError proves
// success:false from the DELETE (the shape confirmed by the Fase 4b
// empty-body probe: {"success":false,"message":"Field invoiceId not
// found"}) is treated as a failure, not a silent success.
func TestRemoverRegistroFinanceiro_Fatura_ServerRejection_SurfacesError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/invoice/remove", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":false,"message":"Field invoiceId not found"}`)
	})
	client := newTestClient(t, mux)

	_, err := RemoverRegistroFinanceiro(ctxWithToken("tok"), client, RemoverRegistroFinanceiroArgs{
		Tipo: "fatura", Confirmacao: true, CienteIrreversivel: true, InvoiceID: intPtr(99),
	})
	if !errors.Is(err, ErrOperacaoNaoConfirmadaFeegow) {
		t.Fatalf("RemoverRegistroFinanceiro error = %v, want ErrOperacaoNaoConfirmadaFeegow", err)
	}
	if strings.Contains(err.Error(), "Field invoiceId not found") {
		t.Fatalf("error leaked the raw Feegow message: %q", err.Error())
	}
}

func TestRemoverRegistroFinanceiro_Pagamento_RequiresPositivePaymentID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a missing/invalid payment_id: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := RemoverRegistroFinanceiro(ctxWithToken("tok"), client, RemoverRegistroFinanceiroArgs{
		Tipo: "pagamento", Confirmacao: true, CienteIrreversivel: true,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("RemoverRegistroFinanceiro error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestRemoverRegistroFinanceiro_Pagamento_Success_UsesPaymentIdField(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/payment/remove", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"message":"Pagamento removido"}`)
	})
	client := newTestClient(t, mux)

	result, err := RemoverRegistroFinanceiro(ctxWithToken("tok"), client, RemoverRegistroFinanceiroArgs{
		Tipo: "pagamento", Confirmacao: true, CienteIrreversivel: true, PaymentID: intPtr(55),
	})
	if err != nil {
		t.Fatalf("RemoverRegistroFinanceiro: %v", err)
	}
	if !result.Removido {
		t.Fatalf("result = %+v, want Removido=true", result)
	}
	if gotBody["paymentId"] != float64(55) {
		t.Fatalf("wire body = %+v, want paymentId=55", gotBody)
	}
}

// TestRemoverRegistroFinanceiro_AuditsWrite proves every successful removal
// is audited with the id of the record actually removed — not just that
// SOME removal happened. This is the most destructive write in the package;
// a log line without the id is useless after the fact to answer "which
// fatura was deleted?".
func TestRemoverRegistroFinanceiro_AuditsWrite(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/invoice/remove", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"message":"ok"}`)
	})
	client := newTestClient(t, mux)

	if _, err := RemoverRegistroFinanceiro(ctxWithToken("tok"), client, RemoverRegistroFinanceiroArgs{
		Tipo: "fatura", Confirmacao: true, CienteIrreversivel: true, InvoiceID: intPtr(4711),
	}); err != nil {
		t.Fatalf("RemoverRegistroFinanceiro: %v", err)
	}
	if !strings.Contains(buf.String(), "ADMIN WRITE remover_registro_financeiro:fatura — invoiceId=4711") {
		t.Fatalf("write not audited with the removed record's id: %s", buf.String())
	}
}

// TestRemoverRegistroFinanceiro_Pagamento_AuditsWrite proves the same for
// tipo=pagamento, with its own field label (paymentId, not invoiceId) — the
// two branches of doRemove must never blur which kind of id was removed.
func TestRemoverRegistroFinanceiro_Pagamento_AuditsWrite(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/payment/remove", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"message":"ok"}`)
	})
	client := newTestClient(t, mux)

	if _, err := RemoverRegistroFinanceiro(ctxWithToken("tok"), client, RemoverRegistroFinanceiroArgs{
		Tipo: "pagamento", Confirmacao: true, CienteIrreversivel: true, PaymentID: intPtr(99),
	}); err != nil {
		t.Fatalf("RemoverRegistroFinanceiro: %v", err)
	}
	if !strings.Contains(buf.String(), "ADMIN WRITE remover_registro_financeiro:pagamento — paymentId=99") {
		t.Fatalf("write not audited with the removed record's id: %s", buf.String())
	}
}
