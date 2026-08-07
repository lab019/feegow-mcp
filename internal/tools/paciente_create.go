package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// CriarPacienteArgs is criar_paciente's argument shape. /patient/create's
// verified contract requires nome_completo AND at least one of
// cpf/celular/data_nascimento/email — enforced in criarPaciente before any
// Feegow request is built.
type CriarPacienteArgs struct {
	NomeCompleto   string `json:"nome_completo" jsonschema:"Nome completo do paciente. Obrigatório."`
	CPF            string `json:"cpf,omitempty" jsonschema:"CPF do paciente (apenas dígitos)."`
	Celular        string `json:"celular,omitempty" jsonschema:"Celular do paciente (apenas dígitos)."`
	DataNascimento string `json:"data_nascimento,omitempty" jsonschema:"Data de nascimento, ISO-8601 (YYYY-MM-DD)."`
	Email          string `json:"email,omitempty" jsonschema:"E-mail do paciente."`

	ConfirmacaoPaciente bool `json:"confirmacao_paciente" jsonschema:"Confirmação EXPLÍCITA do PACIENTE (nunca do agente/modelo) de que deseja criar este cadastro. Ao menos um entre cpf, celular, data_nascimento ou email precisa ser informado, além do nome_completo."`
}

// CriarPaciente resolves to identificar_paciente's exact result shape
// (paciente_id, ja_existia) — never PII — because a caller of this tool
// never needs anything else: it either created a new cadastro (ja_existia:
// false) or found the caller was describing one that already exists
// (ja_existia: true), and either way the only thing anything downstream of
// this tool (agendar, cancelar, ...) needs is the id.
//
// Before ever calling /patient/create, this tries to IDENTIFY an existing
// cadastro from whatever complete fact-pair the caller's args happen to
// form (cpf+data_nascimento or celular+nome_completo — the exact same rule
// identificar_paciente enforces) and returns that instead of creating a
// duplicate. Only when no such pair is present, or it resolves to
// ErrNaoLocalizado, does this proceed to create.
func CriarPaciente(ctx context.Context, client *feegow.Client, args CriarPacienteArgs) (*IdentificarPacienteResult, error) {
	result, err := criarPaciente(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func criarPaciente(ctx context.Context, client *feegow.Client, args CriarPacienteArgs) (*IdentificarPacienteResult, error) {
	if err := requireConfirmacaoPaciente(args.ConfirmacaoPaciente, "criar_paciente"); err != nil {
		return nil, err
	}

	nome := strings.TrimSpace(args.NomeCompleto)
	if nome == "" {
		return nil, &ArgumentError{Msg: "nome_completo é obrigatório"}
	}
	cpf := strings.TrimSpace(args.CPF)
	celular := strings.TrimSpace(args.Celular)
	dob := strings.TrimSpace(args.DataNascimento)
	email := strings.TrimSpace(args.Email)
	if cpf == "" && celular == "" && dob == "" && email == "" {
		return nil, &ArgumentError{Msg: "informe ao menos um entre cpf, celular, data_nascimento ou email, além do nome_completo"}
	}
	if dob != "" {
		if _, err := time.Parse(feegow.ISO8601, dob); err != nil {
			return nil, &ArgumentError{Msg: "data_nascimento deve estar em ISO-8601 (YYYY-MM-DD)"}
		}
	}

	if existing, err := tryIdentifyForCreate(ctx, client, cpf, dob, celular, nome); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	wire := map[string]any{
		"nome_completo": nome,
		// /patient/create's verified contract requires BOTH nome_completo
		// and nome_paciente — the same value, sent under two wire keys, per
		// the contract this Fase was handed (not deduced by analogy).
		"nome_paciente": nome,
	}
	if cpfDigits := onlyDigits(cpf); cpfDigits != "" {
		wire["cpf"] = cpfDigits
	}
	if celDigits := onlyDigits(celular); celDigits != "" {
		wire["celular"] = celDigits
	}
	if dob != "" {
		// Same reasoning as nome_completo/nome_paciente above: the verified
		// contract lists BOTH data_nascimento and nascimento among
		// /patient/create's optional fields. Sending both under the same
		// value is the robust choice when it is unconfirmed which of the
		// two names the server actually reads.
		wire["data_nascimento"] = dob
		wire["nascimento"] = dob
	}
	if email != "" {
		wire["email"] = email
	}

	resp, err := client.Call(ctx, "patient.create", feegow.Request{Params: wire})
	if err != nil {
		return nil, err
	}

	pacienteID, err := decodeCreatedPatientID(resp.Content)
	if err != nil {
		return nil, err
	}

	auditWrite("criar_paciente", pacienteID, 0)
	return &IdentificarPacienteResult{PacienteID: pacienteID, JaExistia: false}, nil
}

// tryIdentifyForCreate attempts to resolve an existing cadastro from
// whichever complete identity fact-pair is available among cpf/dob/celular/
// nome — exactly identificar_paciente's rule, reused via
// identifyByCPF/identifyByPhone rather than reimplemented. Returns
// (nil, nil) when no complete pair is present, or when the pair that IS
// present resolves to ErrNaoLocalizado (no existing cadastro — proceed to
// create); returns (result, nil) when an existing cadastro was found; and
// propagates any other error (a genuine infra/credential failure) instead
// of silently treating it as "no such cadastro, go ahead and create" — the
// same distinction mapNotFound (paciente.go) already draws.
func tryIdentifyForCreate(ctx context.Context, client *feegow.Client, cpf, dob, celular, nome string) (*IdentificarPacienteResult, error) {
	var (
		result *IdentificarPacienteResult
		err    error
	)
	// KNOWN, DELIBERATE GAP: these are the only two complete fact-pairs this
	// checks. A caller who supplies cpf+email (or any other combination
	// that isn't one of these two exact pairs) without also supplying
	// data_nascimento skips deduplication entirely and falls straight
	// through to /patient/create — even if a cadastro matching that cpf
	// already exists. This is NOT an oversight: identificar_paciente's
	// entire design (see its doc comment) is that a single isolated fact
	// (a CPF alone, an email alone) must never be enough to query for or
	// confirm a cadastro — that would make this endpoint a
	// cadastro-existence oracle, letting a caller test guesses one fact at
	// a time. Extending dedup to more fact combinations would mean
	// extending that same query-by-a-single-fact capability, which is
	// exactly the anti-oracle rule this design refuses to weaken. The
	// accepted cost is duplicate cadastros for callers who don't happen to
	// supply one of the two recognized complete pairs.
	switch {
	case cpf != "" && dob != "":
		result, err = identifyByCPF(ctx, client, cpf, dob)
	case celular != "" && nome != "":
		result, err = identifyByPhone(ctx, client, celular, nome)
	default:
		return nil, nil
	}

	if err == nil {
		// NOT auditWrite: no mutating call happened on this path — the
		// existing cadastro was found and reused, /patient/create was never
		// called. See auditReuse's doc comment (audit.go).
		auditReuse("criar_paciente", result.PacienteID)
		return result, nil
	}
	if errors.Is(err, ErrNaoLocalizado) {
		return nil, nil
	}
	return nil, err
}

// decodeCreatedPatientID extracts the new paciente_id from
// /patient/create's success content. The exact response shape was not
// confirmed end-to-end by Fase 0 (unlike its request contract), so this
// tries every plausible shape in turn — a bare number, a numeric string, or
// an object carrying paciente_id/patient_id — rather than assuming one and
// silently mis-parsing the others. A shape that matches none of them is a
// real operational problem (Feegow's response no longer matches what this
// integration expects), so it is logged at WARN, the same way
// listPatients' shape-unexpected path (paciente.go) already is, and
// returned as an explicit error — never disguised as ErrNaoLocalizado,
// which would misreport a successful creation whose id we simply failed to
// read as if the cadastro never existed.
func decodeCreatedPatientID(content json.RawMessage) (int, error) {
	// Every branch below additionally requires the decoded id to be > 0 —
	// the same régua validateAgendamentoID (guardas.go) already applies to
	// agendamento ids. This matters specifically for `content: null`:
	// json.Unmarshal of a JSON null into any of these shapes (float64,
	// string, or the pointer-field struct) is a documented no-op — err ==
	// nil, value left at its zero value — so without the > 0 check a null
	// content would silently decode to paciente_id 0 and be reported as a
	// full success, the exact opposite of this function's contract. A null
	// (or any non-positive id) instead falls through every shape and hits
	// the same explicit "formato inesperado" error every other
	// unrecognized shape already gets.
	var asNumber float64
	if err := json.Unmarshal(content, &asNumber); err == nil && int(asNumber) > 0 {
		return int(asNumber), nil
	}

	var asString string
	if err := json.Unmarshal(content, &asString); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(asString)); err == nil && n > 0 {
			return n, nil
		}
	}

	var asObject struct {
		PacienteID *int `json:"paciente_id"`
		PatientID  *int `json:"patient_id"`
	}
	if err := json.Unmarshal(content, &asObject); err == nil {
		if asObject.PacienteID != nil && *asObject.PacienteID > 0 {
			return *asObject.PacienteID, nil
		}
		if asObject.PatientID != nil && *asObject.PatientID > 0 {
			return *asObject.PatientID, nil
		}
	}

	logShapeWarning("content de patient.create não tem um formato reconhecido")
	return 0, errors.New("tools: resposta de patient/create em formato inesperado — paciente pode ter sido criado, mas o id não pôde ser lido")
}
