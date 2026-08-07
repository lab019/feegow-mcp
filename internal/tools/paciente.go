package tools

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
	"github.com/lab019/feegow-mcp/internal/loglevel"
)

// IdentidadeArgs is the argument shape shared by identificar_paciente and
// consultar_agenda: exactly one of the two fact-pairs below, never a raw
// paciente_id (see the package doc on ConsultarAgenda for why that matters).
//
// The two pairs exist because a single fact is not an identity check on a
// public, unauthenticated surface: CPF alone is not a secret (bases leak),
// so accepting it alone would turn this service into a lookup oracle for
// whoever bought a leaked CPF list. Requiring two independent facts that
// must both match — CPF+data de nascimento, or telefone+nome completo — is
// what the paciente's own family member (the realistic caller: a child
// rescheduling for a parent, a spouse, a caregiver) knows and a CPF-list
// buyer does not.
type IdentidadeArgs struct {
	CPF            string `json:"cpf,omitempty" jsonschema:"CPF do paciente (apenas dígitos). Use SOMENTE junto com data_nascimento — nunca sozinho, nunca combinado com telefone/nome_completo."`
	DataNascimento string `json:"data_nascimento,omitempty" jsonschema:"Data de nascimento do paciente, ISO-8601 (YYYY-MM-DD). Use junto com cpf."`
	Telefone       string `json:"telefone,omitempty" jsonschema:"Telefone do paciente (apenas dígitos). Use SOMENTE junto com nome_completo — nunca sozinho, nunca combinado com cpf/data_nascimento."`
	NomeCompleto   string `json:"nome_completo,omitempty" jsonschema:"Nome completo do paciente. Use junto com telefone."`
}

// IdentificarPacienteResult is deliberately the minimum needed to act:
// which cadastro this is, and nothing that identifies who that cadastro
// belongs to. No nome, CPF, endereço, documento, histórico or dado clínico
// is ever placed on this struct — see ESPECIFICACAO.md §10 and the package
// doc above. ja_existia is always true in this read-only phase (a resolved
// identity necessarily means the cadastro was found, not created); the
// field exists so the shape is stable if a future phase adds a
// create-on-miss path.
type IdentificarPacienteResult struct {
	PacienteID int  `json:"paciente_id"`
	JaExistia  bool `json:"ja_existia"`
}

// identityPair names which of the two accepted fact-pairs a request
// resolved to.
type identityPair int

const (
	pairInvalid identityPair = iota
	pairCPFBirth
	pairPhoneName
)

// classifyIdentityPair validates args before any Feegow call is made
// (acceptance criterion 2): it accepts exactly one complete pair and
// rejects everything else — a single fact, both pairs at once, or nothing
// at all — as an *ArgumentError.
func classifyIdentityPair(args IdentidadeArgs) (identityPair, error) {
	cpf := strings.TrimSpace(args.CPF)
	dob := strings.TrimSpace(args.DataNascimento)
	tel := strings.TrimSpace(args.Telefone)
	nome := strings.TrimSpace(args.NomeCompleto)

	hasCPFPair := cpf != "" || dob != ""
	hasPhonePair := tel != "" || nome != ""
	cpfComplete := cpf != "" && dob != ""
	phoneComplete := tel != "" && nome != ""

	switch {
	case cpfComplete && !hasPhonePair:
		return pairCPFBirth, nil
	case phoneComplete && !hasCPFPair:
		return pairPhoneName, nil
	case hasCPFPair && hasPhonePair:
		return pairInvalid, &ArgumentError{Msg: "informe apenas um par de identificação por vez: " +
			"cpf+data_nascimento OU telefone+nome_completo, nunca os dois"}
	default:
		return pairInvalid, &ArgumentError{Msg: "identificação incompleta: um fato isolado não é suficiente. " +
			"Informe o par completo cpf+data_nascimento OU telefone+nome_completo"}
	}
}

// IdentificarPaciente is the tool's core contract: given exactly one
// fact-pair, resolve it to a paciente_id (and nothing else) or fail with
// the single uniform ErrNaoLocalizado, indistinguishable whether the
// cadastro does not exist or exists but the second fact did not match
// (acceptance criterion 4) — distinguishing the two would let repeated
// calls enumerate valid CPFs against the clinic's real patient base.
func IdentificarPaciente(ctx context.Context, client *feegow.Client, args IdentidadeArgs) (*IdentificarPacienteResult, error) {
	pair, err := classifyIdentityPair(args)
	if err != nil {
		return nil, err
	}

	switch pair {
	case pairCPFBirth:
		return identifyByCPF(ctx, client, strings.TrimSpace(args.CPF), strings.TrimSpace(args.DataNascimento))
	case pairPhoneName:
		return identifyByPhone(ctx, client, strings.TrimSpace(args.Telefone), strings.TrimSpace(args.NomeCompleto))
	default:
		// Unreachable: classifyIdentityPair never returns pairInvalid
		// alongside a nil error.
		return nil, ErrNaoLocalizado
	}
}

