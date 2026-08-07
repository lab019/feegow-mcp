package tools

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// --- buscar_pacientes -------------------------------------------------

// TestBuscarPacientes_NoTwoFactRequirement proves buscar_pacientes, unlike
// identificar_paciente, resolves with a SINGLE filter — the admin profile
// does not apply the atendimento two-fact identification rule.
func TestBuscarPacientes_NoTwoFactRequirement(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[
			{"patient_id":1,"nome":"A","nascimento":"2000-01-01"},
			{"patient_id":2,"nome":"B","nascimento":"2000-01-02"}
		],"total":2}`)
	})
	client := newTestClient(t, mux)

	result, err := BuscarPacientes(ctxWithToken("tok"), client, BuscarPacientesArgs{CPF: "11111111111"})
	if err != nil {
		t.Fatalf("BuscarPacientes: %v", err)
	}
	if len(result.Pacientes) != 2 {
		t.Fatalf("Pacientes = %+v, want 2 entries", result.Pacientes)
	}
}

// TestBuscarPacientes_NoFiltersAtAll proves an empty filter set is allowed
// (browsing the roster is a normal admin operation) — the cap on Limit is
// what keeps this bounded, not a filter requirement.
func TestBuscarPacientes_NoFiltersAtAll(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := BuscarPacientes(ctxWithToken("tok"), client, BuscarPacientesArgs{}); err != nil {
		t.Fatalf("BuscarPacientes: %v", err)
	}
	if gotQuery.Has("cpf") || gotQuery.Has("telefone") {
		t.Fatalf("unexpected filter params sent with no filters: %v", gotQuery)
	}
}

// TestBuscarPacientes_LimitDefaultsAndCaps proves the pagination ceiling:
// no limit gets a sane default, and a caller-supplied limit above the cap
// is clamped, never honored past it — a tool that could hand back the
// entire patient base in one call is a problem in itself.
func TestBuscarPacientes_LimitDefaultsAndCaps(t *testing.T) {
	cases := []struct {
		name      string
		limit     int
		wantLimit string
	}{
		{"unset defaults", 0, "20"},
		{"within range passes through", 50, "50"},
		{"above cap is clamped", 100000, "100"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotQuery url.Values
			mux := http.NewServeMux()
			mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query()
				writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
			})
			client := newTestClient(t, mux)

			if _, err := BuscarPacientes(ctxWithToken("tok"), client, BuscarPacientesArgs{Limit: c.limit}); err != nil {
				t.Fatalf("BuscarPacientes: %v", err)
			}
			if got := gotQuery.Get("limit"); got != c.wantLimit {
				t.Fatalf("limit query param = %q, want %q", got, c.wantLimit)
			}
		})
	}
}

// TestBuscarPacientes_RejectsNegativeOffsetBeforeAnyFeegowCall proves a
// negative offset is caught client-side.
func TestBuscarPacientes_RejectsNegativeOffsetBeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a negative offset: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := BuscarPacientes(ctxWithToken("tok"), client, BuscarPacientesArgs{Offset: -1})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("BuscarPacientes error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestBuscarPacientes_NormalizesCPFAndTelefone proves punctuated input is
// normalized to digits-only before it reaches Feegow, same discipline
// identificar_paciente already follows.
func TestBuscarPacientes_NormalizesCPFAndTelefone(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := BuscarPacientes(ctxWithToken("tok"), client, BuscarPacientesArgs{
		CPF: "123.456.789-01", Telefone: "(21) 99999-8888",
	}); err != nil {
		t.Fatalf("BuscarPacientes: %v", err)
	}
	if got := gotQuery.Get("cpf"); got != "12345678901" {
		t.Fatalf("cpf query param = %q, want digits-only", got)
	}
	if got := gotQuery.Get("telefone"); got != "21999998888" {
		t.Fatalf("telefone query param = %q, want digits-only", got)
	}
}

// TestBuscarPacientes_RejectsMalformedAlteradoEm proves the alterado_em
// filter is validated as ISO-8601 before any Feegow call.
func TestBuscarPacientes_RejectsMalformedAlteradoEm(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for malformed alterado_em: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := BuscarPacientes(ctxWithToken("tok"), client, BuscarPacientesArgs{AlteradoEm: "10-03-2023"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("BuscarPacientes error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestBuscarPacientes_ReturnsRichFields proves the admin result carries
// display-relevant fields identificar_paciente deliberately omits — the
// atendimento PII-minimization rule does not apply to this profile.
func TestBuscarPacientes_ReturnsRichFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{
			"patient_id":42,"nome":"Fulano de Tal","nome_social":"Fulano",
			"nascimento":"1990-05-10","bairro":"Centro","email":"fulano@example.com",
			"celular":"21955550000","criado_em":"2023-03-10 14:04:00","alterado_em":"2023-03-10 18:30:05"
		}],"total":1}`)
	})
	client := newTestClient(t, mux)

	result, err := BuscarPacientes(ctxWithToken("tok"), client, BuscarPacientesArgs{})
	if err != nil {
		t.Fatalf("BuscarPacientes: %v", err)
	}
	if len(result.Pacientes) != 1 {
		t.Fatalf("Pacientes = %+v, want 1 entry", result.Pacientes)
	}
	got := result.Pacientes[0]
	if got.PacienteID != 42 || got.Nome != "Fulano de Tal" || got.Email != "fulano@example.com" {
		t.Fatalf("Pacientes[0] = %+v, want the full admin-facing fields", got)
	}
}

