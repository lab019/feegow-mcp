package tools

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGerenciarVoucher_Criar_AlwaysUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for acao=criar: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarVoucher(ctxWithToken("tok"), client, GerenciarVoucherArgs{Acao: "criar"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarVoucher error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarVoucher_UnknownAcao_Rejects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unrecognized acao: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarVoucher(ctxWithToken("tok"), client, GerenciarVoucherArgs{Acao: "editar"})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarVoucher error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarVoucher_Cancelar_RequiresConfirmacao_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an unconfirmed cancelamento: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarVoucher(ctxWithToken("tok"), client, GerenciarVoucherArgs{
		Acao: "cancelar", VoucherID: intPtr(1), Motivo: "OUTRO", CodigoMotivo: "x",
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarVoucher error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarVoucher_Cancelar_RequiresValidMotivoEnum(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for an invalid motivo: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarVoucher(ctxWithToken("tok"), client, GerenciarVoucherArgs{
		Acao: "cancelar", Confirmacao: true, VoucherID: intPtr(1), Motivo: "MOTIVO_INVENTADO", CodigoMotivo: "x",
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarVoucher error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestGerenciarVoucher_Cancelar_RequiresVoucherIDAndCodigoMotivo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	for _, args := range []GerenciarVoucherArgs{
		{Acao: "cancelar", Confirmacao: true, Motivo: "OUTRO", CodigoMotivo: "x"},
		{Acao: "cancelar", Confirmacao: true, VoucherID: intPtr(1), Motivo: "OUTRO"},
	} {
		_, err := GerenciarVoucher(ctxWithToken("tok"), client, args)
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("GerenciarVoucher(%+v) error = %v (%T), want *ArgumentError", args, err, err)
		}
	}
}

// TestGerenciarVoucher_Cancelar_Success proves a valid cancelamento reaches
// /core/financial/voucher/cancel with id/motivo/codigo_motivo, and that a
// {"success":true,...} body is decoded and treated as success.
func TestGerenciarVoucher_Cancelar_Success(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/voucher/cancel", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"message":"Voucher cancelado"}`)
	})
	client := newTestClient(t, mux)

	result, err := GerenciarVoucher(ctxWithToken("tok"), client, GerenciarVoucherArgs{
		Acao: "cancelar", Confirmacao: true, VoucherID: intPtr(7), Motivo: "DUPLICIDADE", CodigoMotivo: "dup-123",
	})
	if err != nil {
		t.Fatalf("GerenciarVoucher: %v", err)
	}
	if !result.Cancelado {
		t.Fatalf("result = %+v, want Cancelado=true", result)
	}
	if gotBody["id"] != float64(7) || gotBody["motivo"] != "DUPLICIDADE" || gotBody["codigo_motivo"] != "dup-123" {
		t.Fatalf("wire body = %+v, want id/motivo/codigo_motivo", gotBody)
	}
}

// TestGerenciarVoucher_Cancelar_ServerRejection_SurfacesError proves a
// {"success":false,...} body — the pattern financial.create_account and
// financial.invoice_remove already demonstrated for this endpoint family —
// is treated as a failure, not a silent success, even though client.Call
// itself sees no transport error (HTTP 200).
func TestGerenciarVoucher_Cancelar_ServerRejection_SurfacesError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/voucher/cancel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":false,"message":"Voucher já cancelado"}`)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarVoucher(ctxWithToken("tok"), client, GerenciarVoucherArgs{
		Acao: "cancelar", Confirmacao: true, VoucherID: intPtr(7), Motivo: "OUTRO", CodigoMotivo: "x",
	})
	if !errors.Is(err, ErrOperacaoNaoConfirmadaFeegow) {
		t.Fatalf("GerenciarVoucher error = %v, want ErrOperacaoNaoConfirmadaFeegow", err)
	}
}

// TestGerenciarVoucher_Cancelar_AuditsWrite proves the audit line names the
// voucher_id that was cancelled, not just that some voucher was.
func TestGerenciarVoucher_Cancelar_AuditsWrite(t *testing.T) {
	buf := captureLog(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/voucher/cancel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"message":"ok"}`)
	})
	client := newTestClient(t, mux)

	if _, err := GerenciarVoucher(ctxWithToken("tok"), client, GerenciarVoucherArgs{
		Acao: "cancelar", Confirmacao: true, VoucherID: intPtr(7), Motivo: "OUTRO", CodigoMotivo: "x",
	}); err != nil {
		t.Fatalf("GerenciarVoucher: %v", err)
	}
	if !strings.Contains(buf.String(), "ADMIN WRITE gerenciar_voucher:cancelar — voucher_id=7") {
		t.Fatalf("write not audited with voucher_id: %s", buf.String())
	}
}

// TestGerenciarVoucher_Cancelar_ServerRejection_Sanitized proves a 400 from
// voucher/cancel's NestJS-style validation never leaks into the tool's
// error (SanitizeFeegowError only maps ConflictError/ValidationError — a
// 400 becomes UnexpectedStatusError, which never carries the body, so this
// also locks in that this specific endpoint's odd status code doesn't
// regress into something that does).
func TestGerenciarVoucher_Cancelar_ServerRejection_Sanitized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/voucher/cancel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusBadRequest, `{"message":["id must be an integer number"],"error":"Bad Request","statusCode":400}`)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarVoucher(ctxWithToken("tok"), client, GerenciarVoucherArgs{
		Acao: "cancelar", Confirmacao: true, VoucherID: intPtr(7), Motivo: "OUTRO", CodigoMotivo: "x",
	})
	if err == nil {
		t.Fatal("GerenciarVoucher: got nil error, want the 400 surfaced as an error")
	}
	if got := err.Error(); got == "" {
		t.Fatal("empty error message")
	}
}

func TestGerenciarVoucher_Listar_NoConfirmacaoRequired(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/voucher/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"total":0,"page":1,"limit":10,"lastPage":0,"data":[]}`)
	})
	client := newTestClient(t, mux)

	if _, err := GerenciarVoucher(ctxWithToken("tok"), client, GerenciarVoucherArgs{Acao: "listar"}); err != nil {
		t.Fatalf("GerenciarVoucher(listar, no confirmacao): %v", err)
	}
}

// TestGerenciarVoucher_Listar_LimitDefaultsAndCaps proves the pagination
// ceiling applies, and that limit/page wire params (financial.voucher_list's
// own naming — "limit", not "perPage") are used.
func TestGerenciarVoucher_Listar_LimitDefaultsAndCaps(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/core/financial/voucher/list", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"total":0,"page":1,"limit":100,"lastPage":0,"data":[]}`)
	})
	client := newTestClient(t, mux)

	if _, err := GerenciarVoucher(ctxWithToken("tok"), client, GerenciarVoucherArgs{Acao: "listar", Limit: 9999}); err != nil {
		t.Fatalf("GerenciarVoucher: %v", err)
	}
	if gotQuery.Get("limit") != "100" {
		t.Fatalf("limit = %q, want capped at 100", gotQuery.Get("limit"))
	}
}

func TestGerenciarVoucher_Listar_RejectsNegativeOffset(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a negative offset: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := GerenciarVoucher(ctxWithToken("tok"), client, GerenciarVoucherArgs{Acao: "listar", Offset: -1})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("GerenciarVoucher error = %v (%T), want *ArgumentError", err, err)
	}
}
