package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds the admin profile's patient READ tools —
// buscar_pacientes, obter_paciente and consultar_paciente_clinico. Unlike
// internal/tools/paciente.go's identificar_paciente (atendimento), none of
// these apply the public-channel restrictions ESPECIFICACAO.md §3 reserves
// for the unauthenticated atendimento surface: no two-fact identification
// requirement, no minimized "id only" result, no uniform not-found error.
// The admin caller is the clínica itself, already authenticated by its own
// Feegow token before the call ever reaches this service — the operador
// genuinely needs to see the paciente's data to do their job, and gating
// that here would not protect anything the token gate upstream doesn't
// already cover.
//
// LGPD still applies to LOGGING, though, exactly as strictly as it does for
// atendimento: nothing here ever writes a CPF, nome, telefone or data de
// nascimento to a log line (see logShapeWarning/auditAdminWrite's own
// discipline), and every list result is capped — a tool that can hand back
// the clinic's entire patient base in one payload is a problem in itself,
// independent of who is allowed to ask for it.

// --- buscar_pacientes -------------------------------------------------

// buscarPacientesDefaultLimit and buscarPacientesMaxLimit bound every
// buscar_pacientes call: a page size the caller doesn't set at all gets a
// sane default, and no caller-supplied value — however large — can push a
// single call past the ceiling. This is the "teto de resultados" this
// tool's task description calls for: unlike identificar_paciente's fixed,
// argument-less patientListLimit (a directed lookup that only ever expects
// a small handful of candidates), buscar_pacientes is a genuine browsing
// tool with caller-controlled pagination — the cap exists precisely
// because that pagination is caller-controlled.
const (
	buscarPacientesDefaultLimit = 20
	buscarPacientesMaxLimit     = 100
)

// BuscarPacientesArgs is buscar_pacientes' argument shape: every filter
// /patient/list documents, all optional — this tool is a free-form search,
// not a directed lookup, so (unlike IdentidadeArgs) no combination of
// fields is required. Limit/Offset are the canonical, already-honest
// feegow.Pagination semantics patient.list uses on the wire (true
// deslocamento, no translation trap) — normalized here to a caller-safe
// range before ever reaching the client.
type BuscarPacientesArgs struct {
	CPF             string `json:"cpf,omitempty" jsonschema:"CPF do paciente (apenas dígitos)."`
	Telefone        string `json:"telefone,omitempty" jsonschema:"Telefone do paciente (apenas dígitos)."`
	DataAniversario string `json:"data_aniversario,omitempty" jsonschema:"Filtro por dia e mês de nascimento (SEM ano), formato MM-DD — ex.: \"01-30\" para 30 de janeiro. Nunca é decisivo sozinho: não carrega o ano."`
	OrigemID        *int   `json:"origem_id,omitempty" jsonschema:"Filtra por origem do cadastro (ver consultar_paciente_clinico com tipo=origens para os valores)."`
	AlteradoEm      string `json:"alterado_em,omitempty" jsonschema:"Filtro por data da última alteração do cadastro, ISO-8601 (YYYY-MM-DD)."`
	ProgramaSaude   *bool  `json:"programa_saude,omitempty" jsonschema:"Se true, inclui os programas de saúde de cada paciente na resposta."`
	Limit           int    `json:"limit,omitempty" jsonschema:"Limite de resultados por página. Default 20, teto 100 — nunca retorna a base inteira de pacientes de uma vez."`
	Offset          int    `json:"offset,omitempty" jsonschema:"Quantos registros pular antes da página (deslocamento real)."`
}

// PacienteResumo is one buscar_pacientes result entry: enough to identify
// and contact a paciente and decide whether it is the right one, without
// carrying every field /patient/list returns (tabela_id and sexo_id are
// internal Feegow classifiers with no display value on their own, so they
// are left unmapped the same way patientListEntry in paciente.go leaves
// most of this same response's fields unmapped — for admin, that omission
// is about payload size, not about hiding PII, unlike the atendimento
// profile's identificar_paciente).
type PacienteResumo struct {
	PacienteID int    `json:"paciente_id"`
	Nome       string `json:"nome"`
	NomeSocial string `json:"nome_social,omitempty"`
	Nascimento string `json:"nascimento,omitempty"` // ISO-8601 (YYYY-MM-DD) — this endpoint's own convention, see BuscarPacientes' doc comment.
	Bairro     string `json:"bairro,omitempty"`
	Email      string `json:"email,omitempty"`
	Celular    string `json:"celular,omitempty"`
	CriadoEm   string `json:"criado_em,omitempty"`
	AlteradoEm string `json:"alterado_em,omitempty"`
}

