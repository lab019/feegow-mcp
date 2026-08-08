package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds remover_registro_financeiro — deliberately its OWN tool,
// not a branch of gerenciar_conta, per this Fase's explicit instruction:
// /core/financial/invoice/remove and /core/financial/payment/remove are the
// two most destructive writes in the entire project. Both DELETE a
// financial record from the clínica's own Feegow instance; neither has a
// documented "undo" anywhere in the 85-endpoint inventory. Isolating them in
// their own tool, instead of one more acao alongside atualizar_nfse/pagar in
// gerenciar_conta, keeps them from being one careless acao string away from
// an everyday operation an agent might call routinely.
//
// Both actions require requireConfirmacaoReforcada (guardas.go), not the
// ordinary single confirmacao=true every other admin write in this package
// accepts: a SECOND, independently-named boolean (ciente_irreversivel) that
// means "I understand this cannot be undone", checked after confirmacao but
// still before either id is validated — an unconfirmed call must never leak
// which id would have been targeted.

const (
	tipoRemocaoFatura    = "fatura"
	tipoRemocaoPagamento = "pagamento"
)

// RemoverRegistroFinanceiroTipos returns every valid `tipo` value, sorted,
// for use in the MCP tool's description/schema and in tests.
func RemoverRegistroFinanceiroTipos() []string {
	return []string{tipoRemocaoFatura, tipoRemocaoPagamento}
}

// RemoverRegistroFinanceiroArgs is remover_registro_financeiro's argument
// shape. InvoiceID applies to tipo=fatura, PaymentID to tipo=pagamento —
// never both at once, matched to the shape each underlying DELETE endpoint
// actually takes (financial.invoice_remove: invoiceId; financial.payment_remove:
// paymentId — confirmed by the Fase 4b empty-body probe, see their Registry
// Notes).
type RemoverRegistroFinanceiroArgs struct {
	Tipo string `json:"tipo" jsonschema:"O que remover: fatura ou pagamento."`

	InvoiceID *int `json:"invoice_id,omitempty" jsonschema:"ID da fatura (invoice) a remover. Obrigatório com tipo=fatura."`
	PaymentID *int `json:"payment_id,omitempty" jsonschema:"ID do pagamento a remover. Obrigatório com tipo=pagamento."`

	Confirmacao        bool `json:"confirmacao" jsonschema:"Confirmação EXPLÍCITA do OPERADOR da clínica (nunca assumida pelo agente/modelo) de que deseja remover este registro financeiro."`
	CienteIrreversivel bool `json:"ciente_irreversivel" jsonschema:"SEGUNDA confirmação, independente de \"confirmacao\": true afirma que o operador está CIENTE de que esta remoção é PERMANENTE — apaga o registro financeiro do cliente sem nenhuma forma de desfazer documentada na API da Feegow."`
}

// RemoverRegistroFinanceiroResult is remover_registro_financeiro's result.
type RemoverRegistroFinanceiroResult struct {
	Removido bool `json:"removido"`
}

// RemoverRegistroFinanceiro dispatches args.Tipo to the matching Feegow
// DELETE endpoint. IRREVERSÍVEL: ver o doc comment deste arquivo.
func RemoverRegistroFinanceiro(ctx context.Context, client *feegow.Client, args RemoverRegistroFinanceiroArgs) (*RemoverRegistroFinanceiroResult, error) {
	result, err := removerRegistroFinanceiro(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func removerRegistroFinanceiro(ctx context.Context, client *feegow.Client, args RemoverRegistroFinanceiroArgs) (*RemoverRegistroFinanceiroResult, error) {
	if err := requireConfirmacaoReforcada(args.Confirmacao, args.CienteIrreversivel, "remover_registro_financeiro"); err != nil {
		return nil, err
	}

	switch args.Tipo {
	case tipoRemocaoFatura:
		return removerFatura(ctx, client, args)
	case tipoRemocaoPagamento:
		return removerPagamento(ctx, client, args)
	default:
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"tipo %q não é reconhecido; valores aceitos: %v", args.Tipo, RemoverRegistroFinanceiroTipos(),
		)}
	}
}

func removerFatura(ctx context.Context, client *feegow.Client, args RemoverRegistroFinanceiroArgs) (*RemoverRegistroFinanceiroResult, error) {
	if args.InvoiceID == nil || *args.InvoiceID <= 0 {
		return nil, &ArgumentError{Msg: "invoice_id é obrigatório para tipo=fatura e deve ser um identificador positivo"}
	}
	return doRemove(ctx, client, "financial.invoice_remove", "invoiceId", *args.InvoiceID, "remover_registro_financeiro:fatura")
}

func removerPagamento(ctx context.Context, client *feegow.Client, args RemoverRegistroFinanceiroArgs) (*RemoverRegistroFinanceiroResult, error) {
	if args.PaymentID == nil || *args.PaymentID <= 0 {
		return nil, &ArgumentError{Msg: "payment_id é obrigatório para tipo=pagamento e deve ser um identificador positivo"}
	}
	return doRemove(ctx, client, "financial.payment_remove", "paymentId", *args.PaymentID, "remover_registro_financeiro:pagamento")
}

// doRemove is the shared DELETE call both branches make: same wire shape
// ({success,message}, EnvelopeNone — see financial.invoice_remove's Registry
// Notes), just a different endpoint id and field name. The audit line
// carries recordID (labeled with wireField, e.g. "invoiceId=4711") — this is
// the most destructive write in the package, so the log MUST say which
// fatura/pagamento was removed, not just that a removal happened.
func doRemove(ctx context.Context, client *feegow.Client, id feegow.EndpointID, wireField string, recordID int, auditTool string) (*RemoverRegistroFinanceiroResult, error) {
	resp, err := client.Call(ctx, id, feegow.Request{
		Params: map[string]any{wireField: recordID},
	})
	if err != nil {
		return nil, err
	}

	var body struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(resp.Content, &body); err != nil {
		return nil, fmt.Errorf("tools: decoding %s response: %w", id, err)
	}
	if err := checkEnvelopeNoneSuccess(body.Success); err != nil {
		return nil, err
	}

	auditAdminWriteRecord(auditTool, wireField, recordID)
	return &RemoverRegistroFinanceiroResult{Removido: true}, nil
}