// patientListLimit bounds every patient.list call this package makes. A
// directed lookup — both required facts already filtering server-side — is
// never expected to need more than a small handful of candidates back, and
// this is what keeps the call from ever becoming the unbounded enumeration
// /patient/list was excluded from the atendimento profile for (see this
// file's package-level doc and ESPECIFICACAO.md §10): the danger is a call
// with no filter and no cap, not the endpoint itself.
const patientListLimit = 5

// dataAniversarioFormat is the Go reference-time layout for /patient/list's
// "data_aniversario" filter. doc.txt's prose describes this field as
// "dd-mm", but its own worked example ("data_aniversario=01-30" filtering
// toward a nascimento of "...-01-30") is unambiguously MM-DD — a
// self-contradiction within doc.txt itself, same category as the ones
// ESPECIFICACAO.md §8 already tracks. The worked example is implemented
// here, matching how every other such contradiction in this codebase is
// resolved (see internal/feegow/registry.go's "lock.list" entry for the
// same pattern). This filter is never the decisive check either way — see
// identifyByCPF.
const dataAniversarioFormat = "01-02"

// patientListEntry is the subset of one /patient/list response entry this
// package reads. Every other field the endpoint returns (nome_social,
// bairro, tabela_id, sexo_id, email, celular, criado_em, alterado_em,
// programa_de_saude, ...) is deliberately left unmapped: it is read from
// the wire and then simply never touched, which is what keeps it out of
// every result this package returns.
type patientListEntry struct {
	PatientID  *int   `json:"patient_id"`
	Nome       string `json:"nome"`
	Nascimento string `json:"nascimento"` // ISO-8601 (YYYY-MM-DD) on /patient/list — unlike /patient/search's DD-MM-YYYY.
}

// identifyByCPF resolves the CPF+data de nascimento pair against
// /patient/list. "cpf" is /patient/list's documented filter (doc.txt's
// query-params table for "Listar pacientes"); "data_aniversario" is sent
// alongside it purely to narrow the search server-side — it carries no
// year, so it can never be the decisive check. The year only lives in
// nascimento, which is why the comparison against the caller's full
// ISO-8601 dob happens here, in memory, after the call.
func identifyByCPF(ctx context.Context, client *feegow.Client, cpf, dob string) (*IdentificarPacienteResult, error) {
	cpfDigits := onlyDigits(cpf)
	if cpfDigits == "" {
		return nil, ErrNaoLocalizado
	}
	dobTime, err := time.Parse(feegow.ISO8601, dob)
	if err != nil {
		return nil, &ArgumentError{Msg: "data_nascimento deve estar em ISO-8601 (YYYY-MM-DD)"}
	}

	patients, err := listPatients(ctx, client, map[string]any{
		"cpf":              cpfDigits,
		"data_aniversario": dobTime.Format(dataAniversarioFormat),
	})
	if err != nil {
		return nil, err
	}
	// Exactly one candidate, never the first of several: more than one
	// match is ambiguous (never picked from), and zero is a plain
	// not-found — both collapse to the same uniform error.
	if len(patients) != 1 {
		return nil, ErrNaoLocalizado
	}

	patient := patients[0]
	if patient.Nascimento != dobTime.Format(feegow.ISO8601) {
		return nil, ErrNaoLocalizado
	}

	return &IdentificarPacienteResult{PacienteID: *patient.PatientID, JaExistia: true}, nil
}

// identifyByPhone resolves the telefone+nome completo pair against
// /patient/list, whose "telefone" query param is clearly documented (unlike
// /patient/search, whose only documented filters are paciente_id/
// paciente_cpf — see internal/feegow/registry.go's patient.search Notes).
// The name is compared here, in memory, using the same normalization
// identifyByCPF's sibling comparison relies on elsewhere in this package.
func identifyByPhone(ctx context.Context, client *feegow.Client, telefone, nomeCompleto string) (*IdentificarPacienteResult, error) {
	telDigits := onlyDigits(telefone)
	if telDigits == "" || nomeCompleto == "" {
		return nil, ErrNaoLocalizado
	}

	patients, err := listPatients(ctx, client, map[string]any{"telefone": telDigits})
	if err != nil {
		return nil, err
	}
	if len(patients) != 1 {
		return nil, ErrNaoLocalizado
	}

	patient := patients[0]
	if normalizeName(patient.Nome) != normalizeName(nomeCompleto) {
		return nil, ErrNaoLocalizado
	}

	return &IdentificarPacienteResult{PacienteID: *patient.PatientID, JaExistia: true}, nil
}