// BuscarPacientesResult is buscar_pacientes' result.
type BuscarPacientesResult struct {
	Pacientes []PacienteResumo `json:"pacientes"`
}

// patientListAdminEntry is the subset of one /patient/list response entry
// this tool maps — richer than paciente.go's patientListEntry (which is
// deliberately minimal for the atendimento identity-resolution path), but
// still not every field the endpoint returns: tabela_id, sexo_id and
// programa_de_saude are left unmapped, per PacienteResumo's doc comment.
type patientListAdminEntry struct {
	PatientID  int    `json:"patient_id"`
	Nome       string `json:"nome"`
	NomeSocial string `json:"nome_social"`
	Nascimento string `json:"nascimento"` // ISO-8601 (YYYY-MM-DD) on /patient/list.
	Bairro     string `json:"bairro"`
	Email      string `json:"email"`
	Celular    string `json:"celular"`
	CriadoEm   string `json:"criado_em"`
	AlteradoEm string `json:"alterado_em"`
}

// BuscarPacientes searches the clinic's patient base by any combination of
// /patient/list's filters — never requiring two independent facts the way
// identificar_paciente does, since this tool's caller is the clínica
// itself, already authenticated by its own Feegow token (see this file's
// package doc). Its own answer to "don't let this become unbounded
// enumeration" is the Limit/Offset cap, not an identity requirement.
func BuscarPacientes(ctx context.Context, client *feegow.Client, args BuscarPacientesArgs) (*BuscarPacientesResult, error) {
	result, err := buscarPacientes(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func buscarPacientes(ctx context.Context, client *feegow.Client, args BuscarPacientesArgs) (*BuscarPacientesResult, error) {
	if args.Offset < 0 {
		return nil, &ArgumentError{Msg: "offset não pode ser negativo"}
	}
	limit := args.Limit
	switch {
	case limit <= 0:
		limit = buscarPacientesDefaultLimit
	case limit > buscarPacientesMaxLimit:
		limit = buscarPacientesMaxLimit
	}

	wire := map[string]any{}
	if cpf := onlyDigits(args.CPF); cpf != "" {
		wire["cpf"] = cpf
	}
	if tel := onlyDigits(args.Telefone); tel != "" {
		wire["telefone"] = tel
	}
	if args.DataAniversario != "" {
		wire["data_aniversario"] = args.DataAniversario
	}
	if args.OrigemID != nil {
		wire["origem_id"] = *args.OrigemID
	}
	if args.AlteradoEm != "" {
		if _, err := time.Parse(feegow.ISO8601, args.AlteradoEm); err != nil {
			return nil, &ArgumentError{Msg: "alterado_em deve estar em ISO-8601 (YYYY-MM-DD)"}
		}
		wire["alterado_em"] = args.AlteradoEm
	}
	if args.ProgramaSaude != nil {
		wire["programa_saude"] = *args.ProgramaSaude
	}

	resp, err := client.Call(ctx, "patient.list", feegow.Request{
		Params:     wire,
		Pagination: &feegow.Pagination{Limit: limit, Offset: args.Offset},
	})
	if err != nil {
		return nil, err
	}

	var entries []patientListAdminEntry
	if err := json.Unmarshal(resp.Content, &entries); err != nil {
		return nil, fmt.Errorf("tools: decoding patient/list response: %w", err)
	}

	pacientes := make([]PacienteResumo, 0, len(entries))
	for _, e := range entries {
		pacientes = append(pacientes, PacienteResumo{
			PacienteID: e.PatientID,
			Nome:       e.Nome,
			NomeSocial: e.NomeSocial,
			Nascimento: e.Nascimento,
			Bairro:     e.Bairro,
			Email:      e.Email,
			Celular:    e.Celular,
			CriadoEm:   e.CriadoEm,
			AlteradoEm: e.AlteradoEm,
		})
	}
	return &BuscarPacientesResult{Pacientes: pacientes}, nil
}

// --- obter_paciente -----------------------------------------------------

// ObterPacienteArgs is obter_paciente's argument shape: a bare paciente_id
// — this is a detail-by-id lookup (via /patient/search), so there is
// nothing else to identify it by. paciente_id is normally learned from a
// prior buscar_pacientes call.
type ObterPacienteArgs struct {
	PacienteID int `json:"paciente_id" jsonschema:"ID do paciente (obtido via buscar_pacientes ou consultar_paciente_clinico). Obrigatório."`
}

// ObterPacienteResult is obter_paciente's result: /patient/search's content
// passed through as-is (endereço, documentos, convênios, programas de
// saúde — the full cadastro), the same "business blob, no need to
// hand-type every field" choice ListarCatalogo's CatalogoResult already
// makes. IMPORTANT: this endpoint's own "nascimento" field is DD-MM-YYYY,
// UNLIKE buscar_pacientes'/PacienteResumo's ISO-8601 — /patient/search and
// /patient/list simply disagree on this, confirmed by the Fase 0 smoke
// test (see internal/feegow/registry.go's patient.search Notes). Passed
// through unconverted rather than guessed at: this is admin-facing raw
// Feegow data, not a normalized result the way consultar_agenda's is.
type ObterPacienteResult struct {
	Paciente any `json:"paciente"`
}

// ObterPaciente returns the full cadastro for a known paciente_id via
// /patient/search — the "cadastro completo" complement to
// buscar_pacientes' summary list.
func ObterPaciente(ctx context.Context, client *feegow.Client, args ObterPacienteArgs) (*ObterPacienteResult, error) {
	result, err := obterPaciente(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func obterPaciente(ctx context.Context, client *feegow.Client, args ObterPacienteArgs) (*ObterPacienteResult, error) {
	if args.PacienteID <= 0 {
		return nil, &ArgumentError{Msg: "paciente_id é obrigatório"}
	}

	resp, err := client.Call(ctx, "patient.search", feegow.Request{
		Params: map[string]any{"paciente_id": args.PacienteID},
	})
	if err != nil {
		return nil, err
	}

	var paciente any
	if err := json.Unmarshal(resp.Content, &paciente); err != nil {
		return nil, fmt.Errorf("tools: decoding patient/search response: %w", err)
	}
	return &ObterPacienteResult{Paciente: paciente}, nil
}

// --- consultar_paciente_clinico ------------------------------------------

// The tipo values consultar_paciente_clinico accepts. "origens" and
// "tabelas_particulares" round out every /patient/* read endpoint Fase 4a
// scoped in (see ESPECIFICACAO.md's Fase 4a task) that isn't buscar_pacientes
// or obter_paciente: both are small clinic-wide catalogs (origem_id ->
// nome_origem, tabela_id -> nome_tabela) used to interpret fields
// PacienteResumo/ObterPacienteResult already return, grouped here rather
// than given their own single-purpose tools.
const (
	tipoDependentes         = "dependentes"
	tipoPedidosExame        = "pedidos_exame"
	tipoProgramasSaude      = "programas_saude"
	tipoLinhaTempo          = "linha_tempo"
	tipoElegibilidade       = "elegibilidade"
	tipoOrigens             = "origens"
	tipoTabelasParticulares = "tabelas_particulares"
)

// ConsultarPacienteClinicoTipos returns every valid `tipo` value, sorted,
// for use in the MCP tool's description/schema and in tests.
func ConsultarPacienteClinicoTipos() []string {
	return []string{
		tipoDependentes,
		tipoElegibilidade,
		tipoLinhaTempo,
		tipoOrigens,
		tipoPedidosExame,
		tipoProgramasSaude,
		tipoTabelasParticulares,
	}
}

// consultarPacienteClinicoMaxLimit bounds tipo=programas_saude's
// pagination — the only tipo here that supports it — same reasoning as
// buscarPacientesMaxLimit.
const (
	consultarPacienteClinicoDefaultLimit = 20
	consultarPacienteClinicoMaxLimit     = 100
)

// ConsultarPacienteClinicoArgs is consultar_paciente_clinico's argument
// shape: one tipo selects which endpoint answers the call, and the fields
// below are each meaningful only for specific tipos — the tool validates
// that per branch, not by making every field its own required argument
// (which would force every call to supply fields it does not use).
type ConsultarPacienteClinicoArgs struct {
	Tipo       string `json:"tipo" jsonschema:"Qual informação consultar: dependentes, pedidos_exame, programas_saude, linha_tempo, elegibilidade, origens, tabelas_particulares."`
	PacienteID int    `json:"paciente_id,omitempty" jsonschema:"ID do paciente. Obrigatório para tipo=dependentes, pedidos_exame, linha_tempo, elegibilidade. NÃO se aplica a tipo=programas_saude, origens, tabelas_particulares (catálogos da clínica, não específicos de um paciente) — se informado nesses casos, é ignorado."`

	// pedidos_exame (todos obrigatórios com esse tipo, per doc.txt)
	DataInicio string `json:"data_inicio,omitempty" jsonschema:"Início do filtro por data, ISO-8601 (YYYY-MM-DD). Obrigatório com tipo=pedidos_exame; opcional com tipo=programas_saude."`
	DataFim    string `json:"data_fim,omitempty" jsonschema:"Fim do filtro por data, ISO-8601 (YYYY-MM-DD). Obrigatório com tipo=pedidos_exame; opcional com tipo=programas_saude."`
	TipoPedido *int   `json:"tipo_pedido,omitempty" jsonschema:"1 = pedido padrão, 2 = pedido SADT. Obrigatório com tipo=pedidos_exame."`

	// programas_saude (catálogo — paciente_id é ignorado)
	ProgramaID     *int   `json:"programa_id,omitempty" jsonschema:"Filtro por id do programa. Só com tipo=programas_saude."`
	NomePrograma   string `json:"nome_programa,omitempty" jsonschema:"Filtro por nome do programa. Só com tipo=programas_saude."`
	ConvenioID     *int   `json:"convenio_id,omitempty" jsonschema:"Filtro por convênio. Só com tipo=programas_saude."`
	StatusPrograma *int   `json:"status_programa,omitempty" jsonschema:"1 = ativo, 0 = inativo. Só com tipo=programas_saude."`
	TipoProgramaID *int   `json:"tipo_programa_id,omitempty" jsonschema:"Filtro por tipo de programa. Só com tipo=programas_saude."`
	Limit          int    `json:"limit,omitempty" jsonschema:"Limite de resultados (paginação). Só com tipo=programas_saude; default 20, teto 100."`
	Offset         int    `json:"offset,omitempty" jsonschema:"Deslocamento (paginação). Só com tipo=programas_saude."`
}

// ConsultarPacienteClinicoResult is consultar_paciente_clinico's result:
// whatever the resolved endpoint's content was, passed through as-is —
// same "business/admin blob" choice as ObterPacienteResult and
// CatalogoResult, since the shape varies drastically by tipo (a flat list
// of dependentes, a nested pedidos_exame tree, a bare {elegivel,term}
// object for elegibilidade...) and admin has no PII-minimization
// requirement to design around here.
type ConsultarPacienteClinicoResult struct {
	Itens any `json:"itens"`
}

// ConsultarPacienteClinico dispatches args.Tipo to the matching Feegow
// endpoint. An unrecognized tipo is an *ArgumentError, rejected before any
// Feegow call — same principle listar_catalogo's unrecognized-tipo path
// already follows.
func ConsultarPacienteClinico(ctx context.Context, client *feegow.Client, args ConsultarPacienteClinicoArgs) (*ConsultarPacienteClinicoResult, error) {
	result, err := consultarPacienteClinico(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func consultarPacienteClinico(ctx context.Context, client *feegow.Client, args ConsultarPacienteClinicoArgs) (*ConsultarPacienteClinicoResult, error) {
	switch args.Tipo {
	case tipoDependentes:
		return clinicoByPacienteID(ctx, client, "patient.list_dependents", args.PacienteID)
	case tipoLinhaTempo:
		return clinicoByPacienteID(ctx, client, "medical_record.timeline", args.PacienteID)
	case tipoElegibilidade:
		return clinicoByPacienteID(ctx, client, "patient.check_eligibility", args.PacienteID)
	case tipoPedidosExame:
		return consultarPedidosExame(ctx, client, args)
	case tipoProgramasSaude:
		return consultarProgramasSaude(ctx, client, args)
	case tipoOrigens:
		return clinicoNoParams(ctx, client, "patient.list_sources")
	case tipoTabelasParticulares:
		return clinicoNoParams(ctx, client, "patient.list_privates")
	default:
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"tipo %q não é reconhecido; valores aceitos: %v", args.Tipo, ConsultarPacienteClinicoTipos(),
		)}
	}
}

// clinicoByPacienteID handles the three tipos whose only input is
// paciente_id (dependentes, linha_tempo, elegibilidade).
func clinicoByPacienteID(ctx context.Context, client *feegow.Client, id feegow.EndpointID, pacienteID int) (*ConsultarPacienteClinicoResult, error) {
	if pacienteID <= 0 {
		return nil, &ArgumentError{Msg: "paciente_id é obrigatório para este tipo"}
	}
	return callClinico(ctx, client, id, map[string]any{"paciente_id": pacienteID})
}

// clinicoNoParams handles the two catalog tipos (origens,
// tabelas_particulares) that take no parameters at all.
func clinicoNoParams(ctx context.Context, client *feegow.Client, id feegow.EndpointID) (*ConsultarPacienteClinicoResult, error) {
	return callClinico(ctx, client, id, nil)
}

// consultarPedidosExame handles tipo=pedidos_exame: paciente_id,
// data_inicio, data_fim and tipo_pedido are all mandatory per doc.txt (see
// internal/feegow/registry.go's patient.exam_requests Notes).
func consultarPedidosExame(ctx context.Context, client *feegow.Client, args ConsultarPacienteClinicoArgs) (*ConsultarPacienteClinicoResult, error) {
	if args.PacienteID <= 0 {
		return nil, &ArgumentError{Msg: "paciente_id é obrigatório para tipo=pedidos_exame"}
	}
	if args.DataInicio == "" || args.DataFim == "" {
		return nil, &ArgumentError{Msg: "data_inicio e data_fim são obrigatórios para tipo=pedidos_exame"}
	}
	if _, err := time.Parse(feegow.ISO8601, args.DataInicio); err != nil {
		return nil, &ArgumentError{Msg: "data_inicio deve estar em ISO-8601 (YYYY-MM-DD)"}
	}
	if _, err := time.Parse(feegow.ISO8601, args.DataFim); err != nil {
		return nil, &ArgumentError{Msg: "data_fim deve estar em ISO-8601 (YYYY-MM-DD)"}
	}
	if args.TipoPedido == nil || (*args.TipoPedido != 1 && *args.TipoPedido != 2) {
		return nil, &ArgumentError{Msg: "tipo_pedido é obrigatório para tipo=pedidos_exame e deve ser 1 (padrão) ou 2 (SADT)"}
	}

	return callClinico(ctx, client, "patient.exam_requests", map[string]any{
		"paciente_id": args.PacienteID,
		"data_inicio": args.DataInicio,
		"data_fim":    args.DataFim,
		"tipo_pedido": *args.TipoPedido,
	})
}

// consultarProgramasSaude handles tipo=programas_saude: a catalog query
// with NO paciente_id filter (see internal/feegow/registry.go's
// patient.health_programs Notes) — paciente_id, if the caller supplied it,
// is simply never forwarded.
func consultarProgramasSaude(ctx context.Context, client *feegow.Client, args ConsultarPacienteClinicoArgs) (*ConsultarPacienteClinicoResult, error) {
	if args.Offset < 0 {
		return nil, &ArgumentError{Msg: "offset não pode ser negativo"}
	}
	limit := args.Limit
	switch {
	case limit <= 0:
		limit = consultarPacienteClinicoDefaultLimit
	case limit > consultarPacienteClinicoMaxLimit:
		limit = consultarPacienteClinicoMaxLimit
	}

	wire := map[string]any{}
	if args.ProgramaID != nil {
		wire["programa_id"] = *args.ProgramaID
	}
	if args.NomePrograma != "" {
		wire["nome_programa"] = args.NomePrograma
	}
	if args.ConvenioID != nil {
		wire["convenio_id"] = *args.ConvenioID
	}
	if args.StatusPrograma != nil {
		wire["status"] = *args.StatusPrograma
	}
	if args.TipoProgramaID != nil {
		wire["tipo_programa_id"] = *args.TipoProgramaID
	}
	if args.DataInicio != "" {
		if _, err := time.Parse(feegow.ISO8601, args.DataInicio); err != nil {
			return nil, &ArgumentError{Msg: "data_inicio deve estar em ISO-8601 (YYYY-MM-DD)"}
		}
		wire["data_start"] = args.DataInicio
	}
	if args.DataFim != "" {
		if _, err := time.Parse(feegow.ISO8601, args.DataFim); err != nil {
			return nil, &ArgumentError{Msg: "data_fim deve estar em ISO-8601 (YYYY-MM-DD)"}
		}
		wire["data_end"] = args.DataFim
	}

	resp, err := client.Call(ctx, "patient.health_programs", feegow.Request{
		Params:     wire,
		Pagination: &feegow.Pagination{Limit: limit, Offset: args.Offset},
	})
	if err != nil {
		return nil, err
	}
	return decodeClinico(resp)
}

// callClinico is the shared "call this endpoint, decode its content as-is"
// tail every simple tipo branch above uses.
func callClinico(ctx context.Context, client *feegow.Client, id feegow.EndpointID, params map[string]any) (*ConsultarPacienteClinicoResult, error) {
	resp, err := client.Call(ctx, id, feegow.Request{Params: params})
	if err != nil {
		return nil, err
	}
	return decodeClinico(resp)
}

func decodeClinico(resp *feegow.Response) (*ConsultarPacienteClinicoResult, error) {
	var itens any
	if err := json.Unmarshal(resp.Content, &itens); err != nil {
		return nil, fmt.Errorf("tools: decoding response: %w", err)
	}
	return &ConsultarPacienteClinicoResult{Itens: itens}, nil
}
