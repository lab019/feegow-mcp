package tools

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestBuscarHorariosLivres_InvalidTipo_RejectsBeforeFeegowCall and the two
// tests that follow it are the argument-validation half of
// buscar_horarios_livres: a bad tipo, or a missing id for the chosen tipo,
// never reaches Feegow.
func TestBuscarHorariosLivres_InvalidTipo_RejectsBeforeFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an invalid tipo: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := BuscarHorariosLivres(ctxWithToken("tok"), client, HorariosLivresArgs{
		Tipo: "X", DataInicio: "2026-08-01", DataFim: "2026-08-31",
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestBuscarHorariosLivres_TipoE_RequiresEspecialidadeID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := BuscarHorariosLivres(ctxWithToken("tok"), client, HorariosLivresArgs{
		Tipo: "E", DataInicio: "2026-08-01", DataFim: "2026-08-31",
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestBuscarHorariosLivres_TipoP_RequiresProcedimentoID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := BuscarHorariosLivres(ctxWithToken("tok"), client, HorariosLivresArgs{
		Tipo: "P", DataInicio: "2026-08-01", DataFim: "2026-08-31",
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestBuscarHorariosLivres_RemovesBlockedSlot is acceptance criterion 6:
// a slot available-schedule reports as free, but that a matching lock/list
// entry covers, must not appear in the result — while a free slot the lock
// does not cover must.
func TestBuscarHorariosLivres_RemovesBlockedSlot(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/available-schedule", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{
			"success": true,
			"content": {
				"profissional_id": {
					"1": {
						"local_id": {
							"0": {
								"2026-08-10": ["08:00:00", "09:00:00"]
							}
						}
					}
				}
			}
		}`)
	})
	mux.HandleFunc("/lock/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{
			"success": true,
			"content": [
				{
					"id": 1,
					"date_start": "2026-08-01",
					"date_end": "2026-08-31",
					"time_start": "07:00:00",
					"time_end": "08:30:00",
					"professional_id": 1,
					"week_day": [],
					"units": []
				}
			]
		}`)
	})
	client := newTestClient(t, mux)

	result, err := BuscarHorariosLivres(ctxWithToken("tok"), client, HorariosLivresArgs{
		Tipo:            "E",
		EspecialidadeID: intPtr(10),
		DataInicio:      "2026-08-01",
		DataFim:         "2026-08-31",
	})
	if err != nil {
		t.Fatalf("BuscarHorariosLivres: %v", err)
	}

	want := []HorarioLivre{
		{ProfissionalID: 1, LocalID: 0, Data: "2026-08-10", Horario: "09:00:00"},
	}
	if len(result.Horarios) != len(want) || result.Horarios[0] != want[0] {
		t.Fatalf("Horarios = %+v, want %+v (08:00:00 removed by the lock, 09:00:00 kept)", result.Horarios, want)
	}
}

// TestBuscarHorariosLivres_FeegowErrorBody_NeverLeaksPII is the regression
// test for the adversarial review's most severe finding, applied to this
// tool: a 409 or 422 from Feegow's /appoints/available-schedule can carry a
// free-text body with patient PII. None of that body — planted here as
// distinct name/CPF markers — may survive into the error this tool returns.
func TestBuscarHorariosLivres_FeegowErrorBody_NeverLeaksPII(t *testing.T) {
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
			`{"especialidade_id":["conflito para ` + plantedName + `, CPF ` + plantedCPF + `"]}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/appoints/available-schedule", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, c.status, c.body)
			})
			client := newTestClient(t, mux)

			_, err := BuscarHorariosLivres(ctxWithToken("tok"), client, HorariosLivresArgs{
				Tipo: "E", EspecialidadeID: intPtr(10),
				DataInicio: "2026-08-01", DataFim: "2026-08-31",
			})
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			msg := err.Error()
			if strings.Contains(msg, plantedName) || strings.Contains(msg, "Fulano") || strings.Contains(msg, plantedCPF) || strings.Contains(msg, "111.111.111-11") {
				t.Fatalf("BuscarHorariosLivres error leaked the Feegow error body: %q", msg)
			}
		})
	}
}

// TestIsBlocked_TimeComparisonIsNumericNotLexicographic is the regression
// test for the string-comparison bug the adversarial review found: Go
// compares "07:00" < "07:00:00" as true (a rune-by-rune prefix compare), so
// a slot reported as "07:00" against a lock starting at "07:00:00" used to
// read as "before the lock" and escape it entirely. Comparing normalized
// seconds-since-midnight instead must block it.
func TestIsBlocked_TimeComparisonIsNumericNotLexicographic(t *testing.T) {
	locks := []lockEntry{{
		DateStart: "2026-08-01", DateEnd: "2026-08-31",
		TimeStart: "07:00:00", TimeEnd: "08:30:00",
	}}

	if !isBlocked(locks, 1, 0, "2026-08-10", "07:00") {
		t.Fatal(`isBlocked("07:00" against lock "07:00:00"-"08:30:00") = false, want true (string comparison would wrongly read "07:00" as before "07:00:00")`)
	}
}

// TestIsBlocked_TimeBoundaries_StartInclusiveEndInclusive covers the edges
// around a lock's window: exactly at time_start, exactly at time_end (this
// package treats the end as inclusive — a slot AT the boundary is still
// locked), and the first instant strictly after time_end, which must be
// free again.
func TestIsBlocked_TimeBoundaries_StartInclusiveEndInclusive(t *testing.T) {
	locks := []lockEntry{{
		DateStart: "2026-08-01", DateEnd: "2026-08-31",
		TimeStart: "07:00:00", TimeEnd: "08:30:00",
	}}

	cases := []struct {
		name    string
		horario string
		want    bool
	}{
		{"exactly at time_start", "07:00:00", true},
		{"just before time_start", "06:59:59", false},
		{"exactly at time_end (inclusive)", "08:30:00", true},
		{"just after time_end", "08:30:01", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isBlocked(locks, 1, 0, "2026-08-10", c.horario); got != c.want {
				t.Fatalf("isBlocked(horario=%q) = %v, want %v", c.horario, got, c.want)
			}
		})
	}
}

// TestIsBlocked_UnparseableHorario_FailsSafeToBlocked proves a horário this
// package can't parse as a time of day never silently escapes a lock that
// otherwise applies (profissional/unidade/data/semana all match) — per the
// fail-safe philosophy documented on isBlocked, "can't evaluate" must never
// mean "offer it".
func TestIsBlocked_UnparseableHorario_FailsSafeToBlocked(t *testing.T) {
	locks := []lockEntry{{
		DateStart: "2026-08-01", DateEnd: "2026-08-31",
		TimeStart: "07:00:00", TimeEnd: "08:30:00",
	}}

	if !isBlocked(locks, 1, 0, "2026-08-10", "not-a-time") {
		t.Fatal(`isBlocked with an unparseable horário = false, want true (fail-safe: never offer a slot you couldn't evaluate)`)
	}
}

// TestIsBlocked_LocalIDFromSlot_NotCallerUnidadeFilter is the regression
// test for the second bug the adversarial review found: isBlocked must be
// evaluated against the slot's own local_id, not the caller's optional
// unidade_id filter — otherwise, whenever the caller omits unidade_id
// ("todas as unidades"), a lock restricted to one unit ends up applying to
// every unit's slots.
func TestIsBlocked_LocalIDFromSlot_NotCallerUnidadeFilter(t *testing.T) {
	locks := []lockEntry{{
		DateStart: "2026-08-01", DateEnd: "2026-08-31",
		Units: []string{"5"},
	}}

	// localID=5: numeric correlation is confident, and it matches — blocked.
	if !isBlocked(locks, 1, 5, "2026-08-10", "09:00:00") {
		t.Fatal("isBlocked(localID=5, lock units=[5]) = false, want true")
	}
	// localID=7: numeric correlation is confident, and it does NOT match —
	// over-blocking every unit just because the caller asked for "todas as
	// unidades" is exactly the bug being fixed here.
	if isBlocked(locks, 1, 7, "2026-08-10", "09:00:00") {
		t.Fatal("isBlocked(localID=7, lock units=[5]) = true, want false (different unit, confidently correlated)")
	}
}

// TestIsBlocked_AmbiguousUnitCorrelation_FailsSafeToBlocked proves that
// when a lock's units values can't be confidently correlated against the
// slot's local_id (a non-numeric entry — local_id/unidade_id are two
// different, unverified ID spaces per ESPECIFICACAO.md and
// internal/feegow/registry.go's lock.list notes), the slot is blocked
// rather than silently offered, even though the two id spaces might turn
// out unrelated.
func TestIsBlocked_AmbiguousUnitCorrelation_FailsSafeToBlocked(t *testing.T) {
	locks := []lockEntry{{
		DateStart: "2026-08-01", DateEnd: "2026-08-31",
		Units: []string{"unidade-5"},
	}}

	if !isBlocked(locks, 1, 7, "2026-08-10", "09:00:00") {
		t.Fatal("isBlocked with a non-numeric lock unit = false, want true (ambiguous correlation must fail toward blocked)")
	}
}

// TestBuscarHorariosLivres_UsesSlotLocalID_NotCallerUnidadeFilter is the
// end-to-end regression test for the wiring bug: with unidade_id omitted
// ("todas as unidades"), a lock restricted to units=["5"] must still block
// only the slots actually in local_id 5, and must NOT swallow a free slot
// reported under a different local_id — the exact over-blocking-everywhere
// bug that follows from passing args.UnidadeID (always nil here) into
// isBlocked instead of each slot's own local_id.
func TestBuscarHorariosLivres_UsesSlotLocalID_NotCallerUnidadeFilter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/available-schedule", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{
			"success": true,
			"content": {
				"profissional_id": {
					"1": {
						"local_id": {
							"5": {"2026-08-10": ["09:00:00"]},
							"7": {"2026-08-10": ["09:00:00"]}
						}
					}
				}
			}
		}`)
	})
	mux.HandleFunc("/lock/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{
			"success": true,
			"content": [
				{
					"id": 1,
					"date_start": "2026-08-01",
					"date_end": "2026-08-31",
					"time_start": "",
					"time_end": "",
					"professional_id": 1,
					"week_day": [],
					"units": ["5"]
				}
			]
		}`)
	})
	client := newTestClient(t, mux)

	result, err := BuscarHorariosLivres(ctxWithToken("tok"), client, HorariosLivresArgs{
		Tipo:            "E",
		EspecialidadeID: intPtr(10),
		DataInicio:      "2026-08-01",
		DataFim:         "2026-08-31",
		// UnidadeID deliberately absent: "todas as unidades" is exactly the
		// case that used to make isBlocked skip the Units filter entirely.
	})
	if err != nil {
		t.Fatalf("BuscarHorariosLivres: %v", err)
	}

	want := []HorarioLivre{
		{ProfissionalID: 1, LocalID: 7, Data: "2026-08-10", Horario: "09:00:00"},
	}
	if len(result.Horarios) != len(want) || result.Horarios[0] != want[0] {
		t.Fatalf("Horarios = %+v, want %+v (local_id 5 blocked by units=[5], local_id 7 kept)", result.Horarios, want)
	}
}

