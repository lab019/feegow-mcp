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
		// Confirmed by the Fase 0 smoke test: a window of data_start=
		// 01-01-2024 to data_end=31-12-2026 (3 years) returns 409
		// "Intervalo de data deve ser menor que 6 meses." — undocumented
		// in doc.txt. A follow-up targeted smoke test (5 probes bisecting
		// the exact cutoff) confirmed the real rule is 180 calendar DAYS,
		// not 6 calendar months — see MaxRangeDays' doc comment in
		// types.go for the measurements. validateDateRange (dates.go)
		// turns a too-wide window into a DateRangeTooWideError before the
		// request ever leaves this process, instead of a caller-facing
		// 409 sanitized down to an opaque "houve um conflito" (see
		// tools.SanitizeFeegowError).
		MaxRangeDays: 180,
		Envelope:     EnvelopeStandard,
		Verified:     true,
		Notes: "Paginação (start/offset) só foi observada com list_procedures=1 nos testes manuais " +
			"originais; a Fase 0 tentou isolar isso com uma janela válida (01-01-2026 a 01-03-2026) " +
			"mas a sandbox não tinha agendamentos nessa janela — o campo \"total\" apareceu com E sem " +
			"list_procedures=1, então a alegação \"paginação só liga com list_procedures=1\" não pôde " +
			"ser confirmada nem refutada. Ver ESPECIFICACAO.md §5 e Fase 0 RELATORIO.md item a.4.",
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
		Notes: "Campo de horário se chama \"horario\" (confirmado pela Fase 0: enviar \"hora\", como a " +
			"tabela de parâmetros de doc.txt sugere, gera 422 {\"horario\":[...]} — \"hora\" é ignorado " +
			"pela API). O mesmo campo \"horario\" é usado em /appoints/reschedule; não são nomes " +
			"diferentes, era um artefato de erro da doc (ESPECIFICACAO.md §7.5 e Fase 0 RELATORIO.md " +
			"item a.7/c.5). Guarda de negócio \"sem agendamento retroativo\" é responsabilidade da " +
			"tool (Fase 3), não deste client — confirmado 422 {\"data\":[\"...posterior ou igual a " +
			"today\"]} para data retroativa.\n" +
			"Duas regras de negócio descobertas pela Fase 0, NÃO implementadas por este client " +
			"(responsabilidade da tool de escrita, Fase 3):\n" +
			"  1. \"plano=1\" NÃO exige \"valor=0\" no servidor, apesar do aviso da doc (\"ATENÇÃO: Se " +
			"plano_id = 1 valor deverá ser 0\") — criado agendamento com plano=1 e valor=9999 sem " +
			"erro. Sem uma guarda própria da tool, um agendamento de convênio com preço errado entra " +
			"em silêncio.\n" +
			"  2. Existe um SEGUNDO 409 não documentado, distinto do \"horário ocupado\": " +
			"\"Esse paciente já possui um agendamento nessa agenda.\" — dispara quando o MESMO " +
			"paciente já tem qualquer outro agendamento com o mesmo profissional, independente do " +
			"horário. A tool de criação precisa distinguir essa mensagem da de horário ocupado.",
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
		Notes: "Campo de horário se chama \"horario\" — confirmado ponta a ponta pela Fase 0 " +
			"(POST com horario=\"16:00:00\" → 200 \"Agendamento remarcado\"). O mesmo nome é usado " +
			"em /appoints/new-appoint (ver Notes lá): não são dois campos diferentes, era um " +
			"artefato de erro da doc.",
	},

	// appoints.confirm ("Confirmar agendamento") is UNDOCUMENTED — found by
	// the Fase 0 systematic probing of ~40 name/path variations, not in
	// doc.txt's 85 endpoints. Confirmed end-to-end against the real API:
	// agendamento_id inexistente → 409 "Agendamento não encontrado" (mesmo
	// padrão do 409 documentado de /appoints/cancel-appoint); agendamento_id
	// real (criado nesta sessão) → 200 "Agendamento confirmado com
	// sucesso". A variante kebab-case "confirm-appoint" NÃO existe (bate o
	// fingerprint de rota ausente — ver RouteNotFoundError). Perfil:
	// atendimento (confirmar consulta é fluxo central do paciente).
	// Registrado aqui para a Fase 3 não precisar redescobrir o contrato —
	// a tool (confirmar_agendamento) NÃO é criada nesta fase, é escrita.
	"appoints.confirm": {
		ID:       "appoints.confirm",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/appoints/confirm",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Não documentado em doc.txt — descoberto pela Fase 0. Contrato: " +
			"{\"agendamento_id\": <int>}. Confirmado ponta a ponta: 200 " +
			"\"Agendamento confirmado com sucesso\"; com id inexistente, 409 " +
			"\"Agendamento não encontrado\". Perfil atendimento. Tool ainda não existe — Fase 3.",
	},

	// The two v2 write/discovery endpoints below were found by the Fase 0
	// probe under /v1/api/v2/... (the same HostAPI prefix, just with a
	// "/v2" segment before the group name — no client change needed to
	// reach them, see Host's doc comment). Both came back with a REAL
	// validation 422 (named fields, not RouteNotFoundError's empty-message
	// fingerprint) proving the route exists and confirming its field
	// names — but neither was exercised end-to-end (no full v2 create, to
	// avoid multiplying test agendamentos), so Verified stays false and
	// no DateParams are declared: the exact wire date FORMAT for v2 was
	// never independently observed, only inferred by field-name match with
	// v1 — exactly the kind of inference this package's doc comment says
	// never to make silently.
	"appoints.new_appoint_v2": {
		ID:       "appoints.new_appoint_v2",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/v2/appoints/new-appoint",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "Não documentado em doc.txt. POST sem corpo → 422 de validação REAL (não o " +
			"fingerprint de rota ausente) nomeando os campos: local_id, paciente_id, data, horario, " +
			"valor, plano, procedimento_id, profissional_id — mesmos nomes do v1, inclusive " +
			"\"horario\" (reforça que new-appoint usa \"horario\", não \"hora\"). Formato de data e " +
			"envelope de sucesso NÃO confirmados (nenhuma criação completa foi tentada em v2). Sem " +
			"DateParams aqui de propósito: o formato \"data\" seria inferido por analogia com o v1, " +
			"não observado — deixado para a Fase 3/4 confirmar antes de modelar.",
	},

	"appoints.available_schedule_v2": {
		ID:       "appoints.available_schedule_v2",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/v2/appoints/available-schedule",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "Não documentado em doc.txt. GET sem params → 422 de validação REAL nomeando os " +
			"campos: tipo, data_start, data_end — mesmos nomes do v1. Formato de data e envelope de " +
			"sucesso NÃO confirmados. Sem DateParams aqui pelo mesmo motivo que " +
			"appoints.new_appoint_v2: formato não observado diretamente em v2.",
	},

	// appoints.status_update ("Atualizar status") is documented (doc.txt),
	// but only confirmed by the Fase 0 smoke test negatively: POST with
	// lowercase field names (agendamento_id/status_id, as a naive caller
	// would guess) returned a REAL 422 naming the actual fields in
	// PascalCase — AgendamentoID, StatusID — matching doc.txt's own
	// example exactly. No full success round trip was attempted (that
	// would mutate a real agendamento's status as a side effect of a
	// scan, which Fase 0 deliberately avoided beyond the create/cancel
	// pair already used for appoints.new_appoint). Verified stays false
	// for that reason alone; the wire contract itself is trustworthy.
	"appoints.status_update": {
		ID:       "appoints.status_update",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/appoints/statusUpdate",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "Confirmado pela Fase 0 só por rejeição: enviar agendamento_id/status_id " +
			"(lowercase, como um caller ingênuo tentaria) devolveu 422 nomeando os campos " +
			"reais em PascalCase — AgendamentoID, StatusID — batendo com o exemplo de " +
			"doc.txt. Nenhuma chamada de sucesso ponta a ponta foi feita (seria uma escrita " +
			"real sobre o status de um agendamento, fora do escopo da varredura). Corpo " +
			"(POST): AgendamentoID (numeric, obrigatório), StatusID (numeric, obrigatório — " +
			"doc.txt manda como string no exemplo, mas o campo é numeric; aceitar int aqui), " +
			"Obs (string, opcional), HoraChegada (string HH:MM, opcional, só para o status " +
			"\"aguardando\"). Envelope assumido {success,content} (padrão do grupo " +
			"Agendamentos, igual /appoints/cancel-appoint), não observado diretamente.",
	},

	// appoints.queue_position ("Gerar senha de atendimento") — confirmado
	// PONTA A PONTA pela Fase 0: GET ?unidade_id=0&tipo_senha=1 → 200,
	// content real {"posicao":1,"tipoSenha":1,"tipoFormatado":"P"}. A
	// chave de nível superior é "sucess" (com esse erro de digitação),
	// NÃO "success" — confirmado tanto no corpo real quanto no próprio
	// exemplo de doc.txt (mesmo typo nos dois), então não é um bug desta
	// integração. EnvelopeStandard aqui faria parseSuccess nunca achar
	// "success" e tratar toda resposta de sucesso como um 409 — mesma
	// razão de patient.check_eligibility logo abaixo.
	"appoints.queue_position": {
		ID:       "appoints.queue_position",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/appoints/queue-position",
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Resposta usa a chave \"sucess\" (com esse erro de digitação, não \"success\") " +
			"— confirmado ponta a ponta pela Fase 0 (200, content real: posicao, tipoSenha, " +
			"tipoFormatado) e o próprio doc.txt já mostra o mesmo typo no exemplo. Por isso " +
			"EnvelopeNone: usar EnvelopeStandard faria parseSuccess nunca achar \"success\" e " +
			"tratar toda resposta de sucesso como um 409. QUERY PARAMS: unidade_id (numeric, " +
			"obrigatório — 0 é um valor real, \"unidade principal\", confirmado pelo próprio " +
			"exemplo de doc.txt), tipo_senha (numeric 0-4, obrigatório: 0=G, 1=P, 2=C, 3=E, " +
			"4=R — doc.txt não expande o que cada letra significa).",
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
			"Implementado como YYYY-MM-DD (bate com o exemplo e com ESPECIFICACAO.md §5). " +
			"Fase 0 tentou o smoke test contra a API real e ficou INCONCLUSIVO por falta de dados: " +
			"a sandbox não tem nenhum bloqueio cadastrado (não há endpoint de criação de bloqueio " +
			"entre os 85 documentados), então tanto date_start=10-05-2023&date_end=29-05-2023 " +
			"(DD-MM-YYYY) quanto date_start=2023-05-10&date_end=2023-05-29 (YYYY-MM-DD) devolveram " +
			"200 com content:[] — nenhum dos dois formatos gerou erro, mas nenhum retornou registro " +
			"para provar qual filtro realmente funciona. O que a Fase 0 CONFIRMOU: date_start e " +
			"date_end são obrigatórios JUNTOS (422 {\"date_start\":[...],\"date_end\":[...]} quando " +
			"ambos ausentes), apesar da doc marcá-los como opcionais \"com bloqueio_id\".",
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

	// patient.edit_v2 is UNDOCUMENTED — found under the /v2 prefix (see
	// appoints.new_appoint_v2's comment on why no Host change is needed).
	// Only POST works: PUT on the same path was tried and rejected with
	// an explicit "The PUT method is not supported for this route.
	// Supported methods: POST." (the real-error 422 shape, not
	// RouteNotFoundError's empty-message fingerprint — proves the route
	// exists under POST).
	"patient.edit_v2": {
		ID:       "patient.edit_v2",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/v2/patient/edit",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "Não documentado em doc.txt. POST sem corpo → 422 real nomeando só \"paciente_id\" " +
			"como obrigatório. PUT no mesmo path → 422 real \"The PUT method is not supported... " +
			"Supported methods: POST.\", confirmando que só POST é aceito. Contrato além de " +
			"paciente_id (quais campos são editáveis, formato de datas) NÃO confirmado — nenhuma " +
			"edição completa foi tentada em v2.",
	},

	// patient.list_dependents ("Listar dependentes") — confirmado ponta a
	// ponta pela Fase 0: GET ?paciente_id=1 → 200, envelope padrão
	// {success,content,total}, content:[] para um paciente sem
	// dependentes. Único parâmetro é paciente_id (obrigatório).
	"patient.list_dependents": {
		ID:       "patient.list_dependents",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/patient/list-dependents",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes:    "Único parâmetro é paciente_id (obrigatório, numeric). Perfil admin (dado de paciente).",
	},

	// patient.list_sources ("Listar origens") — confirmado ponta a ponta
	// pela Fase 0: GET sem parâmetros → 200, envelope padrão
	// {success,content,total}, 11 origens reais devolvidas. Catálogo da
	// clínica (não é dado de paciente individual), mas vive sob /patient
	// na API — interpreta o origem_id que aparece em patient.list/
	// patient.search.
	"patient.list_sources": {
		ID:       "patient.list_sources",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/patient/list-sources",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes:    "Sem parâmetros. Catálogo (origens de cadastro), não dado individual de paciente.",
	},

	// patient.list_privates ("Listar tabelas particulares") — confirmado
	// ponta a ponta pela Fase 0: GET sem parâmetros → 200, envelope
	// padrão {success,content,total}. Catálogo da clínica, mesma situação
	// de patient.list_sources — interpreta o tabela_id que aparece em
	// patient.edit/patient.create.
	"patient.list_privates": {
		ID:       "patient.list_privates",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/patient/list-privates",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes:    "Sem parâmetros. Catálogo (tabelas particulares), não dado individual de paciente.",
	},

	// patient.health_programs ("Listar programas de saúde") — confirmado
	// ponta a ponta pela Fase 0: GET sem parâmetros → 200, envelope
	// padrão {success,content,total}. IMPORTANTE: apesar de viver sob o
	// grupo "Pacientes" da doc, este endpoint NÃO tem filtro por
	// paciente_id — é o catálogo dos programas de saúde que a clínica
	// mantém (programa_id, nome_programa, tipo_programa_id, ...), não os
	// programas em que um paciente específico está inscrito (essa
	// informação vem embutida no próprio content de patient.search, campo
	// "programa_de_saude"). internal/tools precisa saber disso para não
	// tratar paciente_id como um filtro real aqui.
	"patient.health_programs": {
		ID:     "patient.health_programs",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/patient/health-programs",
		Pagination: PaginationSpec{
			Kind:        PaginationLimitOffset,
			LimitParam:  "limit",
			OffsetParam: "offset",
		},
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "SEM filtro por paciente_id — é o catálogo de programas de saúde da clínica " +
			"(programa_id, nome_programa, convenio_id, status, tipo_programa_id), não os " +
			"programas de um paciente específico (isso já vem embutido em patient.search's " +
			"content.programa_de_saude). data_start/data_end (yyyy-mm-dd) já chegam prontos " +
			"— sem tradução necessária, mesmo padrão de patient.create/patient.edit.",
	},

	// patient.exam_requests ("Listar pedidos de exâmes") — confirmado
	// ponta a ponta pela Fase 0: GET ?paciente_id=1 → 200, envelope padrão
	// {success,content,total}, content:[] para paciente sem pedidos.
	// paciente_id, data_inicio, data_fim e tipo_pedido são todos
	// obrigatórios per doc.txt (nenhum tem a tag "(opcional)").
	"patient.exam_requests": {
		ID:       "patient.exam_requests",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/patient/exam-requests",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "paciente_id, data_inicio, data_fim e tipo_pedido (1=Padrão, 2=SADT) são " +
			"obrigatórios per doc.txt. paciente_cpf é opcional, junto com paciente_id. " +
			"data_inicio/data_fim já chegam em YYYY-MM-DD — sem tradução necessária, mesmo " +
			"padrão de patient.create/patient.edit.",
	},

	// patient.upload_base64 ("Upload de arquivo para o prontuário") NÃO
	// foi exercitado pela Fase 0 de propósito: uma chamada real geraria um
	// arquivo de teste dentro do prontuário de um paciente da sandbox,
	// um efeito colateral mais invasivo que os agendamentos de teste
	// criados/cancelados, e fora do escopo de uma varredura de leitura.
	// Contrato só a partir de doc.txt. A resposta de sucesso documentada é
	// {"success":true,"fileId":N,"content":"..."} — fileId vive FORA do
	// campo "content", então EnvelopeStandard perderia esse dado (só
	// devolve o content); por isso EnvelopeNone, com a tool decodificando
	// o corpo cru (mesma razão de appoints.queue_position acima).
	"patient.upload_base64": {
		ID:       "patient.upload_base64",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/patient/upload-base64",
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "NÃO exercitado pela Fase 0 (evitado de propósito: geraria um arquivo real no " +
			"prontuário de um paciente de teste). Contrato só a partir de doc.txt: paciente_id " +
			"OU (cpf+nascimento) — um dos dois obrigatório —, base64_file (string, " +
			"obrigatório, formato \"data:<content-type>;base64,<hash>\"), arquivo_descricao " +
			"(opcional), arquivo_id (opcional, substitui um arquivo existente). Resposta de " +
			"sucesso documentada: {\"success\":true,\"fileId\":N,\"content\":\"Arquivo " +
			"enviado com sucesso.\"} — fileId fora do campo \"content\", por isso EnvelopeNone " +
			"em vez de EnvelopeStandard (que descartaria fileId).",
	},

	// medical_record.timeline ("Prontuário: Linha do tempo") is
	// UNDOCUMENTED — found by the Fase 0 probe. Confirmed end-to-end:
	// GET ?paciente_id=3 → 200, envelope padrão {success, content} com
	// content:[] (paciente de teste sem histórico). Perfil: admin
	// (prontuário) — fora do escopo do perfil atendimento.
	"medical_record.timeline": {
		ID:       "medical_record.timeline",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/medical-record/timeline",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes:    "Não documentado em doc.txt — descoberto pela Fase 0. Perfil admin (prontuário).",
	},

	// patient.check_eligibility ("Verificar elegibilidade do paciente") is
	// UNDOCUMENTED — found by the Fase 0 probe. Confirmed end-to-end: GET
	// ?paciente_id=3 → 200, corpo {"success": true, "elegivel": false,
	// "term": null} — SEM o envelope {success,content} padrão (é
	// EnvelopeNone: os campos success/elegivel/term vêm soltos no corpo).
	// Perfil: admin.
	"patient.check_eligibility": {
		ID:       "patient.check_eligibility",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/patient/check-eligibility",
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Não documentado em doc.txt — descoberto pela Fase 0. Resposta é " +
			"{\"success\": bool, \"elegivel\": bool, \"term\": ...} — NÃO usa o envelope padrão " +
			"{success,content} (por isso EnvelopeNone aqui, apesar de ter um campo \"success\"). " +
			"Perfil admin.",
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

	// company.list_unity_v2 is UNDOCUMENTED — found under the /v2 prefix.
	// Fully confirmed: GET → 200, mesmo shape (matriz/unidades) do v1.
	"company.list_unity_v2": {
		ID:       "company.list_unity_v2",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/v2/company/list-unity",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes:    "Não documentado em doc.txt. Confirma que o prefixo v2 existe e reaproveita o endpoint v1 (mesmo shape de resposta).",
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
		Verified: true,
		Notes: "Paginação (page/perPage) CONFIRMADA pela Fase 0: enviados page=1&perPage=10, " +
			"ecoados de volta corretamente em page/perPage/pages na resposta, e listados em " +
			"foundParameters. initialDate/endDate seguem INCONCLUSIVOS: testado sem data, com " +
			"YYYY-MM-DD e com DD-MM-YYYY — as três chamadas voltaram 200 com a mesma resposta vazia " +
			"(count:0, sandbox sem contratos cadastrados); nenhum formato gerou erro, mas nenhum " +
			"prova qual (se algum) é o certo — por isso continuam não modelados como DateParam aqui " +
			"(não deduzido por analogia com outros campos ISO da API). Doc bug conhecido " +
			"(ESPECIFICACAO.md §8): \"perPage padrão é 1\" no texto, mas o exemplo usa 10.",
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
		Verified: false,
		Notes: "INACESSÍVEL neste ambiente, confirmado pela Fase 0: core.feegow.com.br (HostCoreBR) " +
			"não resolve — erro de conexão (proxy 502 / túnel falhou), o domínio não está servindo " +
			"tráfego. Só core.feegow.com (sem \".br\") respondeu na varredura, mas o único path " +
			"testado sob esse host (financial2/external/private-table/list, ver " +
			"financial.private_table_list) devolveu 404 — não testamos o path de estoque " +
			"especificamente sob core.feegow.com. Resposta é um array JSON puro (sem qualquer " +
			"envelope) quando o endpoint responde; único parâmetro é \"unity\" (body). Não trocar o " +
			"Host para HostCore sem antes confirmar que este path específico existe lá — inferir " +
			"por analogia seria exatamente o erro que esta Nota existe para evitar.",
	},

	// --- Laudos (api.feegow.com/v1/api) ---------------------------------
	//
	// doc.txt's own worked example for this endpoint uses "GET" (title
	// says "POST /medical-reports/create", the request example shows
	// "GET") — one of the doc's known self-contradictions. Confirmed by
	// the Fase 0 smoke test: GET with the documented params → 422
	// "The GET method is not supported for this route. Supported
	// methods: POST." (real-error 422 shape, not RouteNotFoundError).
	// POST with the same params → 200 (with a bogus agendamento_id=1 the
	// response body was {"success":false,"message":"..."} — confirmed by
	// resultados.json's medical-reports/create#POST-with-body record:
	// top_level_keys ["success","message"], has_success_content_envelope
	// false. That is NOT the {success,content} envelope this package calls
	// EnvelopeStandard, so EnvelopeStandard here would make parseSuccess
	// look for a "content" field that was never sent, fall through to its
	// empty fallback and silently discard Feegow's actual message inside
	// a ConflictError{Content: ""}. EnvelopeNone (like
	// patient.check_eligibility above) is the honest choice: it never
	// assumes a wrapper shape, so it stays correct even though a genuine
	// success response was never observed — the caller (Fase 2+ tool)
	// inspects "success"/"message" in the raw body itself.
	"medical_reports.create": {
		ID:       "medical_reports.create",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/medical-reports/create",
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Doc bug conhecido: o título de doc.txt diz \"POST /medical-reports/create\", mas o " +
			"exemplo de URL usa GET. Confirmado pela Fase 0: GET → 422 \"The GET method is not " +
			"supported for this route. Supported methods: POST.\"; POST é aceito (200 com " +
			"agendamento_id inexistente devolveu {\"success\":false,\"message\":\"...\"} — um erro " +
			"interno do Feegow para agendamento inexistente). Method, Path e Envelope=EnvelopeNone " +
			"estão confirmados pelo corpo real (NÃO usa o envelope {success,content} padrão, apesar " +
			"de ter um campo \"success\" — mesma situação de patient.check_eligibility acima); uma " +
			"resposta de sucesso real não foi observada, então o shape exato do conteúdo em caso de " +
			"sucesso continua desconhecido — EnvelopeNone não depende disso, só repassa o corpo cru.",
	},

	// financial.find_invoice_by_nfse ("Obter invoices por nota fiscal") —
	// doc.txt documents it as GET with a query param whose name it never
	// actually spells correctly. Confirmed by the Fase 0 smoke test: GET →
	// 422 "The GET method is not supported... Supported methods: POST.";
	// POST with {"numero_nfse": "1"} (the name doc.txt's example URL
	// implies) → 422 real validation naming "nfse_numero" — the ACTUAL
	// field name is nfse_numero, not numero_nfse.
	"financial.find_invoice_by_nfse": {
		ID:       "financial.find_invoice_by_nfse",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/financial/find-invoice-by-nfse-number",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "Doc bug conhecido: doc.txt documenta como GET; confirmado pela Fase 0 que é POST " +
			"(\"The GET method is not supported for this route. Supported methods: POST.\"). Campo " +
			"do corpo é \"nfse_numero\" (confirmado por 422 real {\"nfse_numero\":[...]}), NÃO " +
			"\"numero_nfse\" como o nome do parâmetro na URL de exemplo da doc sugere. Envelope de " +
			"sucesso assumido {success,content} (padrão do grupo Financeiro), não observado " +
			"diretamente — só o 422 foi exercitado.",
	},

	// --- Financeiro (api.feegow.com/v1/api e core.feegow.com) -----------
	//
	// The one endpoint ESPECIFICACAO.md calls out as having *real*
	// limit/offset (true deslocamento) semantics — but see Notes: the
	// Fase 0 smoke test could not exercise that claim, because the route
	// itself 404s in this environment. PaginationLimitOffset stays
	// declared regardless: the *scheme* is a real thing this package
	// models (patient.list is the other, verified, example of it), this
	// entry just can no longer vouch for this specific endpoint using it.
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
		Verified: false,
		Notes: "ROTA AUSENTE neste ambiente, confirmado pela Fase 0: GET com todos os parâmetros " +
			"documentados corretos (cpf, dataInicio, dataFim, unidadeId, limit, offset) devolveu " +
			"404 (HTML, rota não encontrada) tanto com offset=0 quanto offset=1 — não é 403 (sem " +
			"indício de bloqueio por escopo), parece rota ausente/desativada nesta licença. Não deu " +
			"pra confirmar nem refutar a alegação de \"offset com deslocamento real\" por isso. " +
			"PaginationLimitOffset NÃO foi removido do registry: o esquema continua existindo como " +
			"conceito (patient.list é o outro exemplo dele, esse sim verificado) — só não há mais um " +
			"endpoint verificado comprovando este uso específico.",
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
		Verified: false,
		Notes: "404 REAL confirmado pela Fase 0, não erro de host: core.feegow.com.br (HostCoreBR) " +
			"nem conecta (erro de conexão), mas core.feegow.com (HostCore, já o host usado aqui) " +
			"respondeu — com 404 (página HTML genérica, não JSON) neste path exato, mesmo com os " +
			"parâmetros corretos (unityId, page, perPage). Não é 403 (sugere rota ausente, não " +
			"falta de escopo). Veredito: o host declarado (core.feegow.com) está certo, mas o path " +
			"não está acessível nesta sandbox/licença. Resposta seria {page,pages,perPage,data," +
			"count} sem campo \"success\" — não confirmado, apenas o que a doc descreve. Mesmo " +
			"caminho \"financial2/external\" que /stock.location_list, sob um TLD diferente " +
			"(core.feegow.com vs core.feegow.com.br) — doc bug conhecido, ver ESPECIFICACAO.md §8.",
	},
}
