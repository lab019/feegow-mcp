package tools

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGerenciarFaturamento_UnknownAcao_Rejects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarFaturamento(ctxWithToken("tok"), client, GerenciarFaturamentoArgs{Acao: "apagar"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarFaturamento error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarFaturamento_Buscar_RequiresBillingTypeIDAndBilling(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	for _, args := range []GerenciarFaturamentoArgs{
		{Acao: "buscar", Billing: "G-1"},
		{Acao: "buscar", BillingTypeID: intPtr(1)},
	} {
		_, err := GerenciarFaturamento(ctxWithToken("tok"), client, args)
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("GerenciarFaturamento(%+v) error = %v (%T), want *ArgumentError", args, err, err)
		}
	}
}

// TestGerenciarFaturamento_Buscar_NoConfirmacaoRequired_Success proves
// acao=buscar (a GET, a read) reaches /billing/insurances-billing without
// confirmacao, dereferencing billing_type_id to a plain int on the wire
// (GET's query-string path has no case for a raw *int).
func TestGerenciarFaturamento_Buscar_NoConfirmacaoRequired_Success(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/billing/insurances-billing", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{},"total":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := GerenciarFaturamento(ctxWithToken("tok"), client, GerenciarFaturamentoArgs{
		Acao: "buscar", BillingTypeID: intPtr(1), Billing: "G-1",
	}); err != nil {
		t.Fatalf("GerenciarFaturamento(buscar, no confirmacao): %v", err)
	}
	if gotQuery.Get("billing_type_id") != "1" || gotQuery.Get("billing") != "G-1" {
		t.Fatalf("query = %v, want billing_type_id=1 and billing=G-1", gotQuery)
	}
}

func TestGerenciarFaturamento_Editar_RequiresConfirmacao_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unconfirmed editar: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarFaturamento(ctxWithToken("tok"), client, GerenciarFaturamentoArgs{
		Acao: "editar", BillingID: 1, BillingTypeID: intPtr(1),
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarFaturamento error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarFaturamento_Editar_RequiresBillingIDAndBillingTypeID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	for _, args := range []GerenciarFaturamentoArgs{
		{Acao: "editar", Confirmacao: true, BillingTypeID: intPtr(1)},
		{Acao: "editar", Confirmacao: true, BillingID: 1},
	} {
		_, err := GerenciarFaturamento(ctxWithToken("tok"), client, args)
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("GerenciarFaturamento(%+v) error = %v (%T), want *ArgumentError", args, err, err)
		}
	}
}

func TestGerenciarFaturamento_Editar_Success(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/billing/insurances-billing", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Guia atualizada"}`)
	})
	client := newTestClient(t, mux)

	result, err := GerenciarFaturamento(ctxWithToken("tok"), client, GerenciarFaturamentoArgs{
		Acao: "editar", Confirmacao: true, BillingID: float64(9), BillingTypeID: intPtr(2),
	})
	if err != nil {
		t.Fatalf("GerenciarFaturamento: %v", err)
	}
	if !result.Sucesso {
		t.Fatalf("result = %+v, want Sucesso=true", result)
	}
	if gotBody["billing_id"] != float64(9) || gotBody["billing_type_id"] != float64(2) {
		t.Fatalf("wire body = %+v, want billing_id/billing_type_id", gotBody)
	}
}

func TestGerenciarFaturamento_InserirGuia_RequiresConfirmacao_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unconfirmed inserir_guia: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarFaturamento(ctxWithToken("tok"), client, GerenciarFaturamentoArgs{
		Acao: "inserir_guia", BillingTypeID: intPtr(1),
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarFaturamento error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestGerenciarFaturamento_InserirGuia_RequiresAllSixteenFields proves
// EVERY one of the 16 fields the Fase 4c smoke test confirmed as
// obrigatório is enforced client-side, including billing_type_id — a *int,
// checked separately from the other 15 (any) fields precisely because a
// nil *int boxed into a map[string]any would NOT compare equal to nil
// (see faturamentoInserirGuia's doc comment).
func TestGerenciarFaturamento_InserirGuia_RequiresAllSixteenFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	// Only Confirmacao set — everything else missing.
	_, err := GerenciarFaturamento(ctxWithToken("tok"), client, GerenciarFaturamentoArgs{
		Acao: "inserir_guia", Confirmacao: true,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarFaturamento error = %v (%T), want *ArgumentError", err, err)
	}
	for _, campo := range []string{
		"billing_type_id", "unit_id", "insurance_id", "insurance_plan_id",
		"applicant_professional_council_id", "number_on_the_requesting_council", "UF_Requesting_Council",
		"requesting_CBO_code", "hired", "carrier_code", "hired_requester_ID",
		"hired_requester_code_at_carrier", "ANS_registry", "CNES_code",
		"requesting_professional_ID", "request_date",
	} {
		if !strings.Contains(argErr.Msg, campo) {
			t.Fatalf("error message = %q, missing field %q", argErr.Msg, campo)
		}
	}
}

// TestGerenciarFaturamento_InserirGuia_BillingTypeIDAloneNotEnough proves
// billing_type_id being set does NOT satisfy the check for the other 15
// fields — a narrower regression guard than the "all missing" case above.
func TestGerenciarFaturamento_InserirGuia_BillingTypeIDAloneNotEnough(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarFaturamento(ctxWithToken("tok"), client, GerenciarFaturamentoArgs{
		Acao: "inserir_guia", Confirmacao: true, BillingTypeID: intPtr(1),
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarFaturamento error = %v (%T), want *ArgumentError", err, err)
	}
	if strings.Contains(argErr.Msg, "billing_type_id") {
		t.Fatalf("error message = %q, must NOT list billing_type_id as missing (it was provided)", argErr.Msg)
	}
}

func TestGerenciarFaturamento_InserirGuia_Success(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/billing/insurances-billing", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Guia inserida"}`)
	})
	client := newTestClient(t, mux)

	result, err := GerenciarFaturamento(ctxWithToken("tok"), client, GerenciarFaturamentoArgs{
		Acao:                           "inserir_guia",
		Confirmacao:                    true,
		BillingTypeID:                  intPtr(1),
		UnitID:                         1,
		InsuranceID:                    1,
		InsurancePlanID:                1,
		ApplicantProfessionalCouncilID: 1,
		NumberOnTheRequestingCouncil:   "123",
		UFRequestingCouncil:            "SP",
		RequestingCBOCode:              "225124",
		Hired:                          "1",
		CarrierCode:                    "1",
		HiredRequesterID:               "1",
		HiredRequesterCodeAtCarrier:    "1",
		ANSRegistry:                    "123456",
		CNESCode:                       "1234567",
		RequestingProfessionalID:       1,
		RequestDate:                    "2026-01-01",
	})
	if err != nil {
		t.Fatalf("GerenciarFaturamento: %v", err)
	}
	if !result.Sucesso {
		t.Fatalf("result = %+v, want Sucesso=true", result)
	}
	if gotBody["billing_type_id"] != float64(1) || gotBody["request_date"] != "2026-01-01" {
		t.Fatalf("wire body = %+v, missing expected fields", gotBody)
	}
}
