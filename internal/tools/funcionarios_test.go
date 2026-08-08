package tools

import (
	"net/http"
	"testing"
)

func TestListarFuncionarios_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/employee/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"id":1,"nome":"Fulano"}],"total":1}`)
	})
	client := newTestClient(t, mux)

	result, err := ListarFuncionarios(ctxWithToken("tok"), client)
	if err != nil {
		t.Fatalf("ListarFuncionarios: %v", err)
	}
	list, ok := result.Funcionarios.([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("result.Funcionarios = %#v, want a one-element array", result.Funcionarios)
	}
}

func TestListarFuncionarios_ErrorSanitized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/employee/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnprocessableEntity, `{"campo":["algo deu errado"]}`)
	})
	client := newTestClient(t, mux)

	_, err := ListarFuncionarios(ctxWithToken("tok"), client)
	if err == nil {
		t.Fatal("ListarFuncionarios: got nil error, want the 422 surfaced (sanitized)")
	}
	if err.Error() != ErrEntradaInvalidaFeegow.Error() {
		t.Fatalf("ListarFuncionarios error = %v, want the sanitized ErrEntradaInvalidaFeegow", err)
	}
}
