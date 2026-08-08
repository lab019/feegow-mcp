package tools

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// --- consultar_estoque -------------------------------------------------

func TestConsultarEstoque_UnknownTipo_Rejects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unrecognized tipo: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarEstoque(ctxWithToken("tok"), client, ConsultarEstoqueArgs{Tipo: "nao_existe"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarEstoque error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestConsultarEstoque_Posicao_NoFiltersRequired(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/base/product/position", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"data":[],"count":"0","page":1,"perPage":20,"pages":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := ConsultarEstoque(ctxWithToken("tok"), client, ConsultarEstoqueArgs{Tipo: "posicao"}); err != nil {
		t.Fatalf("ConsultarEstoque: %v", err)
	}
}

// TestConsultarEstoque_Posicao_UsesPortugueseFilterNames proves posicao's
// filters reach Feegow with their confirmed Portuguese names.
func TestConsultarEstoque_Posicao_UsesPortugueseFilterNames(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/base/product/position", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"data":[],"count":"0","page":1,"perPage":20,"pages":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := ConsultarEstoque(ctxWithToken("tok"), client, ConsultarEstoqueArgs{
		Tipo: "posicao", Fabricante: "Acme", Produto: "Luva", Categoria: "EPI", Localizacao: "Sala 1",
	}); err != nil {
		t.Fatalf("ConsultarEstoque: %v", err)
	}
	for _, key := range []string{"fabricante", "produto", "categoria", "localizacao"} {
		if _, has := gotBody[key]; !has {
			t.Fatalf("wire body = %+v, missing %q", gotBody, key)
		}
	}
}

// TestConsultarEstoque_ListaProdutos_UsesEnglishFilterNames proves
// lista_produtos' filters use their own, confirmed English names — a
// genuinely different set from tipo=posicao (see the args' doc comments).
func TestConsultarEstoque_ListaProdutos_UsesEnglishFilterNames(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/base/product/list", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"data":[],"count":"0","page":1,"perPage":20,"pages":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := ConsultarEstoque(ctxWithToken("tok"), client, ConsultarEstoqueArgs{
		Tipo: "lista_produtos", ProdutoID: intPtr(1), Category: "EPI", Location: "Sala 1", Producer: "Acme", ProductType: "consumivel",
	}); err != nil {
		t.Fatalf("ConsultarEstoque: %v", err)
	}
	for _, key := range []string{"id", "category", "location", "producer", "type"} {
		if _, has := gotBody[key]; !has {
			t.Fatalf("wire body = %+v, missing %q", gotBody, key)
		}
	}
}

func TestConsultarEstoque_LimitDefaultsAndCaps(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/base/product/list", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"data":[],"count":"0","page":1,"perPage":100,"pages":0}`)
	})
	client := newTestClient(t, mux)

	if _, err := ConsultarEstoque(ctxWithToken("tok"), client, ConsultarEstoqueArgs{Tipo: "lista_produtos", Limit: 99999}); err != nil {
		t.Fatalf("ConsultarEstoque: %v", err)
	}
	if gotBody["perPage"] != "100" {
		t.Fatalf("perPage = %v, want capped at 100", gotBody["perPage"])
	}
}

func TestConsultarEstoque_RejectsNegativeOffset(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a negative offset: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := ConsultarEstoque(ctxWithToken("tok"), client, ConsultarEstoqueArgs{Tipo: "posicao", Offset: -1})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("ConsultarEstoque error = %v (%T), want *ArgumentError", err, err)
	}
}

// --- movimentar_estoque -------------------------------------------------

