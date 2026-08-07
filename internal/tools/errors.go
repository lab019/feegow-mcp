// Package tools holds the atendimento (customer-service) tool logic this
// service exposes: pure functions of (context, *feegow.Client, args) that
// return typed results or errors, with zero dependency on
// modelcontextprotocol/go-sdk. internal/mcpserver is the only package that
// wires these into actual MCP tools — see its package doc comment. Keeping
// the SDK out of this package is what lets every test here run without
// network and without spinning up an MCP session (ESPECIFICACAO.md §4).
package tools

import (
	"errors"
	"strings"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// ErrNaoLocalizado is the single, uniform outcome for every patient-lookup
// failure this package can produce: a CPF/telefone that matches no
// cadastro, and a CPF/telefone that matches a cadastro but whose second
// fact (data de nascimento / nome completo) does not. Deliberately the same
// sentinel value for both — see the package doc on IdentificarPaciente — so
// a caller (and, downstream, an attacker probing with a purchased CPF list)
// can never distinguish "does not exist" from "exists, but you don't know
// the second fact" from the error text, error type, or timing-insensitive
// behavior. Never wrap this error with dynamic content (a CPF, a name): that
// would defeat the entire point of it being a single sentinel.
var ErrNaoLocalizado = errors.New("não consegui localizar esse cadastro")

// ArgumentError reports a caller-side problem with a tool's arguments —
// caught and rejected before any Feegow request is built. It is a distinct
// type from ErrNaoLocalizado (never conflated) because the two mean very
// different things: ArgumentError is "you called this wrong", to be fixed
// by the caller re-reading the tool's schema; ErrNaoLocalizado is "your
// input was well-formed, but no such patient could be confirmed."
type ArgumentError struct {
	Msg string
}

func (e *ArgumentError) Error() string { return e.Msg }

// ErrConflitoFeegow is the sanitized stand-in for any feegow.ConflictError
// (HTTP 409) a tool in this package would otherwise surface. See
// SanitizeFeegowError — never wrap this with the original Feegow body.
var ErrConflitoFeegow = errors.New("feegow: houve um conflito ao processar a solicitação junto à clínica; " +
	"os dados podem estar desatualizados ou já existir um registro equivalente")

// ErrEntradaInvalidaFeegow is the sanitized stand-in for any
// feegow.ValidationError (HTTP 422) a tool in this package would otherwise
// surface. See SanitizeFeegowError — never wrap this with the original
// Feegow body.
var ErrEntradaInvalidaFeegow = errors.New("feegow: um ou mais parâmetros enviados não foram aceitos pela clínica; " +
	"revise os dados informados")

// ErrOperacaoNaoConfirmadaFeegow is the sanitized stand-in for a
// success:false outcome from an EnvelopeNone endpoint — see
// checkEnvelopeNoneSuccess. EnvelopeNone endpoints (patient.upload_base64,
// appoints.queue_position, ...) always answer with HTTP 200, so their
// success:false case never becomes a feegow.ConflictError and never routes
// through SanitizeFeegowError; without this sentinel a tool would be
// tempted to interpolate the raw Content straight into its own error, the
// exact PII leak (a Feegow free-text body carrying a patient's nome/CPF)
// the adversarial review caught in anexar_ao_prontuario. Never wrap this
// with the original Feegow body.
var ErrOperacaoNaoConfirmadaFeegow = errors.New("feegow: a operação não foi confirmada pela clínica; " +
	"verifique os dados informados")

// checkEnvelopeNoneSuccess is the single choke point every EnvelopeNone
// tool with a raw top-level "success" (or equivalent) field routes its
// post-decode check through — the EnvelopeNone counterpart to
// SanitizeFeegowError. An EnvelopeNone response never goes through
// parseSuccess's success check (that is the whole reason EnvelopeNone
// exists for these endpoints — see internal/feegow/registry.go's notes on
// patient.upload_base64/appoints.queue_position), so nothing upstream of
// the tool itself ever verifies success for it. A tool that forgets this
// check either silently reports a Feegow-side failure as its own success
// (gerar_senha_atendimento before this fix) or leaks Feegow's raw
// free-text body straight into its own error (anexar_ao_prontuario before
// this fix) — both closed by routing every EnvelopeNone tool's
// success:false case through this one function instead of each tool
// hand-rolling (and potentially forgetting) its own sanitization.
func checkEnvelopeNoneSuccess(success bool) error {
	if !success {
		return ErrOperacaoNaoConfirmadaFeegow
	}
	return nil
}

// SanitizeFeegowError is the single choke point every atendimento tool
// that can surface a raw Feegow error routes through before returning to
// its caller. It maps feegow.ConflictError (409, whose Content is Feegow's
// raw free-text error body) and feegow.ValidationError (422, whose Fields
// echoes back field names — both observed by the adversarial review to
// carry patient PII straight from the request, e.g. a name or CPF in a
// "pendência financeira" message) to the two fixed, PII-free sentinels
// above. The caller still learns *which* kind of problem happened — a
// business conflict vs. bad input — just never the Feegow body itself,
// which can and does end up in a public-facing agent transcript
// (ESPECIFICACAO.md §10; the same LGPD reasoning internal/feegow/errors.go
// already applies to 5xx bodies, extended here to 409/422).
//
// Every other error type — ArgumentError, ErrNaoLocalizado,
// feegow.CredentialError, feegow.InternalError,
// feegow.UnexpectedStatusError, a transport error, a decoding error,
// context.Canceled — passes through unchanged: none of those carry a
// Feegow-controlled free-text body, and disguising them would hide a real
// operational problem instead of a privacy one.
//
// A tool wires this in once, at its single outermost error-return point
// (see ConsultarAgenda, BuscarHorariosLivres), rather than case-by-case at
// every client.Call site — so a new Feegow call added later inside that
// function does not have to remember to sanitize anything itself; it
// already flows through this one call on the way out. This is what makes
// the discipline hard to regress, per the adversarial review that flagged
// this gap.
func SanitizeFeegowError(err error) error {
	if err == nil {
		return nil
	}
	var conflict *feegow.ConflictError
	if errors.As(err, &conflict) {
		return ErrConflitoFeegow
	}
	var validation *feegow.ValidationError
	if errors.As(err, &validation) {
		return ErrEntradaInvalidaFeegow
	}
	return err
}

// ErrHorarioOcupado is the sanitized, ACTIONABLE stand-in for Feegow's
// undocumented 409 "Já existe um agendamento para esse horario e
// profissional" (confirmed against the real API by Fase 0) — a plain race:
// buscar_horarios_livres reported the slot free, and someone else took it
// between that read and this write. ESPECIFICACAO.md §7 regra 4 is explicit
// that this is recoverable state, not a failure: the caller should offer
// the patient different times, not just report "an error happened" the way
// the opaque ErrConflitoFeegow would. See classifyAppointConflict, the
// single place that distinguishes this from ErrPacienteJaTemAgendamento
// below (same HTTP status, same envelope, different free-text Content —
// conflating them would lose exactly the distinction this sentinel exists
// for).
var ErrHorarioOcupado = errors.New("feegow: esse horário acabou de ficar indisponível — outra pessoa agendou entre a consulta e a confirmação; ofereça outros horários ao paciente")

// ErrPacienteJaTemAgendamento is the sanitized, ACTIONABLE stand-in for
// Feegow's undocumented, and undocumented-anywhere-in-doc.txt, 409 "Esse
// paciente já possui um agendamento nessa agenda." — confirmed by Fase 0.
// Deliberately NOT the same sentinel as ErrHorarioOcupado: this is not a
// race and has nothing to do with the specific horário requested — it
// fires because the SAME paciente already has ANY other agendamento with
// the SAME profissional, no matter when. "tente outro horário" is the
// wrong advice for this one; the actionable fix is a different
// profissional or dealing with the existing agendamento first (e.g.
// remarcar it) — which is also why remarcar must call /appoints/reschedule
// directly rather than "new-appoint then cancel-appoint": the new-appoint
// half of that sequence would hit exactly this 409 before the old
// agendamento is ever cancelled.
var ErrPacienteJaTemAgendamento = errors.New("feegow: esse paciente já tem um agendamento com esse profissional — avise o paciente e sugira remarcar o existente ou escolher outro profissional")

// conflictMsgHorarioOcupado and conflictMsgPacienteJaTem are the exact,
// Fase-0-verified free-text Content strings Feegow's 409 carries for the
// two conflicts above. Matched with strings.Contains rather than equality:
// the verified strings are the load-bearing substrings, and matching on
// them tolerates surrounding punctuation/whitespace this client has not
// independently observed rather than silently falling through to the
// opaque ErrConflitoFeegow the moment Feegow's wording shifts by a
// character.
const (
	conflictMsgHorarioOcupado = "Já existe um agendamento para esse horario e profissional"
	conflictMsgPacienteJaTem  = "Esse paciente já possui um agendamento nessa agenda"
)

// classifyAppointConflict is agendar and remarcar's single outermost
// error-return choke point (mirroring SanitizeFeegowError's role for the
// read tools): it recognizes the two Fase-0-verified 409 conflicts specific
// to booking/moving an agendamento and maps them to their own actionable
// sentinels above, before falling back to SanitizeFeegowError for every
// other error (including every other 409 Content, and every 422) — those
// still get the generic, PII-safe treatment. Never applied to cancelar or
// confirmar: neither one creates or moves an agendamento, so neither of
// these two specific conflicts is a real outcome for them — resolveOwnedAgendamento
// (agendamento_escrita.go) already turns "id not mine" into ErrNaoLocalizado
// before either tool's underlying Feegow call is ever made.
func classifyAppointConflict(err error) error {
	var conflict *feegow.ConflictError
	if errors.As(err, &conflict) {
		switch {
		case strings.Contains(conflict.Content, conflictMsgHorarioOcupado):
			return ErrHorarioOcupado
		case strings.Contains(conflict.Content, conflictMsgPacienteJaTem):
			return ErrPacienteJaTemAgendamento
		}
	}
	return SanitizeFeegowError(err)
}
