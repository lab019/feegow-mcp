package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds the Estoque group's two tools: consultar_estoque (reads —
// posição de produtos, lista de produtos) and movimentar_estoque (writes).
// Of the 7 endpoints ESPECIFICACAO.md §13 lists for this group, only 3 are
// reachable at all: /core/financial/base/product/position,
// /core/financial/base/product/list and
// /core/financial/financial-stock/product/insert all live under HostAPI
// (api.feegow.com/v1/api), which resolves. The other 4 — product entry,
// movement, exit, and location list — all live under
// financial2/external/financial-stock/... on core.feegow.com.br (HostCoreBR),
// confirmed DEAD by the Fase 0 smoke test (the host itself doesn't resolve
// in this environment — see stock.location_list's Registry Notes). Every
// acao in movimentar_estoque that would target one of those four is
// recognized-but-refused via ArgumentIndisponivel — never a call that
// certainly fails against a host that never responds.

const (
	tipoEstoquePosicao = "posicao"
	tipoEstoqueLista   = "lista_produtos"
)

// ConsultarEstoqueTipos returns every valid `tipo` value, sorted, for use in
// the MCP tool's description/schema and in tests.
func ConsultarEstoqueTipos() []string {
	return []string{tipoEstoqueLista, tipoEstoquePosicao}
}

// consultarEstoqueDefaultLimit/MaxLimit bound both tipos' pagination — same
// reasoning as every other paginated admin list in this package. Feegow's
// own default (perPage=100, confirmed by the Fase 4b smoke test) is
// deliberately NOT reused as this tool's default: 20 keeps a single call's
// payload predictable, matching the rest of the admin surface rather than
// Feegow's own, endpoint-specific default.
const (
	consultarEstoqueDefaultLimit = 20
	consultarEstoqueMaxLimit     = 100
)

// ConsultarEstoqueArgs is consultar_estoque's argument shape. The filter
// field NAMES genuinely differ between the two tipos (confirmed by the Fase
// 4b smoke test's foundParameters echo) — posicao uses Portuguese names,
// lista_produtos uses English ones — kept as separate fields below rather
// than unified, to avoid inventing a shared name neither endpoint actually
// accepts.
type ConsultarEstoqueArgs struct {
	Tipo string `json:"tipo" jsonschema:"O que consultar: posicao, lista_produtos."`

	// posicao (/core/financial/base/product/position)
	Fabricante  string `json:"fabricante,omitempty" jsonschema:"Filtro por fabricante. Só com tipo=posicao."`
	Produto     string `json:"produto,omitempty" jsonschema:"Filtro por produto. Só com tipo=posicao."`
	Categoria   string `json:"categoria,omitempty" jsonschema:"Filtro por categoria. Só com tipo=posicao."`
	Localizacao string `json:"localizacao,omitempty" jsonschema:"Filtro por localização. Só com tipo=posicao."`
	DataInicio  string `json:"data_inicio,omitempty" jsonschema:"Filtro por data (formato não confirmado pela Fase 4b — repassado como texto livre). Só com tipo=posicao."`
	DataFim     string `json:"data_fim,omitempty" jsonschema:"Filtro por data (formato não confirmado pela Fase 4b — repassado como texto livre). Só com tipo=posicao."`

	// lista_produtos (/core/financial/base/product/list)
	ProdutoID   *int   `json:"produto_id,omitempty" jsonschema:"Filtro por id do produto. Só com tipo=lista_produtos."`
	Category    string `json:"category,omitempty" jsonschema:"Filtro por categoria (nome do campo em inglês nesta rota — diferente de tipo=posicao). Só com tipo=lista_produtos."`
	Location    string `json:"location,omitempty" jsonschema:"Filtro por localização. Só com tipo=lista_produtos."`
	Producer    string `json:"producer,omitempty" jsonschema:"Filtro por fabricante. Só com tipo=lista_produtos."`
	ProductType string `json:"product_type,omitempty" jsonschema:"Filtro por tipo de produto. Só com tipo=lista_produtos."`

	Limit  int `json:"limit,omitempty" jsonschema:"Limite de resultados por página. Default 20, teto 100."`
	Offset int `json:"offset,omitempty" jsonschema:"Quantos registros pular antes da página."`
}

// ConsultarEstoqueResult is consultar_estoque's result: whatever the
// resolved endpoint's raw body was, passed through as-is — same
// "business/admin blob" choice as ConsultarFinanceiroResult.
type ConsultarEstoqueResult struct {
	Itens any `json:"itens"`
}