// TestBuscarPacientes_NoPIIInLogs is item (e)'s coverage for
// buscar_pacientes: the /patient/list response this tool decodes carries
// nome/celular directly — none of it may ever reach the process log, even
// though this tool never audits (it is a read, not a write).
func TestBuscarPacientes_NoPIIInLogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{
			"patient_id":42,"nome":"Segredo Pessoal","celular":"21955550000"
		}],"total":1}`)
	})

	assertNoPIIInLog(t, mux, []string{"Segredo Pessoal", "21955550000"}, func(client *feegow.Client) error {
		_, err := BuscarPacientes(ctxWithToken("tok"), client, BuscarPacientesArgs{})
		return err
	})
}

// --- obter_paciente -----------------------------------------------------

// TestObterPaciente_RequiresPacienteID proves paciente_id is validated
// before any Feegow call.
func TestObterPaciente_RequiresPacienteID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a missing paciente_id: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ObterPaciente(ctxWithToken("tok"), client, ObterPacienteArgs{})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ObterPaciente error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestObterPaciente_RoundTripsFullCadastro proves the full /patient/search
// content passes through, including the DD-MM-YYYY nascimento field this
// endpoint uses (unlike buscar_pacientes' ISO-8601).
func TestObterPaciente_RoundTripsFullCadastro(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/search", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{
			"id":6563,"nome":"Carlos Luiz","nascimento":"27-02-1986",
			"documentos":{"cpf":"01234567890"},"convenios":[{"convenio_id":98}]
		}}`)
	})
	client := newTestClient(t, mux)

	result, err := ObterPaciente(ctxWithToken("tok"), client, ObterPacienteArgs{PacienteID: 6563})
	if err != nil {
		t.Fatalf("ObterPaciente: %v", err)
	}
	if got := gotQuery.Get("paciente_id"); got != "6563" {
		t.Fatalf("paciente_id query param = %q, want %q", got, "6563")
	}
	m, ok := result.Paciente.(map[string]any)
	if !ok {
		t.Fatalf("Paciente = %#v, want a decoded object", result.Paciente)
	}
	if m["nascimento"] != "27-02-1986" {
		t.Fatalf("nascimento = %v, want the raw DD-MM-YYYY value passed through unconverted", m["nascimento"])
	}
}

