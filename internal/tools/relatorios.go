package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds the Fase 4c "Relatórios" group (2 endpoints per
// ESPECIFICACAO.md §13): listar_relatorios (/reports/list — the catalog of
// report definitions this clínica can generate) and gerar_relatorio
// (/reports/generate — runs one of them). Kept as two separate tools,
// unlike most of this package's other read/write groupings, because
// there's only one endpoint on each side — a single tipo/acao switch would
// be a switch with exactly one case, adding indirection without buying
// anything.
//
// gerar_relatorio requires NO confirmação, unlike every other POST write
// tool in this package: it does not create, edit or delete any clinic
// record — it runs a report DEFINITION (one of the ids listar_relatorios
// returns) and hands back computed/aggregated data, the same "this POST
// doesn't mutate anything a confirmação gate would protect" reasoning
// gerar_senha_atendimento's doc comment already applies to
// /appoints/queue-position (see internal/tools/agenda_admin.go).

// ListarRelatoriosResult is listar_relatorios' result: /reports/list's
// content passed through as-is (id, categoria, nome, o slug "Arquivo" que
// gerar_relatorio espera em "report", status) — same "business blob"
// choice as every other admin catalog result in this package.
type ListarRelatoriosResult struct {
	Relatorios any `json:"relatorios"`
}

// ListarRelatorios lists every report definition this clínica can generate,
// via /reports/list — a bare JSON array on the wire (EnvelopeNone; see
// reports.list's Notes in internal/feegow/registry.go), decoded here as-is.
func ListarRelatorios(ctx context.Context, client *feegow.Client) (*ListarRelatoriosResult, error) {
	result, err := listarRelatorios(ctx, client)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func listarRelatorios(ctx context.Context, client *feegow.Client) (*ListarRelatoriosResult, error) {
	resp, err := client.Call(ctx, "reports.list", feegow.Request{})
	if err != nil {
		return nil, err
	}
	var relatorios any
	if err := json.Unmarshal(resp.Content, &relatorios); err != nil {
		return nil, fmt.Errorf("tools: decoding reports/list response: %w", err)
	}
	return &ListarRelatoriosResult{Relatorios: relatorios}, nil
}

// GerarRelatorioArgs is gerar_relatorio's argument shape. Filtros is a
// free-form extension point: the Fase 4c smoke test confirmed only
// "report" is required (an empty-otherwise call returned reportId/route/
// reportName populated but columns/filters/data all `false`, suggesting
// additional, undocumented filter parameters exist to populate real data
// — none were sounded, so this tool does not invent field names for them).
type GerarRelatorioArgs struct {
	Report  string         `json:"report" jsonschema:"Slug do relatório a gerar — o campo \"Arquivo\" retornado por listar_relatorios (ex.: \"schedule-appointments\"). Obrigatório."`
	Filtros map[string]any `json:"filtros,omitempty" jsonschema:"Filtros adicionais opcionais, repassados como estão — NÃO confirmados contra a API real; sem eles, reports.generate devolveu columns/filters/data todos vazios (false) na sondagem desta fase."`
}

// GerarRelatorioResult is gerar_relatorio's result: /reports/generate's
// whole response body passed through as-is (reportId, route, reportName,
// columns, filters, data — see reports.generate's Notes in
// internal/feegow/registry.go for the exact shape observed).
type GerarRelatorioResult struct {
	Relatorio any `json:"relatorio"`
}

// GerarRelatorio generates a report via /reports/generate.
func GerarRelatorio(ctx context.Context, client *feegow.Client, args GerarRelatorioArgs) (*GerarRelatorioResult, error) {
	result, err := gerarRelatorio(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func gerarRelatorio(ctx context.Context, client *feegow.Client, args GerarRelatorioArgs) (*GerarRelatorioResult, error) {
	if args.Report == "" {
		return nil, &ArgumentError{Msg: "report é obrigatório (ver listar_relatorios para os valores válidos)"}
	}

	wire := map[string]any{"report": args.Report}
	for k, v := range args.Filtros {
		wire[k] = v
	}

	resp, err := client.Call(ctx, "reports.generate", feegow.Request{Params: wire})
	if err != nil {
		return nil, err
	}

	// reports.generate é EnvelopeNone (ver seu Notes em
	// internal/feegow/registry.go) e decodifica DELIBERADAMENTE como `any`
	// genérico, nunca um struct fixo: um "report" válido devolve um OBJETO
	// {success,reportId,route,reportName,columns,filters,data}, mas um
	// "report" que não corresponde a nenhum relatório real devolve um
	// ARRAY JSON VAZIO ([]) — dois shapes de sucesso (200) completamente
	// diferentes para o mesmo endpoint, confirmados pela Fase 4c. Um struct
	// fixo com campo Success quebraria a decodificação do segundo caso; um
	// checkEnvelopeNoneSuccess não se aplica aqui pela mesma razão. O
	// caller recebe o que a Feegow devolveu, seja objeto ou array vazio.
	var relatorio any
	if err := json.Unmarshal(resp.Content, &relatorio); err != nil {
		return nil, fmt.Errorf("tools: decoding reports/generate response: %w", err)
	}
	return &GerarRelatorioResult{Relatorio: relatorio}, nil
}
