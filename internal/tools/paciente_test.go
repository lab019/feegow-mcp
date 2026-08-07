package tools

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestIdentificarPaciente_SingleFactOrMixedPairs_RejectBeforeFeegowCall is
// acceptance criterion 2: a single fact (or an incomplete/mixed
// combination) is rejected as an *ArgumentError before any Feegow request
// is built — proven here by a handler that fails the test if it is ever
// hit at all.
func TestIdentificarPaciente_SingleFactOrMixedPairs_RejectBeforeFeegowCall(t *testing.T) {
	cases := []struct {
		name string
		args IdentidadeArgs
	}{
		{"only cpf", IdentidadeArgs{CPF: "11111111111"}},
		{"only data_nascimento", IdentidadeArgs{DataNascimento: "2000-01-01"}},
		{"only telefone", IdentidadeArgs{Telefone: "21999998888"}},
		{"only nome_completo", IdentidadeArgs{NomeCompleto: "Fulano de Tal"}},
		{"nothing at all", IdentidadeArgs{}},
		{"both pairs at once", IdentidadeArgs{
			CPF: "11111111111", DataNascimento: "2000-01-01",
			Telefone: "21999998888", NomeCompleto: "Fulano de Tal",
		}},
		{"cpf pair plus a stray telefone", IdentidadeArgs{
			CPF: "11111111111", DataNascimento: "2000-01-01", Telefone: "21999998888",
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				t.Fatalf("unexpected Feegow call for invalid args %+v: %s %s", c.args, r.Method, r.URL)
			})
			client := newTestClient(t, mux)

			_, err := IdentificarPaciente(ctxWithToken("tok"), client, c.args)
			var argErr *ArgumentError
			if !errors.As(err, &argErr) {
				t.Fatalf("IdentificarPaciente(%+v) error = %v (%T), want *ArgumentError", c.args, err, err)
			}
		})
	}
}

// TestIdentificarPaciente_CPFPair_Success proves the CPF+data de nascimento
// path resolves to a paciente_id via patient.list, sending cpf, a low
// limit (never unbounded) and a data_aniversario narrowing filter — but
// never a telefone filter.
func TestIdentificarPaciente_CPFPair_Success(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":123,"nome":"X","nascimento":"1995-03-15"}],"total":1}`)
	})
	client := newTestClient(t, mux)

	result, err := IdentificarPaciente(ctxWithToken("tok"), client, IdentidadeArgs{
		CPF: "123.456.789-01", DataNascimento: "1995-03-15",
	})
	if err != nil {
		t.Fatalf("IdentificarPaciente: %v", err)
	}
	if result.PacienteID != 123 || !result.JaExistia {
		t.Fatalf("result = %+v, want {PacienteID:123 JaExistia:true}", result)
	}
	if got := gotQuery.Get("cpf"); got != "12345678901" {
		t.Fatalf("cpf query param = %q, want digits-only %q", got, "12345678901")
	}
	if got := gotQuery.Get("data_aniversario"); got != "03-15" {
		t.Fatalf("data_aniversario query param = %q, want %q", got, "03-15")
	}
	if got := gotQuery.Get("limit"); got != "5" {
		t.Fatalf("limit query param = %q, want a fixed low limit %q — never unbounded", got, "5")
	}
	if gotQuery.Has("telefone") {
		t.Fatalf("CPF pair leaked a telefone param: %v", gotQuery)
	}
}

