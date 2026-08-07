package tools

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// appointsSearchByOwner stands up a fake /appoints/search that only ever
// returns agendamentoID for paciente_id == ownerPatientID — the same
// scoping resolveOwnedAgendamento relies on. Used by every "posse" test
// below to prove an identity that resolves to a DIFFERENT paciente_id never
// sees agendamentoID as theirs.
func appointsSearchByOwner(t *testing.T, mux *http.ServeMux, ownerPatientID, agendamentoID int) {
	t.Helper()
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("paciente_id") != strconv.Itoa(ownerPatientID) {
			writeJSON(t, w, http.StatusOK, `{"success":true,"content":[]}`)
			return
		}
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[
			{"agendamento_id": `+strconv.Itoa(agendamentoID)+`, "data":"07-08-2026","horario":"09:00:00",
			 "profissional_id":1,"especialidade_id":1,"procedimento_id":1,"unidade_id":1,"status_id":1}
		]}`)
	})
}

// patientList registers /patient/list returning exactly one candidate
// (patientID, matching the CPF+DOB pair every test in this file uses).
func patientList(t *testing.T, mux *http.ServeMux, patientID int) {
	t.Helper()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		cpf := r.URL.Query().Get("cpf")
		body := `{"success":true,"content":[{"patient_id":` + strconv.Itoa(patientID) + `,"nome":"X","nascimento":"2000-01-01"}],"total":1}`
		if cpf == "" {
			body = `{"success":true,"content":[]}`
		}
		writeJSON(t, w, http.StatusOK, body)
	})
}

func identidadeFor(cpf string) IdentidadeArgs {
	return IdentidadeArgs{CPF: cpf, DataNascimento: "2000-01-01"}
}

// --- cancelar -------------------------------------------------------------

func TestCancelar_RequiresConfirmacaoPaciente_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for unconfirmed cancelar: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := Cancelar(ctxWithToken("tok"), client, CancelarArgs{
		IdentidadeArgs: identidadeFor("11111111111"), AgendamentoID: 10, ConfirmacaoPaciente: false,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("Cancelar error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestCancelar_RejectsNonPositiveAgendamentoID_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a non-positive agendamento_id: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	for _, id := range []int{0, -5} {
		_, err := Cancelar(ctxWithToken("tok"), client, CancelarArgs{
			IdentidadeArgs: identidadeFor("11111111111"), AgendamentoID: id, ConfirmacaoPaciente: true,
		})
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("Cancelar(agendamento_id=%d) error = %v (%T), want *ArgumentError", id, err, err)
		}
	}
}

// TestCancelar_PosseCheck_BothSides is THE load-bearing test for this
// phase's central design decision: an agendamento_id that does NOT belong
// to the identified patient must fail — with the exact same ErrNaoLocalizado
// identificar_paciente already uses, never a distinct "not yours" error —
// and must never reach /appoints/cancel-appoint; the SAME id, requested by
// the patient it actually belongs to, must succeed and reach it. Guessing a
// number must never cancel a stranger's consulta, and knowing the two
// identification facts must still let the real owner act.
func TestCancelar_PosseCheck_BothSides(t *testing.T) {
	const ownerPatientID = 777
	const agendamentoID = 55

	t.Run("id does not belong to the identified patient", func(t *testing.T) {
		var cancelHit bool
		mux := http.NewServeMux()
		patientList(t, mux, 999) // a DIFFERENT patient resolves from this identity
		appointsSearchByOwner(t, mux, ownerPatientID, agendamentoID)
		mux.HandleFunc("/appoints/cancel-appoint", func(w http.ResponseWriter, r *http.Request) {
			cancelHit = true
			writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Agendamento cancelado"}`)
		})
		client := newTestClient(t, mux)

		_, err := Cancelar(ctxWithToken("tok"), client, CancelarArgs{
			IdentidadeArgs: identidadeFor("22222222222"), AgendamentoID: agendamentoID, ConfirmacaoPaciente: true,
		})
		if !errors.Is(err, ErrNaoLocalizado) {
			t.Fatalf("Cancelar (id not owned) error = %v, want ErrNaoLocalizado", err)
		}
		if cancelHit {
			t.Fatal("Cancelar called /appoints/cancel-appoint for an agendamento_id that does not belong to the identified patient")
		}
	})

	t.Run("id belongs to the identified patient", func(t *testing.T) {
		var gotBody map[string]any
		mux := http.NewServeMux()
		patientList(t, mux, ownerPatientID)
		appointsSearchByOwner(t, mux, ownerPatientID, agendamentoID)
		mux.HandleFunc("/appoints/cancel-appoint", func(w http.ResponseWriter, r *http.Request) {
			decodeJSONBody(t, r, &gotBody)
			writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Agendamento cancelado"}`)
		})
		client := newTestClient(t, mux)

		result, err := Cancelar(ctxWithToken("tok"), client, CancelarArgs{
			IdentidadeArgs: identidadeFor("11111111111"), AgendamentoID: agendamentoID, ConfirmacaoPaciente: true,
		})
		if err != nil {
			t.Fatalf("Cancelar (id owned): %v", err)
		}
		if !result.Cancelado {
			t.Fatal("result.Cancelado = false, want true")
		}
		if got := gotBody["agendamento_id"]; got != float64(agendamentoID) {
			t.Fatalf("agendamento_id sent = %v, want %d", got, agendamentoID)
		}
		if got := gotBody["motivo_id"]; got != float64(1) {
			t.Fatalf("motivo_id sent = %v, want the fixed 1 (Solicitado pelo Paciente) — never caller-chosen", got)
		}
	})
}

