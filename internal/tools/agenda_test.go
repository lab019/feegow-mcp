package tools

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestConsultarAgenda_SingleFact_RejectsBeforeAnyFeegowCall proves
// consultar_agenda inherits identificar_paciente's argument validation —
// an incomplete fact pair never reaches Feegow, for either endpoint this
// tool calls.
func TestConsultarAgenda_SingleFact_RejectsBeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for invalid args: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarAgenda(ctxWithToken("tok"), client, IdentidadeArgs{CPF: "11111111111"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarAgenda error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestConsultarAgenda_UnidentifiedPatient_NeverCallsAppointsSearch proves
// the identity resolution is structural, not best-effort: when
// identificação fails, /appoints/search is never even attempted, and the
// uniform ErrNaoLocalizado propagates unchanged.
//
// "not found" is simulated the way the real Feegow API actually reports it
// (verified against the sandbox): HTTP 200 with an empty content array, not
// an HTTP error status — see the same note on
// TestIdentificarPaciente_NotFoundAndMismatch_ProduceIdenticalError.
func TestConsultarAgenda_UnidentifiedPatient_NeverCallsAppointsSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("appoints/search must never be called for an unidentified patient")
	})
	client := newTestClient(t, mux)

	_, err := ConsultarAgenda(ctxWithToken("tok"), client, IdentidadeArgs{
		CPF: "11111111111", DataNascimento: "2000-01-01",
	})
	if !errors.Is(err, ErrNaoLocalizado) {
		t.Fatalf("err = %v, want ErrNaoLocalizado", err)
	}
}

// TestConsultarAgenda_Success proves the happy path: identity resolves
// internally, appoints/search is called with the resolved paciente_id (not
// anything the caller supplied), dates are normalized to ISO-8601, and no
// clinical note field leaks into the result.
func TestConsultarAgenda_Success(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":777,"nome":"X","nascimento":"2000-01-01"}],"total":1}`)
	})
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[
			{
				"agendamento_id": 30,
				"data": "07-08-2024",
				"horario": "09:00:00",
				"paciente_id": 777,
				"procedimento_id": 3,
				"status_id": 1,
				"profissional_id": 1,
				"especialidade_id": 98,
				"unidade_id": 1,
				"notas": "informação clínica sigilosa"
			}
		]}`)
	})
	client := newTestClient(t, mux)

	result, err := ConsultarAgenda(ctxWithToken("tok"), client, IdentidadeArgs{
		CPF: "11111111111", DataNascimento: "2000-01-01",
	})
	if err != nil {
		t.Fatalf("ConsultarAgenda: %v", err)
	}

	if got := gotQuery.Get("paciente_id"); got != "777" {
		t.Fatalf("appoints/search paciente_id = %q, want %q (the resolved id)", got, "777")
	}

	if len(result.Agendamentos) != 1 {
		t.Fatalf("Agendamentos = %+v, want exactly 1 entry", result.Agendamentos)
	}
	want := Agendamento{
		AgendamentoID:   30,
		Data:            "2024-08-07",
		Horario:         "09:00:00",
		ProfissionalID:  1,
		EspecialidadeID: 98,
		ProcedimentoID:  3,
		UnidadeID:       1,
		StatusID:        1,
	}
	if got := result.Agendamentos[0]; got != want {
		t.Fatalf("Agendamentos[0] = %+v, want %+v", got, want)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshaling result: %v", err)
	}
	if strings.Contains(string(raw), "sigilosa") {
		t.Fatalf("consultar_agenda result leaked a clinical note: %s", raw)
	}
}

// TestConsultarAgenda_FeegowErrorBody_NeverLeaksPII is the regression test
// for the adversarial review's most severe finding: a 409 or 422 from
// Feegow's /appoints/search can carry a free-text body stuffed with patient
// PII (the review reproduced "Paciente Fulano de Tal (CPF 111.111.111-11)
// possui pendência financeira"). None of that body — planted here as
// distinct name/CPF markers — may survive into the error this tool returns
// to its caller (and, from there, a public agent transcript).
func TestConsultarAgenda_FeegowErrorBody_NeverLeaksPII(t *testing.T) {
	const plantedName = "Fulano de Tal da Silva"
	const plantedCPF = "111.111.111-11"

	cases := []struct {
		name   string
		status int
		body   string
	}{
		{
			"409 conflict body", http.StatusConflict,
			`{"success":false,"content":"Paciente ` + plantedName + ` (CPF ` + plantedCPF + `) possui pendência financeira"}`,
		},
		{
			"422 validation body", http.StatusUnprocessableEntity,
			`{"paciente_id":["já existe agendamento para ` + plantedName + `, CPF ` + plantedCPF + `"]}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":777,"nome":"X","nascimento":"2000-01-01"}],"total":1}`)
			})
			mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, c.status, c.body)
			})
			client := newTestClient(t, mux)

			_, err := ConsultarAgenda(ctxWithToken("tok"), client, IdentidadeArgs{
				CPF: "11111111111", DataNascimento: "2000-01-01",
			})
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			msg := err.Error()
			if strings.Contains(msg, plantedName) || strings.Contains(msg, "Fulano") || strings.Contains(msg, plantedCPF) || strings.Contains(msg, "111.111.111-11") {
				t.Fatalf("ConsultarAgenda error leaked the Feegow error body: %q", msg)
			}
		})
	}
}
