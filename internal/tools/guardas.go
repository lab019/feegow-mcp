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

// requireConfirmacao enforces guard 5 for a write tool, on EITHER profile:
// an explicit boolean confirmation is required before any Feegow mutating
// call, checked first, ahead of every other guard — an unconfirmed call
// should never leak *any* information about which other argument was
// wrong, since the caller had no business asking Feegow anything yet.
//
// whoConfirms names, in Portuguese, who that confirmation must actually
// come from — the human on the other end of THIS tool's conversation, never
// confirmation manufactured by the calling agent/model itself. Which human
// that is differs by profile: on the public, unauthenticated atendimento
// channel it is the paciente (see IdentidadeArgs' doc comment); on the
// private, Feegow-token-authenticated admin channel it is the clínica's own
// operador (see registerAdminOnlyTools' package doc in internal/mcpserver).
// Generalized here, instead of two near-identical guards, so both profiles
// share one implementation instead of drifting apart by hand.
func requireConfirmacao(confirmed bool, tool, whoConfirms string) error {
	if !confirmed {
		return &ArgumentError{Msg: fmt.Sprintf(
			"%s exige confirmação explícita — ela precisa vir do(a) %s na conversa, "+
				"nunca ser assumida pelo agente", tool, whoConfirms)}
	}
	return nil
}

// requireConfirmacaoPaciente is requireConfirmacao specialized for the
// atendimento profile's confirmacao_paciente argument — kept as a named
// wrapper (rather than inlining "paciente" at every call site) since every
// atendimento write tool already calls it by this name.
func requireConfirmacaoPaciente(confirmed bool, tool string) error {
	return requireConfirmacao(confirmed, tool, "paciente")
}

// requireConfirmacaoOperador is requireConfirmacao specialized for the
// admin profile's confirmacao argument: the confirmation must come from the
// clínica's own operador (the human running this admin tool), never from
// the paciente (who has no part in this private, staff-facing channel — see
// this file's package doc and ESPECIFICACAO.md §3 on why the admin profile
// applies none of atendimento's public-channel restrictions).
func requireConfirmacaoOperador(confirmed bool, tool string) error {
	return requireConfirmacao(confirmed, tool, "operador da clínica")
}

// requireConfirmacaoReforcada is requireConfirmacaoOperador's stronger
// sibling, for the two operations Fase 4b singles out as the most
// destructive in the entire project: remover_registro_financeiro's
// invoice/payment removal (financial.invoice_remove, financial.payment_remove
// — DELETE calls against the clínica's own financial records, irreversible
// on the Feegow side, no "cancel" or "undo" endpoint documented anywhere in
// this registry). A single confirmacao=true, indistinguishable from the one
// every other admin write tool already accepts, is not enough friction for
// an operation an agent could otherwise fire off exactly like
// atualizar_paciente — this requires a SECOND, independently-named boolean
// (cienteIrreversivel) that means something different from "eu quero fazer
// isso": specifically, "eu entendo que isso é permanente e não pode ser
// desfeito". Checked in the same order requireConfirmacao already
// establishes (confirmation first, before any other argument is even
// looked at) — an unconfirmed call must never leak which id was wrong.
func requireConfirmacaoReforcada(confirmado, cienteIrreversivel bool, tool string) error {
	if err := requireConfirmacaoOperador(confirmado, tool); err != nil {
		return err
	}
	if !cienteIrreversivel {
		return &ArgumentError{Msg: fmt.Sprintf(
			"%s exige uma SEGUNDA confirmação (ciente_irreversivel=true) — este registro financeiro "+
				"é apagado de forma PERMANENTE, sem desfazer; confirmacao=true sozinho não é suficiente "+
				"para esta operação", tool)}
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
// DELIBERATELY LOOSE, on purpose — do not "fix" this back to a strict
// same-day UTC comparison: this service has no notion of the clinic's own
// timezone (ESPECIFICACAO.md does not define one), and Feegow itself is the
// authority on where the "hoje" boundary actually falls — it already
// enforces the exact cutoff server-side, with the correct notion of today,
// on every write. A strict `t.Before(todayUTC)` check sounds equivalent but
// is not: for any timezone behind UTC (e.g. America/Sao_Paulo, UTC-3),
// there is a multi-hour window every single day — 21:00-23:59 local time —
// where UTC has already rolled to tomorrow while it is still "hoje" for the
// patient. A strict check rejects that patient's own today as "retroactive"
// during that window, every day, right in the middle of typical reception
// hours — a real, reproducible false rejection this guard must never cause,
// since it adds no security by rejecting it (Feegow enforces the true
// boundary regardless).
//
// So this only rejects what is unambiguously past in EVERY timezone: more
// than one full day before UTC "hoje". That still catches the guard's
// actual purpose — a gross caller mistake like agendar for last month or
// last year — without ever second-guessing a date that could legitimately
// be "hoje" somewhere. The precise same-day/next-day boundary is left to
// Feegow, which is the authority on it.
func validateNotPast(role, date string) error {
	t, err := time.Parse(feegow.ISO8601, date)
	if err != nil {
		return &ArgumentError{Msg: fmt.Sprintf("%s deve estar em ISO-8601 (YYYY-MM-DD)", role)}
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	cutoff := today.AddDate(0, 0, -1) // one full day of slack — see doc comment above
	if t.Before(cutoff) {
		return &ArgumentError{Msg: fmt.Sprintf(
			"%s não pode ser retroativa: %s é anterior a hoje — não é possível agendar no passado", role, date)}
	}
	return nil
}