// TestBuscarHorariosLivres_UnidadeIDZeroVsAbsent_ProduceDifferentRequests
// is acceptance criterion 7: unidade_id=0 ("unidade principal") and an
// absent unidade_id ("todas as unidades") must reach Feegow as visibly
// different requests, never silently collapsed to the same query.
func TestBuscarHorariosLivres_UnidadeIDZeroVsAbsent_ProduceDifferentRequests(t *testing.T) {
	var gotQueries []url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/available-schedule", func(w http.ResponseWriter, r *http.Request) {
		gotQueries = append(gotQueries, r.URL.Query())
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"profissional_id":{}}}`)
	})
	mux.HandleFunc("/lock/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[]}`)
	})
	client := newTestClient(t, mux)

	base := HorariosLivresArgs{
		Tipo: "E", EspecialidadeID: intPtr(1),
		DataInicio: "2026-08-01", DataFim: "2026-08-31",
	}

	if _, err := BuscarHorariosLivres(ctxWithToken("tok"), client, base); err != nil {
		t.Fatalf("BuscarHorariosLivres (unidade_id ausente): %v", err)
	}

	withZero := base
	withZero.UnidadeID = intPtr(0)
	if _, err := BuscarHorariosLivres(ctxWithToken("tok"), client, withZero); err != nil {
		t.Fatalf("BuscarHorariosLivres (unidade_id=0): %v", err)
	}

	if len(gotQueries) != 2 {
		t.Fatalf("got %d available-schedule requests, want 2", len(gotQueries))
	}
	if gotQueries[0].Has("unidade_id") {
		t.Fatalf("unidade_id ausente ainda assim foi enviado na query: %v", gotQueries[0])
	}
	if got := gotQueries[1].Get("unidade_id"); got != "0" {
		t.Fatalf("unidade_id=0 não chegou como \"0\" na query: got %q (%v)", got, gotQueries[1])
	}
}
