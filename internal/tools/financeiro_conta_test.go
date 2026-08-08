package tools

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestGerenciarConta_RequiresConfirmacao_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unconfirmed gerenciar_conta: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	agendamento := 1
	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{
		Acao: "criar_por_agendamento", AgendamentoID: &agendamento, Confirmacao: false,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarConta error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarConta_UnknownAcao_RejectsAfterConfirmacao(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unrecognized acao: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{Acao: "nao_existe", Confirmacao: true})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarConta error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestGerenciarConta_AssociarConta_AlwaysUnavailable proves acao=associar_conta
// is recognized but never reaches Feegow — the contract could not be
// confirmed by the Fase 4b smoke test (see the file's doc comment).
func TestGerenciarConta_AssociarConta_AlwaysUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for acao=associar_conta: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{Acao: "associar_conta", Confirmacao: true})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarConta error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarConta_Criar_RequiresEveryTopLevelField(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an incomplete acao=criar request: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	table, user, unity := 1, 1, 1
	base := GerenciarContaArgs{Acao: "criar", Confirmacao: true}
	cases := []GerenciarContaArgs{
		base,
		{Acao: "criar", Confirmacao: true, Type: "C"},
		{Acao: "criar", Confirmacao: true, Type: "C", Date: "2026-01-01"},
		{Acao: "criar", Confirmacao: true, Type: "C", Date: "2026-01-01", Table: &table, User: &user, Unity: &unity},
		{Acao: "criar", Confirmacao: true, Type: "C", Date: "2026-01-01", Table: &table, User: &user, Unity: &unity, Account: map[string]any{"id": 1}},
		{Acao: "criar", Confirmacao: true, Type: "C", Date: "2026-01-01", Table: &table, User: &user, Unity: &unity, Account: map[string]any{"id": 1}, Items: []any{map[string]any{"id": 1}}},
	}
	for i, args := range cases {
		_, err := GerenciarConta(ctxWithToken("tok"), client, args)
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("case %d: GerenciarConta error = %v (%T), want *ArgumentError", i, err, err)
		}
	}
}

// TestGerenciarConta_Criar_Success proves a fully-formed request reaches
// /core/financial/invoice/create with the confirmed top-level field names.
func TestGerenciarConta_Criar_Success(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/invoice/create", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"id":1}`)
	})
	client := newTestClient(t, mux)

	table, user, unity := 1, 2, 3
	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{
		Acao: "criar", Confirmacao: true,
		Type: "C", Date: "2026-01-01", Table: &table, User: &user, Unity: &unity,
		Account: map[string]any{"id": 1}, Items: []any{map[string]any{"id": 1}}, Installments: []any{map[string]any{"id": 1}},
	})
	if err != nil {
		t.Fatalf("GerenciarConta: %v", err)
	}
	if gotBody["type"] != "C" || gotBody["date"] != "2026-01-01" {
		t.Fatalf("wire body = %+v, want type/date passed through", gotBody)
	}
	if gotBody["table"] != float64(1) || gotBody["user"] != float64(2) || gotBody["unity"] != float64(3) {
		t.Fatalf("wire body = %+v, want table/user/unity passed through as ints", gotBody)
	}
}

// TestGerenciarConta_Criar_RejectsEmptyAccountObject proves an empty
// account:{} (a well-formed JSON object, but empty) is rejected the same as
// an omitted account — the field's own doc comment and the endpoint's
// Registry Notes both say "objeto não-vazio, obrigatório", and only checking
// for nil let an empty map slip through.
func TestGerenciarConta_Criar_RejectsEmptyAccountObject(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an empty account object: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	table, user, unity := 1, 1, 1
	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{
		Acao: "criar", Confirmacao: true, Type: "C", Date: "2026-01-01",
		Table: &table, User: &user, Unity: &unity,
		Account:      map[string]any{},
		Items:        []any{map[string]any{"id": 1}},
		Installments: []any{map[string]any{"id": 1}},
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarConta(account={}) error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestGerenciarConta_Criar_ServerRejection_SurfacesError proves a
// {"success":false,...} body from financial.invoice_create — the same
// business-failure pattern already confirmed for financial.create_account
// and financial.invoice_remove — is treated as a failure, not reported as
// Sucesso=true just because the HTTP call itself returned no error.
func TestGerenciarConta_Criar_ServerRejection_SurfacesError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/invoice/create", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":false,"message":"Tabela particular inválida"}`)
	})
	client := newTestClient(t, mux)

	table, user, unity := 1, 1, 1
	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{
		Acao: "criar", Confirmacao: true, Type: "C", Date: "2026-01-01",
		Table: &table, User: &user, Unity: &unity,
		Account: map[string]any{"id": 1}, Items: []any{map[string]any{"id": 1}}, Installments: []any{map[string]any{"id": 1}},
	})
	if !errors.Is(err, ErrOperacaoNaoConfirmadaFeegow) {
		t.Fatalf("GerenciarConta error = %v, want ErrOperacaoNaoConfirmadaFeegow", err)
	}
}

func TestGerenciarConta_CriarPorAgendamento_RequiresPositiveAgendamentoID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a missing/invalid agendamento_id: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	for _, args := range []GerenciarContaArgs{
		{Acao: "criar_por_agendamento", Confirmacao: true},
		{Acao: "criar_por_agendamento", Confirmacao: true, AgendamentoID: intPtr(0)},
		{Acao: "criar_por_agendamento", Confirmacao: true, AgendamentoID: intPtr(-1)},
	} {
		_, err := GerenciarConta(ctxWithToken("tok"), client, args)
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("GerenciarConta(%+v) error = %v (%T), want *ArgumentError", args, err, err)
		}
	}
}

// TestGerenciarConta_CriarPorAgendamento_DecodesMsgEnvelope proves the tool
// decodes financial.create-account's EnvelopeNone {success,msg} shape
// (confirmed against the real sandbox — see the endpoint's Registry Notes)
// and surfaces success:false as an error rather than a silent success.
func TestGerenciarConta_CriarPorAgendamento_DecodesMsgEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/create-account", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":false,"msg":"O agendamento_id informado não se encontra na nossa base de dados."}`)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{
		Acao: "criar_por_agendamento", Confirmacao: true, AgendamentoID: intPtr(1),
	})
	if !errors.Is(err, ErrOperacaoNaoConfirmadaFeegow) {
		t.Fatalf("GerenciarConta error = %v, want ErrOperacaoNaoConfirmadaFeegow", err)
	}
}

