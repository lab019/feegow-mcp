package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds gerenciar_conta, the write-side counterpart to
// consultar_financeiro for everything that creates or settles a "conta"
// (Feegow's term for an invoice/financial-account record): creating one
// from scratch, creating one from an agendamento, paying one directly or
// via an agendamento, and updating its NFS-e number. Every acao requires
// requireConfirmacaoOperador — the ordinary "escrita com confirmação
// explícita" discipline every admin write tool in this package follows —
// EXCEPT the two removal operations, which live in their own
// remover_registro_financeiro tool with a stronger, reinforced confirmation
// (see that file's doc comment for why a single confirmacao=true is not
// enough friction for an irreversible delete).
//
// "associar_conta" (Associação de conta financeira,
// /core/financial/account/association) is conspicuously NOT one of the
// acoes below. The Fase 4b smoke test tried three different request bodies
// against it — empty, {"id":1}, {"accountId":1,"unity":1} — and all three
// returned the exact same 400 "Account not found", never once naming a
// required field the way every other endpoint in this file's group does on
// a bad request. There was no way to learn this endpoint's real body
// contract from the sandbox, and inventing field names to wire up anyway
// would be exactly the kind of unverified guess ESPECIFICACAO.md's rule
// (never infer a contract by analogy) exists to prevent. Calling
// gerenciar_conta with acao="associar_conta" returns a clear
// ArgumentIndisponivel instead of a call that would certainly fail against
// a contract nobody confirmed.

const (
	acaoContaCriar               = "criar"
	acaoContaCriarPorAgendamento = "criar_por_agendamento"
	acaoContaPagar               = "pagar"
	acaoContaPagarAgendamento    = "pagar_agendamento"
	acaoContaAtualizarNfse       = "atualizar_nfse"
	acaoContaAssociar            = "associar_conta" // recognized, deliberately refused — see doc comment above
)

// GerenciarContaAcoes returns every valid `acao` value, sorted, for use in
// the MCP tool's description/schema and in tests.
func GerenciarContaAcoes() []string {
	return []string{
		acaoContaAssociar,
		acaoContaAtualizarNfse,
		acaoContaCriar,
		acaoContaCriarPorAgendamento,
		acaoContaPagar,
		acaoContaPagarAgendamento,
	}
}