// ConsultarEstoque dispatches args.Tipo to the matching Feegow endpoint.
func ConsultarEstoque(ctx context.Context, client *feegow.Client, args ConsultarEstoqueArgs) (*ConsultarEstoqueResult, error) {
	result, err := consultarEstoque(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func consultarEstoque(ctx context.Context, client *feegow.Client, args ConsultarEstoqueArgs) (*ConsultarEstoqueResult, error) {
	limit, offset, err := clampEstoqueLimit(args.Limit, args.Offset)
	if err != nil {
		return nil, err
	}

	switch args.Tipo {
	case tipoEstoquePosicao:
		wire := map[string]any{}
		if args.Fabricante != "" {
			wire["fabricante"] = args.Fabricante
		}
		if args.Produto != "" {
			wire["produto"] = args.Produto
		}
		if args.Categoria != "" {
			wire["categoria"] = args.Categoria
		}
		if args.Localizacao != "" {
			wire["localizacao"] = args.Localizacao
		}
		if args.DataInicio != "" {
			wire["dataInicio"] = args.DataInicio
		}
		if args.DataFim != "" {
			wire["dataFim"] = args.DataFim
		}
		return estoqueCall(ctx, client, "stock.product_position", wire, limit, offset)

	case tipoEstoqueLista:
		wire := map[string]any{}
		if args.ProdutoID != nil {
			wire["id"] = *args.ProdutoID
		}
		if args.Category != "" {
			wire["category"] = args.Category
		}
		if args.Location != "" {
			wire["location"] = args.Location
		}
		if args.Producer != "" {
			wire["producer"] = args.Producer
		}
		if args.ProductType != "" {
			wire["type"] = args.ProductType
		}
		return estoqueCall(ctx, client, "stock.product_list", wire, limit, offset)

	default:
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"tipo %q não é reconhecido; valores aceitos: %v", args.Tipo, ConsultarEstoqueTipos(),
		)}
	}
}

func clampEstoqueLimit(limit, offset int) (int, int, error) {
	if offset < 0 {
		return 0, 0, &ArgumentError{Msg: "offset não pode ser negativo"}
	}
	switch {
	case limit <= 0:
		limit = consultarEstoqueDefaultLimit
	case limit > consultarEstoqueMaxLimit:
		limit = consultarEstoqueMaxLimit
	}
	return limit, offset, nil
}

func estoqueCall(ctx context.Context, client *feegow.Client, id feegow.EndpointID, wire map[string]any, limit, offset int) (*ConsultarEstoqueResult, error) {
	resp, err := client.Call(ctx, id, feegow.Request{
		Params:     wire,
		Pagination: &feegow.Pagination{Limit: limit, Offset: offset},
	})
	if err != nil {
		return nil, err
	}
	var itens any
	if err := json.Unmarshal(resp.Content, &itens); err != nil {
		return nil, fmt.Errorf("tools: decoding %s response: %w", id, err)
	}
	return &ConsultarEstoqueResult{Itens: itens}, nil
}

// --- movimentar_estoque -----------------------------------------------

const (
	acaoEstoqueInserirProduto = "inserir_produto"
	acaoEstoqueEntrada        = "entrada"      // recognized, deliberately refused — dead host, see doc comment above
	acaoEstoqueSaida          = "saida"        // recognized, deliberately refused — dead host, see doc comment above
	acaoEstoqueMovimentacao   = "movimentacao" // recognized, deliberately refused — dead host, see doc comment above
)

// MovimentarEstoqueAcoes returns every valid `acao` value, sorted, for use
// in the MCP tool's description/schema and in tests.
func MovimentarEstoqueAcoes() []string {
	return []string{acaoEstoqueEntrada, acaoEstoqueInserirProduto, acaoEstoqueMovimentacao, acaoEstoqueSaida}
}

// MovimentarEstoqueArgs is movimentar_estoque's argument shape. Every field
// below (besides Confirmacao) maps 1:1 to stock.product_insert's 11
// confirmed required fields (see its Registry Notes) — all integers, no
// product name/description field is accepted by this endpoint at all
// (confirmed by a real, successful insert during the Fase 4b smoke test).
type MovimentarEstoqueArgs struct {
	Acao string `json:"acao" jsonschema:"O que fazer: inserir_produto. (entrada, saida e movimentacao são reconhecidas mas indisponíveis — ver a descrição da tool.)"`

	TipoProduto            *int `json:"tipo_produto,omitempty" jsonschema:"Tipo do produto. Obrigatório com acao=inserir_produto."`
	CategoriaID            *int `json:"categoria_id,omitempty" jsonschema:"ID da categoria. Obrigatório com acao=inserir_produto."`
	FabricanteID           *int `json:"fabricante_id,omitempty" jsonschema:"ID do fabricante. Obrigatório com acao=inserir_produto."`
	LocalizacaoID          *int `json:"localizacao_id,omitempty" jsonschema:"ID da localização. Obrigatório com acao=inserir_produto."`
	DiasAvisoValidade      *int `json:"dias_aviso_validade,omitempty" jsonschema:"Dias de antecedência para aviso de validade. Obrigatório com acao=inserir_produto."`
	ApresentacaoQuantidade *int `json:"apresentacao_quantidade,omitempty" jsonschema:"Quantidade da apresentação. Obrigatório com acao=inserir_produto."`
	ApresentacaoUnidade    *int `json:"apresentacao_unidade,omitempty" jsonschema:"ID da unidade de apresentação. Obrigatório com acao=inserir_produto."`
	EstoqueMinimo          *int `json:"estoque_minimo,omitempty" jsonschema:"Estoque mínimo. Obrigatório com acao=inserir_produto."`
	EstoqueMaximo          *int `json:"estoque_maximo,omitempty" jsonschema:"Estoque máximo. Obrigatório com acao=inserir_produto."`
	PrecoCompra            *int `json:"preco_compra,omitempty" jsonschema:"Preço de compra (em centavos). Obrigatório com acao=inserir_produto."`
	PrecoVenda             *int `json:"preco_venda,omitempty" jsonschema:"Preço de venda (em centavos). Obrigatório com acao=inserir_produto."`

	Confirmacao bool `json:"confirmacao" jsonschema:"Confirmação EXPLÍCITA do OPERADOR da clínica (nunca assumida pelo agente/modelo) de que deseja inserir este produto no estoque."`
}

