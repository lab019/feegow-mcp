package tools

import (
	"errors"
	"net/http"
	"testing"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// TestListarCatalogo_UnknownTipo_RejectsBeforeFeegowCall proves an
// unrecognized tipo never reaches Feegow — a handler that would fail the
// test if hit at all is what enforces "before", not just "instead of".
func TestListarCatalogo_UnknownTipo_RejectsBeforeFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unknown tipo: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ListarCatalogo(ctxWithToken("tok"), client, CatalogoArgs{Tipo: "nao-existe"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ListarCatalogo error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestListarCatalogo_EveryTipoCallsItsRegisteredEndpoint proves every tipo
// this tool advertises actually maps to a real, distinct Feegow endpoint
// and round-trips its content back out.
func TestListarCatalogo_EveryTipoCallsItsRegisteredEndpoint(t *testing.T) {
	for tipo, id := range catalogEndpoints {
		t.Run(tipo, func(t *testing.T) {
			d, ok := feegow.Registry[id]
			if !ok {
				t.Fatalf("tipo %q maps to endpoint id %q, which is not in feegow.Registry", tipo, id)
			}

			called := false
			mux := http.NewServeMux()
			mux.HandleFunc(d.Path, func(w http.ResponseWriter, r *http.Request) {
				called = true
				writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"id":1,"marker":"`+tipo+`"}]}`)
			})
			client := newTestClient(t, mux)

			result, err := ListarCatalogo(ctxWithToken("tok"), client, CatalogoArgs{Tipo: tipo})
			if err != nil {
				t.Fatalf("ListarCatalogo(%q): %v", tipo, err)
			}
			if !called {
				t.Fatalf("tipo %q never called %s", tipo, d.Path)
			}

			items, ok := result.Itens.([]any)
			if !ok || len(items) != 1 {
				t.Fatalf("ListarCatalogo(%q).Itens = %#v, want a 1-element list", tipo, result.Itens)
			}
		})
	}
}

// TestCatalogTypes_MatchesTheSpecifiedSet locks in that the tipo values
// this tool advertises are exactly the ones ESPECIFICACAO.md §6 / the Fase
// 2 task describe: unidades, locais, especialidades, convênios,
// procedimentos (+tipos, grupos, pacotes), profissionais, canais, motivos,
// status de agendamento.
func TestCatalogTypes_MatchesTheSpecifiedSet(t *testing.T) {
	want := map[string]bool{
		"unidades":              true,
		"locais":                true,
		"especialidades":        true,
		"convenios":             true,
		"procedimentos":         true,
		"procedimentos_tipos":   true,
		"procedimentos_grupos":  true,
		"procedimentos_pacotes": true,
		"profissionais":         true,
		"canais":                true,
		"motivos":               true,
		"status_agendamento":    true,
	}
	got := CatalogTypes()
	if len(got) != len(want) {
		t.Fatalf("CatalogTypes() = %v (%d values), want %d values", got, len(got), len(want))
	}
	for _, tipo := range got {
		if !want[tipo] {
			t.Errorf("CatalogTypes() contains unexpected tipo %q", tipo)
		}
	}
}
