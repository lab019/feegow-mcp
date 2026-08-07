package tools

import (
	"errors"
	"net/http"
	"net/url"
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
