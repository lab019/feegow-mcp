package tools

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func validCriarPacienteArgs() CriarPacienteArgs {
	return CriarPacienteArgs{
		NomeCompleto:        "Joana Da Silva",
		CPF:                 "11122233344",
		DataNascimento:      "1990-05-10",
		ConfirmacaoPaciente: true,
	}
}

func TestCriarPaciente_RequiresConfirmacaoPaciente_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for unconfirmed criar_paciente: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	args := validCriarPacienteArgs()
	args.ConfirmacaoPaciente = false
	_, err := CriarPaciente(ctxWithToken("tok"), client, args)
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("CriarPaciente error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestCriarPaciente_RequiresNomeCompleto_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	args := validCriarPacienteArgs()
	args.NomeCompleto = "   "
	_, err := CriarPaciente(ctxWithToken("tok"), client, args)
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("CriarPaciente error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestCriarPaciente_RequiresAtLeastOneOtherFact is the /patient/create
// contract this Fase was handed: nome_completo alone is not enough, ao
// menos um de cpf/celular/data_nascimento/email is also required.
func TestCriarPaciente_RequiresAtLeastOneOtherFact_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := CriarPaciente(ctxWithToken("tok"), client, CriarPacienteArgs{
		NomeCompleto: "Joana Da Silva", ConfirmacaoPaciente: true,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("CriarPaciente error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestCriarPaciente_AlreadyExists_CPFPair_ReturnsExisting_NeverCreates is
// the core "don't duplicate" contract: when the cpf+data_nascimento pair
// already resolves to a cadastro, criar_paciente returns it with
// ja_existia=true and /patient/create is never called.
func TestCriarPaciente_AlreadyExists_CPFPair_ReturnsExisting_NeverCreates(t *testing.T) {
	var createHit bool
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":42,"nome":"Joana Da Silva","nascimento":"1990-05-10"}],"total":1}`)
	})
	mux.HandleFunc("/patient/create", func(w http.ResponseWriter, r *http.Request) {
		createHit = true
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":999}`)
	})
	client := newTestClient(t, mux)

	result, err := CriarPaciente(ctxWithToken("tok"), client, validCriarPacienteArgs())
	if err != nil {
		t.Fatalf("CriarPaciente: %v", err)
	}
	if createHit {
		t.Fatal("CriarPaciente called /patient/create for a cadastro that already exists")
	}
	if result.PacienteID != 42 || !result.JaExistia {
		t.Fatalf("result = %+v, want {PacienteID:42 JaExistia:true}", result)
	}
}

// TestCriarPaciente_AlreadyExists_NeverLogsAsWrite proves the dedup path
// (tryIdentifyForCreate finding an existing cadastro, /patient/create never
// called) is never recorded as if a mutating write happened: auditWrite's
// doc comment defines "WRITE" as "a tool actually executed a mutating
// Feegow call", so this outcome must log a distinct "REUSE" line instead —
// otherwise anyone auditing how many cadastros criar_paciente created would
// overcount by every deduplicated lookup.
func TestCriarPaciente_AlreadyExists_NeverLogsAsWrite(t *testing.T) {
	buf := captureLog(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":42,"nome":"Joana Da Silva","nascimento":"1990-05-10"}],"total":1}`)
	})
	mux.HandleFunc("/patient/create", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("CriarPaciente called /patient/create for a cadastro that already exists")
	})
	client := newTestClient(t, mux)

	if _, err := CriarPaciente(ctxWithToken("tok"), client, validCriarPacienteArgs()); err != nil {
		t.Fatalf("CriarPaciente: %v", err)
	}

	logged := buf.String()
	if strings.Contains(logged, "WRITE") {
		t.Fatalf("dedup (reuse) path logged as a WRITE — auditWrite must never fire when /patient/create was not called: %s", logged)
	}
	if !strings.Contains(logged, "REUSE") {
		t.Fatalf("dedup (reuse) path did not log a REUSE line: %s", logged)
	}
}

// TestCriarPaciente_AlreadyExists_PhonePair_ReturnsExisting_NeverCreates is
// the same guarantee via the celular+nome_completo pair.
func TestCriarPaciente_AlreadyExists_PhonePair_ReturnsExisting_NeverCreates(t *testing.T) {
	var createHit bool
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":77,"nome":"Joana Da Silva","nascimento":"1990-05-10"}],"total":1}`)
	})
	mux.HandleFunc("/patient/create", func(w http.ResponseWriter, r *http.Request) {
		createHit = true
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":999}`)
	})
	client := newTestClient(t, mux)

	result, err := CriarPaciente(ctxWithToken("tok"), client, CriarPacienteArgs{
		NomeCompleto: "Joana Da Silva", Celular: "21999998888", ConfirmacaoPaciente: true,
	})
	if err != nil {
		t.Fatalf("CriarPaciente: %v", err)
	}
	if createHit {
		t.Fatal("CriarPaciente called /patient/create for a cadastro that already exists")
	}
	if result.PacienteID != 77 || !result.JaExistia {
		t.Fatalf("result = %+v, want {PacienteID:77 JaExistia:true}", result)
	}
}

