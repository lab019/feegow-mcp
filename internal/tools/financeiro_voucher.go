package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds gerenciar_voucher, covering the three Voucher endpoints
// ESPECIFICACAO.md §13 documents (create, cancel, list) under one tool —
// TWO, not three, actually reach Feegow. financial.voucher_create's
// contract could not be confirmed by the Fase 4b smoke test: every attempt
// — GET (a deliberately wrong method), POST with an empty body, POST with a
// plausible {paciente_id, valor, descricao} body — came back as the exact
// same generic HTML 500 page ("Ocorreu um erro"), never once a JSON
// validation error naming a field the way voucher/cancel and every other
// financial write endpoint in this phase did. There is no reliable way to
// tell "wrong field names" apart from "this route is broken in this
// sandbox" from that signal, and this package's rule against inventing an
// unverified contract applies here exactly as it does to
// gerenciar_conta's "associar_conta" (see that file's doc comment). Calling
// gerenciar_voucher with acao="criar" returns a clear ArgumentIndisponivel.
//
// "editar" is NOT one of the acoes below, unlike the task description that
// originally sketched this tool's shape: ESPECIFICACAO.md §13's Voucher
// group lists exactly three endpoints (create, cancel, list) — there is no
// "update voucher" endpoint documented anywhere in the 85-endpoint
// inventory to build one on top of.

const (
	acaoVoucherCriar    = "criar" // recognized, deliberately refused — see doc comment above
	acaoVoucherCancelar = "cancelar"
	acaoVoucherListar   = "listar"
)

// motivosVoucherCancelamento are the exact enum values Fase 4b's smoke test
// confirmed /core/financial/voucher/cancel accepts for "motivo" (a 400
// naming them precisely: "ERRO_EMISSAO, FRAUDE_DETECTADA, DUPLICIDADE,
// OUTRO") — validated client-side before the call, same principle as
// validateAgendamentoID rejecting an obviously-bad id before it ever
// reaches Feegow.
var motivosVoucherCancelamento = map[string]bool{
	"ERRO_EMISSAO":     true,
	"FRAUDE_DETECTADA": true,
	"DUPLICIDADE":      true,
	"OUTRO":            true,
}

// GerenciarVoucherAcoes returns every valid `acao` value, sorted, for use
// in the MCP tool's description/schema and in tests.
func GerenciarVoucherAcoes() []string {
	return []string{acaoVoucherCancelar, acaoVoucherCriar, acaoVoucherListar}
}

// voucherListDefaultLimit/MaxLimit bound acao=listar's pagination — same
// reasoning and numbers as every other paginated admin list in this
// package.
const (
	voucherListDefaultLimit = 20
	voucherListMaxLimit     = 100
)

// GerenciarVoucherArgs is gerenciar_voucher's argument shape.
type GerenciarVoucherArgs struct {
	Acao string `json:"acao" jsonschema:"O que fazer: cancelar, listar. (\"criar\" é reconhecido mas indisponível — ver a descrição da tool.)"`

	// cancelar
	VoucherID    *int   `json:"voucher_id,omitempty" jsonschema:"ID do voucher a cancelar. Obrigatório com acao=cancelar."`
	Motivo       string `json:"motivo,omitempty" jsonschema:"Motivo do cancelamento: ERRO_EMISSAO, FRAUDE_DETECTADA, DUPLICIDADE ou OUTRO. Obrigatório com acao=cancelar."`
	CodigoMotivo string `json:"codigo_motivo,omitempty" jsonschema:"Código/detalhe adicional do motivo. Obrigatório com acao=cancelar."`
	Confirmacao  bool   `json:"confirmacao" jsonschema:"Confirmação EXPLÍCITA do OPERADOR da clínica (nunca assumida pelo agente/modelo). Obrigatória para acao=cancelar; ignorada (não exigida) para acao=listar, que é uma leitura."`

	// listar
	Limit  int `json:"limit,omitempty" jsonschema:"Limite de resultados por página. Default 20, teto 100. Só com acao=listar."`
	Offset int `json:"offset,omitempty" jsonschema:"Quantos registros pular antes da página. Só com acao=listar."`
}

// GerenciarVoucherResult is gerenciar_voucher's result. Itens carries
// acao=listar's page of vouchers; Cancelado is set for acao=cancelar.
type GerenciarVoucherResult struct {
	Itens     any  `json:"itens,omitempty"`
	Cancelado bool `json:"cancelado,omitempty"`
}

