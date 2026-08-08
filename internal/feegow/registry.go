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
			"sucesso continua desconhecido — EnvelopeNone não depende disso, só repassa o corpo cru.\n" +
			"Fase 4c: POST com corpo vazio → 422 {\"success\":false,\"message\":{\"agendamento_id\":" +
			"[\"...obrigatório.\"],\"laudo_base64\":[\"...obrigatório.\"]}} — confirma os dois campos " +
			"exigidos e é o primeiro exemplo real do TERCEIRO formato de 422 que classify422 " +
			"(client.go) passou a tratar: o mapa campo->mensagens vem ANINHADO dentro de \"message\" " +
			"(que nos outros formatos é string), não solto no nível raiz. tools.RegistrarLaudo NÃO " +
			"foi exercitada até uma criação real (instrução explícita desta fase: o prontuário de uma " +
			"licença de teste não tem endpoint de limpeza) — só agendamento_id e laudo_base64 são " +
			"expostos como obrigatórios; campos adicionais opcionais, se existirem, não foram " +
			"sondados.",
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
		Verified: true,
		Notes: "Doc bug conhecido: doc.txt documenta como GET; confirmado pela Fase 0 que é POST " +
			"(\"The GET method is not supported for this route. Supported methods: POST.\"). Campo " +
			"do corpo é \"nfse_numero\" (confirmado por 422 real {\"nfse_numero\":[...]}), NÃO " +
			"\"numero_nfse\" como o nome do parâmetro na URL de exemplo da doc sugere. Envelope de " +
			"sucesso CONFIRMADO pela Fase 4b: POST {\"nfse_numero\":\"1\"} → 200 " +
			"{\"success\":true,\"total\":0,\"content\":[]} — o padrão {success,content} do grupo " +
			"Financeiro, mais um campo \"total\" solto que EnvelopeStandard simplesmente ignora.",
	},

	// --- Financeiro, Fase 4b (api.feegow.com/v1/api) --------------------
	//
	// Every entry below was confirmed against the real sandbox by the
	// Fase 4b smoke test (see internal/tools' consultar_financeiro,
	// gerenciar_conta, gerenciar_voucher, remover_registro_financeiro
	// doc comments for how each is used). A second wire family shows up
	// here for the first time: several "core/financial/base/..." and
	// "core/financial/..." endpoints are NOT the {success,content} Laravel
	// envelope every endpoint above uses — they answer with a bare object
	// on success (EnvelopeNone) and, on a validation failure, a NestJS-style
	// {"message":[...],"error":"Bad Request","statusCode":400} shape this
	// package's classifyError does not have a case for (400 falls into the
	// generic, body-free UnexpectedStatusError — safe against PII by
	// construction, since that type never carries the response body, but
	// it does mean these endpoints' validation errors read as an opaque
	// "status HTTP 400 não documentado" to a caller instead of a specific
	// field list). Flagged per-entry below where it applies.

	"financial.list_suppliers": {
		ID:       "financial.list_suppliers",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/financial/list-suppliers",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Confirmado pela Fase 4b: GET sem parâmetros → 200 {\"success\":true,\"content\":[]," +
			"\"total\":0}, sandbox sem fornecedores. limit/offset foram aceitos sem erro quando " +
			"enviados, mas a resposta (sandbox vazia) não permitiu provar se filtram de verdade — " +
			"por isso NENHUM PaginationSpec é declarado aqui (nada a confirmar não é o mesmo que " +
			"confirmado): consultar_financeiro não expõe paginação para este tipo.",
	},

	// financial.search_supplier ("Informações do fornecedor") tem um bug
	// real do lado da Feegow, confirmado pela Fase 4b: com o único
	// parâmetro que a própria API exige (fornecedor_id) devidamente
	// enviado, a resposta AINDA É um 422 — {"success":false,"cod_erro":0,
	// "message":"Undefined index: id"}, um erro de PHP vazando (o
	// handler busca um índice "id" que nunca existe na query string).
	// Isso não é RouteNotFoundError (a mensagem não é vazia) nem um 422
	// de validação normal (o campo que falta nem é o que foi documentado
	// como obrigatório) — é um bug de implementação do lado da Feegow que
	// parece impedir esta operação de sempre funcionar nesta licença.
	"financial.search_supplier": {
		ID:       "financial.search_supplier",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/financial/search-supplier",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "GET sem parâmetros → 422 {\"fornecedor_id\":[\"O campo fornecedor id é " +
			"obrigatório.\"]} (confirma o nome do parâmetro: fornecedor_id). MAS enviar " +
			"fornecedor_id=1 (ou qualquer outro valor testado) TAMBÉM devolve 422, com uma " +
			"mensagem completamente diferente: {\"success\":false,\"cod_erro\":0,\"message\":" +
			"\"Undefined index: id\"} — um bug real do lado da Feegow (o handler parece indexar " +
			"um parâmetro \"id\" que nunca é enviado). Não foi possível obter uma resposta de " +
			"sucesso desta rota na sandbox. A tool (consultar_financeiro tipo=fornecedor) ainda " +
			"chama o endpoint normalmente — o erro sanitizado que o operador vai ver é genérico " +
			"(\"revise os dados informados\"), o que já é o comportamento correto mesmo sem saber " +
			"se algum dia esse bug é corrigido do lado da Feegow.",
	},

	"financial.list_medical_transfer": {
		ID:     "financial.list_medical_transfer",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/financial/list-medical-transfer",
		DateParams: []DateParam{
			{Role: DateRoleStart, WireName: "data_start", Format: DateBR},
			{Role: DateRoleEnd, WireName: "data_end", Format: DateBR},
		},
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "Confirmado pela Fase 4b que data_start/data_end são obrigatórios JUNTOS (422 " +
			"nomeando os dois quando ausentes). Formato INCONCLUSIVO, mesma situação de lock.list: " +
			"tanto DD-MM-YYYY quanto YYYY-MM-DD foram aceitos sem erro (200, content:[] — sandbox " +
			"sem repasses cadastrados), então nenhum dos dois formatos pôde ser confirmado nem " +
			"refutado por um resultado real. Implementado como DD-MM-YYYY por analogia com o nome " +
			"do parâmetro (\"data_\", não \"date_\") bater com a convenção que /financial/list-invoice " +
			"já usa (DD-MM-YYYY, ESPECIFICACAO.md §5) — mas é uma inferência por convenção de nome, " +
			"não uma observação direta, exatamente o tipo de suposição que este pacote normalmente " +
			"evita; documentado aqui em vez de escondido.",
	},

	// financial.list_invoice ("Listar contas") — data_start/data_end
	// (DD-MM-YYYY, ESPECIFICACAO.md §5) JUNTOS, tipo_transacao e
	// unidade_id são todos obrigatórios, confirmado pela Fase 4b em duas
	// rodadas de 422: a primeira nomeou data_start/data_end/tipo_transacao/
	// unidade_id como ausentes; com os quatro enviados (tipo_transacao=1,
	// um palpite numérico razoável) a resposta seguinte foi um 422 NOVO,
	// só sobre tipo_transacao: "Parâmetro 'tipo_transacao' é obrigatório e
	// deve ser 'C', 'D' ou 'T'" — prova que o campo é um enum de string
	// (C/D/T), não numérico. Com tipo_transacao=C a chamada teve sucesso
	// (200, content:[] — sandbox sem contas cadastradas na janela).
	"financial.list_invoice": {
		ID:     "financial.list_invoice",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/financial/list-invoice",
		DateParams: []DateParam{
			{Role: DateRoleStart, WireName: "data_start", Format: DateBR},
			{Role: DateRoleEnd, WireName: "data_end", Format: DateBR},
		},
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "data_start/data_end (DD-MM-YYYY), tipo_transacao ('C', 'D' ou 'T' — STRING, " +
			"apesar de um número parecer razoável à primeira vista) e unidade_id são todos " +
			"obrigatórios — confirmado ponta a ponta pela Fase 4b, incluindo uma chamada de " +
			"sucesso real com os quatro parâmetros corretos.",
	},

	// financial.list_sales ("Listagem de Vendas") usa date_start/date_end
	// (YYYY-MM-DD — nome DIFERENTE de list-invoice, que usa data_start em
	// DD-MM-YYYY; a mesma "Listar contas x Listar vendas" divergência que
	// motivou a nota no prompt desta fase). unidade_id também é
	// obrigatório aqui, o que não estava óbvio antes de medir: a primeira
	// rodada (só sem parâmetros) já mostrou os três; enviar só
	// date_start/date_end confirmou que unidade_id continua exigido
	// separadamente. Sucesso real confirmado com os três.
	"financial.list_sales": {
		ID:     "financial.list_sales",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/financial/list-sales",
		DateParams: []DateParam{
			{Role: DateRoleStart, WireName: "date_start", Format: ISO8601},
			{Role: DateRoleEnd, WireName: "date_end", Format: ISO8601},
		},
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "date_start/date_end (YYYY-MM-DD, nome \"date_\" — diferente de list-invoice, que " +
			"usa \"data_\" em DD-MM-YYYY) E unidade_id são todos obrigatórios — confirmado ponta a " +
			"ponta pela Fase 4b, incluindo uma chamada de sucesso real com os três.",
	},

	// financial.credit_card_flags ("Obter bandeiras de cartão de crédito")
	// — confirmado ponta a ponta pela Fase 4b: GET sem parâmetros → 200,
	// 31 bandeiras reais devolvidas. O campo do nome da bandeira é
	// "Bandeira" (com B maiúsculo) — o único campo com essa capitalização
	// em toda a superfície financeira medida até aqui; os outros campos
	// da mesma entrada ("id") continuam minúsculos.
	"financial.credit_card_flags": {
		ID:       "financial.credit_card_flags",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/financial/credit-card-flags",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Sem parâmetros. Content é uma lista de {\"id\":N,\"Bandeira\":\"...\"} — reparar no " +
			"\"Bandeira\" com B maiúsculo (confirmado pela Fase 4b), inconsistente com o resto do " +
			"payload (\"id\" minúsculo) e com a convenção do restante da API.",
	},

	// financial.current_accounts, financial.cost_center e
	// financial.financial_category (respectivamente "Obter Contas
	// Correntes", "Centros de Custos" e "Categoria financeira / Plano de
	// contas") são POST mas funcionam como uma LEITURA filtrável — corpo
	// vazio já devolve a listagem inteira, sem exigir nenhum campo. As
	// três compartilham o MESMO envelope, nunca visto nas seções acima
	// deste registry: {"data":[...],"count":N,"page":1,"perPage":100,
	// "pages":N,"version":"3.0","foundParameters":[...]} — sem campo
	// "success" (por isso EnvelopeNone), com paginação page/perPage
	// (PaginationPagePerPage) e um campo "foundParameters" que a própria
	// API usa para ecoar quais filtros ela reconheceu — confirmado
	// listando os nomes reais dos filtros de cada endpoint.
	"financial.current_accounts": {
		ID:     "financial.current_accounts",
		Host:   HostAPI,
		Method: http.MethodPost,
		Path:   "/core/financial/base/current-accounts",
		Pagination: PaginationSpec{
			Kind:        PaginationPagePerPage,
			LimitParam:  "perPage",
			OffsetParam: "page",
		},
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Corpo vazio → 200, foundParameters=[\"id\",\"unity\",\"accountType\",\"perPage\"," +
			"\"page\"] — confirma os três filtros opcionais (id, unity, accountType) além da " +
			"paginação. Confirmado ponta a ponta pela Fase 4b.",
	},

	"financial.cost_center": {
		ID:     "financial.cost_center",
		Host:   HostAPI,
		Method: http.MethodPost,
		Path:   "/core/financial/base/cost-center",
		Pagination: PaginationSpec{
			Kind:        PaginationPagePerPage,
			LimitParam:  "perPage",
			OffsetParam: "page",
		},
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Corpo vazio → 200, foundParameters=[\"perPage\",\"page\"] — sem nenhum filtro além " +
			"da paginação (sandbox sem centros de custo cadastrados). Confirmado ponta a ponta pela " +
			"Fase 4b, mesmo envelope de financial.current_accounts.",
	},

	"financial.financial_category": {
		ID:     "financial.financial_category",
		Host:   HostAPI,
		Method: http.MethodPost,
		Path:   "/core/financial/base/financial-category",
		Pagination: PaginationSpec{
			Kind:        PaginationPagePerPage,
			LimitParam:  "perPage",
			OffsetParam: "page",
		},
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Corpo vazio → 200 com 17 categorias reais (\"plano de contas\": type expense/income, " +
			"id, name, position, parentId), foundParameters=[\"id\",\"type\",\"perPage\",\"page\"] — " +
			"confirma os filtros id/type além da paginação. Confirmado ponta a ponta pela Fase 4b, " +
			"mesmo envelope de financial.current_accounts.",
	},

	// financial.update_invoice_nfse ("Atualizar Número da Nota Fiscal
	// Eletrônica") — só o 422 de corpo vazio foi exercitado pela Fase 4b,
	// deliberadamente (uma chamada de sucesso real editaria uma invoice
	// de verdade na sandbox, sem uma invoice de teste conhecida para
	// apontar). Nomeia os dois campos obrigatórios com clareza.
	"financial.update_invoice_nfse": {
		ID:       "financial.update_invoice_nfse",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/financial/update-invoice-nfse-number",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "422 de corpo vazio confirmado pela Fase 4b: {\"invoice_id\":[\"O campo invoice id " +
			"é obrigatório.\"],\"nfse_numero\":[\"O campo nfse numero é obrigatório.\"]} — nomes " +
			"snake_case, mesmo estilo Laravel do resto do grupo Financeiro em api.feegow.com " +
			"(diferente do estilo camelCase/NestJS de financial.pay_movement e " +
			"financial.invoice_create abaixo, apesar de todos viverem sob o mesmo host). Envelope " +
			"de sucesso assumido {success,content} por analogia com o resto do grupo — não " +
			"observado diretamente (só o 422 foi exercitado, de propósito).",
	},

	// financial.invoice_remove e financial.payment_remove ("Remover
	// Fatura" / "Remover Pagamento") são as duas escritas mais
	// destrutivas do escopo desta fase — apagam registro financeiro real
	// do cliente. Por instrução explícita desta fase, NENHUMA delas foi
	// exercitada além do 422/400 de corpo vazio: o suficiente para
	// confirmar Host/Path/Method/nome do campo, sem nunca arriscar
	// apagar algo real. Ambas usam DELETE de verdade (confirmado: um POST
	// no mesmo path devolveu 404 REAL — "Página não encontrada", não o
	// fingerprint de 422 vazio — provando que só DELETE está registrado
	// nessa rota).
	"financial.invoice_remove": {
		ID:       "financial.invoice_remove",
		Host:     HostAPI,
		Method:   http.MethodDelete,
		Path:     "/core/financial/invoice/remove",
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "DELETE de corpo vazio → 400 {\"success\":false,\"message\":\"Field invoiceId not " +
			"found\"} — confirma o nome do campo (invoiceId, camelCase) e o método (POST no mesmo " +
			"path → 404 real, não o fingerprint de rota inexistente). Deliberadamente NÃO " +
			"exercitado com um invoiceId real (apagaria um registro financeiro de verdade) — " +
			"instrução explícita desta fase. Envelope {success,message} — SEM campo \"content\" — " +
			"por isso EnvelopeNone; a resposta de sucesso real nunca foi observada.",
	},

	"financial.payment_remove": {
		ID:       "financial.payment_remove",
		Host:     HostAPI,
		Method:   http.MethodDelete,
		Path:     "/core/financial/payment/remove",
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "Mesma situação de financial.invoice_remove: DELETE de corpo vazio → 400 " +
			"{\"success\":false,\"message\":\"Field paymentId not found\"} (campo paymentId, " +
			"camelCase). Deliberadamente NÃO exercitado com um paymentId real. EnvelopeNone pelo " +
			"mesmo motivo.",
	},

	// financial.pay_movement e financial.pay_booking ("Pagamento de
	// Conta" / "Pagar Agendamento") — só o 422 de corpo vazio foi
	// exercitado, deliberadamente: uma chamada de sucesso real
	// registraria um pagamento de verdade contra uma invoice/agendamento
	// da sandbox. Nomes de campo em camelCase (diferente do resto do
	// grupo Financeiro em Laravel/snake_case), mas ainda validados no
	// estilo Laravel (422, mapa campo->mensagens) — uma mistura que só a
	// medição revelou.
	"financial.pay_movement": {
		ID:       "financial.pay_movement",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/financial/pay-movement",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "422 de corpo vazio confirmado pela Fase 4b, nomeando 8 campos obrigatórios em " +
			"camelCase: invoiceId, movementId, amount, associationId, accountId, paymentMethod, " +
			"paymentDate, paymentName. Tipos exatos (amount em centavos? paymentMethod é um id " +
			"numérico?) NÃO confirmados — a validação 422 só afirma \"obrigatório\", sem checar " +
			"tipo. Envelope de sucesso assumido {success,content} por analogia — não observado.",
	},

	"financial.pay_booking": {
		ID:       "financial.pay_booking",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/financial/pay-booking",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "422 de corpo vazio confirmado pela Fase 4b, nomeando 5 campos obrigatórios em " +
			"camelCase: bookingId, amount, associationId, accountId, paymentMethod, paymentDate " +
			"(sem paymentName, diferente de financial.pay_movement). Tipos exatos não confirmados. " +
			"Envelope de sucesso assumido {success,content} por analogia — não observado.",
	},

	// financial.create_account ("Criar Conta por Agendamento") — ÚNICO
	// endpoint financeiro desta fase com uma resposta REAL end-to-end
	// (agendamento_id=1, que não existe na sandbox): 200 {"success":false,
	// "msg":"O agendamento_id informado não se encontra na nossa base de
	// dados."}. Achado importante: NÃO usa o envelope {success,content}
	// padrão — o campo de mensagem se chama "msg", não "content". Se este
	// endpoint fosse registrado como EnvelopeStandard, parseSuccess
	// procuraria por "content" (ausente), success:false ainda viraria um
	// ConflictError mas com Content="" — a mensagem de erro real da
	// Feegow ("agendamento_id não encontrado") seria descartada em
	// silêncio. EnvelopeNone evita esse bug: a tool decodifica
	// success/msg do corpo cru diretamente.
	"financial.create_account": {
		ID:       "financial.create_account",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/financial/create-account",
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Corpo vazio → 422 {\"agendamento_id\":[\"O campo agendamento_id é obrigatório.\"]} " +
			"(Laravel-style). Com agendamento_id=1 (inexistente na sandbox) → 200 REAL " +
			"{\"success\":false,\"msg\":\"O agendamento_id informado não se encontra na nossa base " +
			"de dados.\"} — confirma ponta a ponta que o envelope de sucesso NÃO é {success," +
			"content}: o campo é \"msg\". EnvelopeStandard aqui perderia essa mensagem (procuraria " +
			"por \"content\", que não existe).",
	},

	// financial.voucher_create ("Criação de Voucher") — a Fase 4b não
	// conseguiu confirmar NENHUM contrato aqui: toda tentativa (GET no
	// path — método errado esperado —, POST com corpo vazio, POST com um
	// corpo plausível de {paciente_id,valor,descricao}) devolveu o MESMO
	// HTML de erro 500 genérico do Feegow ("Ocorreu um erro"), não um
	// JSON de validação. Ou seja: nenhuma medição conseguiu distinguir
	// "campo errado" de "rota quebrada nesta sandbox/licença" — ao
	// contrário de financial.pay_movement/pay_booking acima (que pelo
	// menos nomeiam os campos via 422 real). tools.GerenciarVoucher
	// (ação "criar") NÃO chama este endpoint por causa disso — devolve
	// erro claro de indisponibilidade em vez de uma chamada que
	// certamente vai falhar de forma opaca contra um contrato inventado.
	"financial.voucher_create": {
		ID:       "financial.voucher_create",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/core/financial/voucher/create",
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "500 HTML genérico (\"Ocorreu um erro\") em TODA tentativa da Fase 4b — GET (método " +
			"errado esperado), POST corpo vazio, POST com corpo plausível {paciente_id,valor," +
			"descricao}. Nunca um JSON de validação nomeando campos, ao contrário de todo outro " +
			"POST desta seção. Contrato genuinamente NÃO confirmável nesta sandbox — " +
			"tools.GerenciarVoucher não chama este endpoint (ver seu doc comment).",
	},

	// financial.voucher_cancel ("Cancelamento de Voucher") — 400 real
	// (estilo NestJS, igual financial.invoice_create abaixo) nomeando os
	// três campos. A resposta de SUCESSO nunca foi observada (cancelar um
	// voucher de verdade exigiria primeiro criar um, e voucher_create
	// está quebrado nesta sandbox — ver acima), então o envelope de
	// sucesso é uma incógnita: tools.GerenciarVoucher (ação "cancelar")
	// decodifica um campo "success" por analogia com o resto do grupo
	// (financial.create_account, financial.invoice_remove) e trata sua
	// ausência/false como falha, em vez de assumir sucesso só porque a
	// chamada não voltou erro de transporte — ver o doc comment da tool.
	"financial.voucher_cancel": {
		ID:       "financial.voucher_cancel",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/core/financial/voucher/cancel",
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "POST corpo vazio → 400 {\"message\":[\"id must be an integer number\",\"O campo " +
			"motivo deve ser um dos seguintes valores: ERRO_EMISSAO, FRAUDE_DETECTADA, DUPLICIDADE, " +
			"OUTRO\",\"codigo_motivo must be a string\"],\"error\":\"Bad Request\"," +
			"\"statusCode\":400} — estilo NestJS (mesmo formato de financial.invoice_create e " +
			"financial.account_association abaixo). Confirma três campos: id (int), motivo (enum " +
			"string) e codigo_motivo (string). Resposta de sucesso NÃO observada (dependeria de um " +
			"voucher real já existente).",
	},

	"financial.voucher_list": {
		ID:     "financial.voucher_list",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/core/financial/voucher/list",
		Pagination: PaginationSpec{
			Kind:        PaginationPagePerPage,
			LimitParam:  "limit",
			OffsetParam: "page",
		},
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Confirmado ponta a ponta pela Fase 4b: GET sem parâmetros → 200 {\"total\":0," +
			"\"page\":1,\"limit\":10,\"lastPage\":0,\"data\":[]}; GET ?page=2&limit=5 → 200 com " +
			"page/limit ecoados de volta, provando que a paginação é real. Envelope ÚNICO nesta " +
			"seção: nem {success,content} nem o {data,count,page,perPage,...} de " +
			"financial.current_accounts — usa \"limit\" (não \"perPage\") e \"total\"/\"lastPage\" " +
			"em vez de \"count\"/\"pages\". Mesmo PaginationKind (PagePerPage, página 1-indexada), " +
			"nome de parâmetro diferente.",
	},

	// financial.invoice_create ("Criação da Conta") — o corpo mais
	// complexo desta fase: validação NestJS (estilo diferente do resto
	// do grupo Financeiro) nomeando uma estrutura aninhada. type é um
	// enum de UM caractere (C ou D — crédito/débito, mesma convenção
	// binária de outros campos "tipo" já vistos na API), date é
	// ISO-8601, table/user/unity são ids inteiros, account é um objeto
	// não-vazio, items e installments são arrays não-vazios — nenhum dos
	// três (account/items/installments) teve sua forma INTERNA
	// confirmada (a validação para na primeira camada; um corpo com esses
	// três campos presentes, mas vazios/malformados por dentro, não foi
	// tentado para não arriscar uma criação real).
	"financial.invoice_create": {
		ID:       "financial.invoice_create",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/core/financial/invoice/create",
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "POST corpo vazio → 400 estilo NestJS nomeando: type (enum \"C\"/\"D\", " +
			"obrigatório), date (ISO-8601, <=10 chars, obrigatório), table (int, obrigatório), " +
			"user (int, obrigatório), unity (int, obrigatório), account (objeto não-vazio, " +
			"obrigatório), items (array com >=1 item, obrigatório), installments (array com >=1 " +
			"item, obrigatório). A forma INTERNA de account/items/installments não foi confirmada " +
			"— a validação de primeira camada não foi ultrapassada de propósito (evitar risco de " +
			"criação real). tools.GerenciarConta (ação \"criar\") expõe os campos de topo " +
			"tipados e passa account/items/installments como JSON livre, documentando essa lacuna " +
			"para quem chamar a tool.",
	},

	// financial.account_association ("Associação de conta financeira") —
	// a Fase 4b tentou três corpos diferentes (vazio, {"id":1},
	// {"accountId":1,"unity":1}) e todos os três devolveram O MESMO erro:
	// 400 {"message":"Account not found","error":"Bad Request",
	// "statusCode":400} — sem NENHUMA validação nomeando campos (diferente
	// de todo outro endpoint NestJS desta seção, que sempre lista os
	// campos que faltam). Como toda tentativa cai direto numa checagem
	// "conta não encontrada", em vez de listar campos obrigatórios, não é
	// possível confirmar o nome real de nenhum campo do corpo desta rota
	// nesta sandbox.
	"financial.account_association": {
		ID:       "financial.account_association",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/core/financial/account/association",
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "Três corpos tentados pela Fase 4b — {}, {\"id\":1}, {\"accountId\":1,\"unity\":1} " +
			"— e os três devolveram o MESMO 400 {\"message\":\"Account not found\",\"error\":" +
			"\"Bad Request\",\"statusCode\":400}, sem NUNCA listar campos obrigatórios (diferente " +
			"de financial.invoice_create/voucher_cancel, que sempre nomeiam o que falta). " +
			"Contrato do corpo genuinamente NÃO confirmável nesta sandbox — tools.GerenciarConta " +
			"NÃO expõe uma ação para este endpoint por causa disso (ver seu doc comment), em vez " +
			"de arriscar um contrato inventado.",
	},

	// --- Estoque, Fase 4b (api.feegow.com/v1/api) -----------------------
	//
	// Dos 7 endpoints de Estoque do inventário (ESPECIFICACAO.md §13),
	// apenas os 3 sob api.feegow.com são alcançáveis: os outros 4 vivem
	// sob core.feegow.com.br (HostCoreBR), confirmado MORTO pela Fase 0
	// (stock.location_list acima) — três entradas adicionais foram
	// acrescentadas abaixo só para completar o inventário/documentação,
	// sem tool alguma por trás.

	"stock.product_position": {
		ID:     "stock.product_position",
		Host:   HostAPI,
		Method: http.MethodPost,
		Path:   "/core/financial/base/product/position",
		Pagination: PaginationSpec{
			Kind:        PaginationPagePerPage,
			LimitParam:  "perPage",
			OffsetParam: "page",
		},
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Corpo vazio → 200, foundParameters=[\"fabricante\",\"produto\",\"categoria\"," +
			"\"localizacao\",\"dataInicio\",\"dataFim\",\"perPage\",\"page\"] — confirma 6 filtros " +
			"opcionais além da paginação. Mesmo envelope {data,count,page,perPage,pages,version," +
			"foundParameters} de financial.current_accounts, mas count vem como STRING (\"0\"), " +
			"não número, aqui e em stock.product_list — inconsistência confirmada pela Fase 4b. " +
			"dataInicio/dataFim: formato não confirmado (sandbox vazia, nenhum filtro de data " +
			"testado com sucesso real) — repassados como string livre, sem tradução DateParam.",
	},

	"stock.product_list": {
		ID:     "stock.product_list",
		Host:   HostAPI,
		Method: http.MethodPost,
		Path:   "/core/financial/base/product/list",
		Pagination: PaginationSpec{
			Kind:        PaginationPagePerPage,
			LimitParam:  "perPage",
			OffsetParam: "page",
		},
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Corpo vazio → 200, foundParameters=[\"id\",\"category\",\"location\",\"producer\"," +
			"\"type\",\"perPage\",\"page\"] — confirma 4 filtros opcionais (nomes em inglês, " +
			"diferente de stock.product_position, que usa nomes em português) além da paginação. " +
			"Mesmo envelope de stock.product_position, incluindo o count-como-string.",
	},

	// stock.product_insert ("Inserir Produto") — o endpoint com a
	// confirmação mais forte desta fase inteira: a Fase 4b não só viu o
	// 400 de validação (corpo vazio, nomeando 11 campos numéricos
	// obrigatórios), como também completou uma inserção REAL (todos os
	// 11 campos como inteiros=1) e recebeu 201 com o registro criado
	// (id incluído). Por isso o SuccessStatus=201 nesta descriptor —
	// sem ele, Client.Call trataria essa resposta de sucesso real como
	// um erro (ver o doc comment de EndpointDescriptor.SuccessStatus).
	// Confirma também que NENHUM campo de nome/descrição textual é
	// exigido — só os 11 numéricos.
	"stock.product_insert": {
		ID:            "stock.product_insert",
		Host:          HostAPI,
		Method:        http.MethodPost,
		Path:          "/core/financial/financial-stock/product/insert",
		Envelope:      EnvelopeNone,
		SuccessStatus: http.StatusCreated,
		Verified:      true,
		Notes: "POST corpo vazio → 400 nomeando 11 campos obrigatórios, todos inteiros: " +
			"TipoProduto, CategoriaID, FabricanteID, LocalizacaoID, DiasAvisoValidade, " +
			"ApresentacaoQuantidade, ApresentacaoUnidade, EstoqueMinimo, EstoqueMaximo, " +
			"PrecoCompra, PrecoVenda. POST com os 11 campos = 1 → 201 REAL (não 200!) com o " +
			"registro criado ecoado em camelCase + sysUser + id. Nenhum campo de nome/descrição " +
			"exigido. ATENÇÃO: cria um produto de verdade na sandbox (id=1, dados sintéticos sem " +
			"significado) — efeito colateral aceito pela Fase 4b, mesmo espírito dos agendamentos " +
			"de teste que a Fase 0 já criava/cancelava.",
	},

	"stock.product_entry": {
		ID:       "stock.product_entry",
		Host:     HostCoreBR,
		Method:   http.MethodPost,
		Path:     "/financial2/external/financial-stock/product/entry",
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "INACESSÍVEL neste ambiente — mesmo motivo de stock.location_list acima: " +
			"core.feegow.com.br (HostCoreBR) não resolve nesta sandbox. Registrado só para " +
			"completar o inventário de Estoque (ESPECIFICACAO.md §13); nenhuma tool chama este " +
			"endpoint.",
	},

	"stock.product_movement": {
		ID:       "stock.product_movement",
		Host:     HostCoreBR,
		Method:   http.MethodPost,
		Path:     "/financial2/external/financial-stock/product/movement",
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "INACESSÍVEL neste ambiente — mesmo motivo de stock.location_list acima. " +
			"Registrado só para completar o inventário de Estoque; nenhuma tool chama este " +
			"endpoint.",
	},

	"stock.product_exit": {
		ID:       "stock.product_exit",
		Host:     HostCoreBR,
		Method:   http.MethodPost,
		Path:     "/financial2/external/financial-stock/product/exit",
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "INACESSÍVEL neste ambiente — mesmo motivo de stock.location_list acima. " +
			"Registrado só para completar o inventário de Estoque; nenhuma tool chama este " +
			"endpoint.",
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

	// --- Propostas, Fase 4c (api.feegow.com/v1/api) ---------------------
	//
	// Every entry below was confirmed against the real sandbox by the Fase
	// 4c smoke test (see internal/tools' gerenciar_propostas doc comment).

	"proposal.list": {
		ID:     "proposal.list",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/proposal/list",
		DateParams: []DateParam{
			{Role: DateRoleStart, WireName: "data_inicio", Format: ISO8601},
			{Role: DateRoleEnd, WireName: "data_fim", Format: ISO8601},
		},
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Confirmado ponta a ponta pela Fase 4c: sem parâmetros → 422 nomeando data_inicio e " +
			"data_fim como obrigatórios QUANDO paciente_id não é fornecido; com paciente_id=1 (sem " +
			"propostas cadastradas) → 422 {\"paciente_id\":[\"Não existem propostas para este " +
			"paciente.\"]} (formato Laravel bare-map comum, tratado como ValidationError normal); com " +
			"data_inicio=2026-01-01&data_fim=2026-02-01 → 200 REAL {\"success\":true,\"content\":[]," +
			"\"total\":0} (ISO-8601, confirmando o formato — não DD-MM-YYYY). limit/offset foram " +
			"aceitos sem erro quando enviados, e uma janela de ~6 anos (2020-01-01 a 2026-01-01) " +
			"também foi aceita sem erro — a sandbox vazia não permitiu provar se algum dos dois " +
			"filtra de verdade, então NENHUM PaginationSpec é declarado aqui (mesmo raciocínio de " +
			"financial.list_suppliers na Fase 4b): gerenciar_propostas impõe seu próprio teto de " +
			"janela de datas (requireDateRange, reaproveitado de financeiro_consulta.go) em vez de " +
			"expor uma paginação não confirmada.",
	},

	// proposal.list_dates ("Listar propostas pela data") — contrato NÃO
	// confirmável pela Fase 4c: mesma situação de financial.voucher_create/
	// financial.account_association na Fase 4b.
	"proposal.list_dates": {
		ID:       "proposal.list_dates",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/proposal/list-dates",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "Contrato NÃO confirmável pela Fase 4c: TODA combinação testada — sem parâmetros, só " +
			"data_inicio, só data_fim, ambas as datas, e nomes alternativos (data, dataInicio, " +
			"dataFim, periodo_inicio/periodo_fim, start/end, data_start/data_end, data_referencia) " +
			"— devolveu o MESMO 400 {\"success\":false,\"message\":\"Só pode buscar utilizando " +
			"apenas uma das datas.\"}, inclusive quando exatamente UMA data era enviada (o que a " +
			"própria mensagem diz que deveria bastar). Mesma situação de " +
			"financial.voucher_create/financial.account_association na Fase 4b: nenhuma sondagem " +
			"conseguiu isolar o nome real do(s) parâmetro(s) de data. tools.GerenciarPropostas " +
			"(acao=listar_por_data) NÃO chama este endpoint por causa disso — devolve " +
			"ArgumentIndisponivel em vez de uma chamada que certamente falharia de forma opaca.",
	},

	// proposal.create ("Criar proposta") — mesmo limite de risco que
	// financial.invoice_create na Fase 4b: os campos de TOPO são
	// confirmados, mas a forma interna de "procedimentos" não foi sondada
	// para não arriscar criar uma proposta real sem endpoint de remoção
	// documentado.
	"proposal.create": {
		ID:       "proposal.create",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/proposal/create",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "POST corpo vazio → 422 nomeando 5 campos obrigatórios: proposer_id, paciente_id, " +
			"status_id, proposal_date, procedimentos. Com os quatro primeiros = 1 (placeholders) e " +
			"procedimentos=[] → 422 NOVO: proposer_id e paciente_id \"O campo ... selecionado é " +
			"inválido\" (regra tipo \"exists\", contra ids reais desta licença) e procedimentos " +
			"ainda \"obrigatório\" (array vazio não satisfaz — precisa ter pelo menos um item). A " +
			"forma INTERNA de cada item de procedimentos NÃO foi confirmada — completar uma criação " +
			"real exigiria paciente/proposer/procedimento reais desta licença, e não há endpoint de " +
			"remoção de proposta documentado (mesmo risco de financial.invoice_create na Fase 4b). " +
			"Envelope de sucesso ASSUMIDO {success,content} por analogia com proposal.list/" +
			"proposal.proposal_url (mesmo grupo, ambos confirmados com esse envelope) — não " +
			"observado diretamente para este endpoint.",
	},

	// proposal.change_status ("Mudar status da proposta") — nomes de campo
	// em INGLÊS (proposal_id), diferente de proposal.proposal_url, que usa
	// PORTUGUÊS (proposta_id) para o mesmo conceito.
	"proposal.change_status": {
		ID:       "proposal.change_status",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/proposal/change-status",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "POST corpo vazio → 422 nomeando proposal_id e status_id como obrigatórios (nomes em " +
			"INGLÊS \"proposal_id\", DIFERENTE de proposal.proposal_url, que usa \"proposta_id\" em " +
			"PORTUGUÊS para o mesmo conceito — mais uma divergência de nomenclatura confirmada nesta " +
			"API). Com proposal_id=1, status_id=1 → 422 só sobre proposal_id (\"selecionado é " +
			"inválido\") — confirma que status_id=1 É um valor de status válido, e que proposal_id " +
			"passa por uma checagem \"exists\" contra propostas reais desta licença. Nenhuma " +
			"proposta real foi mutada (evitar mudar o status de algo de verdade sem saber o efeito). " +
			"Envelope de sucesso assumido por analogia, mesma razão de proposal.create.",
	},

	"proposal.proposal_url": {
		ID:       "proposal.proposal_url",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/proposal/proposal-url",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Confirmado ponta a ponta pela Fase 4c: sem parâmetros → 422 {\"proposta_id\":[\"O " +
			"campo proposta id é obrigatório.\"]} (nome em PORTUGUÊS \"proposta_id\" — ver a nota de " +
			"proposal.change_status sobre essa divergência com o resto do grupo); com proposta_id=1 " +
			"(proposta inexistente nesta sandbox) → 200 REAL {\"success\":true,\"content\":false} — " +
			"content é um BOOLEAN quando não há URL disponível para o id, não uma string nem null.",
	},

	// --- Laudos, Fase 4c (api.feegow.com/v1/api) -------------------------

	// medical_reports.get_laudos_list ("Listar laudos") — contrato NÃO
	// confirmável pela Fase 4c, mesma classe de problema que
	// proposal.list_dates acima.
	"medical_reports.get_laudos_list": {
		ID:       "medical_reports.get_laudos_list",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/medical-reports/get-laudos-list",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "Contrato NÃO confirmável pela Fase 4c: toda combinação testada (sem parâmetros, " +
			"data_inicio/data_fim, date_start/date_end, data, data_referencia, periodo_inicio/" +
			"periodo_fim, start/end, dataInicio/dataFim, agendamento_id) devolveu o MESMO 422 " +
			"{\"success\":false,\"cod_erro\":0,\"message\":\"Data missing\"} — o nome real do(s) " +
			"parâmetro(s) de data não pôde ser isolado. POST no mesmo path → 422 real \"The POST " +
			"method is not supported for this route. Supported methods: GET, HEAD.\", confirmando " +
			"que GET é o método certo (não é um erro de método mascarando o problema). Mesma " +
			"situação de proposal.list_dates acima: tools.ConsultarLaudos (acao=listar) devolve " +
			"ArgumentIndisponivel em vez de adivinhar.",
	},

	"medical_reports.get_labs_report_file": {
		ID:       "medical_reports.get_labs_report_file",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/medical-reports/get-labs-report-file",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Confirmado ponta a ponta pela Fase 4c: sem parâmetros → 422 " +
			"{\"success\":true,\"content\":{\"lab_report_id\":[\"O campo lab report id é " +
			"obrigatório.\"]}} — o QUARTO formato de 422 que classify422 (client.go) trata: o mapa " +
			"campo->mensagens vem aninhado dentro de \"content\" (não \"message\"), com \"success\" " +
			"deixado em true apesar de ser um 422 real (irrelevante para a classificação, que " +
			"despacha por status HTTP, não pelo corpo — ver EnvelopeKind). Com lab_report_id=1 " +
			"(laudo inexistente) → 200 REAL {\"success\":true,\"content\":{\"status\":3,\"msg\":" +
			"\"Arquivo não existe\"},\"total\":2} — envelope padrão {success,content,total} " +
			"confirmado.",
	},

	// medical_reports.search ("Visualizar laudo registrado no Feegow a
	// partir do agendamento") — laudo é dado clínico; consultar_laudos
	// (acao=visualizar) devolve o mínimo útil e nunca despeja o conteúdo do
	// laudo em log (ver internal/tools/laudos_consulta.go).
	"medical_reports.search": {
		ID:       "medical_reports.search",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/medical-reports/search",
		Envelope: EnvelopeNone,
		Verified: false,
		Notes: "Sem parâmetros → 422 {\"success\":false,\"message\":{\"agendamento_id\":[\"O campo " +
			"agendamento id é obrigatório.\"]}} — o TERCEIRO formato de 422 (ver classify422 em " +
			"client.go): mapa aninhado dentro de \"message\". Com agendamento_id=1 (sem laudo " +
			"associado) → 409 REAL {\"success\":false,\"Message\":\"Não foi encontrado resultado " +
			"para este laudo\"} — reparar no \"Message\" com M MAIÚSCULO, diferente de todo outro " +
			"409 desta API (que usa \"content\", minúsculo, para o texto do erro); classifyError já " +
			"cobre isso sem precisar de um case novo (seu fallback usa o corpo inteiro como Content " +
			"quando \"content\" está ausente/vazio). Nenhuma resposta de SUCESSO (laudo realmente " +
			"encontrado) foi observada — não há laudo cadastrado nesta sandbox e a Fase 4c não " +
			"registrou um real (ver medical_reports.create). EnvelopeNone porque nem o 422 nem o " +
			"409 usam o campo \"content\" padrão — mesma lógica de " +
			"medical_reports.create/patient.check_eligibility: assumir EnvelopeStandard arriscaria " +
			"procurar por \"content\" e descartar uma resposta real em silêncio.",
	},

	// --- Faturamento, Fase 4c (api.feegow.com/v1/api) --------------------
	//
	// As três abaixo compartilham o MESMO path — /billing/insurances-billing
	// — sob três métodos HTTP diferentes (GET/PUT/POST), o único caso assim
	// no inventário desta API. Por instrução EXPLÍCITA desta fase, nenhuma
	// operação real foi completada (o faturamento de uma licença de teste
	// não tem endpoint de limpeza) — só o 422 de corpo vazio foi sondado
	// para cada verbo.

	"billing.search_guide": {
		ID:       "billing.search_guide",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/billing/insurances-billing",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "GET sem parâmetros → 422 {\"billing_type_id\":[\"O campo billing type id é " +
			"obrigatório.\"],\"billing\":[\"O campo billing é obrigatório.\"]}. \"billing\" não tem " +
			"sufixo \"_id\" como todo outro identificador desta API (billing_type_id aqui mesmo, " +
			"billing_id no PUT abaixo) — pode ser o mesmo conceito de billing_id sob outro nome " +
			"(mais uma divergência de nomenclatura já documentada nesta API) ou um valor de outro " +
			"tipo (ex.: número da guia); NÃO confirmado, por isso exposto como valor livre (any) na " +
			"tool, nunca como *int. Envelope assumido {success,content} por analogia com o resto da " +
			"API — não observado (instrução explícita desta fase: parar no 422 de corpo vazio).",
	},

	"billing.edit_guide": {
		ID:       "billing.edit_guide",
		Host:     HostAPI,
		Method:   http.MethodPut,
		Path:     "/billing/insurances-billing",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "PUT sem parâmetros → 422 {\"billing_id\":[\"O campo billing id é obrigatório.\"]," +
			"\"billing_type_id\":[\"O campo billing type id é obrigatório.\"]} — SÓ dois campos " +
			"nomeados por essa validação de primeira camada (diferente do POST abaixo, que nomeia " +
			"16); pode haver mais campos opcionais/condicionais não revelados sem enviar os dois " +
			"primeiros. Confirma que PUT é aceito nesta rota (ver EndpointDescriptor.Validate em " +
			"types.go para o porquê de PUT precisar entrar no allow-list de Method). Envelope " +
			"assumido por analogia, não observado (instrução explícita desta fase: parar no 422 de " +
			"corpo vazio, sem editar uma guia real).",
	},

	// billing.insert_guide ("Inserir Guia") — o corpo com MAIS campos
	// obrigatórios de todo este registry (16). Por instrução EXPLÍCITA
	// desta fase, NENHUMA inserção real foi tentada.
	"billing.insert_guide": {
		ID:       "billing.insert_guide",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/billing/insurances-billing",
		Envelope: EnvelopeStandard,
		Verified: false,
		Notes: "POST corpo vazio → 422 nomeando 16 campos obrigatórios: billing_type_id, unit_id, " +
			"insurance_id, insurance_plan_id, applicant_professional_council_id, " +
			"number_on_the_requesting_council, UF_Requesting_Council, requesting_CBO_code, hired, " +
			"carrier_code, hired_requester_ID, hired_requester_code_at_carrier, ANS_registry, " +
			"CNES_code, requesting_professional_ID, request_date. Só a OBRIGATORIEDADE e o NOME de " +
			"cada campo foram confirmados — nenhum tipo exato (int vs string), formato " +
			"(request_date livre? ISO-8601?) ou campo opcional adicional foi sondado além desta " +
			"única validação de primeira camada, deliberadamente (mesmo limite que " +
			"financial.invoice_create já aceitou na Fase 4b, e por instrução explícita desta fase: " +
			"o faturamento de uma licença de teste não tem endpoint de limpeza). " +
			"tools.GerenciarFaturamento (acao=inserir_guia) expõe os 16 campos individualmente mas " +
			"tipados como valor livre (any) — nunca assume int/string sem confirmação, e nunca " +
			"trava um caller real com um tipo Go incorreto que esta fase não teve como verificar. " +
			"Envelope de sucesso assumido por analogia — não observado.",
	},

	// --- Relatórios, Fase 4c (api.feegow.com/v1/api) ---------------------

	"reports.list": {
		ID:       "reports.list",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/reports/list",
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Confirmado ponta a ponta pela Fase 4c: GET sem parâmetros → 200, ARRAY JSON PURO, " +
			"sem NENHUM envelope — nem {success,content} nem {data,count,...} — por isso " +
			"EnvelopeNone: EnvelopeStandard faria parseSuccess procurar por um campo \"success\" " +
			"que não existe no nível raiz (a resposta É a lista). Cada item: {\"id\":N,\"Ct\":\"...\"" +
			" (categoria/módulo),\"Relatorio\":\"...\" (nome de exibição),\"Arquivo\":\"...\" (slug " +
			"usado como \"report\" em reports.generate),\"sysActive\":1,\"Permissoes\":\"...\"," +
			"\"StatusRelatorioID\":1,\"NomeStatus\":\"Disponível\",\"CorStatus\":null," +
			"\"Habilitado\":1}.",
	},

	"reports.generate": {
		ID:       "reports.generate",
		Host:     HostAPI,
		Method:   http.MethodPost,
		Path:     "/reports/generate",
		Envelope: EnvelopeNone,
		Verified: true,
		Notes: "Confirmado ponta a ponta pela Fase 4c: POST corpo vazio → 422 {\"report\":[\"O " +
			"campo report é obrigatório.\"]}; POST {\"report\":\"schedule-appointments\"} (o " +
			"\"Arquivo\" de um relatório real listado por reports.list) → 200 REAL " +
			"{\"success\":true,\"reportId\":53,\"route\":\"schedule-appointments\",\"reportName\":" +
			"\"Agendamentos\",\"columns\":false,\"filters\":false,\"data\":false} — SEM campo " +
			"\"content\" (por isso EnvelopeNone). columns/filters/data vieram todos `false` (não um " +
			"array/objeto) sem nenhum filtro adicional enviado — provavelmente exigem parâmetros " +
			"extras não documentados pelo inventário desta fase para popular dados de verdade; não " +
			"sondado além do campo \"report\" obrigatório. ACHADO IMPORTANTE: com um \"report\" que " +
			"NÃO corresponde a nenhum relatório real (ex.: \"nao-existe\") → 200 REAL, corpo `[]` — um " +
			"ARRAY JSON vazio, um shape TOTALMENTE DIFERENTE do objeto {success,reportId,...} do caso " +
			"válido, sem nenhum campo \"success\" para checar. tools.GerarRelatorio decodifica a " +
			"resposta como `any` genérico (nunca assume um dos dois shapes) exatamente por causa " +
			"disso — checkEnvelopeNoneSuccess não se aplica aqui: forçar um struct com campo Success " +
			"quebraria a decodificação do caso `[]`, transformando um \"report\" inválido (resultado " +
			"vazio, não um erro) num erro de decodificação.",
	},

	// --- Funcionários, Fase 4c (api.feegow.com/v1/api) --------------------
	"employee.list": {
		ID:       "employee.list",
		Host:     HostAPI,
		Method:   http.MethodGet,
		Path:     "/employee/list",
		Envelope: EnvelopeStandard,
		Verified: true,
		Notes: "Confirmado ponta a ponta pela Fase 4c: GET sem parâmetros → 200 {\"success\":true," +
			"\"content\":[],\"total\":0}, sandbox sem funcionários cadastrados. Sem parâmetros " +
			"testados além disso.",
	},
}
