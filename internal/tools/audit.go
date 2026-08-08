package tools

import (
	"fmt"
	"log"

	"github.com/lab019/feegow-mcp/internal/loglevel"
)

// auditWrite records that an atendimento write tool actually executed a
// mutating Feegow call — ESPECIFICACAO.md §7 guard 6. It is deliberately
// PII-free, the same discipline internal/feegow's per-request log line and
// internal/tools/paciente.go's logShapeWarning already follow: only the
// tool name, the paciente_id the two-fact identity pair resolved to (an
// internal Feegow identifier, not personal data by itself — the same
// reasoning identificar_paciente's own result already relies on) and,
// when relevant, the agendamento_id acted upon ever reach this line —
// never a CPF, telefone, nome or data de nascimento.
//
// The line spells out explicitly that the identity behind pacienteID is
// CLAIMED, never verified: this is a public, unauthenticated channel (see
// IdentidadeArgs' doc comment) by deliberate product decision, and an
// operator reading this log later must not mistake "this write happened"
// for "we confirmed who did it" — that conflation is exactly what would
// turn an honest audit trail into a false sense of authentication.
//
// agendamentoID is optional context: pass 0 when the write isn't about a
// specific existing agendamento (agendar, criar_paciente) — 0 is never a
// real Feegow id (validateAgendamentoID rejects it as a caller argument),
// so it is unambiguous as an "not applicable" sentinel here.
//
// Gated by loglevel.Verbose(), like every other informational line this
// service emits — a noise knob, never a safety control: the fields logged
// are already bounded/PII-free regardless of level.
func auditWrite(tool string, pacienteID, agendamentoID int) {
	if !loglevel.Verbose() {
		return
	}
	if agendamentoID != 0 {
		log.Printf("feegow: WRITE %s — identidade ALEGADA (não verificada) resolveu paciente_id=%d; agendamento_id=%d",
			tool, pacienteID, agendamentoID)
		return
	}
	log.Printf("feegow: WRITE %s — identidade ALEGADA (não verificada) resolveu paciente_id=%d", tool, pacienteID)
}

// auditReuse records that criar_paciente resolved an EXISTING cadastro via
// tryIdentifyForCreate (paciente_create.go) and therefore never called
// /patient/create — the opposite outcome of auditWrite, and deliberately a
// distinct log line rather than a call to auditWrite: auditWrite's own doc
// comment defines it as recording that a mutating Feegow call actually
// happened, and this path is precisely the one where it did not. Reusing
// auditWrite here would make every "cadastro já existia" lookup
// indistinguishable, in the log, from a real /patient/create — inflating
// whatever downstream count relies on that line (e.g. "quantos cadastros
// criar_paciente criou hoje") by every deduplicated call. Same PII
// discipline as auditWrite: only the tool name and the resolved paciente_id
// ever reach this line.
func auditReuse(tool string, pacienteID int) {
	if !loglevel.Verbose() {
		return
	}
	log.Printf("feegow: REUSE %s — identidade ALEGADA (não verificada) resolveu paciente_id=%d; cadastro já existia, patient/create NÃO foi chamado",
		tool, pacienteID)
}

// auditAdminWrite is auditWrite's admin-profile counterpart: it records
// that an admin write tool actually executed a mutating Feegow call.
// Deliberately a DIFFERENT log line, not a call to auditWrite, because the
// "identidade ALEGADA (não verificada)" framing auditWrite always spells
// out is specific to atendimento's unauthenticated, self-declared identity
// — it does not apply here. The admin profile's caller is authenticated by
// the clínica's own Feegow token before the call ever reaches this service
// (see internal/mcpserver's package doc on the two profiles), so this line
// only needs to record WHAT happened, never hedge about WHO claims to be
// acting.
//
// Same PII discipline as auditWrite regardless: only internal Feegow
// identifiers — paciente_id, agendamento_id — ever reach this line, never a
// CPF, telefone, nome or data de nascimento. Pass 0 for whichever id does
// not apply to a given write (e.g. agendamentoID for atualizar_paciente,
// pacienteID for atualizar_status_agendamento) — 0 is never a real Feegow
// id, so it is unambiguous as a "not applicable" sentinel, same convention
// auditWrite's agendamentoID already uses.
func auditAdminWrite(tool string, pacienteID, agendamentoID int) {
	if !loglevel.Verbose() {
		return
	}
	switch {
	case pacienteID != 0 && agendamentoID != 0:
		log.Printf("feegow: ADMIN WRITE %s — paciente_id=%d; agendamento_id=%d", tool, pacienteID, agendamentoID)
	case pacienteID != 0:
		log.Printf("feegow: ADMIN WRITE %s — paciente_id=%d", tool, pacienteID)
	case agendamentoID != 0:
		log.Printf("feegow: ADMIN WRITE %s — agendamento_id=%d", tool, agendamentoID)
	default:
		log.Printf("feegow: ADMIN WRITE %s", tool)
	}
}