// TestCancelar_UnidentifiedPatient_NeverCallsAppointsSearchOrCancel proves
// identity resolution failing (before any posse check) short-circuits
// everything downstream.
func TestCancelar_UnidentifiedPatient_NeverCallsAppointsSearchOrCancel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[],"total":0}`)
	})
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("appoints/search must never be called for an unidentified patient")
	})
	mux.HandleFunc("/appoints/cancel-appoint", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("cancel-appoint must never be called for an unidentified patient")
	})
	client := newTestClient(t, mux)

	_, err := Cancelar(ctxWithToken("tok"), client, CancelarArgs{
		IdentidadeArgs: identidadeFor("11111111111"), AgendamentoID: 10, ConfirmacaoPaciente: true,
	})
	if !errors.Is(err, ErrNaoLocalizado) {
		t.Fatalf("Cancelar error = %v, want ErrNaoLocalizado", err)
	}
}

// --- remarcar ---------------------------------------------------------

func validRemarcarArgs(cpf string, agendamentoID int) RemarcarArgs {
	return RemarcarArgs{
		IdentidadeArgs:      identidadeFor(cpf),
		AgendamentoID:       agendamentoID,
		NovaData:            time.Now().AddDate(0, 0, 3).Format(feegow.ISO8601),
		NovoHorario:         "10:00:00",
		ConfirmacaoPaciente: true,
	}
}

func TestRemarcar_RequiresConfirmacaoPaciente_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for unconfirmed remarcar: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	args := validRemarcarArgs("11111111111", 10)
	args.ConfirmacaoPaciente = false
	_, err := Remarcar(ctxWithToken("tok"), client, args)
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("Remarcar error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestRemarcar_RejectsRetroactiveNovaData_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a retroactive remarcar: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	args := validRemarcarArgs("11111111111", 10)
	args.NovaData = time.Now().AddDate(0, 0, -2).Format(feegow.ISO8601)
	_, err := Remarcar(ctxWithToken("tok"), client, args)
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("Remarcar error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestRemarcar_PosseCheck_BothSides mirrors TestCancelar_PosseCheck_BothSides
// for /appoints/reschedule.
func TestRemarcar_PosseCheck_BothSides(t *testing.T) {
	const ownerPatientID = 42
	const agendamentoID = 99

	t.Run("id does not belong to the identified patient", func(t *testing.T) {
		var rescheduleHit bool
		mux := http.NewServeMux()
		patientList(t, mux, 1)
		appointsSearchByOwner(t, mux, ownerPatientID, agendamentoID)
		mux.HandleFunc("/appoints/reschedule", func(w http.ResponseWriter, r *http.Request) {
			rescheduleHit = true
			writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Agendamento remarcado"}`)
		})
		client := newTestClient(t, mux)

		_, err := Remarcar(ctxWithToken("tok"), client, validRemarcarArgs("22222222222", agendamentoID))
		if !errors.Is(err, ErrNaoLocalizado) {
			t.Fatalf("Remarcar (id not owned) error = %v, want ErrNaoLocalizado", err)
		}
		if rescheduleHit {
			t.Fatal("Remarcar called /appoints/reschedule for an agendamento_id that does not belong to the identified patient")
		}
	})

	t.Run("id belongs to the identified patient", func(t *testing.T) {
		var gotBody map[string]any
		mux := http.NewServeMux()
		patientList(t, mux, ownerPatientID)
		appointsSearchByOwner(t, mux, ownerPatientID, agendamentoID)
		mux.HandleFunc("/appoints/reschedule", func(w http.ResponseWriter, r *http.Request) {
			decodeJSONBody(t, r, &gotBody)
			writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Agendamento remarcado"}`)
		})
		client := newTestClient(t, mux)

		args := validRemarcarArgs("11111111111", agendamentoID)
		result, err := Remarcar(ctxWithToken("tok"), client, args)
		if err != nil {
			t.Fatalf("Remarcar (id owned): %v", err)
		}
		if !result.Remarcado {
			t.Fatal("result.Remarcado = false, want true")
		}
		if got := gotBody["agendamento_id"]; got != float64(agendamentoID) {
			t.Fatalf("agendamento_id sent = %v, want %d", got, agendamentoID)
		}
		if got := gotBody["motivo_id"]; got != float64(1) {
			t.Fatalf("motivo_id sent = %v, want the fixed 1", got)
		}
		if got := gotBody["horario"]; got != "10:00:00" {
			t.Fatalf("horario sent = %v, want %q", got, "10:00:00")
		}
		wantData := time.Now().AddDate(0, 0, 3).Format(feegow.DateBR)
		if got := gotBody["data"]; got != wantData {
			t.Fatalf("data sent = %v, want DD-MM-YYYY %q", got, wantData)
		}
	})
}