// TestIdentificarPaciente_PhonePair_Success proves the telefone+nome
// completo path matches on a normalized (case/whitespace-insensitive) name
// comparison and sends only the telefone filter (plus the fixed low
// limit) to patient.list.
func TestIdentificarPaciente_PhonePair_Success(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":55,"nome":"  joana   da  silva ","nascimento":"1970-01-01"}],"total":1}`)
	})
	client := newTestClient(t, mux)

	result, err := IdentificarPaciente(ctxWithToken("tok"), client, IdentidadeArgs{
		Telefone: "(21) 99999-8888", NomeCompleto: "Joana Da Silva",
	})
	if err != nil {
		t.Fatalf("IdentificarPaciente: %v", err)
	}
	if result.PacienteID != 55 || !result.JaExistia {
		t.Fatalf("result = %+v, want {PacienteID:55 JaExistia:true}", result)
	}
	if got := gotQuery.Get("telefone"); got != "21999998888" {
		t.Fatalf("telefone query param = %q, want digits-only %q", got, "21999998888")
	}
	if got := gotQuery.Get("limit"); got != "5" {
		t.Fatalf("limit query param = %q, want a fixed low limit %q — never unbounded", got, "5")
	}
	if gotQuery.Has("cpf") {
		t.Fatalf("phone pair leaked a cpf param: %v", gotQuery)
	}
}

// TestIdentificarPaciente_PhonePair_NameMismatch proves a phone that
// resolves to a real cadastro, but with a different name on file, is
// rejected — the phone alone is not sufficient.
func TestIdentificarPaciente_PhonePair_NameMismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":55,"nome":"Outra Pessoa","nascimento":"1970-01-01"}],"total":1}`)
	})
	client := newTestClient(t, mux)

	_, err := IdentificarPaciente(ctxWithToken("tok"), client, IdentidadeArgs{
		Telefone: "21999998888", NomeCompleto: "Joana da Silva",
	})
	if !errors.Is(err, ErrNaoLocalizado) {
		t.Fatalf("name mismatch: err = %v, want ErrNaoLocalizado", err)
	}
}

// TestIdentificarPaciente_MoreThanOneMatch_IsAmbiguousNeverPicksFirst proves
// that when patient.list returns more than one candidate, the result is
// ambiguous — never resolved by picking the first entry, even when that
// first entry would otherwise match.
func TestIdentificarPaciente_MoreThanOneMatch_IsAmbiguousNeverPicksFirst(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[
			{"patient_id":1,"nome":"A","nascimento":"2000-01-01"},
			{"patient_id":2,"nome":"B","nascimento":"2000-01-01"}
		],"total":2}`)
	})
	client := newTestClient(t, mux)

	_, err := IdentificarPaciente(ctxWithToken("tok"), client, IdentidadeArgs{
		CPF: "11111111111", DataNascimento: "2000-01-01",
	})
	if !errors.Is(err, ErrNaoLocalizado) {
		t.Fatalf("ambiguous multi-match: err = %v, want ErrNaoLocalizado", err)
	}
}

// TestIdentificarPaciente_NeverCallsPatientSearch proves identificar_paciente
// resolves identity exclusively through patient.list: /patient/search is
// never reached by either fact-pair, even on a successful resolution.
func TestIdentificarPaciente_NeverCallsPatientSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/search", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("identificar_paciente must never call /patient/search — see the patient.search Notes in internal/feegow/registry.go")
	})
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":9,"nome":"Joana Da Silva","nascimento":"2000-01-01"}],"total":1}`)
	})
	client := newTestClient(t, mux)

	if _, err := IdentificarPaciente(ctxWithToken("tok"), client, IdentidadeArgs{
		CPF: "11111111111", DataNascimento: "2000-01-01",
	}); err != nil {
		t.Fatalf("IdentificarPaciente (cpf pair): %v", err)
	}
	if _, err := IdentificarPaciente(ctxWithToken("tok"), client, IdentidadeArgs{
		Telefone: "21999998888", NomeCompleto: "Joana Da Silva",
	}); err != nil {
		t.Fatalf("IdentificarPaciente (phone pair): %v", err)
	}
}