// MovimentarEstoqueResult is movimentar_estoque's result.
type MovimentarEstoqueResult struct {
	Inserido  bool `json:"inserido"`
	ProdutoID int  `json:"produto_id,omitempty"`
}

// MovimentarEstoque dispatches args.Acao to the matching Feegow endpoint.
func MovimentarEstoque(ctx context.Context, client *feegow.Client, args MovimentarEstoqueArgs) (*MovimentarEstoqueResult, error) {
	result, err := movimentarEstoque(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func movimentarEstoque(ctx context.Context, client *feegow.Client, args MovimentarEstoqueArgs) (*MovimentarEstoqueResult, error) {
	if err := requireConfirmacaoOperador(args.Confirmacao, "movimentar_estoque"); err != nil {
		return nil, err
	}

	switch args.Acao {
	case acaoEstoqueInserirProduto:
		return movimentarEstoqueInserirProduto(ctx, client, args)
	case acaoEstoqueEntrada, acaoEstoqueSaida, acaoEstoqueMovimentacao:
		return nil, ArgumentIndisponivel("movimentar_estoque", args.Acao,
			"o endpoint correspondente vive sob core.feegow.com.br, host confirmado MORTO pela Fase 0 (não resolve nesta rede/ambiente)")
	default:
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"acao %q não é reconhecida; valores aceitos: %v", args.Acao, MovimentarEstoqueAcoes(),
		)}
	}
}

func movimentarEstoqueInserirProduto(ctx context.Context, client *feegow.Client, args MovimentarEstoqueArgs) (*MovimentarEstoqueResult, error) {
	required := map[string]*int{
		"tipo_produto":            args.TipoProduto,
		"categoria_id":            args.CategoriaID,
		"fabricante_id":           args.FabricanteID,
		"localizacao_id":          args.LocalizacaoID,
		"dias_aviso_validade":     args.DiasAvisoValidade,
		"apresentacao_quantidade": args.ApresentacaoQuantidade,
		"apresentacao_unidade":    args.ApresentacaoUnidade,
		"estoque_minimo":          args.EstoqueMinimo,
		"estoque_maximo":          args.EstoqueMaximo,
		"preco_compra":            args.PrecoCompra,
		"preco_venda":             args.PrecoVenda,
	}
	for field, v := range required {
		if v == nil {
			return nil, &ArgumentError{Msg: fmt.Sprintf("%s é obrigatório para acao=inserir_produto", field)}
		}
	}

	resp, err := client.Call(ctx, "stock.product_insert", feegow.Request{
		Params: map[string]any{
			"TipoProduto":            *args.TipoProduto,
			"CategoriaID":            *args.CategoriaID,
			"FabricanteID":           *args.FabricanteID,
			"LocalizacaoID":          *args.LocalizacaoID,
			"DiasAvisoValidade":      *args.DiasAvisoValidade,
			"ApresentacaoQuantidade": *args.ApresentacaoQuantidade,
			"ApresentacaoUnidade":    *args.ApresentacaoUnidade,
			"EstoqueMinimo":          *args.EstoqueMinimo,
			"EstoqueMaximo":          *args.EstoqueMaximo,
			"PrecoCompra":            *args.PrecoCompra,
			"PrecoVenda":             *args.PrecoVenda,
		},
	})
	if err != nil {
		return nil, err
	}

	var body struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(resp.Content, &body); err != nil {
		return nil, fmt.Errorf("tools: decoding stock/product/insert response: %w", err)
	}

	auditAdminWriteRecord("movimentar_estoque:inserir_produto", "produto_id", body.ID)
	return &MovimentarEstoqueResult{Inserido: true, ProdutoID: body.ID}, nil
}
