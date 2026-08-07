package tools

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lab019/feegow-mcp/internal/feegow"
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

// TestAtualizarStatusAgendamento_NoPIIInLogs is item (e)'s coverage for
// atualizar_status_agendamento: Obs is caller-supplied free text that could
// carry patient PII (e.g. an operador typing a patient's name into the
// observação) — auditAdminWrite must only ever log agendamento_id, never
// Obs itself.
func TestAtualizarStatusAgendamento_NoPIIInLogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/statusUpdate", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"msg":"ok"}}`)
	})

	assertNoPIIInLog(t, mux, []string{"Segredo Pessoal"}, func(client *feegow.Client) error {
		_, err := AtualizarStatusAgendamento(ctxWithToken("tok"), client, AtualizarStatusAgendamentoArgs{
			AgendamentoID: 1, StatusID: 1, Obs: "Paciente Segredo Pessoal confirmou", Confirmacao: true,
		})
		return err
	})
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

// TestGerarSenhaAtendimento_SuccessKeyVariants is item (b)'s regression
// test: before this fix, gerarSenhaAtendimento decoded
// appoints/queue-position's content without EVER checking success at all —
// a success:false (or "sucess":false) response silently returned
// {posicao:0, tipoSenha:0, tipoFormatado:""} as if the ticket had really
// been generated. queuePositionSuccess must resolve every shape correctly:
// the real typo'd key, the correctly-spelled key as a forward-compatible
// fallback (in case Feegow ever fixes the typo server-side), an explicit
// false under either spelling, and — worst of all — neither key present,
// which must NOT be read as a silent success.
func TestGerarSenhaAtendimento_SuccessKeyVariants(t *testing.T) {
	unidade := 1
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"real typo'd key, true", `{"sucess":true,"content":{"posicao":5,"tipoSenha":1,"tipoFormatado":"P"}}`, false},
		{"corrected key, true (if Feegow ever fixes the typo)", `{"success":true,"content":{"posicao":5,"tipoSenha":1,"tipoFormatado":"P"}}`, false},
		{"real typo'd key, false", `{"sucess":false,"content":{"posicao":0,"tipoSenha":0,"tipoFormatado":""}}`, true},
		{"corrected key, false", `{"success":false,"content":{"posicao":0,"tipoSenha":0,"tipoFormatado":""}}`, true},
		{"neither key present", `{"content":{"posicao":0,"tipoSenha":0,"tipoFormatado":""}}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/appoints/queue-position", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, c.body)
			})
			client := newTestClient(t, mux)

			result, err := GerarSenhaAtendimento(ctxWithToken("tok"), client, GerarSenhaAtendimentoArgs{
				UnidadeID: &unidade, TipoSenha: 1,
			})
			if c.wantErr {
				if err == nil {
					t.Fatalf("GerarSenhaAtendimento(%s) = %+v, nil, want an error instead of a silent success with posicao=0", c.name, result)
				}
				return
			}
			if err != nil {
				t.Fatalf("GerarSenhaAtendimento(%s): %v", c.name, err)
			}
			if result.Posicao != 5 {
				t.Fatalf("GerarSenhaAtendimento(%s) result = %+v, want Posicao=5", c.name, result)
			}
		})
	}
}

// TestGerarSenhaAtendimento_ServerRejection_NeverLeaksPII completes item
// (a)'s "qualquer outra tool que use EnvelopeNone" coverage:
// appoints/queue-position is the third EnvelopeNone endpoint wired to a
// tool in this package. gerarSenhaAtendimento never interpolates the raw
// body into its error (unlike anexarAoProntuario before its fix), but this
// locks that in as a regression test rather than an implicit property.
func TestGerarSenhaAtendimento_ServerRejection_NeverLeaksPII(t *testing.T) {
	const plantedFreeText = "Fulano de Tal da Silva CPF 111.111.111-11"

	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/queue-position", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"sucess":false,"content":{"posicao":0,"tipoSenha":0,"tipoFormatado":"","mensagem":"`+plantedFreeText+`"}}`)
	})
	client := newTestClient(t, mux)

	unidade := 1
	_, err := GerarSenhaAtendimento(ctxWithToken("tok"), client, GerarSenhaAtendimentoArgs{
		UnidadeID: &unidade, TipoSenha: 1,
	})
	if !errors.Is(err, ErrOperacaoNaoConfirmadaFeegow) {
		t.Fatalf("GerarSenhaAtendimento error = %v, want ErrOperacaoNaoConfirmadaFeegow", err)
	}
	if s := err.Error(); strings.Contains(s, plantedFreeText) {
		t.Fatalf("GerarSenhaAtendimento error leaked the raw Feegow body: %q", s)
	}
}

// TestGerarSenhaAtendimento_AuditsWrite is item (c)'s regression test:
// gerar_senha_atendimento's fix added an auditAdminWriteQueue call —
// previously this write skipped auditing entirely, unlike every other admin
// write tool in this package. Proves the audit line fires on success and
// carries exactly unidade_id/tipo_senha.
func TestGerarSenhaAtendimento_AuditsWrite(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/queue-position", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"sucess":true,"content":{"posicao":1,"tipoSenha":1,"tipoFormatado":"P"}}`)
	})
	client := newTestClient(t, mux)

	unidade := 7
	if _, err := GerarSenhaAtendimento(ctxWithToken("tok"), client, GerarSenhaAtendimentoArgs{
		UnidadeID: &unidade, TipoSenha: 1,
	}); err != nil {
		t.Fatalf("GerarSenhaAtendimento: %v", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "ADMIN WRITE gerar_senha_atendimento") {
		t.Fatalf("gerar_senha_atendimento never audited its write: %s", logged)
	}
	if !strings.Contains(logged, "unidade_id=7") || !strings.Contains(logged, "tipo_senha=1") {
		t.Fatalf("audit line missing unidade_id/tipo_senha: %s", logged)
	}
}

// TestGerarSenhaAtendimento_NoPIIInLogs is item (e)'s coverage for
// gerar_senha_atendimento: this endpoint carries no patient data by design
// (see auditAdminWriteQueue's doc comment), so this proves that holds even
// if Feegow's response ever grows an unexpected free-text field —
// queuePositionBody's Content struct has no field for it, so it is simply
// never decoded, let alone logged.
func TestGerarSenhaAtendimento_NoPIIInLogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/queue-position", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"sucess":true,"content":{"posicao":1,"tipoSenha":1,"tipoFormatado":"P","observacao":"Segredo Pessoal"}}`)
	})

	unidade := 1
	assertNoPIIInLog(t, mux, []string{"Segredo Pessoal"}, func(client *feegow.Client) error {
		_, err := GerarSenhaAtendimento(ctxWithToken("tok"), client, GerarSenhaAtendimentoArgs{
			UnidadeID: &unidade, TipoSenha: 1,
		})
		return err
	})
}
