package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds consultar_laudos, the Fase 4c read-side entry point into
// the "Laudos" endpoint group (4 endpoints per ESPECIFICACAO.md §13):
// listar, obter_arquivo, visualizar. The fourth endpoint of the group
// (registrar — /medical-reports/create) is a WRITE and lives in its own
// tool, registrar_laudo (laudos_registro.go) — same separation
// consultar_financeiro/gerenciar_conta already establish.
//
// LAUDO É DADO CLÍNICO: every result this tool returns passes through
// whatever Feegow sent (this package has no way to know a laudo's internal
// shape ahead of time — it varies by exam type), but nothing here ever logs
// laudo content, only tool name and the ids involved — same LGPD discipline
// audit.go already applies everywhere else, just spelled out here because
// this is the one grouping in this package whose payload IS clinical data,
// not administrative/financial data.
//
// acao=listar is recognized but ALWAYS unavailable: the Fase 4c smoke test
// tried every plausible date parameter name and combination against
// /medical-reports/get-laudos-list (no params, data_inicio/data_fim,
// date_start/date_end, data, data_referencia, periodo_inicio/periodo_fim,
// start/end, dataInicio/dataFim, even agendamento_id) and every single
// attempt returned the exact same 422 {"message":"Data missing"} — the real
// parameter name could not be isolated. Same class of unconfirmable
// contract as gerenciar_propostas' listar_por_data (see its doc comment).

const (
	acaoLaudoListar       = "listar" // recognized, deliberately refused — see doc comment above
	acaoLaudoObterArquivo = "obter_arquivo"
	acaoLaudoVisualizar   = "visualizar"
)

// ConsultarLaudosAcoes returns every valid `acao` value, sorted, for use in
// the MCP tool's description/schema and in tests.
func ConsultarLaudosAcoes() []string {
	return []string{acaoLaudoListar, acaoLaudoObterArquivo, acaoLaudoVisualizar}
}

// ConsultarLaudosArgs is consultar_laudos' argument shape: one acao selects
// which endpoint answers the call.
type ConsultarLaudosArgs struct {
	Acao string `json:"acao" jsonschema:"O que consultar: listar, obter_arquivo, visualizar. (\"listar\" é reconhecida mas indisponível — ver a descrição da tool.)"`

	// obter_arquivo (/medical-reports/get-labs-report-file)
	LabReportID *int `json:"lab_report_id,omitempty" jsonschema:"ID do arquivo de laudo laboratorial. Obrigatório com acao=obter_arquivo."`

	// visualizar (/medical-reports/search)
	AgendamentoID *int `json:"agendamento_id,omitempty" jsonschema:"ID do agendamento associado ao laudo. Obrigatório com acao=visualizar."`
}

// ConsultarLaudosResult is consultar_laudos' result: whatever the resolved
// endpoint's content was, passed through as-is — same "business/admin blob"
// choice as ConsultarFinanceiroResult, ConsultarPacienteClinicoResult etc.
// Content here CAN be clinical data (see this file's doc comment) — the
// minimization this tool applies is about what reaches a LOG line, never
// about what reaches the caller, whose Feegow token already authorizes
// seeing it.
type ConsultarLaudosResult struct {
	Itens any `json:"itens"`
}

// ConsultarLaudos dispatches args.Acao to the matching Feegow endpoint.
func ConsultarLaudos(ctx context.Context, client *feegow.Client, args ConsultarLaudosArgs) (*ConsultarLaudosResult, error) {
	result, err := consultarLaudos(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func consultarLaudos(ctx context.Context, client *feegow.Client, args ConsultarLaudosArgs) (*ConsultarLaudosResult, error) {
	switch args.Acao {
	case acaoLaudoListar:
		return nil, ArgumentIndisponivel("consultar_laudos", acaoLaudoListar,
			"a Fase 4c tentou toda combinação plausível de nome de parâmetro de data contra "+
				"/medical-reports/get-laudos-list e todas devolveram o mesmo 422 genérico "+
				"\"Data missing\" — o nome real do(s) parâmetro(s) não pôde ser isolado")
	case acaoLaudoObterArquivo:
		return laudoObterArquivo(ctx, client, args)
	case acaoLaudoVisualizar:
		return laudoVisualizar(ctx, client, args)
	default:
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"acao %q não é reconhecida; valores aceitos: %v", args.Acao, ConsultarLaudosAcoes(),
		)}
	}
}

func laudoObterArquivo(ctx context.Context, client *feegow.Client, args ConsultarLaudosArgs) (*ConsultarLaudosResult, error) {
	if args.LabReportID == nil || *args.LabReportID <= 0 {
		return nil, &ArgumentError{Msg: "lab_report_id é obrigatório para acao=obter_arquivo"}
	}

	resp, err := client.Call(ctx, "medical_reports.get_labs_report_file", feegow.Request{
		Params: map[string]any{"lab_report_id": *args.LabReportID},
	})
	if err != nil {
		return nil, err
	}
	return decodeLaudos(resp)
}

func laudoVisualizar(ctx context.Context, client *feegow.Client, args ConsultarLaudosArgs) (*ConsultarLaudosResult, error) {
	if args.AgendamentoID == nil || *args.AgendamentoID <= 0 {
		return nil, &ArgumentError{Msg: "agendamento_id é obrigatório para acao=visualizar"}
	}

	// medical_reports.search é EnvelopeNone (ver seu Notes em
	// internal/feegow/registry.go): nenhuma resposta de sucesso real foi
	// observada pela Fase 4c, então este pacote não assume nenhum shape de
	// wrapper — o corpo cru é repassado como está, exatamente como
	// finCall/decodeClinico já fazem para os outros tipos EnvelopeNone
	// desta superfície admin.
	resp, err := client.Call(ctx, "medical_reports.search", feegow.Request{
		Params: map[string]any{"agendamento_id": *args.AgendamentoID},
	})
	if err != nil {
		return nil, err
	}
	return decodeLaudos(resp)
}

// decodeLaudos is the shared "call already made, decode its content as-is"
// tail every branch above uses.
func decodeLaudos(resp *feegow.Response) (*ConsultarLaudosResult, error) {
	var itens any
	if err := json.Unmarshal(resp.Content, &itens); err != nil {
		return nil, fmt.Errorf("tools: decoding response: %w", err)
	}
	return &ConsultarLaudosResult{Itens: itens}, nil
}
