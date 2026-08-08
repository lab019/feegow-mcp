package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds gerenciar_propostas, the Fase 4c entry point into the
// "Propostas" endpoint group (5 endpoints per ESPECIFICACAO.md §13):
// listar, listar_por_data, criar, mudar_status, obter_url — all grouped
// under one acao-dispatched tool, the same shape ConsultarFinanceiro/
// GerenciarConta already establish, rather than five near-identical tools.
//
// acao=listar_por_data is recognized but ALWAYS unavailable: the Fase 4c
// smoke test tried every plausible date parameter name and combination
// against /proposal/list-dates — no params, one date, both dates, and
// alternate names (data, dataInicio, dataFim, periodo_inicio/periodo_fim,
// start/end, data_start/data_end) — and every single attempt returned the
// exact same 400 "Só pode buscar utilizando apenas uma das datas.", even
// when exactly one date was sent (which the message itself says should be
// enough). There is no way to distinguish "wrong parameter name" from "this
// route is broken in this sandbox" from that signal — the same class of
// unconfirmable contract financial.voucher_create and
// financial.account_association hit in Fase 4b (see their Registry Notes
// and gerenciar_conta/gerenciar_voucher's doc comments). Calling
// gerenciar_propostas with acao="listar_por_data" returns a clear
// ArgumentIndisponivel instead of a call that would certainly fail opaquely.
//
// The API's own field-naming inconsistency shows up twice in this group:
// acao=mudar_status's underlying endpoint (/proposal/change-status) names
// its identifier "proposal_id" (English), while acao=obter_url's endpoint
// (/proposal/proposal-url) names the SAME concept "proposta_id"
// (Portuguese) — both confirmed by the Fase 4c smoke test. This tool
// exposes a single PropostaID field and translates it to whichever wire
// name the resolved acao actually needs, so a caller never has to track
// which of the two spellings applies.

const (
	acaoPropostaListar        = "listar"
	acaoPropostaListarPorData = "listar_por_data" // recognized, deliberately refused — see doc comment above
	acaoPropostaCriar         = "criar"
	acaoPropostaMudarStatus   = "mudar_status"
	acaoPropostaObterUrl      = "obter_url"
)

// GerenciarPropostasAcoes returns every valid `acao` value, sorted, for use
// in the MCP tool's description/schema and in tests.
func GerenciarPropostasAcoes() []string {
	return []string{
		acaoPropostaCriar,
		acaoPropostaListar,
		acaoPropostaListarPorData,
		acaoPropostaMudarStatus,
		acaoPropostaObterUrl,
	}
}

// GerenciarPropostasArgs is gerenciar_propostas' argument shape: one acao
// selects which endpoint the call resolves to, and every field below is
// meaningful only for the acao(s) noted in its own description — same
// per-branch validation pattern as ConsultarFinanceiroArgs/GerenciarContaArgs.
type GerenciarPropostasArgs struct {
	Acao string `json:"acao" jsonschema:"O que fazer: listar, listar_por_data, criar, mudar_status, obter_url. (\"listar_por_data\" é reconhecida mas indisponível — ver a descrição da tool.)"`

	// listar (/proposal/list) e criar (/proposal/create)
	PacienteID *int   `json:"paciente_id,omitempty" jsonschema:"ID do paciente. Com acao=listar, filtra as propostas desse paciente e dispensa data_inicio/data_fim; com acao=criar, é o paciente da nova proposta (obrigatório)."`
	DataInicio string `json:"data_inicio,omitempty" jsonschema:"Início do filtro por data, ISO-8601 (YYYY-MM-DD). Obrigatório com acao=listar quando paciente_id não é informado."`
	DataFim    string `json:"data_fim,omitempty" jsonschema:"Fim do filtro por data, ISO-8601 (YYYY-MM-DD). Obrigatório com acao=listar quando paciente_id não é informado."`

	// criar (/proposal/create)
	ProposerID    *int   `json:"proposer_id,omitempty" jsonschema:"ID do profissional/usuário proponente. Obrigatório com acao=criar."`
	StatusID      *int   `json:"status_id,omitempty" jsonschema:"ID do status da proposta. Obrigatório com acao=criar e acao=mudar_status."`
	ProposalDate  string `json:"proposal_date,omitempty" jsonschema:"Data da proposta, ISO-8601 (YYYY-MM-DD). Obrigatório com acao=criar."`
	Procedimentos []any  `json:"procedimentos,omitempty" jsonschema:"Procedimentos da proposta (lista não-vazia) — estrutura interna dos itens NÃO confirmada contra a API real (ver docs/feegow-api.md, proposal.create). Obrigatório (>=1 item) com acao=criar."`

	// mudar_status (/proposal/change-status) e obter_url (/proposal/proposal-url)
	// — MESMO conceito (id da proposta), nomes de campo DIFERENTES na API
	// real (proposal_id vs proposta_id) — ver o doc comment deste arquivo.
	PropostaID *int `json:"proposta_id,omitempty" jsonschema:"ID da proposta. Obrigatório com acao=mudar_status e acao=obter_url."`

	Confirmacao bool `json:"confirmacao" jsonschema:"Confirmação EXPLÍCITA do OPERADOR da clínica (nunca assumida pelo agente/modelo). Obrigatória para acao=criar e acao=mudar_status; ignorada (não exigida) para acao=listar/listar_por_data/obter_url, que são leituras."`
}

