package feegow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lab019/feegow-mcp/internal/auth"
	"github.com/lab019/feegow-mcp/internal/loglevel"
)

// Request is the canonical shape every call into this package's public
// API takes, regardless of which Feegow endpoint it targets.
//
// Params carries endpoint-specific fields using each endpoint's own wire
// name (e.g. "paciente_id", "profissional_id") — those names are already
// consistent across the API and are not part of the normalization
// problem this package solves, so they pass through unchanged. Only dates
// and pagination — the two axes doc.txt disagrees with itself on from
// endpoint to endpoint — get a typed, translated slot below.
type Request struct {
	Params map[string]any

	// DateStart, DateEnd, Date carry ISO-8601 (YYYY-MM-DD) values for
	// whichever date role(s) the target endpoint declares (see
	// EndpointDescriptor.DateParams). nil means "not provided" — omitted
	// from the request entirely, never sent as an empty string. A non-nil
	// value that isn't valid ISO-8601 is rejected before any request
	// leaves this process (see InvalidDateError).
	DateStart *string
	DateEnd   *string
	Date      *string

	// Pagination carries limit/offset in canonical deslocamento
	// semantics. nil means "let Feegow use its own default page" — valid
	// even for endpoints that do support pagination.
	Pagination *Pagination
}

// Response is what a successful (2xx, envelope-unwrapped) call returns.
// Content is exactly what Feegow's "content" field held (EnvelopeStandard)
// or the endpoint's entire response body (EnvelopeNone) — this package
// does not attempt to further type it, since that is Fase 2+'s job once
// specific tools are built on top of specific endpoints.
type Response struct {
	Content json.RawMessage
}

// Client is the multi-host HTTP client for the Feegow API. Its zero value
// is not usable — construct one with New or NewFromEnv.
type Client struct {
	httpClient   *http.Client
	hostOverride string
}

// New builds a Client using httpClient (a nil value gets a sane default
// timeout) and hostOverride. Passing hostOverride directly (rather than
// only supporting the FEEGOW_HOST_OVERRIDE env var) is what lets tests
// point every Registry host at a single httptest.Server without mutating
// process-global state.
func New(httpClient *http.Client, hostOverride string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{httpClient: httpClient, hostOverride: hostOverride}
}

// NewFromEnv builds a Client the way the running service actually does:
// reading FEEGOW_HOST_OVERRIDE from the process environment. When set,
// EVERY endpoint's host (all four — see ESPECIFICACAO.md §5) is redirected
// to that single value instead of its declared Host; this is the
// documented test/CI stub, never a per-tenant or per-host knob.
func NewFromEnv() *Client {
	return New(nil, os.Getenv("FEEGOW_HOST_OVERRIDE"))
}

// baseURL resolves d's declared Host to the URL prefix an actual request
// is built against: the override when set (used verbatim, expected to
// already carry a scheme, e.g. "http://127.0.0.1:port" from httptest), or
// "https://" + Host otherwise.
func (c *Client) baseURL(host Host) string {
	if c.hostOverride != "" {
		return strings.TrimRight(c.hostOverride, "/")
	}
	return "https://" + string(host)
}

// Call executes one Feegow API call by EndpointID, translating req through
// the endpoint's Registry descriptor on the way out and unwrapping the
// envelope (or classifying the error) on the way back.
//
// It is fail-closed on auth: with no bearer token on ctx (see
// auth.TokenFromContext), no HTTP request is built at all.
func (c *Client) Call(ctx context.Context, id EndpointID, req Request) (*Response, error) {
	d, ok := Registry[id]
	if !ok {
		return nil, fmt.Errorf("feegow: unknown endpoint id %q", id)
	}

	token, ok := auth.TokenFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("feegow: no bearer token in context — refusing to call Feegow (fail-closed)")
	}

	if err := validateDateRange(d, req); err != nil {
		return nil, err
	}

	wire := make(map[string]any, len(req.Params)+4)
	for k, v := range req.Params {
		wire[k] = v
	}
	if err := applyDates(d, req, wire); err != nil {
		return nil, err
	}
	if err := applyPagination(d, req, wire); err != nil {
		return nil, err
	}

	httpReq, err := c.buildRequest(ctx, d, wire, token)
	if err != nil {
		return nil, err
	}

	// Method + path only, deliberately never the query string or JSON
	// body: both can carry patient PII (CPF, name, phone — see the LGPD
	// risk in ESPECIFICACAO.md §10), and the token is never logged either
	// way since it's only ever set as a header, not interpolated here.
	if loglevel.Verbose() {
		log.Printf("feegow: %s %s%s", d.Method, string(d.Host), d.Path)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("feegow: request to %s failed: %w", id, err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("feegow: reading response for %s: %w", id, err)
	}

	wantStatus := http.StatusOK
	if d.SuccessStatus != 0 {
		wantStatus = d.SuccessStatus
	}
	if httpResp.StatusCode != wantStatus {
		callErr := classifyError(httpResp.StatusCode, body)
		// A RouteNotFoundError means THIS SERVICE built a request against a
		// path/method Feegow doesn't recognize — an integration bug, not a
		// caller-supplied bad value (see RouteNotFoundError's doc comment
		// in errors.go). That is worth an operator's attention the same
		// way an unrecognized response shape already is elsewhere in this
		// package, so it gets the same treatment: a WARN line with only
		// method+path (never the query string or body — see the Verbose
		// log line above for why), gated the same way.
		var notFound *RouteNotFoundError
		if errors.As(callErr, &notFound) && loglevel.WarnEnabled() {
			log.Printf("feegow: WARN %s %s%s respondeu 422 com corpo vazio — rota/método provavelmente inexistente (erro de integração, não de input do usuário)", d.Method, string(d.Host), d.Path)
		}
		return nil, callErr
	}
	return parseSuccess(body, d.Envelope)
}