// listPatients calls patient.list with params plus a fixed, low limit
// (patientListLimit — never unbounded) and decodes its content into
// patientListEntry values.
//
// Every failure this function can hit collapses, from identifyByCPF/
// identifyByPhone's point of view, into either a normal empty result or
// ErrNaoLocalizado — acceptance criterion 4 requires that uniformity. But
// two very different situations produce that same ErrNaoLocalizado, and
// only one of them is logged:
//   - Feegow legitimately reporting no match (folded by mapNotFound, e.g. a
//     409/422) is an entirely ordinary outcome of a directed lookup that
//     found nothing — never logged.
//   - The response shape itself being unexpected (content not a JSON array,
//     an element that doesn't decode, an element missing patient_id) means
//     this function can no longer trust what Feegow sent back — that is an
//     operational problem (Feegow's response shape changed under this
//     integration), and it is invisible to whoever operates this service
//     unless it is logged. logShapeWarning does that, at WARN, without ever
//     including the request or response payload.
func listPatients(ctx context.Context, client *feegow.Client, params map[string]any) ([]patientListEntry, error) {
	resp, err := client.Call(ctx, "patient.list", feegow.Request{
		Params:     params,
		Pagination: &feegow.Pagination{Limit: patientListLimit},
	})
	if err != nil {
		return nil, mapNotFound(err)
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(resp.Content, &raw); err != nil {
		logShapeWarning("content de patient.list não é um array JSON")
		return nil, ErrNaoLocalizado
	}

	patients := make([]patientListEntry, 0, len(raw))
	for _, r := range raw {
		var p patientListEntry
		if err := json.Unmarshal(r, &p); err != nil {
			logShapeWarning("elemento de patient.list não decodificável")
			return nil, ErrNaoLocalizado
		}
		if p.PatientID == nil {
			logShapeWarning("elemento de patient.list sem patient_id")
			return nil, ErrNaoLocalizado
		}
		patients = append(patients, p)
	}
	return patients, nil
}

// logShapeWarning makes a shape-unexpected patient.list response visible to
// whoever operates this service — otherwise, if Feegow changes that
// endpoint's response shape, identificar_paciente silently stops working
// and every call just looks like an ordinary "não localizado" to the
// caller, indistinguishable from a real non-match (see this file's
// ErrNaoLocalizado doc and listPatients above).
//
// It never logs request or response data: only this fixed set of
// reason strings and the endpoint name ever reach the log line, per
// ESPECIFICACAO.md §10 — no CPF, telefone, nome, nascimento or response
// body. The caller-facing error is unaffected either way (still the same
// ErrNaoLocalizado) — this only changes what an operator can see.
func logShapeWarning(reason string) {
	logShapeWarningFor("patient.list", reason, "identificar_paciente não consegue mais confirmar identidades")
}

// logShapeWarningFor is logShapeWarning's generalization, shared by every
// tool in this package that needs to surface "an upstream Feegow response
// no longer matches the shape this integration expects" to an operator —
// e.g. agendar's decodeAgendamentoID (agendar.go). Same discipline: never
// logs request or response data, only the fixed endpoint/reason/consequence
// strings its caller passes in, per ESPECIFICACAO.md §10.
func logShapeWarningFor(endpoint, reason, consequence string) {
	if !loglevel.WarnEnabled() {
		return
	}
	log.Printf("feegow: WARN %s — %s (%s)", endpoint, reason, consequence)
}

// mapNotFound folds the two Feegow error shapes that mean "no such
// cadastro" — 409 ConflictError and 422 ValidationError — into
// ErrNaoLocalizado, so a missing filter or an unmatched lookup reads
// exactly like a genuine "patient not found", not like a distinct kind of
// failure an attacker could use to tell the two apart. Every other error
// (credential, internal, unexpected status, transport) passes through
// unchanged: those are infra/credential failures, not lookup outcomes, and
// disguising them would hide a real operational problem behind a
// patient-facing message.
func mapNotFound(err error) error {
	var conflict *feegow.ConflictError
	var validation *feegow.ValidationError
	if errors.As(err, &conflict) || errors.As(err, &validation) {
		return ErrNaoLocalizado
	}
	return err
}