// TestIdentificarPaciente_NeverLeaksPII is acceptance criterion 3: even
// when Feegow's response is stuffed with PII, the result this function
// returns — serialized exactly as an MCP tool result would be — contains
// none of it.
func TestIdentificarPaciente_NeverLeaksPII(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{
			"success": true,
			"content": [
				{
					"patient_id": 42,
					"nome": "Fulano de Tal da Silva",
					"nome_social": "Fulano Social",
					"nascimento": "1990-05-10",
					"bairro": "Centro",
					"email": "fulano@example.com",
					"celular": "(21) 95555-0000",
					"criado_em": "2023-03-10 14:04:00",
					"alterado_em": "2023-03-10 18:30:05",
					"programa_de_saude": [{"programa_id": 10, "nome_programa": "Programa Sigiloso"}]
				}
			],
			"total": 1
		}`)
	})
	client := newTestClient(t, mux)

	result, err := IdentificarPaciente(ctxWithToken("tok"), client, IdentidadeArgs{
		CPF: "11122233344", DataNascimento: "1990-05-10",
	})
	if err != nil {
		t.Fatalf("IdentificarPaciente: %v", err)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshaling result: %v", err)
	}
	s := string(raw)

	forbidden := []string{
		"Fulano", "Silva", "Tal", "Social",
		"Centro",
		"fulano@example.com",
		"95555-0000",
		"11122233344",
		"1990-05-10",
		"Programa Sigiloso",
	}
	for _, needle := range forbidden {
		if strings.Contains(s, needle) {
			t.Fatalf("IdentificarPaciente result leaked PII %q: %s", needle, s)
		}
	}

	if result.PacienteID != 42 || !result.JaExistia {
		t.Fatalf("result = %+v, want {PacienteID:42 JaExistia:true}", result)
	}
}

// TestIdentificarPaciente_NotFoundAndMismatch_ProduceIdenticalError is
// acceptance criterion 4: a cadastro that does not exist at all (Feegow
// 409) and a cadastro that exists but whose second fact does not match
// must be byte-for-byte indistinguishable from the caller's side —
// otherwise the difference itself becomes an oracle for enumerating valid
// CPFs against the clinic's real patient base.
func TestIdentificarPaciente_NotFoundAndMismatch_ProduceIdenticalError(t *testing.T) {
	notFoundMux := http.NewServeMux()
	notFoundMux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusConflict, `{"success":false,"content":"Paciente não existe"}`)
	})
	notFoundClient := newTestClient(t, notFoundMux)
	_, err1 := IdentificarPaciente(ctxWithToken("tok"), notFoundClient, IdentidadeArgs{
		CPF: "99999999999", DataNascimento: "2000-01-01",
	})

	mismatchMux := http.NewServeMux()
	mismatchMux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":7,"nome":"Alguém","nascimento":"1980-01-01"}],"total":1}`)
	})
	mismatchClient := newTestClient(t, mismatchMux)
	_, err2 := IdentificarPaciente(ctxWithToken("tok"), mismatchClient, IdentidadeArgs{
		CPF: "12312312312", DataNascimento: "2000-01-01",
	})

	if err1 == nil || err2 == nil {
		t.Fatalf("want both scenarios to error; got err1=%v err2=%v", err1, err2)
	}
	if err1.Error() != err2.Error() {
		t.Fatalf("not-found and mismatch produced different error text:\n  not-found: %q\n  mismatch:  %q", err1.Error(), err2.Error())
	}
	if !errors.Is(err1, ErrNaoLocalizado) {
		t.Fatalf("not-found error = %v, want ErrNaoLocalizado", err1)
	}
	if !errors.Is(err2, ErrNaoLocalizado) {
		t.Fatalf("mismatch error = %v, want ErrNaoLocalizado", err2)
	}
}

// TestIdentificarPaciente_UnexpectedFeegowFailure_IsNotDisguisedAsNotFound
// proves mapNotFound only folds the two "no such cadastro" shapes (409,
// 422) into ErrNaoLocalizado — a genuine infra/credential failure (5xx) is
// never hidden behind the patient-facing "não localizado" message, which
// would make a real outage invisible to whoever operates this service.
func TestIdentificarPaciente_UnexpectedFeegowFailure_IsNotDisguisedAsNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, `{"error":"boom"}`)
	})
	client := newTestClient(t, mux)

	_, err := IdentificarPaciente(ctxWithToken("tok"), client, IdentidadeArgs{
		CPF: "11111111111", DataNascimento: "2000-01-01",
	})
	if err == nil {
		t.Fatal("want an error for a 5xx Feegow response")
	}
	if errors.Is(err, ErrNaoLocalizado) {
		t.Fatalf("5xx Feegow failure was disguised as ErrNaoLocalizado: %v", err)
	}
}