// GerenciarContaArgs is gerenciar_conta's argument shape: one acao selects
// which endpoint the call resolves to, and every field below is meaningful
// only for the acao(s) noted in its own description — validated per branch,
// same pattern as ConsultarPacienteClinicoArgs/ConsultarFinanceiroArgs.
type GerenciarContaArgs struct {
	Acao string `json:"acao" jsonschema:"O que fazer: criar, criar_por_agendamento, pagar, pagar_agendamento, atualizar_nfse."`

	// criar (/core/financial/invoice/create) — os campos de topo do corpo
	// confirmados pela Fase 4b (ver financial.invoice_create no registry).
	// Account/Items/Installments são passados como JSON livre: a Fase 4b
	// não conseguiu confirmar a forma INTERNA desses três campos sem
	// arriscar uma criação real — ver o Notes do endpoint.
	Type         string `json:"type,omitempty" jsonschema:"'C' (crédito) ou 'D' (débito). Obrigatório com acao=criar."`
	Date         string `json:"date,omitempty" jsonschema:"Data da conta, ISO-8601 (YYYY-MM-DD). Obrigatório com acao=criar."`
	Table        *int   `json:"table,omitempty" jsonschema:"ID da tabela particular. Obrigatório com acao=criar."`
	User         *int   `json:"user,omitempty" jsonschema:"ID do usuário Feegow responsável. Obrigatório com acao=criar."`
	Unity        *int   `json:"unity,omitempty" jsonschema:"ID da unidade. Obrigatório com acao=criar."`
	Account      any    `json:"account,omitempty" jsonschema:"Objeto da conta corrente/associação — estrutura interna NÃO confirmada pela Fase 4b (ver docs/feegow-api.md, financial.invoice_create). Obrigatório (não-vazio) com acao=criar."`
	Items        []any  `json:"items,omitempty" jsonschema:"Itens da conta — estrutura interna NÃO confirmada pela Fase 4b. Obrigatório (>=1 item) com acao=criar."`
	Installments []any  `json:"installments,omitempty" jsonschema:"Parcelas da conta — estrutura interna NÃO confirmada pela Fase 4b. Obrigatório (>=1 item) com acao=criar."`

	// criar_por_agendamento (/financial/create-account)
	AgendamentoID *int `json:"agendamento_id,omitempty" jsonschema:"ID do agendamento a partir do qual criar a conta. Obrigatório com acao=criar_por_agendamento."`

	// pagar (/financial/pay-movement) e pagar_agendamento (/financial/pay-booking)
	InvoiceID     *int   `json:"invoice_id,omitempty" jsonschema:"ID da invoice a pagar. Obrigatório com acao=pagar."`
	MovementID    *int   `json:"movement_id,omitempty" jsonschema:"ID do movimento. Obrigatório com acao=pagar."`
	BookingID     *int   `json:"booking_id,omitempty" jsonschema:"ID do agendamento a pagar. Obrigatório com acao=pagar_agendamento."`
	AssociationID *int   `json:"association_id,omitempty" jsonschema:"ID da associação de conta. Obrigatório com acao=pagar e acao=pagar_agendamento."`
	AccountID     *int   `json:"account_id,omitempty" jsonschema:"ID da conta corrente. Obrigatório com acao=pagar e acao=pagar_agendamento."`
	Amount        *int   `json:"amount,omitempty" jsonschema:"Valor a pagar, em centavos. Obrigatório com acao=pagar e acao=pagar_agendamento."`
	PaymentMethod *int   `json:"payment_method,omitempty" jsonschema:"ID do método de pagamento (ver consultar_financeiro tipo=bandeiras para bandeiras de cartão). Obrigatório com acao=pagar e acao=pagar_agendamento."`
	PaymentDate   string `json:"payment_date,omitempty" jsonschema:"Data do pagamento, ISO-8601 (YYYY-MM-DD). Obrigatório com acao=pagar e acao=pagar_agendamento."`
	PaymentName   string `json:"payment_name,omitempty" jsonschema:"Nome/descrição do pagamento. Obrigatório com acao=pagar."`

	// atualizar_nfse (/financial/update-invoice-nfse-number)
	NfseNumero string `json:"nfse_numero,omitempty" jsonschema:"Novo número da nota fiscal eletrônica. Obrigatório com acao=atualizar_nfse (junto com invoice_id)."`

	Confirmacao bool `json:"confirmacao" jsonschema:"Confirmação EXPLÍCITA do OPERADOR da clínica (nunca assumida pelo agente/modelo) de que deseja executar esta ação financeira."`
}

// GerenciarContaResult is gerenciar_conta's result. Detalhe carries whatever
// free-form message/content Feegow returned alongside the outcome (e.g.
// create-account's "msg" when agendamento_id doesn't resolve to a real
// agendamento) — business text, not patient PII, same "pass it through"
// choice ConsultarFinanceiroResult already makes for reads.
type GerenciarContaResult struct {
	Sucesso bool `json:"sucesso"`
	Detalhe any  `json:"detalhe,omitempty"`
}