func TestGerenciarConta_CriarPorAgendamento_SuccessAuditsWrite(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/create-account", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"msg":"Conta criada"}`)
	})
	client := newTestClient(t, mux)

	result, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{
		Acao: "criar_por_agendamento", Confirmacao: true, AgendamentoID: intPtr(42),
	})
	if err != nil {
		t.Fatalf("GerenciarConta: %v", err)
	}
	if !result.Sucesso {
		t.Fatalf("result = %+v, want Sucesso=true", result)
	}
	if !strings.Contains(buf.String(), "ADMIN WRITE gerenciar_conta:criar_por_agendamento") {
		t.Fatalf("write not audited: %s", buf.String())
	}
}

func TestGerenciarConta_Pagar_RequiresEveryField(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an incomplete acao=pagar request: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{Acao: "pagar", Confirmacao: true})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarConta error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestGerenciarConta_Pagar_Success_UsesCamelCaseWireFields proves the wire
// body uses the confirmed camelCase field names (invoiceId, movementId,
// ...), not a snake_case guess.
func TestGerenciarConta_Pagar_Success_UsesCamelCaseWireFields(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/pay-movement", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"ok"}`)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{
		Acao: "pagar", Confirmacao: true,
		InvoiceID: intPtr(1), MovementID: intPtr(2), AssociationID: intPtr(3), AccountID: intPtr(4),
		Amount: intPtr(1000), PaymentMethod: intPtr(1), PaymentDate: "2026-01-01", PaymentName: "Pagamento",
	})
	if err != nil {
		t.Fatalf("GerenciarConta: %v", err)
	}
	for _, key := range []string{"invoiceId", "movementId", "associationId", "accountId", "amount", "paymentMethod", "paymentDate", "paymentName"} {
		if _, has := gotBody[key]; !has {
			t.Fatalf("wire body = %+v, missing camelCase key %q", gotBody, key)
		}
	}
	if _, has := gotBody["invoice_id"]; has {
		t.Fatalf("wire body leaked a snake_case invoice_id alongside invoiceId: %+v", gotBody)
	}
}

// TestGerenciarConta_Pagar_AuditsWrite proves the audit line names the
// invoice_id that was paid, not just that some payment happened.
func TestGerenciarConta_Pagar_AuditsWrite(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/pay-movement", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"ok"}`)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{
		Acao: "pagar", Confirmacao: true,
		InvoiceID: intPtr(321), MovementID: intPtr(2), AssociationID: intPtr(3), AccountID: intPtr(4),
		Amount: intPtr(1000), PaymentMethod: intPtr(1), PaymentDate: "2026-01-01", PaymentName: "Pagamento",
	})
	if err != nil {
		t.Fatalf("GerenciarConta: %v", err)
	}
	if !strings.Contains(buf.String(), "ADMIN WRITE gerenciar_conta:pagar — invoice_id=321") {
		t.Fatalf("write not audited with the paid invoice's id: %s", buf.String())
	}
}

func TestGerenciarConta_PagarAgendamento_RequiresEveryField(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an incomplete acao=pagar_agendamento request: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{Acao: "pagar_agendamento", Confirmacao: true})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarConta error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarConta_AtualizarNfse_RequiresInvoiceIDAndNumero(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an incomplete acao=atualizar_nfse request: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{Acao: "atualizar_nfse", Confirmacao: true})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarConta error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarConta_AtualizarNfse_Success(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/update-invoice-nfse-number", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"ok"}`)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{
		Acao: "atualizar_nfse", Confirmacao: true, InvoiceID: intPtr(1), NfseNumero: "12345",
	})
	if err != nil {
		t.Fatalf("GerenciarConta: %v", err)
	}
	if gotBody["invoice_id"] != float64(1) || gotBody["nfse_numero"] != "12345" {
		t.Fatalf("wire body = %+v, want invoice_id/nfse_numero (snake_case)", gotBody)
	}
}

// TestGerenciarConta_AtualizarNfse_AuditsWrite proves the audit line names
// the invoice_id whose NFS-e number was updated, not just that some update
// happened.
func TestGerenciarConta_AtualizarNfse_AuditsWrite(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/financial/update-invoice-nfse-number", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"ok"}`)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarConta(ctxWithToken("tok"), client, GerenciarContaArgs{
		Acao: "atualizar_nfse", Confirmacao: true, InvoiceID: intPtr(654), NfseNumero: "12345",
	})
	if err != nil {
		t.Fatalf("GerenciarConta: %v", err)
	}
	if !strings.Contains(buf.String(), "ADMIN WRITE gerenciar_conta:atualizar_nfse — invoice_id=654") {
		t.Fatalf("write not audited with the updated invoice's id: %s", buf.String())
	}
}
