package feegow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lab019/feegow-mcp/internal/auth"
)

// captureLog redirects the standard "log" package's output to a buffer for
// the duration of the test, restoring the previous output on cleanup.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	})
	return &buf
}

// ctxWithToken returns a context carrying tok, the way Middleware would
// set it up for a real request.
func ctxWithToken(tok string) context.Context {
	return auth.WithToken(context.Background(), tok)
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("writing test response: %v", err)
	}
}

// TestCall_SuccessEnvelopeUnwrapped is acceptance criterion 4: a
// {"success":true,"content":...} body has Content handed back to the
// caller, unwrapped.
func TestCall_SuccessEnvelopeUnwrapped(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/specialties/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[{"especialidade_id":1,"nome":"Psicólogo"}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	resp, err := c.Call(ctxWithToken("tok"), "specialties.list", Request{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var content []map[string]any
	if err := json.Unmarshal(resp.Content, &content); err != nil {
		t.Fatalf("unmarshaling Content: %v", err)
	}
	if len(content) != 1 || content[0]["nome"] != "Psicólogo" {
		t.Fatalf("unexpected content: %v", content)
	}
}

// TestCall_EnvelopeNone_RawBodyReturned proves an endpoint declared with
// EnvelopeNone (e.g. the cartao-beneficios datagrids, which respond with a
// bare {"data":...} object and no "success"/"content" wrapper) hands the
// whole response body back as Content, unmodified.
func TestCall_EnvelopeNone_RawBodyReturned(t *testing.T) {
	const rawBody = `{"data":[{"id":"plan-1"}],"count":1,"page":1,"perPage":1}`

	mux := http.NewServeMux()
	mux.HandleFunc("/external/plan/datagrid", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, rawBody)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	resp, err := c.Call(ctxWithToken("tok"), "benefit.plan_datagrid", Request{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if string(resp.Content) != rawBody {
		t.Fatalf("Content = %s, want the raw body verbatim: %s", resp.Content, rawBody)
	}
}

// TestCall_MedicalReportsCreate_200SuccessFalse_MessagePreserved is a
// regression test for the registry.go entry that used to declare
// medical_reports.create as EnvelopeStandard. Feegow's real 200 body for
// this endpoint is {"success":false,"message":"..."} — no "content" field
// at all (confirmed by the Fase 0 resultados.json record for
// medical-reports/create#POST-with-body). With EnvelopeStandard,
// parseSuccess looks for "content", finds nothing, and returns a
// *ConflictError with an EMPTY Content — Feegow's actual message is
// silently discarded. With the corrected EnvelopeNone, the whole raw body
// (message included) comes back verbatim as Response.Content instead of
// being swallowed into an error with no information in it.
func TestCall_MedicalReportsCreate_200SuccessFalse_MessagePreserved(t *testing.T) {
	const rawBody = `{"success":false,"message":"Trying to access array offset on value of type bool"}`

	mux := http.NewServeMux()
	mux.HandleFunc("/medical-reports/create", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, rawBody)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	resp, err := c.Call(ctxWithToken("tok"), "medical_reports.create", Request{})
	if err != nil {
		t.Fatalf("Call: got error %v, want the raw body back (EnvelopeNone never treats success:false as a transport error)", err)
	}
	if string(resp.Content) != rawBody {
		t.Fatalf("Content = %s, want the raw body verbatim (with the real message intact): %s", resp.Content, rawBody)
	}
}

// TestCall_409ConflictError_Distinguishable is acceptance criteria 5 and
// 6's 409 half: a 409 with the standard envelope becomes a *ConflictError
// carrying Feegow's message, and it must NOT also satisfy *ValidationError
// (they are distinct Go types precisely so callers can tell them apart).
func TestCall_409ConflictError_Distinguishable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/new-appoint", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusConflict, `{"success":false,"content":"Horário ocupado"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	_, err := c.Call(ctxWithToken("tok"), "appoints.new_appoint", Request{})
	if err == nil {
		t.Fatal("Call: got nil error, want a ConflictError")
	}

	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("error is not a *ConflictError: %v (%T)", err, err)
	}
	if conflictErr.Content != "Horário ocupado" {
		t.Fatalf("ConflictError.Content = %q, want %q", conflictErr.Content, "Horário ocupado")
	}

	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		t.Fatalf("409 error also matched *ValidationError — the two must be distinguishable")
	}
}

// TestCall_422ValidationError_Distinguishable is the 422 half: a bare
// field->errors map (no envelope) becomes a *ValidationError, and does not
// also satisfy *ConflictError.
func TestCall_422ValidationError_Distinguishable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/new-appoint", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnprocessableEntity, `{"paciente_id":["validation.required"]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	_, err := c.Call(ctxWithToken("tok"), "appoints.new_appoint", Request{})
	if err == nil {
		t.Fatal("Call: got nil error, want a ValidationError")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error is not a *ValidationError: %v (%T)", err, err)
	}
	if got := validationErr.Fields["paciente_id"]; len(got) != 1 || got[0] != "validation.required" {
		t.Fatalf("ValidationError.Fields[paciente_id] = %v, want [validation.required]", got)
	}

	var conflictErr *ConflictError
	if errors.As(err, &conflictErr) {
		t.Fatalf("422 error also matched *ConflictError — the two must be distinguishable")
	}
}

// TestCall_422RouteNotFound_Distinguishable proves the "impressão digital"
// the Fase 0 smoke test found — HTTP 422 with an EMPTY "message" in
// {"success":false,"cod_erro":0,"message":""} — is classified as a
// *RouteNotFoundError, never as a *ValidationError. Confusing the two
// would send an agent off "revising" input that was never the problem
// (see RouteNotFoundError's doc comment).
func TestCall_422RouteNotFound_Distinguishable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/new-appoint", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnprocessableEntity, `{"success":false,"cod_erro":0,"message":""}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	_, err := c.Call(ctxWithToken("tok"), "appoints.new_appoint", Request{})
	if err == nil {
		t.Fatal("Call: got nil error, want a RouteNotFoundError")
	}

	var notFound *RouteNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error is not a *RouteNotFoundError: %v (%T)", err, err)
	}

	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		t.Fatalf("empty-message 422 also matched *ValidationError — the two must be distinguishable")
	}
}

