package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds the three write tools that act on an EXISTING
// agendamento — cancelar, remarcar, confirmar — and the ownership check
// they all share.
//
// The endpoints behind them (/appoints/cancel-appoint, /appoints/reschedule,
// /appoints/confirm) take a bare agendamento_id. On a stateless,
// unauthenticated public channel (see IdentidadeArgs' doc comment), exposing
// that id as a free argument would mean guessing a number acts on ANY
// patient's consulta — that is not the risk the identity design accepted
// (a family member who knows the two identification facts acting on the
// patient's behalf), it is a completely different, unbounded one (anyone
// who can count acting on anyone).
//
// Every tool here closes that gap the same way: resolve the caller's
// identity from the two facts (exactly like identificar_paciente), then
// confirm the given agendamento_id is actually one of THAT identity's own
// bookings — via resolveOwnedAgendamento below — before the mutating call
// is ever made. An id that isn't theirs gets the exact same ErrNaoLocalizado
// identificar_paciente already uses for "cadastro not found": distinguishing
// "this id doesn't exist" from "this id exists but isn't yours" would be an
// oracle for enumerating other patients' agendamento ids, the same
// reasoning ErrNaoLocalizado's doc comment already gives for CPF
// enumeration.

// resolveOwnedAgendamento identifies the patient from args (the same
// cpf+data_nascimento / telefone+nome_completo pair identificar_paciente
// accepts) and confirms agendamentoID belongs to that patient.
//
// It does NOT list the identified patient's agendamentos by paciente_id —
// /appoints/search rejects that shape server-side: data_start/data_end are
// mandatory unless agendamento_id (or created_at_start/created_at_end) is
// present (measured against the real API), so a paciente_id-only query
// would 422 every single time, meaning cancelar/remarcar/confirmar could
// never reach the mutating call at all. Instead it looks up the ONE
// agendamento by id — no dates needed, confirmed against the real API — and
// compares the paciente_id THAT response reports back against the identity
// resolved on our side. This also means posse no longer depends on Feegow's
// paciente_id filter actually scoping results server-side (a single point
// of failure the adversarial review flagged): we read the owner back and
// compare it ourselves.
//
// Returns the resolved paciente_id on success (callers need it for
// auditWrite) or ErrNaoLocalizado for every other outcome — id not found, id
// found but owned by someone else, or an unexpected response shape — never a
// more specific "exists but not yours" error, for the enumeration reason
// above.
func resolveOwnedAgendamento(ctx context.Context, client *feegow.Client, args IdentidadeArgs, agendamentoID int) (int, error) {
	identidade, err := IdentificarPaciente(ctx, client, args)
	if err != nil {
		return 0, err
	}

	resp, err := client.Call(ctx, "appoints.search", feegow.Request{
		Params: map[string]any{"agendamento_id": agendamentoID},
	})
	if err != nil {
		return 0, err
	}

	var entries []appointSearchEntry
	if err := json.Unmarshal(resp.Content, &entries); err != nil {
		return 0, fmt.Errorf("tools: decoding appoints/search response: %w", err)
	}

	for _, e := range entries {
		if e.AgendamentoID == agendamentoID && e.PacienteID == identidade.PacienteID {
			return identidade.PacienteID, nil
		}
	}
	return 0, ErrNaoLocalizado
}

// --- cancelar ---------------------------------------------------------

// CancelarArgs is cancelar's argument shape: the two identification facts,
// the agendamento_id to cancel, and an explicit confirmation. Deliberately
// has NO motivo_id field — see Cancelar's doc comment.
type CancelarArgs struct {
	IdentidadeArgs

	AgendamentoID       int  `json:"agendamento_id" jsonschema:"ID do agendamento a cancelar. Precisa pertencer ao paciente identificado pelos dois fatos acima — use o valor devolvido por consultar_agenda."`
	ConfirmacaoPaciente bool `json:"confirmacao_paciente" jsonschema:"Confirmação EXPLÍCITA do PACIENTE (nunca do agente/modelo) de que deseja cancelar esta consulta."`
}

// CancelarResult is cancelar's result.
type CancelarResult struct {
	Cancelado bool `json:"cancelado"`
}

// motivoSolicitadoPeloPaciente is the fixed motivo_id (1, "Solicitado pelo
// Paciente" — /appoints/motives) this profile always sends to
// /appoints/cancel-appoint and /appoints/reschedule. It is NEVER exposed as
// an argument: this channel only ever speaks for the paciente (see
// IdentidadeArgs' doc comment on the identity design), so letting the model
// pick a different motivo (e.g. "Solicitado pela Clínica") would let it
// falsify the clinic's own cancellation-reason indicator on a
// patient-initiated action — a real, quiet corruption of the clinic's
// reporting, not a cosmetic difference.
const motivoSolicitadoPeloPaciente = 1

