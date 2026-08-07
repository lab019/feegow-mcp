package tools

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// --- atualizar_status_agendamento -----------------------------------------

func TestAtualizarStatusAgendamento_RequiresConfirmacao_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for unconfirmed atualizar_status_agendamento: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := AtualizarStatusAgendamento(ctxWithToken("tok"), client, AtualizarStatusAgendamentoArgs{
		AgendamentoID: 100, StatusID: 7, Confirmacao: false,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("AtualizarStatusAgendamento error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestAtualizarStatusAgendamento_RejectsNonPositiveAgendamentoID_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a non-positive agendamento_id: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	for _, id := range []int{0, -5} {
		_, err := AtualizarStatusAgendamento(ctxWithToken("tok"), client, AtualizarStatusAgendamentoArgs{
			AgendamentoID: id, StatusID: 7, Confirmacao: true,
		})
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("AtualizarStatusAgendamento(agendamento_id=%d) error = %v (%T), want *ArgumentError", id, err, err)
		}
	}
}

func TestAtualizarStatusAgendamento_RequiresStatusID_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a missing status_id: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := AtualizarStatusAgendamento(ctxWithToken("tok"), client, AtualizarStatusAgendamentoArgs{
		AgendamentoID: 100, Confirmacao: true,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("AtualizarStatusAgendamento error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestAtualizarStatusAgendamento_Success_SendsPascalCaseWireFields proves
// the request body uses the real, PascalCase wire field names Fase 0
// confirmed (AgendamentoID, StatusID, Obs, HoraChegada) — not the lowercase
// names a naive caller (or a Go-idiomatic guess) would produce.
func TestAtualizarStatusAgendamento_Success_SendsPascalCaseWireFields(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/statusUpdate", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"msg":"Agendamento alterado com sucesso"}}`)
	})
	client := newTestClient(t, mux)

	result, err := AtualizarStatusAgendamento(ctxWithToken("tok"), client, AtualizarStatusAgendamentoArgs{
		AgendamentoID: 100, StatusID: 7, Obs: "Paciente confirmou o comparecimento.", HoraChegada: "09:15",
		Confirmacao: true,
	})
	if err != nil {
		t.Fatalf("AtualizarStatusAgendamento: %v", err)
	}
	if !result.Atualizado {
		t.Fatalf("result = %+v, want Atualizado=true", result)
	}
	if gotBody["AgendamentoID"] != float64(100) || gotBody["StatusID"] != float64(7) {
		t.Fatalf("wire body = %+v, want PascalCase AgendamentoID/StatusID", gotBody)
	}
	if gotBody["Obs"] != "Paciente confirmou o comparecimento." || gotBody["HoraChegada"] != "09:15" {
		t.Fatalf("wire body = %+v, want PascalCase Obs/HoraChegada", gotBody)
	}
	if _, has := gotBody["agendamento_id"]; has {
		t.Fatalf("wire body leaked a lowercase agendamento_id alongside AgendamentoID: %+v", gotBody)
	}
}

// TestAtualizarStatusAgendamento_ObsAndHoraChegadaAreOptional proves a
// minimal call (only the two required fields) omits Obs/HoraChegada
// entirely from the wire body, rather than sending empty strings.
func TestAtualizarStatusAgendamento_ObsAndHoraChegadaAreOptional(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/statusUpdate", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"msg":"ok"}}`)
	})
	client := newTestClient(t, mux)

	if _, err := AtualizarStatusAgendamento(ctxWithToken("tok"), client, AtualizarStatusAgendamentoArgs{
		AgendamentoID: 1, StatusID: 1, Confirmacao: true,
	}); err != nil {
		t.Fatalf("AtualizarStatusAgendamento: %v", err)
	}
	if _, has := gotBody["Obs"]; has {
		t.Fatalf("wire body = %+v, want Obs omitted when not set", gotBody)
	}
	if _, has := gotBody["HoraChegada"]; has {
		t.Fatalf("wire body = %+v, want HoraChegada omitted when not set", gotBody)
	}
}

// --- gerar_senha_atendimento -----------------------------------------------

func TestGerarSenhaAtendimento_RequiresUnidadeID_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a missing unidade_id: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerarSenhaAtendimento(ctxWithToken("tok"), client, GerarSenhaAtendimentoArgs{TipoSenha: 1})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerarSenhaAtendimento error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestGerarSenhaAtendimento_AcceptsExplicitZeroUnidade proves 0 (unidade
// principal) is a legitimate, distinct value from "omitted" — the same
// *int discipline AgendarArgs.LocalID already establishes.
func TestGerarSenhaAtendimento_AcceptsExplicitZeroUnidade(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/queue-position", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"sucess":true,"content":{"posicao":1,"tipoSenha":1,"tipoFormatado":"P"}}`)
	})
	client := newTestClient(t, mux)

	zero := 0
	result, err := GerarSenhaAtendimento(ctxWithToken("tok"), client, GerarSenhaAtendimentoArgs{
		UnidadeID: &zero, TipoSenha: 1,
	})
	if err != nil {
		t.Fatalf("GerarSenhaAtendimento: %v", err)
	}
	if gotQuery.Get("unidade_id") != "0" {
		t.Fatalf("unidade_id query param = %q, want explicit %q", gotQuery.Get("unidade_id"), "0")
	}
	if result.Posicao != 1 || result.TipoSenha != 1 || result.TipoFormatado != "P" {
		t.Fatalf("result = %+v, want the decoded content", result)
	}
}

func TestGerarSenhaAtendimento_RejectsOutOfRangeTipoSenha_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an out-of-range tipo_senha: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	unidade := 0
	for _, tipo := range []int{-1, 5} {
		_, err := GerarSenhaAtendimento(ctxWithToken("tok"), client, GerarSenhaAtendimentoArgs{
			UnidadeID: &unidade, TipoSenha: tipo,
		})
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("GerarSenhaAtendimento(tipo_senha=%d) error = %v (%T), want *ArgumentError", tipo, err, err)
		}
	}
}

// TestGerarSenhaAtendimento_DecodesDespiteSucessTypo proves the tool
// tolerates the real API's "sucess" (misspelled) top-level key — the whole
// reason appoints.queue_position is registered as EnvelopeNone.
func TestGerarSenhaAtendimento_DecodesDespiteSucessTypo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/queue-position", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"sucess":true,"content":{"posicao":3,"tipoSenha":2,"tipoFormatado":"C"}}`)
	})
	client := newTestClient(t, mux)

	unidade := 1
	result, err := GerarSenhaAtendimento(ctxWithToken("tok"), client, GerarSenhaAtendimentoArgs{
		UnidadeID: &unidade, TipoSenha: 2,
	})
	if err != nil {
		t.Fatalf("GerarSenhaAtendimento: %v", err)
	}
	if result.Posicao != 3 || result.TipoFormatado != "C" {
		t.Fatalf("result = %+v, want the decoded content despite the \"sucess\" typo", result)
	}
}
