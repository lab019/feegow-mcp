package tools

import (
	"fmt"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds the business-rule guards ESPECIFICACAO.md §7 requires
// every atendimento *write* tool to enforce client-side, before any Feegow
// request is built — Fase 0 confirmed each of these against the real API
// (see agendar.go/agendamento_escrita.go's doc comments for the specific
// evidence). They are collected here, instead of duplicated per tool file,
// because every one of them is a caller-input problem (an *ArgumentError),
// not something specific to any single endpoint's wire shape.

// requireConfirmacaoPaciente enforces guard 5: every write this profile
// exposes requires an explicit boolean confirmation before it is allowed to
// act. It is deliberately checked first, ahead of every other guard in each
// tool — an unconfirmed call should never leak *any* information about
// which other argument was wrong, since the caller had no business asking
// Feegow anything yet.
//
// The bool is confirmation from the PACIENTE (the human on the other end of
// this public, unauthenticated channel — see IdentidadeArgs' doc comment),
// never confirmation manufactured by the calling agent/model itself; each
// tool's jsonschema description says so explicitly.
func requireConfirmacaoPaciente(confirmed bool, tool string) error {
	if !confirmed {
		return &ArgumentError{Msg: fmt.Sprintf(
			"%s exige confirmacao_paciente=true — a confirmação precisa vir do paciente na conversa, "+
				"nunca ser assumida pelo agente", tool)}
	}
	return nil
}

// validateAgendamentoID enforces that a caller-supplied agendamento_id is at
// least well-formed (a positive integer) before any Feegow call — a
// zero/negative id is a caller bug, not a posse (ownership) question, so it
// is rejected here rather than reaching resolveOwnedAgendamento
// (agendamento_escrita.go) only to fail there with the less specific
// ErrNaoLocalizado.
func validateAgendamentoID(id int) error {
	if id <= 0 {
		return &ArgumentError{Msg: "agendamento_id deve ser um identificador positivo"}
	}
	return nil
}

// validateNotPast enforces guard 1 (ESPECIFICACAO.md §7 regra 1): no
// retroactive agendamento. Feegow itself rejects a past "data" for
// /appoints/new-appoint with a 422 ("data": ["...posterior ou igual a
// today"]) — confirmed by Fase 0 — but catching it here, before any request
// leaves this process, both avoids the round trip and gives a readable
// Portuguese message instead of the raw validation body
// SanitizeFeegowError would otherwise fold into the opaque
// ErrEntradaInvalidaFeegow.
//
// Comparison is done at UTC day granularity: parseISODate-equivalent
// parsing (time.Parse with a date-only layout) always yields UTC midnight,
// so truncating time.Now() to a UTC day boundary the same way makes "hoje"
// the same calendar day on both sides regardless of the server's local
// timezone — this service has no notion of the clinic's own timezone to
// use instead, and ESPECIFICACAO.md does not define one.
func validateNotPast(role, date string) error {
	t, err := time.Parse(feegow.ISO8601, date)
	if err != nil {
		return &ArgumentError{Msg: fmt.Sprintf("%s deve estar em ISO-8601 (YYYY-MM-DD)", role)}
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if t.Before(today) {
		return &ArgumentError{Msg: fmt.Sprintf(
			"%s não pode ser retroativa: %s é anterior a hoje — não é possível agendar no passado", role, date)}
	}
	return nil
}