// TestObterPaciente_NoPIIInLogs is item (e)'s coverage for obter_paciente:
// the full /patient/search cadastro this tool passes through carries
// nome/cpf/nascimento — none of it may ever reach the process log.
func TestObterPaciente_NoPIIInLogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/search", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{
			"id":6563,"nome":"Segredo Pessoal","documentos":{"cpf":"01234567890"}
		}}`)
	})

	assertNoPIIInLog(t, mux, []string{"Segredo Pessoal", "01234567890"}, func(client *feegow.Client) error {
		_, err := ObterPaciente(ctxWithToken("tok"), client, ObterPacienteArgs{PacienteID: 6563})
		return err
	})
}

// --- consultar_paciente_clinico ------------------------------------------

// TestConsultarPacienteClinico_UnknownTipo_RejectsBeforeFeegowCall proves an
// unrecognized tipo never reaches Feegow.
func TestConsultarPacienteClinico_UnknownTipo_RejectsBeforeFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unknown tipo: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, ConsultarPacienteClinicoArgs{Tipo: "nao-existe"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarPacienteClinico error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestConsultarPacienteClinico_TiposRequiringPacienteID_RejectWithoutIt
// proves dependentes/linha_tempo/elegibilidade all require paciente_id,
// rejected before any Feegow call.
func TestConsultarPacienteClinico_TiposRequiringPacienteID_RejectWithoutIt(t *testing.T) {
	for _, tipo := range []string{tipoDependentes, tipoLinhaTempo, tipoElegibilidade} {
		t.Run(tipo, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				t.Fatalf("unexpected Feegow call for tipo=%s without paciente_id: %s %s", tipo, r.Method, r.URL)
			})
			client := newTestClient(t, mux)

			_, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, ConsultarPacienteClinicoArgs{Tipo: tipo})
			var argErr *ArgumentError
			if !errors.As(err, &argErr) {
				t.Fatalf("ConsultarPacienteClinico(tipo=%s) error = %v (%T), want *ArgumentError", tipo, err, err)
			}
		})
	}
}

// TestConsultarPacienteClinico_DependentesCallsRightEndpoint proves
// tipo=dependentes calls /patient/list-dependents with paciente_id.
func TestConsultarPacienteClinico_DependentesCallsRightEndpoint(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list-dependents", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"id":1,"nome":"X","paciente_id":45}],"total":1}`)
	})
	client := newTestClient(t, mux)

	result, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, ConsultarPacienteClinicoArgs{
		Tipo: tipoDependentes, PacienteID: 5,
	})
	if err != nil {
		t.Fatalf("ConsultarPacienteClinico: %v", err)
	}
	if gotQuery.Get("paciente_id") != "5" {
		t.Fatalf("paciente_id query param = %q, want %q", gotQuery.Get("paciente_id"), "5")
	}
	items, ok := result.Itens.([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("Itens = %#v, want a 1-element list", result.Itens)
	}
}

// TestConsultarPacienteClinico_LinhaTempoCallsMedicalRecordTimeline proves
// tipo=linha_tempo calls /medical-record/timeline.
func TestConsultarPacienteClinico_LinhaTempoCallsMedicalRecordTimeline(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/medical-record/timeline", func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[]}`)
	})
	client := newTestClient(t, mux)

	if _, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, ConsultarPacienteClinicoArgs{
		Tipo: tipoLinhaTempo, PacienteID: 3,
	}); err != nil {
		t.Fatalf("ConsultarPacienteClinico: %v", err)
	}
	if !called {
		t.Fatal("tipo=linha_tempo never called /medical-record/timeline")
	}
}

// TestConsultarPacienteClinico_ElegibilidadeCallsCheckEligibility proves
// tipo=elegibilidade calls /patient/check-eligibility and decodes its
// EnvelopeNone body (no {success,content} wrapper).
func TestConsultarPacienteClinico_ElegibilidadeCallsCheckEligibility(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/check-eligibility", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"elegivel":false,"term":null}`)
	})
	client := newTestClient(t, mux)

	result, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, ConsultarPacienteClinicoArgs{
		Tipo: tipoElegibilidade, PacienteID: 3,
	})
	if err != nil {
		t.Fatalf("ConsultarPacienteClinico: %v", err)
	}
	m, ok := result.Itens.(map[string]any)
	if !ok || m["elegivel"] != false {
		t.Fatalf("Itens = %#v, want the decoded EnvelopeNone body", result.Itens)
	}
}

// TestConsultarPacienteClinico_PedidosExame_RequiresAllFourFields proves
// paciente_id, data_inicio, data_fim and tipo_pedido are all mandatory for
// tipo=pedidos_exame, rejected before any Feegow call.
func TestConsultarPacienteClinico_PedidosExame_RequiresAllFourFields(t *testing.T) {
	tipoPedido := 1
	cases := []struct {
		name string
		args ConsultarPacienteClinicoArgs
	}{
		{"missing paciente_id", ConsultarPacienteClinicoArgs{Tipo: tipoPedidosExame, DataInicio: "2023-03-10", DataFim: "2023-03-10", TipoPedido: &tipoPedido}},
		{"missing data_inicio", ConsultarPacienteClinicoArgs{Tipo: tipoPedidosExame, PacienteID: 1, DataFim: "2023-03-10", TipoPedido: &tipoPedido}},
		{"missing data_fim", ConsultarPacienteClinicoArgs{Tipo: tipoPedidosExame, PacienteID: 1, DataInicio: "2023-03-10", TipoPedido: &tipoPedido}},
		{"missing tipo_pedido", ConsultarPacienteClinicoArgs{Tipo: tipoPedidosExame, PacienteID: 1, DataInicio: "2023-03-10", DataFim: "2023-03-10"}},
		{"invalid tipo_pedido", ConsultarPacienteClinicoArgs{Tipo: tipoPedidosExame, PacienteID: 1, DataInicio: "2023-03-10", DataFim: "2023-03-10", TipoPedido: intPtr(3)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				t.Fatalf("unexpected Feegow call for %s: %s %s", c.name, r.Method, r.URL)
			})
			client := newTestClient(t, mux)

			_, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, c.args)
			var argErr *ArgumentError
			if !errors.As(err, &argErr) {
				t.Fatalf("ConsultarPacienteClinico(%s) error = %v (%T), want *ArgumentError", c.name, err, err)
			}
		})
	}
}

