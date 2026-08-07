package tools

import (
	"context"
	"encoding/json"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// AgendarArgs is agendar's argument shape: the same two identification
// facts identificar_paciente/consultar_agenda require, embedded, plus the
// booking's own data. Like ConsultarAgenda, this NEVER accepts a
// paciente_id directly — see IdentificarPaciente's doc comment on why a raw
// id would be a promise this stateless, unauthenticated service can't keep.
//
// LocalID is a *int, not an int, for the exact reason HorariosLivresArgs'
// UnidadeID is (ESPECIFICACAO.md §7 regra 3): 0 is a legitimate value
// ("unidade principal"), and this field is mandatory — the only way to tell
// "the caller deliberately chose unidade principal" (local_id: 0) apart
// from "the caller forgot to send it" (nil) is to make absence a distinct,
// checkable state instead of overloading the same zero value for both.
//
// EspecialidadeID/ProcedimentoID are both *int and both optional
// individually (Feegow's new-appoint, unlike available-schedule, does not
// document a mandatory choice between the two) but at least one must be
// present — validated in agendar() before any request is built.
type AgendarArgs struct {
	IdentidadeArgs

	LocalID         *int   `json:"local_id" jsonschema:"ID da unidade onde o agendamento acontece. Obrigatório. 0 = unidade principal — omitir não é o mesmo que 0."`
	ProfissionalID  int    `json:"profissional_id" jsonschema:"ID do profissional. Obrigatório."`
	EspecialidadeID *int   `json:"especialidade_id,omitempty" jsonschema:"ID da especialidade. Informe especialidade_id ou procedimento_id (ao menos um)."`
	ProcedimentoID  *int   `json:"procedimento_id,omitempty" jsonschema:"ID do procedimento. Informe especialidade_id ou procedimento_id (ao menos um)."`
	Data            string `json:"data" jsonschema:"Data do agendamento, ISO-8601 (YYYY-MM-DD). Nunca retroativa (deve ser hoje ou futura)."`
	Horario         string `json:"horario" jsonschema:"Horário do agendamento (HH:MM ou HH:MM:SS)."`
	ValorCentavos   int    `json:"valor_centavos" jsonschema:"Valor do agendamento, EM CENTAVOS. Se plano=1 (convênio), valor_centavos precisa ser 0 — a Feegow não valida isso no servidor."`
	Plano           int    `json:"plano" jsonschema:"1 = convênio (exige valor_centavos=0); outros valores conforme /appoints/motives-equivalente de planos da clínica."`
	ConvenioID      *int   `json:"convenio_id,omitempty" jsonschema:"ID do convênio, quando plano=1."`
	ConvenioPlanoID *int   `json:"convenio_plano_id,omitempty" jsonschema:"ID do plano do convênio, quando plano=1."`
	CanalID         *int   `json:"canal_id,omitempty" jsonschema:"ID do canal de agendamento (ver listar_catalogo tipo=canais)."`
	TabelaID        *int   `json:"tabela_id,omitempty" jsonschema:"ID da tabela de preços, quando aplicável."`
	Notas           string `json:"notas,omitempty" jsonschema:"Observação livre sobre o agendamento."`
	Celular         string `json:"celular,omitempty" jsonschema:"Celular de contato para este agendamento (apenas dígitos)."`
	Email           string `json:"email,omitempty" jsonschema:"E-mail de contato para este agendamento."`

	ConfirmacaoPaciente bool `json:"confirmacao_paciente" jsonschema:"Confirmação EXPLÍCITA do PACIENTE (nunca do agente/modelo) de que deseja marcar esta consulta nestes termos."`
}

// AgendarResult is agendar's result. /appoints/new-appoint's success content
// has since been observed end-to-end against the sandbox:
// {"agendamento_id": 9, "eventos": 0}. AgendamentoID is the only field
// forwarded — the caller needs it (a patient wants a reference for their
// booking, and cancelar/remarcar/confirmar all take it as an argument);
// eventos is Feegow-internal noise nobody downstream of this tool has any
// use for, so it is deliberately discarded rather than forwarded "just in
// case".
//
// AgendamentoID is omitted (zero value) when Agendado is true but the id
// could not be read back — see decodeAgendamentoID: the booking still
// happened, this only means the reference number isn't available in this
// response.
type AgendarResult struct {
	Agendado      bool `json:"agendado"`
	AgendamentoID int  `json:"agendamento_id,omitempty"`
}

// decodeAgendamentoID extracts agendamento_id from
// /appoints/new-appoint's success content. Unlike decodeCreatedPatientID
// (paciente_create.go), a shape that doesn't match is NOT treated as a
// failure of the operation: by the time this runs, POST
// /appoints/new-appoint has already returned success, so the agendamento
// already exists in Feegow's ERP regardless of whether this can read its id
// back out. Reporting failure here would tell the patient the booking
// didn't go through when it did — worse than a booking confirmation
// without a reference number. So this returns (0, false) on any mismatch
// (content not an object, no agendamento_id, or a non-numeric one) instead
// of an error, and the caller logs a WARN and still reports success.
func decodeAgendamentoID(content json.RawMessage) (int, bool) {
	var asObject struct {
		AgendamentoID *int `json:"agendamento_id"`
	}
	if err := json.Unmarshal(content, &asObject); err != nil || asObject.AgendamentoID == nil {
		return 0, false
	}
	return *asObject.AgendamentoID, true
}

// Agendar creates an agendamento for a patient identified by the same two
// facts identificar_paciente requires — never a caller-supplied
// paciente_id. See ESPECIFICACAO.md §7 for the four guards enforced before
// any Feegow request is built (retroactive date, convênio valor, local_id
// pointer, explicit confirmation), and classifyAppointConflict (errors.go)
// for how the two Fase-0-verified 409 conflicts specific to booking are
// turned into actionable, distinguishable errors instead of one opaque
// "conflito ao processar a solicitação".
//
// No availability pre-check is made here on purpose: ESPECIFICACAO.md §7
// regra 4 is explicit that "horário ocupado" is a race, and a pre-check
// against /appoints/available-schedule cannot close that race — it would
// only add a second network round trip with the exact same TOCTOU gap. The
// 409 itself, once it happens, is the only reliable signal, and
// classifyAppointConflict turns it into ErrHorarioOcupado, a recoverable
// outcome the caller can act on directly.
func Agendar(ctx context.Context, client *feegow.Client, args AgendarArgs) (*AgendarResult, error) {
	result, err := agendar(ctx, client, args)
	if err != nil {
		return nil, classifyAppointConflict(err)
	}
	return result, nil
}

// agendar is Agendar's implementation, kept separate so every return path
// flows through Agendar's single classifyAppointConflict choke point above
// (mirroring consultarAgenda/buscarHorariosLivres's split for
// SanitizeFeegowError).
func agendar(ctx context.Context, client *feegow.Client, args AgendarArgs) (*AgendarResult, error) {
	if err := requireConfirmacaoPaciente(args.ConfirmacaoPaciente, "agendar"); err != nil {
		return nil, err
	}
	if args.LocalID == nil {
		return nil, &ArgumentError{Msg: "local_id é obrigatório (0 = unidade principal; omitir não é o mesmo que 0)"}
	}
	if args.ProfissionalID <= 0 {
		return nil, &ArgumentError{Msg: "profissional_id é obrigatório"}
	}
	if args.EspecialidadeID == nil && args.ProcedimentoID == nil {
		return nil, &ArgumentError{Msg: "informe especialidade_id ou procedimento_id (ao menos um)"}
	}
	if args.Horario == "" {
		return nil, &ArgumentError{Msg: "horario é obrigatório"}
	}
	if args.ValorCentavos < 0 {
		return nil, &ArgumentError{Msg: "valor_centavos não pode ser negativo"}
	}
	// ESPECIFICACAO.md §7 regra 2 / registry.go's appoints.new_appoint
	// Notes: Feegow's server does NOT enforce "plano=1 exige valor=0"
	// despite its own doc warning — Fase 0 created a convênio agendamento
	// with valor=9999 without error. This is the only guard against that.
	if args.Plano == 1 && args.ValorCentavos != 0 {
		return nil, &ArgumentError{Msg: "quando plano=1 (convênio), valor_centavos deve ser 0 — a Feegow NÃO valida isso no servidor; " +
			"sem esta guarda um agendamento de convênio com preço errado entraria em silêncio no ERP da clínica"}
	}
	if err := validateNotPast("data", args.Data); err != nil {
		return nil, err
	}

	identidade, err := IdentificarPaciente(ctx, client, args.IdentidadeArgs)
	if err != nil {
		return nil, err
	}

	wire := map[string]any{
		"local_id":        *args.LocalID,
		"paciente_id":     identidade.PacienteID,
		"profissional_id": args.ProfissionalID,
		"horario":         args.Horario,
		"valor":           args.ValorCentavos,
		"plano":           args.Plano,
	}
	if args.EspecialidadeID != nil {
		wire["especialidade_id"] = *args.EspecialidadeID
	}
	if args.ProcedimentoID != nil {
		wire["procedimento_id"] = *args.ProcedimentoID
	}
	if args.ConvenioID != nil {
		wire["convenio_id"] = *args.ConvenioID
	}
	if args.ConvenioPlanoID != nil {
		wire["convenio_plano_id"] = *args.ConvenioPlanoID
	}
	if args.CanalID != nil {
		wire["canal_id"] = *args.CanalID
	}
	if args.TabelaID != nil {
		wire["tabela_id"] = *args.TabelaID
	}
	if args.Notas != "" {
		wire["notas"] = args.Notas
	}
	if args.Celular != "" {
		wire["celular"] = args.Celular
	}
	if args.Email != "" {
		wire["email"] = args.Email
	}

	data := args.Data
	resp, err := client.Call(ctx, "appoints.new_appoint", feegow.Request{
		Params: wire,
		Date:   &data,
	})
	if err != nil {
		return nil, err
	}

	auditWrite("agendar", identidade.PacienteID, 0)

	agendamentoID, ok := decodeAgendamentoID(resp.Content)
	if !ok {
		logShapeWarningFor("appoints.new-appoint", "content não tem um agendamento_id reconhecível",
			"agendar não conseguiu ler o agendamento_id de volta — o agendamento foi criado mesmo assim")
		return &AgendarResult{Agendado: true}, nil
	}
	return &AgendarResult{Agendado: true, AgendamentoID: agendamentoID}, nil
}