// buildRequest turns wire into an *http.Request per d's Method: GET
// endpoints get a query string, POST endpoints get a JSON body — matching
// every worked example in doc.txt for the endpoints in Registry (GET
// always uses query params, POST always uses a JSON body; nothing in this
// registry's slice of the API mixes the two).
func (c *Client) buildRequest(ctx context.Context, d EndpointDescriptor, wire map[string]any, token string) (*http.Request, error) {
	base := c.baseURL(d.Host) + d.Path

	var httpReq *http.Request
	var err error

	switch d.Method {
	case http.MethodGet:
		q := url.Values{}
		for k, v := range wire {
			s, verr := queryValue(v)
			if verr != nil {
				return nil, fmt.Errorf("feegow: encoding query param %q: %w", k, verr)
			}
			q.Set(k, s)
		}
		full := base
		if enc := q.Encode(); enc != "" {
			full += "?" + enc
		}
		httpReq, err = http.NewRequestWithContext(ctx, http.MethodGet, full, nil)

	default: // POST
		payload, merr := json.Marshal(wire)
		if merr != nil {
			return nil, fmt.Errorf("feegow: encoding request body: %w", merr)
		}
		httpReq, err = http.NewRequestWithContext(ctx, d.Method, base, bytes.NewReader(payload))
		if err == nil {
			httpReq.Header.Set("Content-Type", "application/json")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("feegow: building request: %w", err)
	}

	httpReq.Header.Set("x-access-token", token)
	return httpReq, nil
}

// queryValue formats a Request.Params value for use as a URL query
// parameter. Only the JSON-scalar-ish types a caller would plausibly pass
// are supported; anything else is a caller bug caught here rather than
// silently stringified into something Feegow won't understand.
func queryValue(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case int:
		return strconv.Itoa(t), nil
	case int64:
		return strconv.FormatInt(t, 10), nil
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(t), nil
	default:
		return "", fmt.Errorf("unsupported query value type %T", v)
	}
}