// TestConsultarPacienteClinico_PedidosExame_Success proves a well-formed
// call sends every required field to /patient/exam-requests.
func TestConsultarPacienteClinico_PedidosExame_Success(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/exam-requests", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	client := newTestClient(t, mux)

	tipoPedido := 2
	if _, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, ConsultarPacienteClinicoArgs{
		Tipo: tipoPedidosExame, PacienteID: 7, DataInicio: "2023-03-10", DataFim: "2023-03-20", TipoPedido: &tipoPedido,
	}); err != nil {
		t.Fatalf("ConsultarPacienteClinico: %v", err)
	}
	if gotQuery.Get("paciente_id") != "7" || gotQuery.Get("data_inicio") != "2023-03-10" ||
		gotQuery.Get("data_fim") != "2023-03-20" || gotQuery.Get("tipo_pedido") != "2" {
		t.Fatalf("query params = %v, want all four fields forwarded", gotQuery)
	}
}

// TestConsultarPacienteClinico_ProgramasSaude_IgnoresPacienteID proves
// tipo=programas_saude never forwards paciente_id — the endpoint has no
// such filter (it is a clinic-wide catalog, see
// internal/feegow/registry.go's patient.health_programs Notes) — and works
// with no paciente_id at all.
func TestConsultarPacienteClinico_ProgramasSaude_IgnoresPacienteID(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/health-programs", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, ConsultarPacienteClinicoArgs{
		Tipo: tipoProgramasSaude, PacienteID: 999,
	}); err != nil {
		t.Fatalf("ConsultarPacienteClinico: %v", err)
	}
	if gotQuery.Has("paciente_id") {
		t.Fatalf("tipo=programas_saude leaked a paciente_id param: %v", gotQuery)
	}
}

// TestConsultarPacienteClinico_ProgramasSaude_CapsLimit proves the same
// pagination ceiling buscar_pacientes enforces also applies here.
func TestConsultarPacienteClinico_ProgramasSaude_CapsLimit(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/health-programs", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, ConsultarPacienteClinicoArgs{
		Tipo: tipoProgramasSaude, Limit: 999999,
	}); err != nil {
		t.Fatalf("ConsultarPacienteClinico: %v", err)
	}
	if got := gotQuery.Get("limit"); got != "100" {
		t.Fatalf("limit query param = %q, want the cap %q", got, "100")
	}
}