// auditAdminWriteRecord is auditAdminWrite's counterpart for admin writes in
// the Financeiro/Estoque groups, whose identifying context is a single
// record id (a fatura, pagamento, voucher, produto — an amount of money or
// a stock item, never a paciente/agendamento) that does not fit
// auditAdminWrite's (pacienteID, agendamentoID) shape. label names WHAT
// recordID is (e.g. "invoice_id", "voucher_id") so the log line says which
// record a write touched — critical for the two irreversible DELETEs in
// remover_registro_financeiro, where "something was removed" without an id
// is useless after the fact. Same PII discipline as auditAdminWrite: label
// and recordID must both be internal Feegow identifiers/field names, never
// patient data (there is none in this domain to begin with).
func auditAdminWriteRecord(tool, label string, recordID int) {
	if !loglevel.Verbose() {
		return
	}
	log.Printf("feegow: ADMIN WRITE %s — %s=%d", tool, label, recordID)
}

// auditRecordIDMaxRunes bounds the rendered form of an opaque record id in
// auditAdminWriteRecordOpaque. The id comes from the caller (the LLM), so
// without a bound a single audit line could carry an arbitrarily long
// string into the service log.
const auditRecordIDMaxRunes = 64

// auditAdminWriteRecordOpaque is auditAdminWriteRecord for the one admin
// write whose record id is NOT an int on our side: gerenciar_faturamento's
// billing_id, typed `any` because Feegow's own type for it was never
// confirmed by a real call (see GerenciarFaturamentoArgs). Before this,
// that write logged auditAdminWrite(tool, 0, 0) — recording that a guia was
// edited while discarding WHICH one, the exact traceability hole
// auditAdminWriteRecord exists to close for its int-typed siblings.
//
// Rendered with %q over a rune-bounded %v: %v because the underlying type
// is genuinely unknown (a JSON number decodes to float64, an id sent as a
// string stays a string), %q because the value is caller-controlled and an
// unescaped newline in a log line is how one audit record becomes two, and
// bounded because an `any` from a JSON payload has no natural length limit.
// Same PII discipline as auditAdminWriteRecord — billing_id identifies a
// guia de faturamento, never a person.
func auditAdminWriteRecordOpaque(tool, label string, recordID any) {
	if !loglevel.Verbose() {
		return
	}
	rendered := fmt.Sprintf("%v", recordID)
	if r := []rune(rendered); len(r) > auditRecordIDMaxRunes {
		rendered = string(r[:auditRecordIDMaxRunes]) + "…"
	}
	log.Printf("feegow: ADMIN WRITE %s — %s=%q", tool, label, rendered)
}

// auditAdminWriteQueue is auditAdminWrite's counterpart for
// gerar_senha_atendimento specifically: that write does not act on a
// paciente_id or agendamento_id (see GerarSenhaAtendimento's doc comment
// — it consumes the next position in a unidade's queue, nothing else), so
// auditAdminWrite's (pacienteID, agendamentoID) shape does not fit. Every
// other admin write tool calls auditAdminWrite; this one exists only
// because gerar_senha_atendimento's identifying context genuinely is
// (unidade_id, tipo_senha) instead — same PII discipline: only internal
// Feegow identifiers, never patient data (there is none here to begin
// with).
func auditAdminWriteQueue(unidadeID, tipoSenha int) {
	if !loglevel.Verbose() {
		return
	}
	log.Printf("feegow: ADMIN WRITE gerar_senha_atendimento — unidade_id=%d; tipo_senha=%d", unidadeID, tipoSenha)
}
