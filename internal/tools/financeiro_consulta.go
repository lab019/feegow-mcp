package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds consultar_financeiro, the Fase 4b read-side entry point
// into the "Financeiro" endpoint group (~23 endpoints per ESPECIFICACAO.md
// §13) — every LIST/lookup endpoint that isn't a write. The three write
// groups (gerenciar_conta, gerenciar_voucher, remover_registro_financeiro —
// see their own files) stay separate tools specifically because a write
// deserves its own confirmação/auditoria gate, never bundled behind a
// read-shaped tipo switch the way listar_catalogo/consultar_paciente_clinico
// already establish for read-only groupings.
//
// Two tipos map to endpoints Fase 0/4b confirmed DEAD in this environment
// (financial.dmed — 404, and financial.private_table_list — 404, both under
// hosts otherwise reachable) and are intentionally recognized-but-refused:
// see ArgumentIndisponivel. They stay listed in ConsultarFinanceiroTipos so
// a caller reading the tool's schema learns about them instead of getting a
// generic "tipo not recognized" for something the inventário genuinely
// documents.

const (
	tipoFinFornecedores    = "fornecedores"
	tipoFinFornecedor      = "fornecedor"
	tipoFinRepasses        = "repasses"
	tipoFinContas          = "contas"
	tipoFinVendas          = "vendas"
	tipoFinBandeiras       = "bandeiras"
	tipoFinContasCorrentes = "contas_correntes"
	tipoFinCentrosCusto    = "centros_custo"
	tipoFinPlanoContas     = "plano_contas"
	tipoFinInvoicePorNfse  = "invoice_por_nfse"
	tipoFinTabelasPrivadas = "tabelas_privadas" // recognized, deliberately refused — see doc comment above
	tipoFinDmed            = "dmed"             // recognized, deliberately refused — see doc comment above
)

// ConsultarFinanceiroTipos returns every valid `tipo` value, sorted, for use
// in the MCP tool's description/schema and in tests.
func ConsultarFinanceiroTipos() []string {
	return []string{
		tipoFinBandeiras,
		tipoFinCentrosCusto,
		tipoFinContas,
		tipoFinContasCorrentes,
		tipoFinDmed,
		tipoFinFornecedor,
		tipoFinFornecedores,
		tipoFinInvoicePorNfse,
		tipoFinPlanoContas,
		tipoFinRepasses,
		tipoFinTabelasPrivadas,
		tipoFinVendas,
	}
}

// consultarFinanceiroDefaultLimit/MaxLimit bound every tipo that supports
// pagination (contas_correntes, centros_custo, plano_contas) — same
// reasoning and same numbers as buscarPacientesMaxLimit
// (paciente_admin.go): a page size the caller doesn't set gets a sane
// default, and no caller-supplied value can push a single call past the
// ceiling.
const (
	consultarFinanceiroDefaultLimit = 20
	consultarFinanceiroMaxLimit     = 100
)

// ConsultarFinanceiroArgs is consultar_financeiro's argument shape: one tipo
// selects which endpoint answers the call, and the fields below are each
// meaningful only for specific tipos — validated per branch, the same
// pattern ConsultarPacienteClinicoArgs already establishes for this
// package's other large read-side grouping.
type ConsultarFinanceiroArgs struct {
	Tipo string `json:"tipo" jsonschema:"Qual informação financeira consultar: fornecedores, fornecedor, repasses, contas, vendas, bandeiras, contas_correntes, centros_custo, plano_contas, invoice_por_nfse, tabelas_privadas, dmed."`

	FornecedorID *int `json:"fornecedor_id,omitempty" jsonschema:"ID do fornecedor. Obrigatório com tipo=fornecedor."`

	// repasses, contas, vendas
	DataInicio string `json:"data_inicio,omitempty" jsonschema:"Início do filtro por data, ISO-8601 (YYYY-MM-DD). Obrigatório com tipo=repasses, contas, vendas."`
	DataFim    string `json:"data_fim,omitempty" jsonschema:"Fim do filtro por data, ISO-8601 (YYYY-MM-DD). Obrigatório com tipo=repasses, contas, vendas."`

	// contas, vendas
	UnidadeID *int `json:"unidade_id,omitempty" jsonschema:"ID da unidade. Obrigatório com tipo=contas, vendas. 0 = unidade principal — omitir não é o mesmo que 0."`

	// contas
	TipoTransacao string `json:"tipo_transacao,omitempty" jsonschema:"'C' (crédito), 'D' (débito) ou 'T' (todos). Obrigatório com tipo=contas."`

	// contas_correntes, plano_contas (filtro opcional por id)
	FiltroID *int `json:"filtro_id,omitempty" jsonschema:"Filtro opcional por id. Só com tipo=contas_correntes ou tipo=plano_contas."`

	// contas_correntes
	AccountType *int `json:"account_type,omitempty" jsonschema:"Filtro opcional por tipo de conta. Só com tipo=contas_correntes."`

	// plano_contas
	PlanoContasTipo string `json:"plano_contas_tipo,omitempty" jsonschema:"Filtro opcional: \"expense\" ou \"income\". Só com tipo=plano_contas."`

	// contas_correntes, centros_custo, plano_contas (paginação)
	Limit  int `json:"limit,omitempty" jsonschema:"Limite de resultados por página. Default 20, teto 100. Só com tipo=contas_correntes, centros_custo, plano_contas."`
	Offset int `json:"offset,omitempty" jsonschema:"Quantos registros pular antes da página. Só com tipo=contas_correntes, centros_custo, plano_contas."`

	// invoice_por_nfse
	NfseNumero string `json:"nfse_numero,omitempty" jsonschema:"Número da nota fiscal eletrônica. Obrigatório com tipo=invoice_por_nfse."`
}