// TestCall_422RouteNotFound_LogsWarning proves the RouteNotFoundError path
// is treated as an operational failure worth an operator's attention (like
// an unrecognized shape already is) rather than passing silently.
func TestCall_422RouteNotFound_LogsWarning(t *testing.T) {
	t.Setenv("LOG_LEVEL", "") // explicit default, so WarnEnabled() can't be off via inherited env
	buf := captureLog(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/new-appoint", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnprocessableEntity, `{"success":false,"cod_erro":0,"message":""}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	_, err := c.Call(ctxWithToken("tok"), "appoints.new_appoint", Request{})
	if err == nil {
		t.Fatal("Call: got nil error, want a RouteNotFoundError")
	}

	if !strings.Contains(buf.String(), "WARN") {
		t.Fatalf("expected a WARN log line for a route-not-found 422, got: %q", buf.String())
	}
}

// TestCall_422MessageStyleValidation proves the OTHER half of the same
// wire shape — {"success":false,"cod_erro":N,"message":"<non-empty>"} — is
// a real validation error (e.g. "The GET method is not supported..."),
// classified as *ValidationError with Message populated, not as a
// *RouteNotFoundError.
func TestCall_422MessageStyleValidation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/medical-reports/create", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnprocessableEntity,
			`{"success":false,"cod_erro":0,"message":"The GET method is not supported for this route. Supported methods: POST."}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	_, err := c.Call(ctxWithToken("tok"), "medical_reports.create", Request{})
	if err == nil {
		t.Fatal("Call: got nil error, want a ValidationError")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error is not a *ValidationError: %v (%T)", err, err)
	}
	if !strings.Contains(validationErr.Message, "GET method is not supported") {
		t.Fatalf("ValidationError.Message = %q, want it to contain the real Feegow message", validationErr.Message)
	}

	var notFound *RouteNotFoundError
	if errors.As(err, &notFound) {
		t.Fatalf("non-empty-message 422 also matched *RouteNotFoundError — the two must be distinguishable")
	}
}

// TestCall_403_CredentialInactive_NotPermissionMessage is acceptance
// criterion 6: 403 must read as "credencial inativa, recadastre", never
// as a permission problem — the exact inversion ESPECIFICACAO.md §5 warns
// against.
func TestCall_403_CredentialInactive_NotPermissionMessage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/specialties/list", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	_, err := c.Call(ctxWithToken("tok"), "specialties.list", Request{})
	if err == nil {
		t.Fatal("Call: got nil error, want a CredentialError")
	}

	var credErr *CredentialError
	if !errors.As(err, &credErr) {
		t.Fatalf("error is not a *CredentialError: %v (%T)", err, err)
	}
	if credErr.Reason != CredentialInactive {
		t.Fatalf("CredentialError.Reason = %v, want CredentialInactive", credErr.Reason)
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "permiss") {
		t.Fatalf("403 message mentions permission (%q) — must say 'credencial inativa, recadastre' instead", err.Error())
	}
	if !strings.Contains(msg, "inativa") || !strings.Contains(msg, "recadastre") {
		t.Fatalf("403 message %q does not clearly say the credential is inactive and needs recadastro", err.Error())
	}
}

// TestCall_401_CredentialMissing proves 401 is classified distinctly from
// 403, per ESPECIFICACAO.md §5 ("401: chave não definida no header" vs
// "403: chave inativa" — two different problems, not the same error).
func TestCall_401_CredentialMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/specialties/list", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	_, err := c.Call(ctxWithToken("tok"), "specialties.list", Request{})

	var credErr *CredentialError
	if !errors.As(err, &credErr) {
		t.Fatalf("error is not a *CredentialError: %v (%T)", err, err)
	}
	if credErr.Reason != CredentialMissing {
		t.Fatalf("CredentialError.Reason = %v, want CredentialMissing", credErr.Reason)
	}
}

// TestCall_5xx_InternalError proves 5xx becomes a distinct, readable
// error and never a panic — and that it does not leak the response body
// (which could echo request data) into the error message.
func TestCall_5xx_InternalError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/specialties/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, `{"success":false,"content":"boom, patient cpf 111.111.111-11 here"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	_, err := c.Call(ctxWithToken("tok"), "specialties.list", Request{})

	var internalErr *InternalError
	if !errors.As(err, &internalErr) {
		t.Fatalf("error is not an *InternalError: %v (%T)", err, err)
	}
	if internalErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("StatusCode = %d, want 500", internalErr.StatusCode)
	}
	if strings.Contains(err.Error(), "111.111.111-11") {
		t.Fatalf("5xx error message leaked response body content: %q", err.Error())
	}
}