func TestMovimentarEstoque_RequiresConfirmacao_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unconfirmed movimentar_estoque: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := MovimentarEstoque(ctxWithToken("tok"), client, MovimentarEstoqueArgs{Acao: "inserir_produto"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("MovimentarEstoque error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestMovimentarEstoque_UnknownAcao_Rejects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unrecognized acao: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := MovimentarEstoque(ctxWithToken("tok"), client, MovimentarEstoqueArgs{Acao: "nao_existe", Confirmacao: true})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("MovimentarEstoque error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestMovimentarEstoque_DeadHostAcoes_AlwaysUnavailable proves entrada,
// saida e movimentacao never reach Feegow — the host (core.feegow.com.br)
// is confirmed dead by Fase 0.
func TestMovimentarEstoque_DeadHostAcoes_AlwaysUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a dead-host acao: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	for _, acao := range []string{"entrada", "saida", "movimentacao"} {
		_, err := MovimentarEstoque(ctxWithToken("tok"), client, MovimentarEstoqueArgs{Acao: acao, Confirmacao: true})
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("MovimentarEstoque(acao=%s) error = %v (%T), want *ArgumentError", acao, err, err)
		}
	}
}

func TestMovimentarEstoque_InserirProduto_RequiresEveryField(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an incomplete inserir_produto request: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := MovimentarEstoque(ctxWithToken("tok"), client, MovimentarEstoqueArgs{Acao: "inserir_produto", Confirmacao: true})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("MovimentarEstoque error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestMovimentarEstoque_InserirProduto_Success proves a fully-formed
// request reaches stock.product_insert with the confirmed PascalCase field
// names, and decodes a 201 response (SuccessStatus override) correctly.
func TestMovimentarEstoque_InserirProduto_Success(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/financial-stock/product/insert", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusCreated, `{"id":42,"tipoProduto":1}`)
	})
	client := newTestClient(t, mux)

	result, err := MovimentarEstoque(ctxWithToken("tok"), client, MovimentarEstoqueArgs{
		Acao: "inserir_produto", Confirmacao: true,
		TipoProduto: intPtr(1), CategoriaID: intPtr(1), FabricanteID: intPtr(1), LocalizacaoID: intPtr(1),
		DiasAvisoValidade: intPtr(1), ApresentacaoQuantidade: intPtr(1), ApresentacaoUnidade: intPtr(1),
		EstoqueMinimo: intPtr(1), EstoqueMaximo: intPtr(1), PrecoCompra: intPtr(1), PrecoVenda: intPtr(1),
	})
	if err != nil {
		t.Fatalf("MovimentarEstoque: %v", err)
	}
	if !result.Inserido || result.ProdutoID != 42 {
		t.Fatalf("result = %+v, want Inserido=true, ProdutoID=42", result)
	}
	for _, key := range []string{
		"TipoProduto", "CategoriaID", "FabricanteID", "LocalizacaoID", "DiasAvisoValidade",
		"ApresentacaoQuantidade", "ApresentacaoUnidade", "EstoqueMinimo", "EstoqueMaximo", "PrecoCompra", "PrecoVenda",
	} {
		if _, has := gotBody[key]; !has {
			t.Fatalf("wire body = %+v, missing PascalCase key %q", gotBody, key)
		}
	}
}

func TestMovimentarEstoque_InserirProduto_AuditsWrite(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/financial-stock/product/insert", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, `{"id":1}`)
	})
	client := newTestClient(t, mux)

	args := MovimentarEstoqueArgs{
		Acao: "inserir_produto", Confirmacao: true,
		TipoProduto: intPtr(1), CategoriaID: intPtr(1), FabricanteID: intPtr(1), LocalizacaoID: intPtr(1),
		DiasAvisoValidade: intPtr(1), ApresentacaoQuantidade: intPtr(1), ApresentacaoUnidade: intPtr(1),
		EstoqueMinimo: intPtr(1), EstoqueMaximo: intPtr(1), PrecoCompra: intPtr(1), PrecoVenda: intPtr(1),
	}
	if _, err := MovimentarEstoque(ctxWithToken("tok"), client, args); err != nil {
		t.Fatalf("MovimentarEstoque: %v", err)
	}
	if !strings.Contains(buf.String(), "ADMIN WRITE movimentar_estoque:inserir_produto — produto_id=1") {
		t.Fatalf("write not audited with the inserted product's id: %s", buf.String())
	}
}