// ConsultarFinanceiroResult is consultar_financeiro's result: whatever the
// resolved endpoint's content was, passed through as-is — same
// "business/admin blob" choice as CatalogoResult and
// ConsultarPacienteClinicoResult, since the shape varies drastically by
// tipo and this is business data, not patient PII.
type ConsultarFinanceiroResult struct {
	Itens any `json:"itens"`
}

// ConsultarFinanceiro dispatches args.Tipo to the matching Feegow endpoint.
func ConsultarFinanceiro(ctx context.Context, client *feegow.Client, args ConsultarFinanceiroArgs) (*ConsultarFinanceiroResult, error) {
	result, err := consultarFinanceiro(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func consultarFinanceiro(ctx context.Context, client *feegow.Client, args ConsultarFinanceiroArgs) (*ConsultarFinanceiroResult, error) {
	switch args.Tipo {
	case tipoFinFornecedores:
		return finCall(ctx, client, "financial.list_suppliers", feegow.Request{})
	case tipoFinFornecedor:
		if args.FornecedorID == nil || *args.FornecedorID <= 0 {
			return nil, &ArgumentError{Msg: "fornecedor_id é obrigatório para tipo=fornecedor"}
		}
		return finCall(ctx, client, "financial.search_supplier", feegow.Request{
			Params: map[string]any{"fornecedor_id": *args.FornecedorID},
		})
	case tipoFinRepasses:
		start, end, err := requireDateRange(args.DataInicio, args.DataFim, tipoFinRepasses)
		if err != nil {
			return nil, err
		}
		return finCall(ctx, client, "financial.list_medical_transfer", feegow.Request{DateStart: &start, DateEnd: &end})
	case tipoFinContas:
		return consultarFinanceiroContas(ctx, client, args)
	case tipoFinVendas:
		return consultarFinanceiroVendas(ctx, client, args)
	case tipoFinBandeiras:
		return finCall(ctx, client, "financial.credit_card_flags", feegow.Request{})
	case tipoFinContasCorrentes:
		return consultarFinanceiroContasCorrentes(ctx, client, args)
	case tipoFinCentrosCusto:
		limit, offset, err := clampFinanceiroLimit(args.Limit, args.Offset)
		if err != nil {
			return nil, err
		}
		return finCall(ctx, client, "financial.cost_center", feegow.Request{
			Pagination: &feegow.Pagination{Limit: limit, Offset: offset},
		})
	case tipoFinPlanoContas:
		return consultarFinanceiroPlanoContas(ctx, client, args)
	case tipoFinInvoicePorNfse:
		if args.NfseNumero == "" {
			return nil, &ArgumentError{Msg: "nfse_numero é obrigatório para tipo=invoice_por_nfse"}
		}
		return finCall(ctx, client, "financial.find_invoice_by_nfse", feegow.Request{
			Params: map[string]any{"nfse_numero": args.NfseNumero},
		})
	case tipoFinTabelasPrivadas:
		return nil, ArgumentIndisponivel("consultar_financeiro", tipoFinTabelasPrivadas,
			"o endpoint (financial.private_table_list, core.feegow.com) devolveu 404 real na sondagem da Fase 0, mesmo com os parâmetros corretos — rota indisponível nesta licença/ambiente")
	case tipoFinDmed:
		return nil, ArgumentIndisponivel("consultar_financeiro", tipoFinDmed,
			"o endpoint (/financial/dmed) devolveu 404 real na sondagem da Fase 0 — rota ausente/desativada nesta licença/ambiente")
	default:
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"tipo %q não é reconhecido; valores aceitos: %v", args.Tipo, ConsultarFinanceiroTipos(),
		)}
	}
}

func consultarFinanceiroContas(ctx context.Context, client *feegow.Client, args ConsultarFinanceiroArgs) (*ConsultarFinanceiroResult, error) {
	start, end, err := requireDateRange(args.DataInicio, args.DataFim, tipoFinContas)
	if err != nil {
		return nil, err
	}
	if args.UnidadeID == nil {
		return nil, &ArgumentError{Msg: "unidade_id é obrigatório para tipo=contas (0 = unidade principal; omitir não é o mesmo que 0)"}
	}
	switch args.TipoTransacao {
	case "C", "D", "T":
	default:
		return nil, &ArgumentError{Msg: "tipo_transacao é obrigatório para tipo=contas e deve ser 'C', 'D' ou 'T'"}
	}
	return finCall(ctx, client, "financial.list_invoice", feegow.Request{
		DateStart: &start,
		DateEnd:   &end,
		Params: map[string]any{
			"tipo_transacao": args.TipoTransacao,
			"unidade_id":     *args.UnidadeID,
		},
	})
}

