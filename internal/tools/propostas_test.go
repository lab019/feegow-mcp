package tools

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGerenciarPropostas_Listar_RequiresPacienteIDOrDateRange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{Acao: "listar"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarPropostas error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestGerenciarPropostas_Listar_ByPacienteID_Success proves paciente_id
// alone (no dates) reaches /proposal/list — confirmed by the Fase 4c smoke
// test to be accepted without data_inicio/data_fim.
func TestGerenciarPropostas_Listar_ByPacienteID_Success(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/proposal/list", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{
		Acao: "listar", PacienteID: intPtr(7),
	}); err != nil {
		t.Fatalf("GerenciarPropostas: %v", err)
	}
	if gotQuery.Get("paciente_id") != "7" {
		t.Fatalf("paciente_id = %q, want 7", gotQuery.Get("paciente_id"))
	}
	if gotQuery.Has("data_inicio") || gotQuery.Has("data_fim") {
		t.Fatalf("dates sent despite paciente_id being provided: %v", gotQuery)
	}
}

func TestGerenciarPropostas_Listar_ByDateRange_Success(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/proposal/list", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{
		Acao: "listar", DataInicio: "2026-01-01", DataFim: "2026-02-01",
	}); err != nil {
		t.Fatalf("GerenciarPropostas: %v", err)
	}
	if gotQuery.Get("data_inicio") != "2026-01-01" || gotQuery.Get("data_fim") != "2026-02-01" {
		t.Fatalf("query = %v, want data_inicio/data_fim in ISO-8601", gotQuery)
	}
}

func TestGerenciarPropostas_ListarPorData_AlwaysUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for acao=listar_por_data: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{Acao: "listar_por_data"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarPropostas error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarPropostas_UnknownAcao_Rejects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unrecognized acao: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{Acao: "apagar"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarPropostas error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarPropostas_Criar_RequiresConfirmacao_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unconfirmed criar: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{
		Acao: "criar", ProposerID: intPtr(1), PacienteID: intPtr(1), StatusID: intPtr(1),
		ProposalDate: "2026-01-01", Procedimentos: []any{map[string]any{"id": 1}},
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarPropostas error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarPropostas_Criar_RequiresEveryField(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	full := GerenciarPropostasArgs{
		Acao: "criar", Confirmacao: true,
		ProposerID: intPtr(1), PacienteID: intPtr(1), StatusID: intPtr(1),
		ProposalDate: "2026-01-01", Procedimentos: []any{map[string]any{"id": 1}},
	}
	cases := []GerenciarPropostasArgs{
		{Acao: "criar", Confirmacao: true, PacienteID: intPtr(1), StatusID: intPtr(1), ProposalDate: "2026-01-01", Procedimentos: full.Procedimentos},
		{Acao: "criar", Confirmacao: true, ProposerID: intPtr(1), StatusID: intPtr(1), ProposalDate: "2026-01-01", Procedimentos: full.Procedimentos},
		{Acao: "criar", Confirmacao: true, ProposerID: intPtr(1), PacienteID: intPtr(1), ProposalDate: "2026-01-01", Procedimentos: full.Procedimentos},
		{Acao: "criar", Confirmacao: true, ProposerID: intPtr(1), PacienteID: intPtr(1), StatusID: intPtr(1), Procedimentos: full.Procedimentos},
		{Acao: "criar", Confirmacao: true, ProposerID: intPtr(1), PacienteID: intPtr(1), StatusID: intPtr(1), ProposalDate: "not-a-date", Procedimentos: full.Procedimentos},
		{Acao: "criar", Confirmacao: true, ProposerID: intPtr(1), PacienteID: intPtr(1), StatusID: intPtr(1), ProposalDate: "2026-01-01"},
	}
	for i, args := range cases {
		_, err := GerenciarPropostas(ctxWithToken("tok"), client, args)
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("case %d: GerenciarPropostas(%+v) error = %v (%T), want *ArgumentError", i, args, err, err)
		}
	}
}

func TestGerenciarPropostas_Criar_Success(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/proposal/create", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"id":42}}`)
	})
	client := newTestClient(t, mux)

	result, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{
		Acao: "criar", Confirmacao: true,
		ProposerID: intPtr(1), PacienteID: intPtr(9), StatusID: intPtr(2),
		ProposalDate: "2026-01-01", Procedimentos: []any{map[string]any{"procedimento_id": 5}},
	})
	if err != nil {
		t.Fatalf("GerenciarPropostas: %v", err)
	}
	if !result.Sucesso {
		t.Fatalf("result = %+v, want Sucesso=true", result)
	}
	if gotBody["proposer_id"] != float64(1) || gotBody["paciente_id"] != float64(9) || gotBody["status_id"] != float64(2) {
		t.Fatalf("wire body = %+v, missing expected top-level fields", gotBody)
	}
}

