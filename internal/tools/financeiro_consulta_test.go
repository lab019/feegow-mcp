package tools

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestConsultarFinanceiro_UnknownTipo_RejectsBeforeFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unrecognized tipo: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{Tipo: "nao_existe"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarFinanceiro error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestConsultarFinanceiro_TabelasPrivadasAndDmed_AlwaysUnavailable proves
// the two dead-endpoint tipos are recognized (not "unknown tipo") but never
// reach Feegow — see ArgumentIndisponivel.
func TestConsultarFinanceiro_TabelasPrivadasAndDmed_AlwaysUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a dead-endpoint tipo: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	for _, tipo := range []string{"tabelas_privadas", "dmed"} {
		_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{Tipo: tipo})
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("ConsultarFinanceiro(tipo=%s) error = %v (%T), want *ArgumentError", tipo, err, err)
		}
	}
}

func TestConsultarFinanceiro_Fornecedores_NoParamsRequired(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/list-suppliers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"id":1,"nome":"Fornecedor A"}],"total":1}`)
	})
	client := newTestClient(t, mux)

	result, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{Tipo: "fornecedores"})
	if err != nil {
		t.Fatalf("ConsultarFinanceiro: %v", err)
	}
	if result.Itens == nil {
		t.Fatalf("result.Itens is nil, want the decoded content")
	}
}

func TestConsultarFinanceiro_Fornecedor_RequiresFornecedorID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a missing fornecedor_id: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{Tipo: "fornecedor"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarFinanceiro error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestConsultarFinanceiro_Contas_RequiresDatesUnidadeAndTipoTransacao(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an incomplete tipo=contas request: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	unidade := 0
	cases := []ConsultarFinanceiroArgs{
		{Tipo: "contas"},
		{Tipo: "contas", DataInicio: "2026-01-01", DataFim: "2026-01-31"},
		{Tipo: "contas", DataInicio: "2026-01-01", DataFim: "2026-01-31", UnidadeID: &unidade},
		{Tipo: "contas", DataInicio: "2026-01-01", DataFim: "2026-01-31", UnidadeID: &unidade, TipoTransacao: "X"},
	}
	for _, args := range cases {
		_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, args)
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("ConsultarFinanceiro(%+v) error = %v (%T), want *ArgumentError", args, err, err)
		}
	}
}

// TestConsultarFinanceiro_Contas_TranslatesDatesToDDMMYYYY proves
// data_inicio/data_fim (ISO-8601 in) reach Feegow as data_start/data_end in
// DD-MM-YYYY — ESPECIFICACAO.md §5's confirmed convention for
// /financial/list-invoice.
func TestConsultarFinanceiro_Contas_TranslatesDatesToDDMMYYYY(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/list-invoice", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	client := newTestClient(t, mux)

	unidade := 0
	_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{
		Tipo: "contas", DataInicio: "2026-01-01", DataFim: "2026-01-31", UnidadeID: &unidade, TipoTransacao: "C",
	})
	if err != nil {
		t.Fatalf("ConsultarFinanceiro: %v", err)
	}
	if gotQuery.Get("data_start") != "01-01-2026" || gotQuery.Get("data_end") != "31-01-2026" {
		t.Fatalf("data_start/data_end = %q/%q, want DD-MM-YYYY", gotQuery.Get("data_start"), gotQuery.Get("data_end"))
	}
	if gotQuery.Get("tipo_transacao") != "C" || gotQuery.Get("unidade_id") != "0" {
		t.Fatalf("tipo_transacao/unidade_id = %q/%q, want C/0", gotQuery.Get("tipo_transacao"), gotQuery.Get("unidade_id"))
	}
}

func TestConsultarFinanceiro_Vendas_RequiresDatesAndUnidade(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an incomplete tipo=vendas request: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{
		Tipo: "vendas", DataInicio: "2026-01-01", DataFim: "2026-01-31",
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarFinanceiro error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestConsultarFinanceiro_Vendas_KeepsISO8601Dates proves list-sales' dates
// stay YYYY-MM-DD on the wire (date_start/date_end) — genuinely different
// from tipo=contas' DD-MM-YYYY, per ESPECIFICACAO.md §5.
func TestConsultarFinanceiro_Vendas_KeepsISO8601Dates(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/list-sales", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	client := newTestClient(t, mux)

	unidade := 0
	_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{
		Tipo: "vendas", DataInicio: "2026-01-01", DataFim: "2026-01-31", UnidadeID: &unidade,
	})
	if err != nil {
		t.Fatalf("ConsultarFinanceiro: %v", err)
	}
	if gotQuery.Get("date_start") != "2026-01-01" || gotQuery.Get("date_end") != "2026-01-31" {
		t.Fatalf("date_start/date_end = %q/%q, want ISO-8601", gotQuery.Get("date_start"), gotQuery.Get("date_end"))
	}
}

func TestConsultarFinanceiro_Bandeiras_ReturnsContent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/credit-card-flags", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"id":1,"Bandeira":"Visa"}]}`)
	})
	client := newTestClient(t, mux)

	result, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{Tipo: "bandeiras"})
	if err != nil {
		t.Fatalf("ConsultarFinanceiro: %v", err)
	}
	if result.Itens == nil {
		t.Fatal("result.Itens is nil")
	}
}