// TestCall_UndocumentedStatus_DoesNotPanic covers status codes outside the
// documented contract (ESPECIFICACAO.md §5 explicitly has no 404, no
// 429) — must degrade to a readable error, never a panic.
func TestCall_UndocumentedStatus_DoesNotPanic(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusTooManyRequests} {
		mux := http.NewServeMux()
		mux.HandleFunc("/specialties/list", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		})
		srv := httptest.NewServer(mux)

		func() {
			defer srv.Close()
			c := New(srv.Client(), srv.URL)
			_, err := c.Call(ctxWithToken("tok"), "specialties.list", Request{})
			if err == nil {
				t.Fatalf("status %d: got nil error, want an error", status)
			}
			var unexpected *UnexpectedStatusError
			if !errors.As(err, &unexpected) {
				t.Fatalf("status %d: error is not *UnexpectedStatusError: %v (%T)", status, err, err)
			}
		}()
	}
}

// TestCall_MalformedResponseBody_DoesNotPanic covers the defensive edge:
// a 200 or 422 with a body that isn't valid JSON at all must still come
// back as an error, never a panic and never a nil *Response with garbage
// inside it.
func TestCall_MalformedResponseBody_DoesNotPanic(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/specialties/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `not json at all`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	_, err := c.Call(ctxWithToken("tok"), "specialties.list", Request{})
	if err == nil {
		t.Fatal("Call with malformed JSON body: got nil error, want an error")
	}
}

