package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds the admin profile's agenda/clinic-operation tools —
// atualizar_status_agendamento (/appoints/statusUpdate) and
// gerar_senha_atendimento (/appoints/queue-position). Unlike Fase 3's
// cancelar/remarcar/confirmar, neither one needs a posse (ownership) check:
// there is no "claimed identity" to verify against on this profile (see
// paciente_admin.go's doc comment) — the operador already has the
// agendamento_id/unidade_id in hand from the clínica's own systems.

// --- atualizar_status_agendamento -----------------------------------------

// AtualizarStatusAgendamentoArgs is atualizar_status_agendamento's argument
// shape, mirroring /appoints/statusUpdate's documented body — including its
// PascalCase wire field names (AgendamentoID, StatusID, Obs, HoraChegada),
// confirmed against the real API by the Fase 0 smoke test (see
// internal/feegow/registry.go's appoints.status_update Notes).
type AtualizarStatusAgendamentoArgs struct {
	AgendamentoID int    `json:"agendamento_id" jsonschema:"ID do agendamento a atualizar. Obrigatório."`
	StatusID      int    `json:"status_id" jsonschema:"ID do novo status (ver listar_catalogo tipo=status_agendamento). Obrigatório."`
	Obs           string `json:"obs,omitempty" jsonschema:"Observação sobre a alteração de status."`
	HoraChegada   string `json:"hora_chegada,omitempty" jsonschema:"Hora de chegada (HH:MM). Só se aplica ao status \"aguardando\"."`

	Confirmacao bool `json:"confirmacao" jsonschema:"Confirmação EXPLÍCITA do OPERADOR da clínica (nunca assumida pelo agente/modelo) de que deseja aplicar esta mudança de status."`
}

// AtualizarStatusAgendamentoResult is atualizar_status_agendamento's result.
type AtualizarStatusAgendamentoResult struct {
	Atualizado bool `json:"atualizado"`
}