// GerenciarConta dispatches args.Acao to the matching Feegow endpoint.
func GerenciarConta(ctx context.Context, client *feegow.Client, args GerenciarContaArgs) (*GerenciarContaResult, error) {
	result, err := gerenciarConta(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func gerenciarConta(ctx context.Context, client *feegow.Client, args GerenciarContaArgs) (*GerenciarContaResult, error) {
	// Confirmação vem antes de QUALQUER outra checagem, para todas as
	// ações — mesma disciplina de requireConfirmacao: uma chamada não
	// confirmada nunca deve vazar qual outro argumento estava errado.
	if err := requireConfirmacaoOperador(args.Confirmacao, "gerenciar_conta"); err != nil {
		return nil, err
	}

	switch args.Acao {
	case acaoContaCriar:
		return gerenciarContaCriar(ctx, client, args)
	case acaoContaCriarPorAgendamento:
		return gerenciarContaCriarPorAgendamento(ctx, client, args)
	case acaoContaPagar:
		return gerenciarContaPagar(ctx, client, args)
	case acaoContaPagarAgendamento:
		return gerenciarContaPagarAgendamento(ctx, client, args)
	case acaoContaAtualizarNfse:
		return gerenciarContaAtualizarNfse(ctx, client, args)
	case acaoContaAssociar:
		return nil, ArgumentIndisponivel("gerenciar_conta", acaoContaAssociar,
			"a Fase 4b tentou três corpos diferentes contra /core/financial/account/association e todos "+
				"devolveram o mesmo erro genérico \"Account not found\", sem nomear nenhum campo obrigatório — "+
				"o contrato do corpo não pôde ser confirmado")
	default:
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"acao %q não é reconhecida; valores aceitos: %v", args.Acao, GerenciarContaAcoes(),
		)}
	}
}

func gerenciarContaCriar(ctx context.Context, client *feegow.Client, args GerenciarContaArgs) (*GerenciarContaResult, error) {
	if args.Type != "C" && args.Type != "D" {
		return nil, &ArgumentError{Msg: "type é obrigatório para acao=criar e deve ser 'C' ou 'D'"}
	}
	if args.Date == "" {
		return nil, &ArgumentError{Msg: "date é obrigatório para acao=criar (ISO-8601 YYYY-MM-DD)"}
	}
	if args.Table == nil || args.User == nil || args.Unity == nil {
		return nil, &ArgumentError{Msg: "table, user e unity são todos obrigatórios para acao=criar"}
	}
	if args.Account == nil {
		return nil, &ArgumentError{Msg: "account é obrigatório (objeto não-vazio) para acao=criar"}
	}
	if len(args.Items) == 0 {
		return nil, &ArgumentError{Msg: "items é obrigatório (pelo menos 1 item) para acao=criar"}
	}
	if len(args.Installments) == 0 {
		return nil, &ArgumentError{Msg: "installments é obrigatório (pelo menos 1 item) para acao=criar"}
	}

	if _, err := client.Call(ctx, "financial.invoice_create", feegow.Request{
		Params: map[string]any{
			"type":         args.Type,
			"date":         args.Date,
			"table":        *args.Table,
			"user":         *args.User,
			"unity":        *args.Unity,
			"account":      args.Account,
			"items":        args.Items,
			"installments": args.Installments,
		},
	}); err != nil {
		return nil, err
	}
	auditAdminWrite("gerenciar_conta:criar", 0, 0)
	return &GerenciarContaResult{Sucesso: true}, nil
}

func gerenciarContaCriarPorAgendamento(ctx context.Context, client *feegow.Client, args GerenciarContaArgs) (*GerenciarContaResult, error) {
	if args.AgendamentoID == nil || *args.AgendamentoID <= 0 {
		return nil, &ArgumentError{Msg: "agendamento_id é obrigatório para acao=criar_por_agendamento e deve ser um identificador positivo"}
	}

	resp, err := client.Call(ctx, "financial.create_account", feegow.Request{
		Params: map[string]any{"agendamento_id": *args.AgendamentoID},
	})
	if err != nil {
		return nil, err
	}

	// EnvelopeNone: a resposta usa "msg", não "content" — ver o Notes de
	// financial.create_account no registry para o porquê.
	var body struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(resp.Content, &body); err != nil {
		return nil, fmt.Errorf("tools: decoding financial/create-account response: %w", err)
	}
	if err := checkEnvelopeNoneSuccess(body.Success); err != nil {
		return nil, err
	}

	auditAdminWrite("gerenciar_conta:criar_por_agendamento", 0, *args.AgendamentoID)
	return &GerenciarContaResult{Sucesso: true, Detalhe: body.Msg}, nil
}