// GerenciarPropostasResult is gerenciar_propostas' result. Itens carries
// acao=listar/obter_url's raw content (a list of propostas, or the
// {url|false} lookup result); Sucesso is set for acao=criar/mudar_status —
// same "business blob, no need to hand-type every field" choice
// ConsultarFinanceiroResult/GerenciarContaResult already make.
type GerenciarPropostasResult struct {
	Itens   any  `json:"itens,omitempty"`
	Sucesso bool `json:"sucesso,omitempty"`
}

// GerenciarPropostas dispatches args.Acao to the matching Feegow endpoint.
func GerenciarPropostas(ctx context.Context, client *feegow.Client, args GerenciarPropostasArgs) (*GerenciarPropostasResult, error) {
	result, err := gerenciarPropostas(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func gerenciarPropostas(ctx context.Context, client *feegow.Client, args GerenciarPropostasArgs) (*GerenciarPropostasResult, error) {
	switch args.Acao {
	case acaoPropostaListar:
		return propostaListar(ctx, client, args)
	case acaoPropostaListarPorData:
		return nil, ArgumentIndisponivel("gerenciar_propostas", acaoPropostaListarPorData,
			"a Fase 4c tentou toda combinação plausível de data (nenhuma, uma, ambas, e nomes "+
				"alternativos) contra /proposal/list-dates e todas devolveram o mesmo 400 genérico "+
				"\"Só pode buscar utilizando apenas uma das datas.\", mesmo com exatamente uma data "+
				"enviada — o nome real do parâmetro não pôde ser isolado")
	case acaoPropostaCriar:
		return propostaCriar(ctx, client, args)
	case acaoPropostaMudarStatus:
		return propostaMudarStatus(ctx, client, args)
	case acaoPropostaObterUrl:
		return propostaObterUrl(ctx, client, args)
	default:
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"acao %q não é reconhecida; valores aceitos: %v", args.Acao, GerenciarPropostasAcoes(),
		)}
	}
}

func propostaListar(ctx context.Context, client *feegow.Client, args GerenciarPropostasArgs) (*GerenciarPropostasResult, error) {
	req := feegow.Request{}
	if args.PacienteID != nil {
		if *args.PacienteID <= 0 {
			return nil, &ArgumentError{Msg: "paciente_id deve ser um identificador positivo"}
		}
		req.Params = map[string]any{"paciente_id": *args.PacienteID}
	} else {
		start, end, err := requireDateRange(args.DataInicio, args.DataFim, "gerenciar_propostas(listar)")
		if err != nil {
			return nil, err
		}
		req.DateStart = &start
		req.DateEnd = &end
	}

	resp, err := client.Call(ctx, "proposal.list", req)
	if err != nil {
		return nil, err
	}
	return decodePropostas(resp)
}