// Cancelar cancels an existing agendamento that belongs to the patient
// identified by the two facts — see this file's package doc comment for the
// posse (ownership) check every tool here shares, and
// motivoSolicitadoPeloPaciente for why motivo_id is fixed, not an argument.
func Cancelar(ctx context.Context, client *feegow.Client, args CancelarArgs) (*CancelarResult, error) {
	result, err := cancelar(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func cancelar(ctx context.Context, client *feegow.Client, args CancelarArgs) (*CancelarResult, error) {
	if err := requireConfirmacaoPaciente(args.ConfirmacaoPaciente, "cancelar"); err != nil {
		return nil, err
	}
	if err := validateAgendamentoID(args.AgendamentoID); err != nil {
		return nil, err
	}

	pacienteID, err := resolveOwnedAgendamento(ctx, client, args.IdentidadeArgs, args.AgendamentoID)
	if err != nil {
		return nil, err
	}

	if _, err := client.Call(ctx, "appoints.cancel_appoint", feegow.Request{
		Params: map[string]any{
			"agendamento_id": args.AgendamentoID,
			"motivo_id":      motivoSolicitadoPeloPaciente,
		},
	}); err != nil {
		return nil, err
	}

	auditWrite("cancelar", pacienteID, args.AgendamentoID)
	return &CancelarResult{Cancelado: true}, nil
}

// --- remarcar -----------------------------------------------------------

// RemarcarArgs is remarcar's argument shape: the two identification facts,
// the agendamento_id being moved, and the new date/time.
type RemarcarArgs struct {
	IdentidadeArgs

	AgendamentoID       int    `json:"agendamento_id" jsonschema:"ID do agendamento a remarcar. Precisa pertencer ao paciente identificado pelos dois fatos acima — use o valor devolvido por consultar_agenda."`
	NovaData            string `json:"nova_data" jsonschema:"Nova data, ISO-8601 (YYYY-MM-DD). Nunca retroativa (deve ser hoje ou futura)."`
	NovoHorario         string `json:"novo_horario" jsonschema:"Novo horário (HH:MM ou HH:MM:SS)."`
	ConfirmacaoPaciente bool   `json:"confirmacao_paciente" jsonschema:"Confirmação EXPLÍCITA do PACIENTE (nunca do agente/modelo) de que deseja remarcar para esta nova data/horário."`
}

// RemarcarResult is remarcar's result.
type RemarcarResult struct {
	Remarcado bool `json:"remarcado"`
}

// Remarcar moves an existing agendamento — that belongs to the identified
// patient — to a new date/horário, via /appoints/reschedule.
//
// This deliberately calls /appoints/reschedule directly, NOT "agendar the
// new slot then cancelar the old one": Fase 0 confirmed that sequence
// cannot work here. The same paciente already has an agendamento with this
// profissional (the one being moved), so the "agendar" half would be
// rejected by Feegow's undocumented 409 "Esse paciente já possui um
// agendamento nessa agenda." (see ErrPacienteJaTemAgendamento, errors.go)
// before this code would ever reach the cancel step — /appoints/reschedule
// is Feegow's actual atomic remarcação, and the only endpoint that works
// for this.
func Remarcar(ctx context.Context, client *feegow.Client, args RemarcarArgs) (*RemarcarResult, error) {
	result, err := remarcar(ctx, client, args)
	if err != nil {
		return nil, classifyAppointConflict(err)
	}
	return result, nil
}

func remarcar(ctx context.Context, client *feegow.Client, args RemarcarArgs) (*RemarcarResult, error) {
	if err := requireConfirmacaoPaciente(args.ConfirmacaoPaciente, "remarcar"); err != nil {
		return nil, err
	}
	if err := validateAgendamentoID(args.AgendamentoID); err != nil {
		return nil, err
	}
	if args.NovoHorario == "" {
		return nil, &ArgumentError{Msg: "novo_horario é obrigatório"}
	}
	if err := validateNotPast("nova_data", args.NovaData); err != nil {
		return nil, err
	}

	pacienteID, err := resolveOwnedAgendamento(ctx, client, args.IdentidadeArgs, args.AgendamentoID)
	if err != nil {
		return nil, err
	}

	novaData := args.NovaData
	if _, err := client.Call(ctx, "appoints.reschedule", feegow.Request{
		Params: map[string]any{
			"agendamento_id": args.AgendamentoID,
			"motivo_id":      motivoSolicitadoPeloPaciente,
			"horario":        args.NovoHorario,
		},
		Date: &novaData,
	}); err != nil {
		return nil, err
	}

	auditWrite("remarcar", pacienteID, args.AgendamentoID)
	return &RemarcarResult{Remarcado: true}, nil
}

// --- confirmar ------------------------------------------------------------

// ConfirmarArgs is confirmar's argument shape: the two identification
// facts, the agendamento_id being confirmed, and an explicit confirmation.
type ConfirmarArgs struct {
	IdentidadeArgs

	AgendamentoID       int  `json:"agendamento_id" jsonschema:"ID do agendamento a confirmar. Precisa pertencer ao paciente identificado pelos dois fatos acima — use o valor devolvido por consultar_agenda."`
	ConfirmacaoPaciente bool `json:"confirmacao_paciente" jsonschema:"Confirmação EXPLÍCITA do PACIENTE (nunca do agente/modelo) de que deseja confirmar presença nesta consulta."`
}

// ConfirmarResult is confirmar's result.
type ConfirmarResult struct {
	Confirmado bool `json:"confirmado"`
}

// Confirmar confirms an existing agendamento that belongs to the identified
// patient, via the undocumented /appoints/confirm endpoint Fase 0 found —
// same posse (ownership) check as Cancelar/Remarcar.
func Confirmar(ctx context.Context, client *feegow.Client, args ConfirmarArgs) (*ConfirmarResult, error) {
	result, err := confirmar(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func confirmar(ctx context.Context, client *feegow.Client, args ConfirmarArgs) (*ConfirmarResult, error) {
	if err := requireConfirmacaoPaciente(args.ConfirmacaoPaciente, "confirmar"); err != nil {
		return nil, err
	}
	if err := validateAgendamentoID(args.AgendamentoID); err != nil {
		return nil, err
	}

	pacienteID, err := resolveOwnedAgendamento(ctx, client, args.IdentidadeArgs, args.AgendamentoID)
	if err != nil {
		return nil, err
	}

	if _, err := client.Call(ctx, "appoints.confirm", feegow.Request{
		Params: map[string]any{"agendamento_id": args.AgendamentoID},
	}); err != nil {
		return nil, err
	}

	auditWrite("confirmar", pacienteID, args.AgendamentoID)
	return &ConfirmarResult{Confirmado: true}, nil
}