func gerenciarContaPagar(ctx context.Context, client *feegow.Client, args GerenciarContaArgs) (*GerenciarContaResult, error) {
	if args.InvoiceID == nil || args.MovementID == nil || args.AssociationID == nil || args.AccountID == nil {
		return nil, &ArgumentError{Msg: "invoice_id, movement_id, association_id e account_id são todos obrigatórios para acao=pagar"}
	}
	if args.Amount == nil {
		return nil, &ArgumentError{Msg: "amount é obrigatório para acao=pagar (valor em centavos)"}
	}
	if args.PaymentMethod == nil {
		return nil, &ArgumentError{Msg: "payment_method é obrigatório para acao=pagar"}
	}
	if args.PaymentDate == "" {
		return nil, &ArgumentError{Msg: "payment_date é obrigatório para acao=pagar (ISO-8601 YYYY-MM-DD)"}
	}
	if args.PaymentName == "" {
		return nil, &ArgumentError{Msg: "payment_name é obrigatório para acao=pagar"}
	}

	if _, err := client.Call(ctx, "financial.pay_movement", feegow.Request{
		Params: map[string]any{
			"invoiceId":     *args.InvoiceID,
			"movementId":    *args.MovementID,
			"associationId": *args.AssociationID,
			"accountId":     *args.AccountID,
			"amount":        *args.Amount,
			"paymentMethod": *args.PaymentMethod,
			"paymentDate":   args.PaymentDate,
			"paymentName":   args.PaymentName,
		},
	}); err != nil {
		return nil, err
	}
	auditAdminWrite("gerenciar_conta:pagar", 0, 0)
	return &GerenciarContaResult{Sucesso: true}, nil
}

func gerenciarContaPagarAgendamento(ctx context.Context, client *feegow.Client, args GerenciarContaArgs) (*GerenciarContaResult, error) {
	if args.BookingID == nil || *args.BookingID <= 0 {
		return nil, &ArgumentError{Msg: "booking_id é obrigatório para acao=pagar_agendamento e deve ser um identificador positivo"}
	}
	if args.AssociationID == nil || args.AccountID == nil {
		return nil, &ArgumentError{Msg: "association_id e account_id são obrigatórios para acao=pagar_agendamento"}
	}
	if args.Amount == nil {
		return nil, &ArgumentError{Msg: "amount é obrigatório para acao=pagar_agendamento (valor em centavos)"}
	}
	if args.PaymentMethod == nil {
		return nil, &ArgumentError{Msg: "payment_method é obrigatório para acao=pagar_agendamento"}
	}
	if args.PaymentDate == "" {
		return nil, &ArgumentError{Msg: "payment_date é obrigatório para acao=pagar_agendamento (ISO-8601 YYYY-MM-DD)"}
	}

	if _, err := client.Call(ctx, "financial.pay_booking", feegow.Request{
		Params: map[string]any{
			"bookingId":     *args.BookingID,
			"associationId": *args.AssociationID,
			"accountId":     *args.AccountID,
			"amount":        *args.Amount,
			"paymentMethod": *args.PaymentMethod,
			"paymentDate":   args.PaymentDate,
		},
	}); err != nil {
		return nil, err
	}
	auditAdminWrite("gerenciar_conta:pagar_agendamento", 0, *args.BookingID)
	return &GerenciarContaResult{Sucesso: true}, nil
}

func gerenciarContaAtualizarNfse(ctx context.Context, client *feegow.Client, args GerenciarContaArgs) (*GerenciarContaResult, error) {
	if args.InvoiceID == nil || *args.InvoiceID <= 0 {
		return nil, &ArgumentError{Msg: "invoice_id é obrigatório para acao=atualizar_nfse"}
	}
	if args.NfseNumero == "" {
		return nil, &ArgumentError{Msg: "nfse_numero é obrigatório para acao=atualizar_nfse"}
	}

	if _, err := client.Call(ctx, "financial.update_invoice_nfse", feegow.Request{
		Params: map[string]any{
			"invoice_id":  *args.InvoiceID,
			"nfse_numero": args.NfseNumero,
		},
	}); err != nil {
		return nil, err
	}
	auditAdminWrite("gerenciar_conta:atualizar_nfse", 0, 0)
	return &GerenciarContaResult{Sucesso: true}, nil
}
