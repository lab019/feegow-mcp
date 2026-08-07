package feegow

import (
	"fmt"
	"sort"
	"strings"
)

// ConflictError is Feegow's 409 shape: {"success": false, "content":
// "String do erro"} — a normal envelope, just with success:false. Content
// is whatever human-readable string Feegow put there (e.g. "Horário
// ocupado", "Paciente não encontrado").
type ConflictError struct {
	Content string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("feegow: conflito (409): %s", e.Content)
}

// ValidationError is a real Feegow 422 — a validation failure on a
// well-formed request against a route that does exist. It carries
// whichever of the two shapes doc.txt (and the Fase 0 smoke test against
// the real API) confirmed this API actually uses for that:
//
//   - Fields: a bare Laravel-style map, e.g.
//     {"paciente_id": ["validation.required"]} — NO envelope at all.
//   - Message: the other 422 shape, {"success":false,"cod_erro":N,
//     "message":"..."} with a non-empty message (e.g. "The GET method is
//     not supported for this route. Supported methods: POST.").
//
// Exactly one of the two is populated per instance — classify422
// (client.go) is the only place that constructs this type. See
// RouteNotFoundError for the sibling shape this is deliberately NOT: the
// same envelope with an EMPTY message, which means the route itself
// doesn't exist and is never a caller-input problem.
type ValidationError struct {
	Fields  map[string][]string
	Message string
}

func (e *ValidationError) Error() string {
	if len(e.Fields) > 0 {
		names := make([]string, 0, len(e.Fields))
		for k := range e.Fields {
			names = append(names, k)
		}
		sort.Strings(names)

		parts := make([]string, 0, len(names))
		for _, name := range names {
			parts = append(parts, fmt.Sprintf("%s: %s", name, strings.Join(e.Fields[name], ", ")))
		}
		return fmt.Sprintf("feegow: entrada inválida (422): %s", strings.Join(parts, "; "))
	}
	if e.Message != "" {
		return fmt.Sprintf("feegow: entrada inválida (422): %s", e.Message)
	}
	return "feegow: entrada inválida (422)"
}

// RouteNotFoundError is Feegow's fingerprint for a route or method that
// does not exist at all: HTTP 422 with body {"success":false,
// "cod_erro":0,"message":""} — an EMPTY message, unlike every real 422
// this API returns (see ValidationError). Confirmed by the Fase 0 smoke
// test: dozens of probes against nonexistent paths all came back with
// this exact empty-message shape, while every genuine validation/method
// error observed had either a populated Fields map or a populated
// Message.
//
// This is an integration bug — this client asked for a path/method
// Feegow doesn't serve — never a caller-supplied bad value, so it must
// NOT be sanitized into "revise os dados informados" the way a real
// feegow.ValidationError is (see tools.SanitizeFeegowError, which
// deliberately does not touch this type — an errors.As for
// *ValidationError never matches it) and it must not send an agent off
// correcting input that was never the problem. Client.Call logs every
// occurrence at WARN, the same way an unrecognized response shape
// already is elsewhere in this package.
type RouteNotFoundError struct{}

func (e *RouteNotFoundError) Error() string {
	return "feegow: rota ou método não encontrado pela API (422 com corpo vazio) — provável erro de integração, não de entrada do usuário"
}

// CredentialReason distinguishes the two ways this service's credential to
// Feegow can be dead. They read almost identically over the wire (both
// 4xx, both about the key) but mean operationally different things, and
// conflating them is exactly the trap ESPECIFICACAO.md §5 warns about:
// 403 is NOT "sem permissão", it is "chave inativa".
type CredentialReason int

const (
	// CredentialMissing is Feegow's 401: the x-access-token header was
	// not sent at all. Should not happen through this client (it always
	// sets the header when a token is present in ctx — see Client.Call),
	// so seeing this in practice points at a bug in this package, not at
	// the caller's credential.
	CredentialMissing CredentialReason = iota
	// CredentialInactive is Feegow's 403: the API key is recognized but
	// has been deactivated clinic-side. This is the one the message must
	// get right — see CredentialError.Error.
	CredentialInactive
)

// CredentialError is Feegow's 401 or 403. Its Error() message is written
// for the reason each status documents (ESPECIFICACAO.md §5): 403 in
// particular must say "credencial inativa, recadastre" and never "sem
// permissão" — the two read as very different problems to an operator.
type CredentialError struct {
	Reason CredentialReason
}

func (e *CredentialError) Error() string {
	switch e.Reason {
	case CredentialInactive:
		return "feegow: credencial da clínica está INATIVA (403) — recadastre a credencial junto à clínica"
	case CredentialMissing:
		return "feegow: chave de API ausente no header x-access-token (401) — recadastre a credencial da clínica"
	default:
		return "feegow: erro de credencial não reconhecido"
	}
}

// InternalError is any 5xx from Feegow. StatusCode is kept for callers
// that want to distinguish 500 from 503, but the response body is
// deliberately NOT captured here: a 5xx body can echo back request data
// (patient PII, per the LGPD risk in ESPECIFICACAO.md §10), and this error
// is exactly the kind of thing that ends up in a tool-call error message
// shown to an agent transcript.
type InternalError struct {
	StatusCode int
}

func (e *InternalError) Error() string {
	return fmt.Sprintf("feegow: erro interno do servidor (%d)", e.StatusCode)
}

// UnexpectedStatusError is any status code this client does not otherwise
// classify. ESPECIFICACAO.md §5 is explicit that the documented contract
// has no 404 and no 429 — either one landing here means Feegow's behavior
// diverged from doc.txt, which is exactly the class of surprise
// ESPECIFICACAO.md §8 says needs a real smoke test, not a guess.
type UnexpectedStatusError struct {
	StatusCode int
}

func (e *UnexpectedStatusError) Error() string {
	return fmt.Sprintf("feegow: status HTTP %d não documentado no contrato (ESPECIFICACAO.md §5 não lista 404 nem 429)", e.StatusCode)
}