// TestIdentificarPaciente_ShapeUnexpected_LogsWarnButKeepsUniformError proves
// the second half of the shape-visibility fix: a patient.list response
// whose shape this package can't trust (here, "content" is an object
// instead of an array) produces the exact same caller-facing error as a
// legitimate mismatch — but, unlike the legitimate mismatch, is logged at
// WARN so an operator can tell the integration broke instead of reading it
// as ordinary "não localizado" traffic. The log line never carries the
// CPF/data de nascimento involved.
func TestIdentificarPaciente_ShapeUnexpected_LogsWarnButKeepsUniformError(t *testing.T) {
	shapeBuf := captureLog(t)
	shapeMux := http.NewServeMux()
	shapeMux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"not":"an array"}}`)
	})
	shapeClient := newTestClient(t, shapeMux)
	_, errShape := IdentificarPaciente(ctxWithToken("tok"), shapeClient, IdentidadeArgs{
		CPF: "11111111111", DataNascimento: "2000-01-01",
	})
	shapeLog := shapeBuf.String()

	mismatchBuf := captureLog(t)
	mismatchMux := http.NewServeMux()
	mismatchMux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":7,"nome":"Alguém","nascimento":"1980-01-01"}],"total":1}`)
	})
	mismatchClient := newTestClient(t, mismatchMux)
	_, errMismatch := IdentificarPaciente(ctxWithToken("tok"), mismatchClient, IdentidadeArgs{
		CPF: "11111111111", DataNascimento: "2000-01-01",
	})
	mismatchLog := mismatchBuf.String()

	if errShape == nil || errMismatch == nil {
		t.Fatalf("want both scenarios to error; got shape=%v mismatch=%v", errShape, errMismatch)
	}
	if errShape.Error() != errMismatch.Error() {
		t.Fatalf("shape-unexpected and legitimate mismatch produced different caller-facing errors:\n  shape:    %q\n  mismatch: %q", errShape.Error(), errMismatch.Error())
	}
	if !errors.Is(errShape, ErrNaoLocalizado) || !errors.Is(errMismatch, ErrNaoLocalizado) {
		t.Fatalf("want both ErrNaoLocalizado; got shape=%v mismatch=%v", errShape, errMismatch)
	}

	if !strings.Contains(shapeLog, "WARN") || !strings.Contains(shapeLog, "patient.list") {
		t.Fatalf("shape-unexpected failure was not logged at WARN: %s", shapeLog)
	}
	if strings.Contains(mismatchLog, "WARN") {
		t.Fatalf("a legitimate non-match must not log a WARN, only the shape-unexpected case should: %s", mismatchLog)
	}

	for _, needle := range []string{"11111111111", "2000-01-01"} {
		if strings.Contains(shapeLog, needle) {
			t.Fatalf("shape-unexpected WARN log leaked patient data %q: %s", needle, shapeLog)
		}
	}
}

// TestIdentificarPaciente_NoPatientPayloadInLogs is acceptance criterion 8:
// the CPF, data de nascimento and patient name involved in a successful
// identification never appear in the process log, even at the most verbose
// LOG_LEVEL.
func TestIdentificarPaciente_NoPatientPayloadInLogs(t *testing.T) {
	buf := captureLog(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":9,"nome":"Segredo Pessoal","nascimento":"2000-02-20"}],"total":1}`)
	})
	client := newTestClient(t, mux)

	if _, err := IdentificarPaciente(ctxWithToken("tok"), client, IdentidadeArgs{
		CPF: "22233344455", DataNascimento: "2000-02-20",
	}); err != nil {
		t.Fatalf("IdentificarPaciente: %v", err)
	}

	logged := buf.String()
	for _, needle := range []string{"Segredo Pessoal", "22233344455", "2000-02-20"} {
		if strings.Contains(logged, needle) {
			t.Fatalf("log output leaked patient data %q: %s", needle, logged)
		}
	}
}
