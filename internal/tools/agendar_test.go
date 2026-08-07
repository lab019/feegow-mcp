package tools

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

func validAgendarArgs() AgendarArgs {
	return AgendarArgs{
		IdentidadeArgs:      IdentidadeArgs{CPF: "11111111111", DataNascimento: "2000-01-01"},
		LocalID:             intPtr(0),
		ProfissionalID:      7,
		EspecialidadeID:     intPtr(98),
		Data:                time.Now().AddDate(0, 0, 1).Format(feegow.ISO8601),
		Horario:             "09:00:00",
		ValorCentavos:       15000,
		Plano:               2,
		ConfirmacaoPaciente: true,
	}
}

// TestAgendar_RequiresConfirmacaoPaciente_BeforeAnyFeegowCall is acceptance
// criterion 5 (ESPECIFICACAO.md §7): an unconfirmed agendar call is
// rejected before any Feegow request — including before identidade
// resolution — is attempted.
func TestAgendar_RequiresConfirmacaoPaciente_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for unconfirmed agendar: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	args := validAgendarArgs()
	args.ConfirmacaoPaciente = false
	_, err := Agendar(ctxWithToken("tok"), client, args)
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("Agendar error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestAgendar_RequiresLocalID proves local_id nil is rejected (guard 3: 0
// is a legitimate explicit value, distinct from "not provided").
func TestAgendar_RequiresLocalID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	args := validAgendarArgs()
	args.LocalID = nil
	_, err := Agendar(ctxWithToken("tok"), client, args)
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("Agendar error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestAgendar_RequiresEspecialidadeOrProcedimento proves at least one of
// the two must be present.
func TestAgendar_RequiresEspecialidadeOrProcedimento(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	args := validAgendarArgs()
	args.EspecialidadeID = nil
	args.ProcedimentoID = nil
	_, err := Agendar(ctxWithToken("tok"), client, args)
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("Agendar error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestAgendar_RejectsRetroactiveData is guard 1 (ESPECIFICACAO.md §7 regra
// 1): an unambiguously past date never reaches Feegow. Two days back, not
// one: validateNotPast (guardas.go) deliberately allows one full day of
// slack around the UTC "hoje" boundary — see its doc comment and
// guardas_test.go's TestValidateNotPast_AcceptsYesterdayUTC for why a
// one-day-back date must NOT be rejected here.
func TestAgendar_RejectsRetroactiveData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a retroactive agendar: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	args := validAgendarArgs()
	args.Data = time.Now().AddDate(0, 0, -2).Format(feegow.ISO8601)
	_, err := Agendar(ctxWithToken("tok"), client, args)
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("Agendar error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestAgendar_PlanoConvenioRequiresZeroValor is guard 2 (ESPECIFICACAO.md
// §7 regra 2): plano=1 with a nonzero valor_centavos is rejected
// client-side — the server does not enforce this (registry.go's
// appoints.new_appoint Notes), so this is the only guard against a
// convênio agendamento silently getting the wrong price.
func TestAgendar_PlanoConvenioRequiresZeroValor(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for plano=1 with nonzero valor: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	args := validAgendarArgs()
	args.Plano = 1
	args.ValorCentavos = 9999
	_, err := Agendar(ctxWithToken("tok"), client, args)
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("Agendar error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestAgendar_Success proves the happy path: identity resolves internally
// (never a caller-supplied paciente_id), local_id=0 is sent as an explicit
// 0 (not omitted), data is translated to DD-MM-YYYY, and horario/valor/
// plano pass through as sent.
func TestAgendar_Success(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":321,"nome":"X","nascimento":"2000-01-01"}],"total":1}`)
	})
	mux.HandleFunc("/appoints/new-appoint", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"agendamento_id":9,"eventos":0},"total":2}`)
	})
	client := newTestClient(t, mux)

	args := validAgendarArgs()
	result, err := Agendar(ctxWithToken("tok"), client, args)
	if err != nil {
		t.Fatalf("Agendar: %v", err)
	}
	if !result.Agendado {
		t.Fatalf("result.Agendado = false, want true")
	}
	if result.AgendamentoID != 9 {
		t.Fatalf("result.AgendamentoID = %d, want 9 (the real /appoints/new-appoint success shape)", result.AgendamentoID)
	}

	if got := gotBody["paciente_id"]; got != float64(321) {
		t.Fatalf("paciente_id sent = %v, want the resolved id 321 (never caller-supplied)", got)
	}
	if got, ok := gotBody["local_id"]; !ok || got != float64(0) {
		t.Fatalf("local_id sent = %v (present=%v), want explicit 0", got, ok)
	}
	wantData := time.Now().AddDate(0, 0, 1).Format(feegow.DateBR)
	if got := gotBody["data"]; got != wantData {
		t.Fatalf("data sent = %v, want DD-MM-YYYY %q", got, wantData)
	}
	if got := gotBody["horario"]; got != "09:00:00" {
		t.Fatalf("horario sent = %v, want %q", got, "09:00:00")
	}
	if got := gotBody["valor"]; got != float64(15000) {
		t.Fatalf("valor sent = %v, want 15000", got)
	}
}

// TestAgendar_HorarioOcupado409_MapsToActionableError proves the
// Fase-0-verified race conflict maps to ErrHorarioOcupado, not the opaque
// ErrConflitoFeegow — see classifyAppointConflict (errors.go).
func TestAgendar_HorarioOcupado409_MapsToActionableError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":321,"nome":"X","nascimento":"2000-01-01"}],"total":1}`)
	})
	mux.HandleFunc("/appoints/new-appoint", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusConflict, `{"success":false,"content":"Já existe um agendamento para esse horario e profissional"}`)
	})
	client := newTestClient(t, mux)

	_, err := Agendar(ctxWithToken("tok"), client, validAgendarArgs())
	if !errors.Is(err, ErrHorarioOcupado) {
		t.Fatalf("Agendar error = %v, want ErrHorarioOcupado", err)
	}
	if errors.Is(err, ErrConflitoFeegow) {
		t.Fatalf("Agendar error unexpectedly also matches the generic ErrConflitoFeegow: %v", err)
	}
}

