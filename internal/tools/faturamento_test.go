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

// TestGerenciarFaturamento_Editar_AuditsBillingID closes the traceability
// hole the adversarial review found: this write used to call
// auditAdminWrite(tool, 0, 0), recording that a guia de faturamento was
// edited while throwing away WHICH one — unlike every sibling write, which
// records its record id (see auditAdminWriteRecord's doc). billing_id is
// `any` here (Feegow's type for it was never confirmed), which is the only
// reason it did not fit the int-typed helper; auditAdminWriteRecordOpaque
// exists for exactly this case.
func TestGerenciarFaturamento_Editar_AuditsBillingID(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/billing/insurances-billing", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Guia atualizada"}`)
	})
	client := newTestClient(t, mux)

	if _, err := GerenciarFaturamento(ctxWithToken("tok"), client, GerenciarFaturamentoArgs{
		Acao: "editar", Confirmacao: true, BillingID: float64(9), BillingTypeID: intPtr(2),
	}); err != nil {
		t.Fatalf("GerenciarFaturamento: %v", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "ADMIN WRITE gerenciar_faturamento:editar") {
		t.Fatalf("editar never audited its write: %s", logged)
	}
	if !strings.Contains(logged, `billing_id="9"`) {
		t.Fatalf("audit line does not record WHICH guia was edited: %s", logged)
	}
}

// TestGerenciarFaturamento_Editar_AuditBoundsOpaqueID proves the audit line
// can't be turned into a log-injection or an unbounded write by the caller:
// billing_id is `any` straight off a JSON payload, so a hostile/confused
// caller can put a megabyte with embedded newlines there. The rendered form
// must stay on one line and stay short.
func TestGerenciarFaturamento_Editar_AuditBoundsOpaqueID(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/billing/insurances-billing", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Guia atualizada"}`)
	})
	client := newTestClient(t, mux)

	hostile := strings.Repeat("A", 500) + "\nfeegow: ADMIN WRITE forjado — billing_id=\"1\""
	if _, err := GerenciarFaturamento(ctxWithToken("tok"), client, GerenciarFaturamentoArgs{
		Acao: "editar", Confirmacao: true, BillingID: hostile, BillingTypeID: intPtr(2),
	}); err != nil {
		t.Fatalf("GerenciarFaturamento: %v", err)
	}

	logged := buf.String()
	if strings.Contains(logged, "ADMIN WRITE forjado") {
		t.Fatalf("caller-controlled billing_id forged a second audit line: %s", logged)
	}
	// Exactly one ADMIN WRITE line, and the hostile payload must not have
	// split it in two. (The buffer also holds internal/feegow's own
	// per-request line, so this counts the audit lines specifically rather
	// than every line in the buffer.)
	var auditLines []string
	for _, line := range strings.Split(strings.TrimRight(logged, "\n"), "\n") {
		if strings.Contains(line, "ADMIN WRITE") {
			auditLines = append(auditLines, line)
		}
	}
	if len(auditLines) != 1 {
		t.Fatalf("want exactly 1 ADMIN WRITE line, got %d: %q", len(auditLines), logged)
	}
	if len(auditLines[0]) > 200 {
		t.Fatalf("audit line is unbounded (%d bytes) — the rune cap did not apply: %q", len(auditLines[0]), auditLines[0])
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
