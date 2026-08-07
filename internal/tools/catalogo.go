package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// catalogEndpoints maps every value listar_catalogo's `tipo` argument
// accepts to the Feegow endpoint it reads. This is the whole reason
// listar_catalogo exists as one tool instead of ~12: a catalog this large is
// hard for a model to navigate as separate tools, and pushes the operator
// toward granting the whole server just to get one of them (see ADR 0002 of
// agent-base-tools, referenced in ESPECIFICACAO.md §6). None of these
// endpoints touch patient data — every one is clinic business data (units,
// specialties, professionals, motives, ...), never PII.
var catalogEndpoints = map[string]feegow.EndpointID{
	"unidades":              "company.list_unity",
	"locais":                "company.list_local",
	"especialidades":        "specialties.list",
	"convenios":             "insurance.list",
	"procedimentos":         "procedures.list",
	"procedimentos_tipos":   "procedures.types",
	"procedimentos_grupos":  "procedures.groups",
	"procedimentos_pacotes": "procedures.bundles",
	"profissionais":         "professional.list",
	"canais":                "appoints.list_channel",
	"motivos":               "appoints.motives",
	"status_agendamento":    "appoints.status",
}

// CatalogTypes returns every valid `tipo` value, sorted, for use in the MCP
// tool's description/schema and in tests.
func CatalogTypes() []string {
	types := make([]string, 0, len(catalogEndpoints))
	for t := range catalogEndpoints {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// CatalogoArgs is listar_catalogo's argument shape.
type CatalogoArgs struct {
	Tipo string `json:"tipo" jsonschema:"Qual catálogo listar. Valores aceitos: unidades, locais, especialidades, convenios, procedimentos, procedimentos_tipos, procedimentos_grupos, procedimentos_pacotes, profissionais, canais, motivos, status_agendamento."`
}

// CatalogoResult is listar_catalogo's result: whatever Feegow's content for
// the chosen tipo was, passed through as-is. Business/catalog data, never
// patient data — no minimization needed here the way identificar_paciente
// and consultar_agenda require it.
type CatalogoResult struct {
	Itens any `json:"itens"`
}

// ListarCatalogo resolves args.Tipo to a Feegow endpoint and returns its
// content. An unrecognized tipo is an ArgumentError — rejected before any
// Feegow call, same principle as identificar_paciente's single-fact
// rejection (acceptance criterion 2), just for a different kind of bad
// input.
func ListarCatalogo(ctx context.Context, client *feegow.Client, args CatalogoArgs) (*CatalogoResult, error) {
	id, ok := catalogEndpoints[args.Tipo]
	if !ok {
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"tipo %q não é um catálogo reconhecido; valores aceitos: %v", args.Tipo, CatalogTypes(),
		)}
	}

	resp, err := client.Call(ctx, id, feegow.Request{})
	if err != nil {
		return nil, err
	}

	var itens any
	if err := json.Unmarshal(resp.Content, &itens); err != nil {
		return nil, fmt.Errorf("tools: decoding %s response: %w", id, err)
	}
	return &CatalogoResult{Itens: itens}, nil
}