func consultarFinanceiroVendas(ctx context.Context, client *feegow.Client, args ConsultarFinanceiroArgs) (*ConsultarFinanceiroResult, error) {
	start, end, err := requireDateRange(args.DataInicio, args.DataFim, tipoFinVendas)
	if err != nil {
		return nil, err
	}
	if args.UnidadeID == nil {
		return nil, &ArgumentError{Msg: "unidade_id é obrigatório para tipo=vendas (0 = unidade principal; omitir não é o mesmo que 0)"}
	}
	return finCall(ctx, client, "financial.list_sales", feegow.Request{
		DateStart: &start,
		DateEnd:   &end,
		Params:    map[string]any{"unidade_id": *args.UnidadeID},
	})
}

func consultarFinanceiroContasCorrentes(ctx context.Context, client *feegow.Client, args ConsultarFinanceiroArgs) (*ConsultarFinanceiroResult, error) {
	limit, offset, err := clampFinanceiroLimit(args.Limit, args.Offset)
	if err != nil {
		return nil, err
	}
	wire := map[string]any{}
	if args.FiltroID != nil {
		wire["id"] = *args.FiltroID
	}
	if args.UnidadeID != nil {
		wire["unity"] = *args.UnidadeID
	}
	if args.AccountType != nil {
		wire["accountType"] = *args.AccountType
	}
	return finCall(ctx, client, "financial.current_accounts", feegow.Request{
		Params:     wire,
		Pagination: &feegow.Pagination{Limit: limit, Offset: offset},
	})
}

func consultarFinanceiroPlanoContas(ctx context.Context, client *feegow.Client, args ConsultarFinanceiroArgs) (*ConsultarFinanceiroResult, error) {
	limit, offset, err := clampFinanceiroLimit(args.Limit, args.Offset)
	if err != nil {
		return nil, err
	}
	wire := map[string]any{}
	if args.FiltroID != nil {
		wire["id"] = *args.FiltroID
	}
	if args.PlanoContasTipo != "" {
		if args.PlanoContasTipo != "expense" && args.PlanoContasTipo != "income" {
			return nil, &ArgumentError{Msg: `plano_contas_tipo deve ser "expense" ou "income"`}
		}
		wire["type"] = args.PlanoContasTipo
	}
	return finCall(ctx, client, "financial.financial_category", feegow.Request{
		Params:     wire,
		Pagination: &feegow.Pagination{Limit: limit, Offset: offset},
	})
}

// requireDateRange validates that both dataInicio/dataFim are present and
// ISO-8601, returning them ready to plug into feegow.Request.DateStart/
// DateEnd. tipo names which tipo the error message should point at.
func requireDateRange(dataInicio, dataFim, tipo string) (string, string, error) {
	if dataInicio == "" || dataFim == "" {
		return "", "", &ArgumentError{Msg: fmt.Sprintf("data_inicio e data_fim são obrigatórios para tipo=%s", tipo)}
	}
	if _, err := time.Parse(feegow.ISO8601, dataInicio); err != nil {
		return "", "", &ArgumentError{Msg: "data_inicio deve estar em ISO-8601 (YYYY-MM-DD)"}
	}
	if _, err := time.Parse(feegow.ISO8601, dataFim); err != nil {
		return "", "", &ArgumentError{Msg: "data_fim deve estar em ISO-8601 (YYYY-MM-DD)"}
	}
	return dataInicio, dataFim, nil
}

// clampFinanceiroLimit normalizes a caller-supplied (limit, offset) pair per
// consultarFinanceiroDefaultLimit/MaxLimit — same shape as buscarPacientes'
// own clamp in paciente_admin.go, extracted here since three tipos in this
// file share it (contas_correntes, centros_custo, plano_contas).
func clampFinanceiroLimit(limit, offset int) (int, int, error) {
	if offset < 0 {
		return 0, 0, &ArgumentError{Msg: "offset não pode ser negativo"}
	}
	switch {
	case limit <= 0:
		limit = consultarFinanceiroDefaultLimit
	case limit > consultarFinanceiroMaxLimit:
		limit = consultarFinanceiroMaxLimit
	}
	return limit, offset, nil
}

// finCall is the shared "call this endpoint, decode its content as-is" tail
// every simple branch above uses — same role as paciente_admin.go's
// callClinico/decodeClinico, one function here since this file only has one
// result type to decode into.
func finCall(ctx context.Context, client *feegow.Client, id feegow.EndpointID, req feegow.Request) (*ConsultarFinanceiroResult, error) {
	resp, err := client.Call(ctx, id, req)
	if err != nil {
		return nil, err
	}
	var itens any
	if err := json.Unmarshal(resp.Content, &itens); err != nil {
		return nil, fmt.Errorf("tools: decoding %s response: %w", id, err)
	}
	return &ConsultarFinanceiroResult{Itens: itens}, nil
}