// TestCall_TokenForwardedAsHeader_NeverLogged is acceptance criterion 7:
// the x-access-token header carries the context's bearer token, and no
// log line this package emits — at DEBUG/INFO verbosity, where request
// logging is on — contains the token.
func TestCall_TokenForwardedAsHeader_NeverLogged(t *testing.T) {
	t.Setenv("LOG_LEVEL", "DEBUG")
	logOut := captureLog(t)

	const secretToken = "super-secret-clinic-token-xyz"
	var gotHeader string

	mux := http.NewServeMux()
	mux.HandleFunc("/specialties/list", func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("x-access-token")
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	if _, err := c.Call(ctxWithToken(secretToken), "specialties.list", Request{}); err != nil {
		t.Fatalf("Call: %v", err)
	}

	if gotHeader != secretToken {
		t.Fatalf("x-access-token header = %q, want %q", gotHeader, secretToken)
	}
	if strings.Contains(logOut.String(), secretToken) {
		t.Fatalf("token leaked into log output: %q", logOut.String())
	}
}

// TestCall_NoTokenInContext_FailsClosed proves a missing bearer token
// never reaches Feegow: no request is sent at all.
func TestCall_NoTokenInContext_FailsClosed(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/specialties/list", func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	_, err := c.Call(context.Background(), "specialties.list", Request{})
	if err == nil {
		t.Fatal("Call with no token in context: got nil error, want a fail-closed rejection")
	}
	if called {
		t.Fatal("Call reached the Feegow stub despite no token in context")
	}
}

// TestCall_UnknownEndpointID errors out cleanly rather than panicking on
// a map miss.
func TestCall_UnknownEndpointID(t *testing.T) {
	c := New(http.DefaultClient, "http://unused.invalid")
	_, err := c.Call(ctxWithToken("tok"), "nonexistent.endpoint", Request{})
	if err == nil {
		t.Fatal("Call with unknown EndpointID: got nil error, want an error")
	}
}

// TestCall_HostOverride_RedirectsAllHosts is acceptance criterion 8:
// FEEGOW_HOST_OVERRIDE (surfaced here via the New(...) constructor's
// explicit hostOverride, and separately below via the real env var/
// NewFromEnv path) must redirect a call regardless of the endpoint's
// declared Host — exercised across three different Registry hosts to
// prove it is not special-cased to one.
func TestCall_HostOverride_RedirectsAllHosts(t *testing.T) {
	hits := map[string]bool{}
	mux := http.NewServeMux()
	mux.HandleFunc("/specialties/list", func(w http.ResponseWriter, r *http.Request) { // HostAPI
		hits["api"] = true
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[]}`)
	})
	mux.HandleFunc("/external/plan/datagrid", func(w http.ResponseWriter, r *http.Request) { // HostBenefit
		hits["benefit"] = true
		writeJSON(t, w, http.StatusOK, `{"data":[]}`)
	})
	mux.HandleFunc("/financial2/external/financial-stock/location/list", func(w http.ResponseWriter, r *http.Request) { // HostCoreBR
		hits["core_br"] = true
		writeJSON(t, w, http.StatusOK, `[]`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)

	for _, id := range []EndpointID{"specialties.list", "benefit.plan_datagrid", "stock.location_list"} {
		if _, err := c.Call(ctxWithToken("tok"), id, Request{}); err != nil {
			t.Fatalf("Call(%s): %v", id, err)
		}
	}

	for _, host := range []string{"api", "benefit", "core_br"} {
		if !hits[host] {
			t.Fatalf("override did not redirect the %s-hosted endpoint to the stub server", host)
		}
	}
}

// TestNewFromEnv_HostOverride proves the actual FEEGOW_HOST_OVERRIDE
// environment variable — the real, documented mechanism — drives the same
// redirection, not just the lower-level New(...) constructor tests use
// elsewhere to avoid env-var races.
func TestNewFromEnv_HostOverride(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/specialties/list", func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("FEEGOW_HOST_OVERRIDE", srv.URL)
	c := NewFromEnv()

	if _, err := c.Call(ctxWithToken("tok"), "specialties.list", Request{}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !called {
		t.Fatal("NewFromEnv() did not honor FEEGOW_HOST_OVERRIDE")
	}
}

// TestNewFromEnv_NoOverride_UsesRealHost proves the absence of
// FEEGOW_HOST_OVERRIDE is also honored: baseURL falls back to the
// endpoint's declared Host, not an empty string or some other default.
func TestNewFromEnv_NoOverride_UsesRealHost(t *testing.T) {
	t.Setenv("FEEGOW_HOST_OVERRIDE", "")
	c := NewFromEnv()
	got := c.baseURL(HostAPI)
	want := "https://" + string(HostAPI)
	if got != want {
		t.Fatalf("baseURL(HostAPI) = %q, want %q", got, want)
	}
}

// TestCall_DateAndPaginationReachQueryString is an end-to-end sanity
// check that GET requests actually carry the translated date and
// pagination values on the wire, not just in unit-level applyDates /
// EncodePagination calls.
func TestCall_DateAndPaginationReachQueryString(t *testing.T) {
	var gotQuery map[string][]string

	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = map[string][]string(r.URL.Query())
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	start := "2018-08-05"
	end := "2018-08-08"
	_, err := c.Call(ctxWithToken("tok"), "appoints.search", Request{
		Params:     map[string]any{"paciente_id": 100003},
		DateStart:  &start,
		DateEnd:    &end,
		Pagination: &Pagination{Limit: 20, Offset: 40},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	check := func(key, want string) {
		got := gotQuery[key]
		if len(got) != 1 || got[0] != want {
			t.Errorf("query[%q] = %v, want [%q]", key, got, want)
		}
	}
	check("data_start", "05-08-2018")
	check("data_end", "08-08-2018")
	check("start", "40")  // real deslocamento
	check("offset", "20") // Feegow's page-size-named-offset
	check("paciente_id", "100003")
}

// TestCall_POSTSendsJSONBody proves POST endpoints carry their translated
// params as a JSON body (Content-Type application/json), not a query
// string.
func TestCall_POSTSendsJSONBody(t *testing.T) {
	var gotBody map[string]any
	var gotContentType string

	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/new-appoint", func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":{"agendamento_id":1}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	date := "2018-08-08"
	_, err := c.Call(ctxWithToken("tok"), "appoints.new_appoint", Request{
		// "horario" (not "hora" — see appoints.new_appoint's Registry Notes
		// and Fase 0 RELATORIO.md item a.7): Params passes through
		// whatever key a caller sends verbatim, so this only proves
		// passthrough, but it's worth using the field's real, confirmed
		// name rather than the doc's wrong one.
		Params: map[string]any{"paciente_id": float64(5), "horario": "15:00:00"},
		Date:   &date,
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["data"] != "08-08-2018" {
		t.Fatalf(`body["data"] = %v, want "08-08-2018"`, gotBody["data"])
	}
	if gotBody["horario"] != "15:00:00" {
		t.Fatalf(`body["horario"] = %v, want "15:00:00"`, gotBody["horario"])
	}
}

// TestCall_DateRangeTooWide_RejectedBeforeRequest is the client-level half
// of the /appoints/search date-range guard the Fase 0 smoke test
// confirmed against the real API (409 "Intervalo de data deve ser menor
// que 6 meses."): a window wider than MaxRangeDays must be rejected as a
// *DateRangeTooWideError BEFORE any HTTP request is built — the handler
// below fails the test if it's ever reached, proving the rejection really
// happens client-side, not just that the error type happens to match.
func TestCall_DateRangeTooWide_RejectedBeforeRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request reached the server — the too-wide window must be rejected before any HTTP call")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	start := "2024-01-01"
	end := "2026-12-31" // 3 years — well past the 180-day limit
	_, err := c.Call(ctxWithToken("tok"), "appoints.search", Request{
		DateStart: &start,
		DateEnd:   &end,
	})
	if err == nil {
		t.Fatal("Call: got nil error, want a DateRangeTooWideError")
	}

	var rangeErr *DateRangeTooWideError
	if !errors.As(err, &rangeErr) {
		t.Fatalf("error is not a *DateRangeTooWideError: %v (%T)", err, err)
	}
	if rangeErr.MaxRangeDays != 180 {
		t.Fatalf("MaxRangeDays = %d, want 180", rangeErr.MaxRangeDays)
	}
	if !strings.Contains(err.Error(), "180") {
		t.Fatalf("error message %q does not mention the actual limit in a readable way", err.Error())
	}
}

// TestCall_DateRangeWithinLimit_ReachesServer is the positive control: a
// window at or within MaxRangeDays must NOT be rejected client-side.
func TestCall_DateRangeWithinLimit_ReachesServer(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	start := "2026-01-01"
	end := "2026-03-01" // 2 months — within the 180-day limit
	_, err := c.Call(ctxWithToken("tok"), "appoints.search", Request{
		DateStart: &start,
		DateEnd:   &end,
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !called {
		t.Fatal("request never reached the server — a within-limit window must not be rejected")
	}
}

// TestCall_DateRangeExactly180Days_ReachesServer and
// TestCall_DateRangeExactly181Days_Rejected pin the exact cutoff the Fase
// 0 follow-up smoke test measured against the real API for
// /appoints/search: 01-01-2026 to 30-06-2026 is 180 days (accepted by
// Feegow) and one day more, to 01-07-2026, is 181 days (rejected). Before
// the MaxRangeDays fix, the old month-based guard
// (end.After(start.AddDate(0,6,0))) used time.Time.After — a STRICT
// comparison — so exactly 6 months (which happens to land on 01-07-2026
// here) was treated as within range by the client despite the server
// rejecting it; these two tests prove the day-based guard now agrees with
// the server exactly at the boundary in both directions.
func TestCall_DateRangeExactly180Days_ReachesServer(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	start := "2026-01-01"
	end := "2026-06-30" // exactly 180 days — accepted by the real API
	_, err := c.Call(ctxWithToken("tok"), "appoints.search", Request{
		DateStart: &start,
		DateEnd:   &end,
	})
	if err != nil {
		t.Fatalf("Call: %v, want the request to reach the server (180 days is the accepted boundary)", err)
	}
	if !called {
		t.Fatal("request never reached the server — exactly 180 days must not be rejected client-side")
	}
}

func TestCall_DateRangeExactly181Days_Rejected(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request reached the server — 181 days must be rejected before any HTTP call")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	start := "2026-01-01"
	end := "2026-07-01" // exactly 181 days — rejected by the real API
	_, err := c.Call(ctxWithToken("tok"), "appoints.search", Request{
		DateStart: &start,
		DateEnd:   &end,
	})
	var rangeErr *DateRangeTooWideError
	if !errors.As(err, &rangeErr) {
		t.Fatalf("error is not a *DateRangeTooWideError: %v (%T)", err, err)
	}
}

// TestCall_DateRangeMonthEndOverflow_Rejected is the regression test for
// the calendar-arithmetic bug the day-based rewrite fixes: with the old
// start.AddDate(0, 6, 0) guard, Aug 31 + 6 months normalizes past
// February's short month all the way to Mar 3 (2027 is not a leap year),
// so an end date of Feb 28 was treated as within range client-side even
// though it is 181 real days after Aug 31 — a window the real API
// rejects. The day-based guard must reject it too.
func TestCall_DateRangeMonthEndOverflow_Rejected(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request reached the server — the month-end-overflow window (181 real days) must be rejected before any HTTP call")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	start := "2026-08-31"
	end := "2027-02-28" // 181 days after Aug 31 — rejected by the real API,
	// but AddDate(0,6,0) on 2026-08-31 overflows to 2027-03-03, which is
	// AFTER 2027-02-28 — the old guard would have let this reach the server.
	_, err := c.Call(ctxWithToken("tok"), "appoints.search", Request{
		DateStart: &start,
		DateEnd:   &end,
	})
	var rangeErr *DateRangeTooWideError
	if !errors.As(err, &rangeErr) {
		t.Fatalf("error is not a *DateRangeTooWideError: %v (%T)", err, err)
	}
}

// TestCall_DateRangeInverted_Rejected is the regression test for Achado 3:
// validateDateRange did not check Start <= End at all, so a caller-supplied
// inverted window (End before Start) sailed through the width guard
// (trivially "narrower" than any positive limit) and would have reached
// Feegow as a nonsensical request instead of failing fast with a readable
// error.
func TestCall_DateRangeInverted_Rejected(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/appoints/search", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request reached the server — an inverted date range must be rejected before any HTTP call")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	start := "2026-06-01"
	end := "2026-01-01" // End before Start
	_, err := c.Call(ctxWithToken("tok"), "appoints.search", Request{
		DateStart: &start,
		DateEnd:   &end,
	})
	if err == nil {
		t.Fatal("Call: got nil error, want a DateRangeInvertedError")
	}

	var invertedErr *DateRangeInvertedError
	if !errors.As(err, &invertedErr) {
		t.Fatalf("error is not a *DateRangeInvertedError: %v (%T)", err, err)
	}
	if invertedErr.Start != start || invertedErr.End != end {
		t.Fatalf("DateRangeInvertedError = {Start:%q End:%q}, want {Start:%q End:%q}", invertedErr.Start, invertedErr.End, start, end)
	}
}