// TestRemarcar_HorarioOcupado409_MapsToActionableError proves remarcar
// shares agendar's conflict classification for the reschedule call.
func TestRemarcar_HorarioOcupado409_MapsToActionableError(t *testing.T) {
	mux := http.NewServeMux()
	patientList(t, mux, 42)
	appointsSearchByOwner(t, mux, 42, 99)
	mux.HandleFunc("/appoints/reschedule", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusConflict, `{"success":false,"content":"Já existe um agendamento para esse horario e profissional"}`)
	})
	client := newTestClient(t, mux)

	_, err := Remarcar(ctxWithToken("tok"), client, validRemarcarArgs("11111111111", 99))
	if !errors.Is(err, ErrHorarioOcupado) {
		t.Fatalf("Remarcar error = %v, want ErrHorarioOcupado", err)
	}
}

// --- confirmar ------------------------------------------------------------

func TestConfirmar_RequiresConfirmacaoPaciente_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for unconfirmed confirmar: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := Confirmar(ctxWithToken("tok"), client, ConfirmarArgs{
		IdentidadeArgs: identidadeFor("11111111111"), AgendamentoID: 10, ConfirmacaoPaciente: false,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("Confirmar error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestConfirmar_PosseCheck_BothSides mirrors the cancelar/remarcar posse
// tests for /appoints/confirm.
func TestConfirmar_PosseCheck_BothSides(t *testing.T) {
	const ownerPatientID = 5
	const agendamentoID = 8

	t.Run("id does not belong to the identified patient", func(t *testing.T) {
		var confirmHit bool
		mux := http.NewServeMux()
		patientList(t, mux, 6)
		appointsSearchByOwner(t, mux, ownerPatientID, agendamentoID)
		mux.HandleFunc("/appoints/confirm", func(w http.ResponseWriter, r *http.Request) {
			confirmHit = true
			writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Agendamento confirmado com sucesso"}`)
		})
		client := newTestClient(t, mux)

		_, err := Confirmar(ctxWithToken("tok"), client, ConfirmarArgs{
			IdentidadeArgs: identidadeFor("22222222222"), AgendamentoID: agendamentoID, ConfirmacaoPaciente: true,
		})
		if !errors.Is(err, ErrNaoLocalizado) {
			t.Fatalf("Confirmar (id not owned) error = %v, want ErrNaoLocalizado", err)
		}
		if confirmHit {
			t.Fatal("Confirmar called /appoints/confirm for an agendamento_id that does not belong to the identified patient")
		}
	})

	t.Run("id belongs to the identified patient", func(t *testing.T) {
		var gotBody map[string]any
		mux := http.NewServeMux()
		patientList(t, mux, ownerPatientID)
		appointsSearchByOwner(t, mux, ownerPatientID, agendamentoID)
		mux.HandleFunc("/appoints/confirm", func(w http.ResponseWriter, r *http.Request) {
			decodeJSONBody(t, r, &gotBody)
			writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Agendamento confirmado com sucesso"}`)
		})
		client := newTestClient(t, mux)

		result, err := Confirmar(ctxWithToken("tok"), client, ConfirmarArgs{
			IdentidadeArgs: identidadeFor("11111111111"), AgendamentoID: agendamentoID, ConfirmacaoPaciente: true,
		})
		if err != nil {
			t.Fatalf("Confirmar (id owned): %v", err)
		}
		if !result.Confirmado {
			t.Fatal("result.Confirmado = false, want true")
		}
		if got := gotBody["agendamento_id"]; got != float64(agendamentoID) {
			t.Fatalf("agendamento_id sent = %v, want %d", got, agendamentoID)
		}
	})
}

// TestPosseCheck_NeverLogsPIIAtAnyOutcome is acceptance criterion 6
// (auditoria sem PII): both the "not owned" and "owned" outcomes of a posse
// check must never place the CPF/data de nascimento used to identify the
// caller into the log, at any log level.
func TestPosseCheck_NeverLogsPIIAtAnyOutcome(t *testing.T) {
	buf := captureLog(t)

	mux := http.NewServeMux()
	patientList(t, mux, 777)
	appointsSearchByOwner(t, mux, 777, 55)
	mux.HandleFunc("/appoints/cancel-appoint", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Agendamento cancelado"}`)
	})
	client := newTestClient(t, mux)

	if _, err := Cancelar(ctxWithToken("tok"), client, CancelarArgs{
		IdentidadeArgs: identidadeFor("11111111111"), AgendamentoID: 55, ConfirmacaoPaciente: true,
	}); err != nil {
		t.Fatalf("Cancelar: %v", err)
	}

	logged := buf.String()
	for _, needle := range []string{"11111111111", "2000-01-01"} {
		if strings.Contains(logged, needle) {
			t.Fatalf("log output leaked patient data %q: %s", needle, logged)
		}
	}
	if !strings.Contains(logged, "ALEGADA") {
		t.Fatalf("audit log did not mark the identity as claimed/unverified: %s", logged)
	}
}