// TestCriarPaciente_NotFound_CreatesNew_SendsDuplicatedFields proves that
// when no existing cadastro resolves, /patient/create is called with both
// nome_completo AND nome_paciente, and both data_nascimento AND nascimento
// — the verified contract's documented redundancy — and CPF sent
// digits-only.
func TestCriarPaciente_NotFound_CreatesNew_SendsDuplicatedFields(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	mux.HandleFunc("/patient/create", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":555}`)
	})
	client := newTestClient(t, mux)

	args := validCriarPacienteArgs()
	args.CPF = "123.456.789-01"
	result, err := CriarPaciente(ctxWithToken("tok"), client, args)
	if err != nil {
		t.Fatalf("CriarPaciente: %v", err)
	}
	if result.PacienteID != 555 || result.JaExistia {
		t.Fatalf("result = %+v, want {PacienteID:555 JaExistia:false}", result)
	}

	if got := gotBody["nome_completo"]; got != "Joana Da Silva" {
		t.Fatalf("nome_completo sent = %v", got)
	}
	if got := gotBody["nome_paciente"]; got != "Joana Da Silva" {
		t.Fatalf("nome_paciente sent = %v, want same value as nome_completo (verified contract requires both)", got)
	}
	if got := gotBody["cpf"]; got != "12345678901" {
		t.Fatalf("cpf sent = %v, want digits-only %q", got, "12345678901")
	}
	if got := gotBody["data_nascimento"]; got != "1990-05-10" {
		t.Fatalf("data_nascimento sent = %v", got)
	}
	if got := gotBody["nascimento"]; got != "1990-05-10" {
		t.Fatalf("nascimento sent = %v, want same value as data_nascimento (verified contract requires both)", got)
	}
}

// TestCriarPaciente_UnrecognizedCreateResponseShape_ReturnsErrorNotNaoLocalizado
// proves decodeCreatedPatientID's failure path is a distinct, honest error
// — never disguised as ErrNaoLocalizado, which would misreport a
// successful creation as "no such patient".
func TestCriarPaciente_UnrecognizedCreateResponseShape_ReturnsErrorNotNaoLocalizado(t *testing.T) {
	shapeBuf := captureLog(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	mux.HandleFunc("/patient/create", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"unexpected":"shape"}}`)
	})
	client := newTestClient(t, mux)

	_, err := CriarPaciente(ctxWithToken("tok"), client, validCriarPacienteArgs())
	if err == nil {
		t.Fatal("want a non-nil error for an unrecognized patient/create response shape")
	}
	if errors.Is(err, ErrNaoLocalizado) {
		t.Fatalf("unrecognized create response shape was disguised as ErrNaoLocalizado: %v", err)
	}
	if !strings.Contains(shapeBuf.String(), "WARN") {
		t.Fatalf("unrecognized shape was not logged at WARN: %s", shapeBuf.String())
	}
}

// TestCriarPaciente_CreateResponseNull_ReturnsErrorNotFalseSuccess is the
// regression test for decodeCreatedPatientID's `content: null` bug:
// json.Unmarshal("null", &v) is a documented no-op (err == nil, v left at
// its zero value) for every shape decodeCreatedPatientID tries, so unguarded
// this decoded null content as paciente_id 0 and returned it as a full,
// successful creation — exactly the opposite of what the function's own doc
// comment promises. It must instead take the same explicit-error path as
// any other unrecognized shape.
func TestCriarPaciente_CreateResponseNull_ReturnsErrorNotFalseSuccess(t *testing.T) {
	shapeBuf := captureLog(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	mux.HandleFunc("/patient/create", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":null}`)
	})
	client := newTestClient(t, mux)

	result, err := CriarPaciente(ctxWithToken("tok"), client, validCriarPacienteArgs())
	if err == nil {
		t.Fatalf("want a non-nil error for content:null, got success result %+v", result)
	}
	if errors.Is(err, ErrNaoLocalizado) {
		t.Fatalf("content:null was disguised as ErrNaoLocalizado: %v", err)
	}
	if !strings.Contains(shapeBuf.String(), "WARN") {
		t.Fatalf("content:null was not logged at WARN: %s", shapeBuf.String())
	}
}

// TestDecodeCreatedPatientID_RejectsNonPositiveIDs is the unit-level
// counterpart of the test above: every shape decodeCreatedPatientID
// recognizes (bare number, numeric string, object) must reject id <= 0 —
// the same régua validateAgendamentoID (guardas.go) applies to agendamento
// ids — not just the null case.
func TestDecodeCreatedPatientID_RejectsNonPositiveIDs(t *testing.T) {
	for _, content := range []string{"null", "0", "-1", `"0"`, `{"paciente_id":0}`, `{"patient_id":-5}`} {
		if _, err := decodeCreatedPatientID(json.RawMessage(content)); err == nil {
			t.Fatalf("decodeCreatedPatientID(%s) = nil error, want an error for a non-positive id", content)
		}
	}
}

// TestCriarPaciente_NeverReturnsPII mirrors
// TestIdentificarPaciente_NeverLeaksPII for the create path.
func TestCriarPaciente_NeverReturnsPII(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	mux.HandleFunc("/patient/create", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":321}`)
	})
	client := newTestClient(t, mux)

	result, err := CriarPaciente(ctxWithToken("tok"), client, validCriarPacienteArgs())
	if err != nil {
		t.Fatalf("CriarPaciente: %v", err)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshaling result: %v", err)
	}
	for _, needle := range []string{"Joana", "Silva", "11122233344", "1990-05-10"} {
		if strings.Contains(string(raw), needle) {
			t.Fatalf("CriarPaciente result leaked PII %q: %s", needle, raw)
		}
	}
}