// classifyError maps a non-200 HTTP status to a distinguishable error
// type, per ESPECIFICACAO.md §5's status table. It is the one place that
// knows 403 means "credencial inativa" and not "sem permissão".
func classifyError(status int, body []byte) error {
	switch status {
	case http.StatusUnauthorized:
		return &CredentialError{Reason: CredentialMissing}

	case http.StatusForbidden:
		return &CredentialError{Reason: CredentialInactive}

	case http.StatusConflict:
		// {"success": false, "content": "String do erro"} — a normal
		// envelope. Fall back to the raw body if it doesn't parse that
		// way, so a 409 never gets silently swallowed into an empty
		// message.
		var env struct {
			Success bool   `json:"success"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(body, &env); err == nil && env.Content != "" {
			return &ConflictError{Content: env.Content}
		}
		return &ConflictError{Content: string(body)}

	case http.StatusUnprocessableEntity:
		return classify422(body)

	default:
		if status >= 500 {
			return &InternalError{StatusCode: status}
		}
		// ESPECIFICACAO.md §5: no 404, no 429 in the documented contract.
		return &UnexpectedStatusError{StatusCode: status}
	}
}

// classify422 tells apart the shapes a 422 arrives in (see ValidationError
// and RouteNotFoundError's doc comments in errors.go), confirmed against
// the real API by the Fase 0 and Fase 4c smoke tests:
//
//  1. {"campo": ["mensagem", ...], ...} — a bare Laravel-style map, no
//     envelope. A real per-field validation failure.
//  2. {"success": false, "cod_erro": N, "message": "..."} — used for
//     BOTH a real error with a human-readable message (e.g. "The GET
//     method is not supported for this route...") AND, with an EMPTY
//     message, as the fingerprint of a route/method that does not exist
//     at all: this API answers a wrong path with 422, not 404.
//  3. {"success": false, "message": {"campo": ["mensagem", ...], ...}} —
//     the SAME per-field validation map as shape 1, but nested one level
//     inside "message" instead of sitting bare at the top. Measured by
//     the Fase 4c smoke test against /medical-reports/search (missing
//     agendamento_id) and /medical-reports/create (missing
//     agendamento_id/laudo_base64). Because ValidationError.Message is a
//     *string, unmarshaling this shape the same way shape 2 does fails
//     silently (env.Message stays nil, since a JSON object can't decode
//     into a *string) and used to fall through to the generic,
//     field-name-less UnexpectedStatusError — never leaking the raw body
//     (SanitizeFeegowError never even sees it), but throwing away exactly
//     the field names that would help a caller fix the request.
//  4. {"success": true, "content": {"campo": ["mensagem", ...], ...}} — the
//     same nested-map idea as shape 3, but under "content" instead of
//     "message", and with "success" left at its normal true value despite
//     this being a real 422. Measured by the Fase 4c smoke test against
//     /medical-reports/get-labs-report-file (missing lab_report_id). Since
//     classifyError only reaches classify422 by HTTP status code (422),
//     the misleading success:true here is irrelevant to the classification
//     — see EnvelopeKind's doc comment on why 2xx and error bodies are
//     handled independently.
//
// Shapes 3 and 4 both resolve to the same *ValidationError{Fields: ...} as
// shape 1 — from a caller's point of view this is the identical outcome
// (a field->messages map), just found one level deeper on the wire.
// Anything that matches none of the four falls back to
// UnexpectedStatusError rather than guessing.
func classify422(body []byte) error {
	var fields map[string][]string
	if err := json.Unmarshal(body, &fields); err == nil && len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}

	// Shapes 3 and 4: the field->messages map nested one level inside
	// "message" or "content". Tried before the flat *string check below,
	// since a wrapper key holding an object (this case) must never be
	// mistaken for one holding a plain string message (shape 2) or the
	// RouteNotFoundError fingerprint (shape 2 with an empty string) — an
	// object simply fails to unmarshal into map[string][]string when it
	// isn't shaped like one, so this loop is a no-op for every other 422
	// shape in this function, including the empty-string fingerprint
	// (a JSON string "" fails to unmarshal into a map, same as any other
	// string does).
	for _, wrapperKey := range [...]string{"message", "content"} {
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(body, &wrapper); err != nil {
			continue
		}
		raw, ok := wrapper[wrapperKey]
		if !ok {
			continue
		}
		var nested map[string][]string
		if err := json.Unmarshal(raw, &nested); err == nil && len(nested) > 0 {
			return &ValidationError{Fields: nested}
		}
	}

	var env struct {
		Message *string `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Message != nil {
		if strings.TrimSpace(*env.Message) == "" {
			return &RouteNotFoundError{}
		}
		return &ValidationError{Message: *env.Message}
	}

	return &UnexpectedStatusError{StatusCode: http.StatusUnprocessableEntity}
}

// parseSuccess unwraps a 200 response body per kind.
func parseSuccess(body []byte, kind EnvelopeKind) (*Response, error) {
	if kind == EnvelopeNone {
		return &Response{Content: json.RawMessage(body)}, nil
	}

	var env struct {
		Success bool            `json:"success"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("feegow: decoding response envelope: %w", err)
	}
	if !env.Success {
		// Defensive: doc.txt shows a success:false body documented under a
		// "5xx" example heading, meaning it can't be assumed this shape
		// only ever arrives with a non-200 transport status. Content may
		// be a JSON string ("String do erro") or something else Feegow
		// decided to put there; unwrap it if it's a string, else fall
		// back to the raw JSON.
		var msg string
		if err := json.Unmarshal(env.Content, &msg); err != nil || msg == "" {
			msg = string(env.Content)
		}
		return nil, &ConflictError{Content: msg}
	}
	return &Response{Content: env.Content}, nil
}