// TestAgendar_PacienteJaTemAgendamento409_MapsToActionableError proves the
// Fase-0-verified, undocumented-elsewhere "same patient already has an
// agendamento with this profissional" 409 maps to its own distinct
// sentinel, never conflated with ErrHorarioOcupado (same HTTP status, same
// envelope, different meaning and different advice).
func TestAgendar_PacienteJaTemAgendamento409_MapsToActionableError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":321,"nome":"X","nascimento":"2000-01-01"}],"total":1}`)
	})
	mux.HandleFunc("/appoints/new-appoint", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusConflict, `{"success":false,"content":"Esse paciente já possui um agendamento nessa agenda."}`)
	})
	client := newTestClient(t, mux)

	_, err := Agendar(ctxWithToken("tok"), client, validAgendarArgs())
	if !errors.Is(err, ErrPacienteJaTemAgendamento) {
		t.Fatalf("Agendar error = %v, want ErrPacienteJaTemAgendamento", err)
	}
	if errors.Is(err, ErrHorarioOcupado) {
		t.Fatalf("Agendar error unexpectedly also matches ErrHorarioOcupado: %v", err)
	}
}

// TestAgendar_OtherConflict409_FallsBackToGenericSanitize proves
// classifyAppointConflict does not over-match: a 409 whose Content is
// neither verified string still gets the generic, PII-safe
// ErrConflitoFeegow treatment.
func TestAgendar_OtherConflict409_FallsBackToGenericSanitize(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":321,"nome":"X","nascimento":"2000-01-01"}],"total":1}`)
	})
	mux.HandleFunc("/appoints/new-appoint", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusConflict, `{"success":false,"content":"Paciente Fulano de Tal possui pendência financeira"}`)
	})
	client := newTestClient(t, mux)

	_, err := Agendar(ctxWithToken("tok"), client, validAgendarArgs())
	if !errors.Is(err, ErrConflitoFeegow) {
		t.Fatalf("Agendar error = %v, want the generic ErrConflitoFeegow", err)
	}
	if strings.Contains(err.Error(), "Fulano") {
		t.Fatalf("Agendar error leaked the Feegow body: %v", err)
	}
}

// TestAgendar_UnexpectedContentShape_StillSucceeds proves
// decodeAgendamentoID's defensive contract: /appoints/new-appoint already
// returned success by the time this runs, so a content shape this
// integration doesn't recognize must NOT be reported as a failed
// agendamento — that would tell the patient the booking didn't go through
// when it did, which is worse than a booking confirmation without a
// reference number. It must still log a WARN so an operator can tell the
// upstream shape changed.
func TestAgendar_UnexpectedContentShape_StillSucceeds(t *testing.T) {
	buf := captureLog(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":321,"nome":"X","nascimento":"2000-01-01"}],"total":1}`)
	})
	mux.HandleFunc("/appoints/new-appoint", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Agendamento realizado"}`)
	})
	client := newTestClient(t, mux)

	result, err := Agendar(ctxWithToken("tok"), client, validAgendarArgs())
	if err != nil {
		t.Fatalf("Agendar: %v, want success despite the unrecognized content shape", err)
	}
	if !result.Agendado {
		t.Fatalf("result.Agendado = false, want true — the agendamento was created upstream regardless of content shape")
	}
	if result.AgendamentoID != 0 {
		t.Fatalf("result.AgendamentoID = %d, want 0 (unreadable from this shape)", result.AgendamentoID)
	}

	if got := buf.String(); !strings.Contains(got, "WARN") || !strings.Contains(got, "appoints.new-appoint") {
		t.Fatalf("unrecognized content shape was not logged at WARN: %s", got)
	}
}

// TestAgendar_ObjectShapeMissingID_StillSucceeds is the same defensive
// contract for the case where content IS an object but agendamento_id is
// absent or non-numeric — not just "content isn't an object at all".
func TestAgendar_ObjectShapeMissingID_StillSucceeds(t *testing.T) {
	buf := captureLog(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"patient_id":321,"nome":"X","nascimento":"2000-01-01"}],"total":1}`)
	})
	mux.HandleFunc("/appoints/new-appoint", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"eventos":0}}`)
	})
	client := newTestClient(t, mux)

	result, err := Agendar(ctxWithToken("tok"), client, validAgendarArgs())
	if err != nil {
		t.Fatalf("Agendar: %v, want success despite the missing agendamento_id", err)
	}
	if !result.Agendado || result.AgendamentoID != 0 {
		t.Fatalf("result = %+v, want {Agendado:true AgendamentoID:0}", result)
	}
	if got := buf.String(); !strings.Contains(got, "WARN") {
		t.Fatalf("missing agendamento_id was not logged at WARN: %s", got)
	}
}