// GerenciarVoucher dispatches args.Acao to the matching Feegow endpoint.
func GerenciarVoucher(ctx context.Context, client *feegow.Client, args GerenciarVoucherArgs) (*GerenciarVoucherResult, error) {
	result, err := gerenciarVoucher(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func gerenciarVoucher(ctx context.Context, client *feegow.Client, args GerenciarVoucherArgs) (*GerenciarVoucherResult, error) {
	switch args.Acao {
	case acaoVoucherCriar:
		return nil, ArgumentIndisponivel("gerenciar_voucher", acaoVoucherCriar,
			"a Fase 4b tentou GET, POST vazio e POST com um corpo plausível contra "+
				"/core/financial/voucher/create e todas as tentativas devolveram o mesmo erro 500 "+
				"genérico do Feegow, nunca uma validação nomeando campos — o contrato não pôde ser confirmado")
	case acaoVoucherCancelar:
		return gerenciarVoucherCancelar(ctx, client, args)
	case acaoVoucherListar:
		return gerenciarVoucherListar(ctx, client, args)
	default:
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"acao %q não é reconhecida; valores aceitos: %v", args.Acao, GerenciarVoucherAcoes(),
		)}
	}
}

func gerenciarVoucherCancelar(ctx context.Context, client *feegow.Client, args GerenciarVoucherArgs) (*GerenciarVoucherResult, error) {
	if err := requireConfirmacaoOperador(args.Confirmacao, "gerenciar_voucher (acao=cancelar)"); err != nil {
		return nil, err
	}
	if args.VoucherID == nil || *args.VoucherID <= 0 {
		return nil, &ArgumentError{Msg: "voucher_id é obrigatório para acao=cancelar e deve ser um identificador positivo"}
	}
	if !motivosVoucherCancelamento[args.Motivo] {
		return nil, &ArgumentError{Msg: "motivo é obrigatório para acao=cancelar e deve ser um de: ERRO_EMISSAO, FRAUDE_DETECTADA, DUPLICIDADE, OUTRO"}
	}
	if args.CodigoMotivo == "" {
		return nil, &ArgumentError{Msg: "codigo_motivo é obrigatório para acao=cancelar"}
	}

	// financial.voucher_cancel's success shape was never observed by the
	// Fase 4b smoke test (cancelling a real voucher requires one to exist,
	// and voucher/create is unusable in this sandbox — see this file's doc
	// comment). Unlike financial.pay_movement/pay_booking (EnvelopeStandard,
	// checked upstream by client.Call), this endpoint is EnvelopeNone: the
	// same family already demonstrated the "HTTP 200 + {"success":false,...}"
	// business-failure pattern twice (financial.create_account,
	// financial.invoice_remove), so "client.Call returned no transport
	// error" is NOT proof of success here — trusting it would repeat
	// exactly the gap those two closed. Decode the same defensive way: a
	// response with no "success" field decodes Success to its zero value
	// (false) and fails closed rather than reading an unrecognized shape as
	// a silent success.
	resp, err := client.Call(ctx, "financial.voucher_cancel", feegow.Request{
		Params: map[string]any{
			"id":            *args.VoucherID,
			"motivo":        args.Motivo,
			"codigo_motivo": args.CodigoMotivo,
		},
	})
	if err != nil {
		return nil, err
	}

	var body struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(resp.Content, &body); err != nil {
		return nil, fmt.Errorf("tools: decoding core/financial/voucher/cancel response: %w", err)
	}
	if err := checkEnvelopeNoneSuccess(body.Success); err != nil {
		return nil, err
	}

	auditAdminWriteRecord("gerenciar_voucher:cancelar", "voucher_id", *args.VoucherID)
	return &GerenciarVoucherResult{Cancelado: true}, nil
}

func gerenciarVoucherListar(ctx context.Context, client *feegow.Client, args GerenciarVoucherArgs) (*GerenciarVoucherResult, error) {
	if args.Offset < 0 {
		return nil, &ArgumentError{Msg: "offset não pode ser negativo"}
	}
	limit := args.Limit
	switch {
	case limit <= 0:
		limit = voucherListDefaultLimit
	case limit > voucherListMaxLimit:
		limit = voucherListMaxLimit
	}

	resp, err := client.Call(ctx, "financial.voucher_list", feegow.Request{
		Pagination: &feegow.Pagination{Limit: limit, Offset: args.Offset},
	})
	if err != nil {
		return nil, err
	}
	var itens any
	if err := json.Unmarshal(resp.Content, &itens); err != nil {
		return nil, fmt.Errorf("tools: decoding core/financial/voucher/list response: %w", err)
	}
	return &GerenciarVoucherResult{Itens: itens}, nil
}