func TestGerenciarPropostas_MudarStatus_RequiresConfirmacao_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unconfirmed mudar_status: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{
		Acao: "mudar_status", PropostaID: intPtr(1), StatusID: intPtr(2),
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarPropostas error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestGerenciarPropostas_MudarStatus_Success_UsesProposalIDWireName proves
// the wire field is "proposal_id" (English) — confirmed by the Fase 4c
// smoke test — distinct from obter_url's "proposta_id" (Portuguese), even
// though both are exposed through the same PropostaID Go field.
func TestGerenciarPropostas_MudarStatus_Success_UsesProposalIDWireName(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/proposal/change-status", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Status alterado"}`)
	})
	client := newTestClient(t, mux)

	if _, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{
		Acao: "mudar_status", Confirmacao: true, PropostaID: intPtr(5), StatusID: intPtr(3),
	}); err != nil {
		t.Fatalf("GerenciarPropostas: %v", err)
	}
	if gotBody["proposal_id"] != float64(5) {
		t.Fatalf("wire body = %+v, want proposal_id=5 (not proposta_id)", gotBody)
	}
	if _, hasPT := gotBody["proposta_id"]; hasPT {
		t.Fatalf("wire body = %+v, must NOT send proposta_id for change-status", gotBody)
	}
}

func TestGerenciarPropostas_MudarStatus_AuditsWrite(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/proposal/change-status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"ok"}`)
	})
	client := newTestClient(t, mux)

	if _, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{
		Acao: "mudar_status", Confirmacao: true, PropostaID: intPtr(5), StatusID: intPtr(3),
	}); err != nil {
		t.Fatalf("GerenciarPropostas: %v", err)
	}
	if !strings.Contains(buf.String(), "ADMIN WRITE gerenciar_propostas:mudar_status — proposta_id=5") {
		t.Fatalf("write not audited with proposta_id: %s", buf.String())
	}
}

func TestGerenciarPropostas_ObterUrl_NoConfirmacaoRequired(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/proposal/proposal-url", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":false}`)
	})
	client := newTestClient(t, mux)

	if _, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{
		Acao: "obter_url", PropostaID: intPtr(1),
	}); err != nil {
		t.Fatalf("GerenciarPropostas(obter_url, no confirmacao): %v", err)
	}
}

// TestGerenciarPropostas_ObterUrl_Success_UsesPropostaIDWireName proves the
// wire field is "proposta_id" (Portuguese) here — the opposite spelling of
// mudar_status' "proposal_id" — and that content:false (no URL available)
// is decoded, not treated as an error.
func TestGerenciarPropostas_ObterUrl_Success_UsesPropostaIDWireName(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/proposal/proposal-url", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":false}`)
	})
	client := newTestClient(t, mux)

	result, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{
		Acao: "obter_url", PropostaID: intPtr(1),
	})
	if err != nil {
		t.Fatalf("GerenciarPropostas: %v", err)
	}
	if gotQuery.Get("proposta_id") != "1" {
		t.Fatalf("query = %v, want proposta_id=1", gotQuery)
	}
	if itens, ok := result.Itens.(bool); !ok || itens != false {
		t.Fatalf("result.Itens = %#v, want false (boolean, per the real API)", result.Itens)
	}
}

func TestGerenciarPropostas_ObterUrl_RequiresPropostaID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarPropostas(ctxWithToken("tok"), client, GerenciarPropostasArgs{Acao: "obter_url"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarPropostas error = %v (%T), want *ArgumentError", err, err)
	}
}
