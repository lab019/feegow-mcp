package tools

import (
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