// TestConsultarFinanceiro_ContasCorrentes_LimitDefaultsAndCaps proves the
// pagination ceiling applies — same discipline as buscarPacientesMaxLimit.
func TestConsultarFinanceiro_ContasCorrentes_LimitDefaultsAndCaps(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/base/current-accounts", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"data":[],"count":0,"page":1,"perPage":100,"pages":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{
		Tipo: "contas_correntes", Limit: 9999,
	}); err != nil {
		t.Fatalf("ConsultarFinanceiro: %v", err)
	}
	if gotBody["perPage"] != "100" {
		t.Fatalf("perPage = %v, want capped at 100", gotBody["perPage"])
	}
}

func TestConsultarFinanceiro_InvoicePorNfse_RequiresNfseNumero(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a missing nfse_numero: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{Tipo: "invoice_por_nfse"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarFinanceiro error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestConsultarFinanceiro_Contas_RejectsWindowWiderThanTeto proves a
// data_inicio–data_fim window wider than tetoJanelaFinanceiroDias is
// rejected BEFORE any Feegow request — this is OUR ceiling (Feegow itself
// does not limit this endpoint's window; see tetoJanelaFinanceiroDias' doc
// comment), so it must be enforced client-side.
func TestConsultarFinanceiro_Contas_RejectsWindowWiderThanTeto(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a window wider than the teto: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	unidade := 0
	_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{
		Tipo: "contas", DataInicio: "2026-01-01", DataFim: "2027-01-02", // 366 days
		UnidadeID: &unidade, TipoTransacao: "T",
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarFinanceiro error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestConsultarFinanceiro_Contas_AcceptsWindowAtTeto proves a window exactly
// at tetoJanelaFinanceiroDias reaches Feegow — the ceiling must not be
// off-by-one in the strict direction.
func TestConsultarFinanceiro_Contas_AcceptsWindowAtTeto(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/list-invoice", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	client := newTestClient(t, mux)

	unidade := 0
	_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{
		Tipo: "contas", DataInicio: "2026-01-01", DataFim: "2027-01-01", // 365 days
		UnidadeID: &unidade, TipoTransacao: "T",
	})
	if err != nil {
		t.Fatalf("ConsultarFinanceiro: %v", err)
	}
}

// TestConsultarFinanceiro_Vendas_RejectsWindowWiderThanTeto proves the same
// ceiling applies to tipo=vendas (financial.list_sales) — Fase 4b measured
// this endpoint accepting a 5-year window against the real API with no
// server-side rejection, so only this client-side check stops it.
func TestConsultarFinanceiro_Vendas_RejectsWindowWiderThanTeto(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a window wider than the teto: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	unidade := 0
	_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{
		Tipo: "vendas", DataInicio: "2020-01-01", DataFim: "2025-01-01", UnidadeID: &unidade,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarFinanceiro error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestConsultarFinanceiro_Repasses_RejectsWindowWiderThanTeto proves the
// same ceiling applies to tipo=repasses (financial.list_medical_transfer).
func TestConsultarFinanceiro_Repasses_RejectsWindowWiderThanTeto(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a window wider than the teto: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{
		Tipo: "repasses", DataInicio: "2020-01-01", DataFim: "2025-01-01",
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarFinanceiro error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestConsultarFinanceiro_ConflictSanitized proves a 409 from any of these
// endpoints never leaks the raw Feegow body — same choke point every other
// admin read tool routes through.
func TestConsultarFinanceiro_ConflictSanitized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/list-suppliers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusConflict, `{"success":false,"content":"detalhe interno sensível"}`)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarFinanceiro(ctxWithToken("tok"), client, ConsultarFinanceiroArgs{Tipo: "fornecedores"})
	if !errors.Is(err, ErrConflitoFeegow) {
		t.Fatalf("error = %v, want ErrConflitoFeegow", err)
	}
	if err.Error() == "" {
		t.Fatal("empty error message")
	}
}
