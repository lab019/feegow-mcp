package feegow

import "net/http"

// Registry is the translation table this package exists to hold — see the
// package doc comment. It is not exhaustive: ESPECIFICACAO.md §13
// inventories 85 endpoints across 16 groups, and populating the rest is
// Fases 2-4's job as each is actually turned into a tool. What is here is
// a representative slice chosen to exercise every axis of inconsistency
// ESPECIFICACAO.md §5 documents: all three date-parameter conventions, all
// three pagination schemes, and three of the four hosts.
//
// Every entry's parameters were extracted from the official Feegow REST
// API v1.0 documentation (146 pages) — never invented, never inferred by
// analogy with a sibling endpoint. Where that documentation was unclear or
// internally contradictory, the descriptor's Verified field is false and
// Notes says exactly what is uncertain and why (see
// EndpointDescriptor.Validate, which enforces that every unverified entry
// carries an explanation).
var Registry = map[EndpointID]EndpointDescriptor{
	// --- Agendamentos (api.feegow.com/v1/api) ---------------------------
	//
	// /appoints/search is the endpoint ESPECIFICACAO.md §5 singles out for
	// its pagination trap: the wire parameter Feegow calls "offset" is the
	// *page size* (default 50), and the wire parameter that is the real
	// offset (records to skip) is called "start". PaginationStartOffset
	// exists specifically to encode that inversion.
	"appoints.search": {
		ID:     "appoints.search",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/appoints/search",
		DateParams: []DateParam{
			{Role: DateRoleStart, WireName: "data_start", Format: DateBR},
			{Role: DateRoleEnd, WireName: "data_end", Format: DateBR},
		},
		Pagination: PaginationSpec{
			Kind:        PaginationStartOffset,
			LimitParam:  "offset", // Feegow's "offset" query param is the page size.
			OffsetParam: "start",  // Feegow's "start" query param is the real deslocamento.
		},
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Paginação só liga quando list_procedures=1 é usado (doc.txt); " +
			"fora isso start/offset são aceitos mas não paginam de fato. Ver ESPECIFICACAO.md §5.",
	},

	"appoints.available_schedule": {
		ID:     "appoints.available_schedule",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/appoints/available-schedule",
		DateParams: []DateParam{
			{Role: DateRoleStart, WireName: "data_start", Format: DateBR},
			{Role: DateRoleEnd, WireName: "data_end", Format: DateBR},
		},
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Doc bug conhecido (ESPECIFICACAO.md §8): parâmetro \"tipo\" é documentado como " +
			"numeric mas os valores reais são \"E\"/\"P\" (string) — não afeta a tradução de data " +
			"modelada aqui, mas a tool desta Fase 2+ precisa saber disso.",
	},

	// /appoints/status, /appoints/motives and /appoints/list-channel are
	// parameterless catalog lookups (Fase 2's listar_catalogo tool):
	// status_id/motivo_id/canal_id are the values other write endpoints
	// (statusUpdate, cancel-appoint/reschedule, new-appoint) reference, but
	// none of those three take a request parameter of their own.
	"appoints.status": {
		ID:       "appoints.status",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/appoints/status",
		Envelope: EnvelopeStandard,
		Verified: true,
	},

	"appoints.motives": {
		ID:       "appoints.motives",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/appoints/motives",
		Envelope: EnvelopeStandard,
		Verified: true,
	},

	"appoints.list_channel": {
		ID:       "appoints.list_channel",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/appoints/list-channel",
		Envelope: EnvelopeStandard,
		Verified: true,
	},

	"appoints.new_appoint": {
		ID:     "appoints.new_appoint",
		Host:   HostAPI,
		Method: http.MethodPost,
		Path:   "/appoints/new-appoint",
		DateParams: []DateParam{
			{Role: DateRoleSingle, WireName: "data", Format: DateBR},
		},
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Campo de horário se chama \"hora\" aqui (em /appoints/reschedule é \"horario\" — " +
			"ver ESPECIFICACAO.md §7.5). Guarda de negócio \"sem agendamento retroativo\" e a regra " +
			"condicional valor/plano são responsabilidade da tool (Fase 3), não deste client.",
	},

	"appoints.cancel_appoint": {
		ID:       "appoints.cancel_appoint",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/appoints/cancel-appoint",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Doc bug conhecido (ESPECIFICACAO.md §8): \"agendamento_id\" está documentado como " +
			"\"Identificação do paciente\" — é o ID do agendamento.",
	},

	"appoints.reschedule": {
		ID:     "appoints.reschedule",
		Host:   HostAPI,
		Method: http.MethodPost,
		Path:   "/appoints/reschedule",
		DateParams: []DateParam{
			{Role: DateRoleSingle, WireName: "data", Format: DateBR},
		},
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes:    "Campo de horário se chama \"horario\" aqui, \"hora\" em /appoints/new-appoint.",
	},

	// --- Bloqueios (api.feegow.com/v1/api) ------------------------------
	//
	// The doc.txt parameter TABLE for this endpoint says "DD-MM-YYYY" for
	// date_start/date_end, but the worked example request AND response
	// both use "2023-05-10" (YYYY-MM-DD) — a direct self-contradiction
	// within doc.txt itself. ESPECIFICACAO.md §5 already resolves this to
	// YYYY-MM-DD, which the worked example supports, so that is what is
	// implemented — but Verified is deliberately false rather than
	// silently trusting either source: this needs the Fase 0 smoke test
	// against a real license before anything is built on top of it.
	"lock.list": {
		ID:     "lock.list",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/lock/list",
		DateParams: []DateParam{
			{Role: DateRoleStart, WireName: "date_start", Format: ISO8601},
			{Role: DateRoleEnd, WireName: "date_end", Format: ISO8601},
		},
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "doc.txt contradiz a si mesmo: a tabela de parâmetros diz DD-MM-YYYY, mas o " +
			"exemplo de request/response usa YYYY-MM-DD (\"date_start\": \"2023-05-10\"). " +
			"Implementado como YYYY-MM-DD (bate com o exemplo e com ESPECIFICACAO.md §5), mas " +
			"precisa do smoke test da Fase 0 antes de virar tool.",
	},

	// --- Pacientes (api.feegow.com/v1/api) ------------------------------
	//
	// doc.txt documents this endpoint twice with different query params:
	// once as paciente_id+photo, once as paciente_cpf+paciente_id+photo+
	// programa_saude — and the second section is literally titled "Buscar
	// paciente passando por cpf e celular" ("by CPF AND cellphone"), yet its
	// own parameter table never lists a celular/phone field. This is one of
	// the doc's known self-contradictions (ESPECIFICACAO.md §8). The
	// Host/Path/Method/Envelope translation here is unambiguous (both
	// examples agree), so Verified stays true.
	//
	// Not reachable from the atendimento profile: this is a detail-by-id
	// lookup (the only parameter either example actually documents is
	// paciente_id) and its response carries no id field at all — expected,
	// since the id was the input — which makes it useless for resolving an
	// identity from cpf/telefone. internal/tools uses patient.list for that
	// (see internal/tools/paciente.go). Stays in Registry for a possible
	// future admin tool that already has a paciente_id in hand, but no
	// atendimento tool calls it.
	"patient.search": {
		ID:       "patient.search",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/patient/search",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Doc bug conhecido (ESPECIFICACAO.md §8): a seção \"Buscar paciente\" tem duas " +
			"tabelas de parâmetros diferentes; uma delas se chama \"...passando por cpf e celular\" " +
			"mas não lista nenhum campo de celular/telefone. Não é usado pelo perfil de " +
			"atendimento: é busca por paciente_id (detalhe, não busca por atributo) e sua " +
			"resposta não contém nenhum campo de id — natural, já que o id foi a entrada —, " +
			"então não serve para resolver identidade a partir de cpf/telefone. " +
			"internal/tools.identifyByCPF/identifyByPhone usam patient.list para isso.",
	},

	// /patient/list's limit/offset are already true deslocamento semantics
	// on the wire ("defina a posição inicial (offset) e o limite de
	// resultados (limit)" — doc.txt) — the identity case for
	// PaginationLimitOffset, included specifically to prove the
	// no-translation-needed path is also exercised by the registry-driven
	// tests, not just the endpoints that need real conversion.
	//
	// This is the endpoint identificar_paciente actually resolves identity
	// against (internal/tools/paciente.go) — its response carries
	// patient_id, nome and nascimento (ISO-8601 here, unlike patient.search's
	// DD-MM-YYYY), and it documents real cpf/telefone filters. It was
	// excluded from the atendimento profile as a *tool* (listing every
	// cadastro is enumeration), not as an implementation detail: the tools
	// built on it always send both required identity filters and a low
	// limit, never call it unfiltered, and no "listar pacientes" tool is
	// exposed on top of it.
	"patient.list": {
		ID:     "patient.list",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/patient/list",
		Pagination: PaginationSpec{
			Kind:        PaginationLimitOffset,
			LimitParam:  "limit",
			OffsetParam: "offset",
		},
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "alterado_em (yyyy-mm-dd) e data_aniversario (dd-mm) são filtros auxiliares, não " +
			"modelados como DateParam aqui: nenhum dos dois é um range start/end nem uma data " +
			"única no sentido dos outros endpoints, e ambos já aceitam o valor como string livre. " +
			"Doc bug conhecido (ESPECIFICACAO.md §8): a prosa descreve data_aniversario como " +
			"\"dd-mm\", mas o próprio exemplo do doc.txt (\"data_aniversario=01-30\" para uma " +
			"nascimento \"...-01-30\") é inequivocamente MM-DD; internal/tools/paciente.go " +
			"implementa o exemplo, não a prosa.",
	},

	// patient.create and patient.edit: data_nascimento/validade in the
	// request body are already ISO-8601 ("yyyy-mm-dd" per doc.txt) — no
	// DateParam is declared because there is no translation to perform,
	// and neither field is a Start/End range or a single filter date in
	// the sense DateRole models. Callers pass them through Request.Params
	// unchanged.
	"patient.create": {
		ID:       "patient.create",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/patient/create",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes:    "data_nascimento e validade já chegam em yyyy-mm-dd — sem tradução necessária.",
	},

	"patient.edit": {
		ID:       "patient.edit",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/patient/edit",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes:    "data_nascimento já chega em yyyy-mm-dd — sem tradução necessária.",
	},

	// --- Empresa (api.feegow.com/v1/api) --------------------------------
	"company.list_unity": {
		ID:       "company.list_unity",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/company/list-unity",
		Envelope: EnvelopeStandard,
		Verified: true,
	},
	"company.list_local": {
		ID:       "company.list_local",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/company/list-local",
		Envelope: EnvelopeStandard,
		Verified: true,
	},

	// --- Especialidades / Convênios (api.feegow.com/v1/api) -------------
	"specialties.list": {
		ID:       "specialties.list",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/specialties/list",
		Envelope: EnvelopeStandard,
		Verified: true,
	},
	"insurance.list": {
		ID:       "insurance.list",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/insurance/list",
		Envelope: EnvelopeStandard,
		Verified: true,
	},

	// --- Procedimentos (api.feegow.com/v1/api) --------------------------
	"procedures.list": {
		ID:       "procedures.list",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/procedures/list",
		Envelope: EnvelopeStandard,
		Verified: true,
	},
	"procedures.types": {
		ID:       "procedures.types",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/procedures/types",
		Envelope: EnvelopeStandard,
		Verified: true,
	},

	// /procedures/groups and /procedures/bundles ("Grupos de procedimentos"
	// / "Listar pacotes" in doc.txt) both take only an optional numeric ID
	// filter (grupo_id / procedimento_id+pacote_id respectively) — no dates,
	// no pagination. listar_catalogo (Fase 2) calls both with no filter to
	// list everything, matching every other catalog entry in this table.
	"procedures.groups": {
		ID:       "procedures.groups",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/procedures/groups",
		Envelope: EnvelopeStandard,
		Verified: true,
	},

	"procedures.bundles": {
		ID:       "procedures.bundles",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/procedures/bundles",
		Envelope: EnvelopeStandard,
		Verified: true,
	},

	// --- Profissionais (api.feegow.com/v1/api) --------------------------
	"professional.list": {
		ID:       "professional.list",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/professional/list",
		Envelope: EnvelopeStandard,
		Verified: true,
	},
	"professional.search": {
		ID:       "professional.search",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/professional/search",
		Envelope: EnvelopeStandard,
		Verified: true,
	},

	// --- Cartão de Benefício (cartao-beneficios.feegow.com) -------------
	//
	// Both datagrid endpoints respond with a bare {"data": [...], "count":
	// ..., "page": ..., ...} object — no {"success","content"} wrapper at
	// all (EnvelopeNone). Query params page/perPage.
	"benefit.contract_datagrid": {
		ID:     "benefit.contract_datagrid",
		Host:   HostBenefit,
		Method: http.MethodGet,
		Path:   "/external/contract/datagrid",
		Pagination: PaginationSpec{
			Kind:        PaginationPagePerPage,
			LimitParam:  "perPage",
			OffsetParam: "page",
		},
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "initialDate/endDate: doc.txt só diz \"string\", sem formato — não modelado como " +
			"DateParam aqui (não deduzido por analogia com outros campos ISO da API). Doc bug " +
			"conhecido (ESPECIFICACAO.md §8): \"perPage padrão é 1\" no texto, mas o exemplo usa " +
			"10. Precisa de smoke test da Fase 0 antes de virar tool.",
	},

	"benefit.plan_datagrid": {
		ID:     "benefit.plan_datagrid",
		Host:   HostBenefit,
		Method: http.MethodGet,
		Path:   "/external/plan/datagrid",
		Pagination: PaginationSpec{
			Kind:        PaginationPagePerPage,
			LimitParam:  "perPage",
			OffsetParam: "page",
		},
		Envelope: EnvelopeNone,
		Verified: true,
		Notes:    "Sem parâmetros de data. perPage default documentado como 500 (diferente do default de contract/datagrid, que é 1).",
	},

	// --- Estoque (core.feegow.com.br) -----------------------------------
	"stock.location_list": {
		ID:       "stock.location_list",
		Host:     HostCoreBR,
		Method:   http.MethodPost,
		Path:     "/financial2/external/financial-stock/location/list",
		Envelope: EnvelopeNone,
		Verified: true,
		Notes:    "Resposta é um array JSON puro (sem qualquer envelope). Único parâmetro é \"unity\" (body).",
	},

	// --- Financeiro (api.feegow.com/v1/api e core.feegow.com) -----------
	//
	// The one endpoint ESPECIFICACAO.md calls out as having *real*
	// limit/offset (true deslocamento) semantics.
	"financial.dmed": {
		ID:     "financial.dmed",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/financial/dmed",
		DateParams: []DateParam{
			{Role: DateRoleStart, WireName: "dataInicio", Format: ISO8601},
			{Role: DateRoleEnd, WireName: "dataFim", Format: ISO8601},
		},
		Pagination: PaginationSpec{
			Kind:        PaginationLimitOffset,
			LimitParam:  "limit",
			OffsetParam: "offset",
		},
		Envelope: EnvelopeStandard,
		Verified: true,
	},

	"financial.private_table_list": {
		ID:     "financial.private_table_list",
		Host:   HostCore,
		Method: http.MethodGet,
		Path:   "/financial2/external/private-table/list",
		Pagination: PaginationSpec{
			Kind:        PaginationPagePerPage,
			LimitParam:  "perPage",
			OffsetParam: "page",
		},
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Resposta é {page,pages,perPage,data,count} sem campo \"success\". Mesmo caminho " +
			"\"financial2/external\" que /stock.location_list, sob um TLD diferente " +
			"(core.feegow.com vs core.feegow.com.br) — doc bug conhecido, ver ESPECIFICACAO.md §8.",
	},
}