// AtualizarStatusAgendamento updates an agendamento's status via
// /appoints/statusUpdate.
func AtualizarStatusAgendamento(ctx context.Context, client *feegow.Client, args AtualizarStatusAgendamentoArgs) (*AtualizarStatusAgendamentoResult, error) {
	result, err := atualizarStatusAgendamento(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func atualizarStatusAgendamento(ctx context.Context, client *feegow.Client, args AtualizarStatusAgendamentoArgs) (*AtualizarStatusAgendamentoResult, error) {
	if err := requireConfirmacaoOperador(args.Confirmacao, "atualizar_status_agendamento"); err != nil {
		return nil, err
	}
	if err := validateAgendamentoID(args.AgendamentoID); err != nil {
		return nil, err
	}
	if args.StatusID <= 0 {
		return nil, &ArgumentError{Msg: "status_id é obrigatório"}
	}

	wire := map[string]any{
		"AgendamentoID": args.AgendamentoID,
		"StatusID":      args.StatusID,
	}
	if args.Obs != "" {
		wire["Obs"] = args.Obs
	}
	if args.HoraChegada != "" {
		wire["HoraChegada"] = args.HoraChegada
	}

	if _, err := client.Call(ctx, "appoints.status_update", feegow.Request{Params: wire}); err != nil {
		return nil, err
	}

	auditAdminWrite("atualizar_status_agendamento", 0, args.AgendamentoID)
	return &AtualizarStatusAgendamentoResult{Atualizado: true}, nil
}

// --- gerar_senha_atendimento -----------------------------------------------

// GerarSenhaAtendimentoArgs is gerar_senha_atendimento's argument shape.
// UnidadeID is a *int, not an int, for the exact reason AgendarArgs'
// LocalID/HorariosLivresArgs' UnidadeID are: 0 is a legitimate value
// ("unidade principal", confirmed by doc.txt's own worked example for this
// endpoint), and the field is mandatory — the only way to tell "the caller
// deliberately chose unidade principal" (0) apart from "the caller forgot
// to send it" (absent) is to make absence a distinct, checkable state.
// TipoSenha has no such ambiguity to guard against (it has no omitempty,
// so the MCP schema already marks it required — see AgendarArgs'
// ConfirmacaoPaciente for the same no-omitempty convention).
type GerarSenhaAtendimentoArgs struct {
	UnidadeID *int `json:"unidade_id" jsonschema:"ID da unidade. Obrigatório. 0 = unidade principal — omitir não é o mesmo que 0."`
	TipoSenha int  `json:"tipo_senha" jsonschema:"Tipo de senha: 0=G, 1=P, 2=C, 3=E, 4=R. Obrigatório."`
}

// GerarSenhaAtendimentoResult is gerar_senha_atendimento's result, mirroring
// /appoints/queue-position's content (posicao, tipoSenha, tipoFormatado).
type GerarSenhaAtendimentoResult struct {
	Posicao       int    `json:"posicao"`
	TipoSenha     int    `json:"tipo_senha"`
	TipoFormatado string `json:"tipo_formatado"`
}

// queuePositionBody mirrors /appoints/queue-position's whole response body
// — EnvelopeNone because the real top-level success key is misspelled
// "sucess" (see internal/feegow/registry.go's appoints.queue_position
// Notes). Sucess and Success are BOTH modeled, as *bool (not bool): Fase 0
// and doc.txt agree the real key is the typo'd "sucess", but the typo is a
// Feegow bug, not a documented contract — if Feegow ever corrects it
// server-side, this must keep working without a code change. Using *bool
// for both lets queuePositionSuccess (below) distinguish "key present,
// value false" from "key absent" for each spelling independently, instead
// of two bools that could not tell "false" from "absent" apart. Only the
// "content" object's own field names are spelled normally.
type queuePositionBody struct {
	Sucess  *bool `json:"sucess"`
	Success *bool `json:"success"`
	Content struct {
		Posicao       int    `json:"posicao"`
		TipoSenha     int    `json:"tipoSenha"`
		TipoFormatado string `json:"tipoFormatado"`
	} `json:"content"`
}

// queuePositionSuccess resolves queuePositionBody's success outcome,
// preferring the confirmed-real "sucess" key but falling back to the
// correctly-spelled "success" in case Feegow ever fixes the typo. Neither
// key present is an unexpected response shape — NOT a silent success: a
// Feegow response that decodes syntactically but carries no success
// signal at all must not be read as "the queue position was generated".
func queuePositionSuccess(body queuePositionBody) (bool, error) {
	switch {
	case body.Sucess != nil:
		return *body.Sucess, nil
	case body.Success != nil:
		return *body.Success, nil
	default:
		return false, fmt.Errorf("tools: resposta inesperada de appoints/queue-position: " +
			"nem \"sucess\" nem \"success\" presentes")
	}
}

// GerarSenhaAtendimento generates a new queue ticket via
// /appoints/queue-position. This is a GET with a side effect (it consumes
// the next queue position) rather than a Feegow resource being edited, so
// — unlike this file's atualizar_status_agendamento — it does not require
// explicit confirmation: it is closer in kind to buscar_horarios_livres
// (a read) than to a write against an existing patient/agendamento record.
func GerarSenhaAtendimento(ctx context.Context, client *feegow.Client, args GerarSenhaAtendimentoArgs) (*GerarSenhaAtendimentoResult, error) {
	result, err := gerarSenhaAtendimento(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func gerarSenhaAtendimento(ctx context.Context, client *feegow.Client, args GerarSenhaAtendimentoArgs) (*GerarSenhaAtendimentoResult, error) {
	if args.UnidadeID == nil {
		return nil, &ArgumentError{Msg: "unidade_id é obrigatório (0 = unidade principal; omitir não é o mesmo que 0)"}
	}
	if args.TipoSenha < 0 || args.TipoSenha > 4 {
		return nil, &ArgumentError{Msg: "tipo_senha deve ser 0 (G), 1 (P), 2 (C), 3 (E) ou 4 (R)"}
	}

	resp, err := client.Call(ctx, "appoints.queue_position", feegow.Request{
		Params: map[string]any{
			"unidade_id": *args.UnidadeID,
			"tipo_senha": args.TipoSenha,
		},
	})
	if err != nil {
		return nil, err
	}

	var body queuePositionBody
	if err := json.Unmarshal(resp.Content, &body); err != nil {
		return nil, fmt.Errorf("tools: decoding appoints/queue-position response: %w", err)
	}
	ok, err := queuePositionSuccess(body)
	if err != nil {
		return nil, err
	}
	if err := checkEnvelopeNoneSuccess(ok); err != nil {
		return nil, err
	}

	auditAdminWriteQueue(*args.UnidadeID, args.TipoSenha)
	return &GerarSenhaAtendimentoResult{
		Posicao:       body.Content.Posicao,
		TipoSenha:     body.Content.TipoSenha,
		TipoFormatado: body.Content.TipoFormatado,
	}, nil
}