// TestConsultarPacienteClinico_OrigensAndTabelasParticulares_NoParams
// proves both catalog tipos call their endpoint with no parameters at all.
func TestConsultarPacienteClinico_OrigensAndTabelasParticulares_NoParams(t *testing.T) {
	cases := []struct {
		tipo string
		path string
	}{
		{tipoOrigens, "/patient/list-sources"},
		{tipoTabelasParticulares, "/patient/list-privates"},
	}
	for _, c := range cases {
		t.Run(c.tipo, func(t *testing.T) {
			var gotQuery url.Values
			mux := http.NewServeMux()
			mux.HandleFunc(c.path, func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query()
				writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"id":1}],"total":1}`)
			})
			client := newTestClient(t, mux)

			result, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, ConsultarPacienteClinicoArgs{Tipo: c.tipo})
			if err != nil {
				t.Fatalf("ConsultarPacienteClinico(tipo=%s): %v", c.tipo, err)
			}
			if len(gotQuery) != 0 {
				t.Fatalf("tipo=%s sent unexpected params: %v", c.tipo, gotQuery)
			}
			if _, ok := result.Itens.([]any); !ok {
				t.Fatalf("tipo=%s Itens = %#v, want a decoded list", c.tipo, result.Itens)
			}
		})
	}
}

// TestConsultarPacienteClinico_TiposWithoutPaginationSupport_NeverForwardLimitOrOffset
// is item (f)'s regression test: programas_saude is the ONLY tipo whose
// underlying endpoint (patient.health-programs) documents limit/offset —
// confirmed against both internal/feegow/registry.go's PaginationSpec (only
// patient.health_programs carries one) and doc.txt, which shows no
// limit/offset param for /patient/list-dependents, /patient/exam-requests,
// /medical-record/timeline (undocumented — Fase 0 only), /patient/list-sources
// or /patient/list-privates. A caller setting Limit/Offset for one of these
// five tipos must never see it silently forwarded as if it did something —
// the response size for these five is whatever Feegow returns, not
// something this tool can cap.
func TestConsultarPacienteClinico_TiposWithoutPaginationSupport_NeverForwardLimitOrOffset(t *testing.T) {
	tipoPedido := 1
	cases := []struct {
		tipo string
		path string
		args ConsultarPacienteClinicoArgs
	}{
		{tipoDependentes, "/patient/list-dependents", ConsultarPacienteClinicoArgs{
			Tipo: tipoDependentes, PacienteID: 5, Limit: 999999, Offset: 10,
		}},
		{tipoPedidosExame, "/patient/exam-requests", ConsultarPacienteClinicoArgs{
			Tipo: tipoPedidosExame, PacienteID: 5, DataInicio: "2023-03-10", DataFim: "2023-03-10",
			TipoPedido: &tipoPedido, Limit: 999999, Offset: 10,
		}},
		{tipoLinhaTempo, "/medical-record/timeline", ConsultarPacienteClinicoArgs{
			Tipo: tipoLinhaTempo, PacienteID: 5, Limit: 999999, Offset: 10,
		}},
		{tipoOrigens, "/patient/list-sources", ConsultarPacienteClinicoArgs{
			Tipo: tipoOrigens, Limit: 999999, Offset: 10,
		}},
		{tipoTabelasParticulares, "/patient/list-privates", ConsultarPacienteClinicoArgs{
			Tipo: tipoTabelasParticulares, Limit: 999999, Offset: 10,
		}},
	}
	for _, c := range cases {
		t.Run(c.tipo, func(t *testing.T) {
			var gotQuery url.Values
			mux := http.NewServeMux()
			mux.HandleFunc(c.path, func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query()
				writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
			})
			client := newTestClient(t, mux)

			if _, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, c.args); err != nil {
				t.Fatalf("ConsultarPacienteClinico(tipo=%s): %v", c.tipo, err)
			}
			if gotQuery.Has("limit") || gotQuery.Has("offset") {
				t.Fatalf("tipo=%s forwarded limit/offset the endpoint does not support: %v", c.tipo, gotQuery)
			}
		})
	}
}

// TestConsultarPacienteClinicoTipos_MatchesTheSpecifiedSet locks in the
// exact tipo set this tool advertises.
func TestConsultarPacienteClinicoTipos_MatchesTheSpecifiedSet(t *testing.T) {
	want := map[string]bool{
		"dependentes":          true,
		"pedidos_exame":        true,
		"programas_saude":      true,
		"linha_tempo":          true,
		"elegibilidade":        true,
		"origens":              true,
		"tabelas_particulares": true,
	}
	got := ConsultarPacienteClinicoTipos()
	if len(got) != len(want) {
		t.Fatalf("ConsultarPacienteClinicoTipos() = %v (%d values), want %d values", got, len(got), len(want))
	}
	for _, tipo := range got {
		if !want[tipo] {
			t.Errorf("ConsultarPacienteClinicoTipos() contains unexpected tipo %q", tipo)
		}
	}
}

// TestConsultarPacienteClinico_NoPIIInLogs proves this admin tool never
// writes patient PII to the process log — LGPD still applies to logging on
// the admin profile, even though the read itself carries no
// two-fact/minimization restriction.
func TestConsultarPacienteClinico_NoPIIInLogs(t *testing.T) {
	buf := captureLog(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list-dependents", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"id":1,"nome":"Segredo Pessoal","paciente_id":45}],"total":1}`)
	})
	client := newTestClient(t, mux)

	if _, err := ConsultarPacienteClinico(ctxWithToken("tok"), client, ConsultarPacienteClinicoArgs{
		Tipo: tipoDependentes, PacienteID: 5,
	}); err != nil {
		t.Fatalf("ConsultarPacienteClinico: %v", err)
	}
	if strings.Contains(buf.String(), "Segredo Pessoal") {
		t.Fatalf("log output leaked patient data: %s", buf.String())
	}
}