func propostaCriar(ctx context.Context, client *feegow.Client, args GerenciarPropostasArgs) (*GerenciarPropostasResult, error) {
	if err := requireConfirmacaoOperador(args.Confirmacao, "gerenciar_propostas (acao=criar)"); err != nil {
		return nil, err
	}
	if args.ProposerID == nil || *args.ProposerID <= 0 {
		return nil, &ArgumentError{Msg: "proposer_id é obrigatório para acao=criar"}
	}
	if args.PacienteID == nil || *args.PacienteID <= 0 {
		return nil, &ArgumentError{Msg: "paciente_id é obrigatório para acao=criar"}
	}
	if args.StatusID == nil {
		return nil, &ArgumentError{Msg: "status_id é obrigatório para acao=criar"}
	}
	if args.ProposalDate == "" {
		return nil, &ArgumentError{Msg: "proposal_date é obrigatório para acao=criar (ISO-8601 YYYY-MM-DD)"}
	}
	if _, err := time.Parse(feegow.ISO8601, args.ProposalDate); err != nil {
		return nil, &ArgumentError{Msg: "proposal_date deve estar em ISO-8601 (YYYY-MM-DD)"}
	}
	if len(args.Procedimentos) == 0 {
		return nil, &ArgumentError{Msg: "procedimentos é obrigatório (pelo menos 1 item) para acao=criar"}
	}

	resp, err := client.Call(ctx, "proposal.create", feegow.Request{
		Params: map[string]any{
			"proposer_id":   *args.ProposerID,
			"paciente_id":   *args.PacienteID,
			"status_id":     *args.StatusID,
			"proposal_date": args.ProposalDate,
			"procedimentos": args.Procedimentos,
		},
	})
	if err != nil {
		return nil, err
	}

	auditAdminWrite("gerenciar_propostas:criar", *args.PacienteID, 0)
	var itens any
	if err := json.Unmarshal(resp.Content, &itens); err != nil {
		return nil, fmt.Errorf("tools: decoding proposal/create response: %w", err)
	}
	return &GerenciarPropostasResult{Sucesso: true, Itens: itens}, nil
}

func propostaMudarStatus(ctx context.Context, client *feegow.Client, args GerenciarPropostasArgs) (*GerenciarPropostasResult, error) {
	if err := requireConfirmacaoOperador(args.Confirmacao, "gerenciar_propostas (acao=mudar_status)"); err != nil {
		return nil, err
	}
	if args.PropostaID == nil || *args.PropostaID <= 0 {
		return nil, &ArgumentError{Msg: "proposta_id é obrigatório para acao=mudar_status"}
	}
	if args.StatusID == nil {
		return nil, &ArgumentError{Msg: "status_id é obrigatório para acao=mudar_status"}
	}

	// Wire field is "proposal_id" (English) — different from obter_url's
	// "proposta_id" (Portuguese), same PropostaID Go field, see this
	// file's doc comment.
	if _, err := client.Call(ctx, "proposal.change_status", feegow.Request{
		Params: map[string]any{
			"proposal_id": *args.PropostaID,
			"status_id":   *args.StatusID,
		},
	}); err != nil {
		return nil, err
	}

	auditAdminWriteRecord("gerenciar_propostas:mudar_status", "proposta_id", *args.PropostaID)
	return &GerenciarPropostasResult{Sucesso: true}, nil
}

func propostaObterUrl(ctx context.Context, client *feegow.Client, args GerenciarPropostasArgs) (*GerenciarPropostasResult, error) {
	if args.PropostaID == nil || *args.PropostaID <= 0 {
		return nil, &ArgumentError{Msg: "proposta_id é obrigatório para acao=obter_url"}
	}

	resp, err := client.Call(ctx, "proposal.proposal_url", feegow.Request{
		Params: map[string]any{"proposta_id": *args.PropostaID},
	})
	if err != nil {
		return nil, err
	}
	return decodePropostas(resp)
}

// decodePropostas is the shared "call already made, decode its content
// as-is" tail every read branch above uses — same role as
// financeiro_consulta.go's finCall.
func decodePropostas(resp *feegow.Response) (*GerenciarPropostasResult, error) {
	var itens any
	if err := json.Unmarshal(resp.Content, &itens); err != nil {
		return nil, fmt.Errorf("tools: decoding response: %w", err)
	}
	return &GerenciarPropostasResult{Itens: itens}, nil
}
