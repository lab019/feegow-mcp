// Package tools holds the atendimento (customer-service) tool logic this
// service exposes: pure functions of (context, *feegow.Client, args) that
// return typed results or errors, with zero dependency on
// modelcontextprotocol/go-sdk. internal/mcpserver is the only package that
// wires these into actual MCP tools — see its package doc comment. Keeping
// the SDK out of this package is what lets every test here run without
// network and without spinning up an MCP session (ESPECIFICACAO.md §4).
package tools

import (
	"errors"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// ErrNaoLocalizado is the single, uniform outcome for every patient-lookup
// failure this package can produce: a CPF/telefone that matches no
// cadastro, and a CPF/telefone that matches a cadastro but whose second
// fact (data de nascimento / nome completo) does not. Deliberately the same
// sentinel value for both — see the package doc on IdentificarPaciente — so
// a caller (and, downstream, an attacker probing with a purchased CPF list)
// can never distinguish "does not exist" from "exists, but you don't know
// the second fact" from the error text, error type, or timing-insensitive
// behavior. Never wrap this error with dynamic content (a CPF, a name): that
// would defeat the entire point of it being a single sentinel.
var ErrNaoLocalizado = errors.New("não consegui localizar esse cadastro")

// ArgumentError reports a caller-side problem with a tool's arguments —
// caught and rejected before any Feegow request is built. It is a distinct
// type from ErrNaoLocalizado (never conflated) because the two mean very
// different things: ArgumentError is "you called this wrong", to be fixed
// by the caller re-reading the tool's schema; ErrNaoLocalizado is "your
// input was well-formed, but no such patient could be confirmed."
type ArgumentError struct {
	Msg string
}

func (e *ArgumentError) Error() string { return e.Msg }

// ErrConflitoFeegow is the sanitized stand-in for any feegow.ConflictError
// (HTTP 409) a tool in this package would otherwise surface. See
// SanitizeFeegowError — never wrap this with the original Feegow body.
var ErrConflitoFeegow = errors.New("feegow: houve um conflito ao processar a solicitação junto à clínica; " +
	"os dados podem estar desatualizados ou já existir um registro equivalente")

// ErrEntradaInvalidaFeegow is the sanitized stand-in for any
// feegow.ValidationError (HTTP 422) a tool in this package would otherwise
// surface. See SanitizeFeegowError — never wrap this with the original
// Feegow body.
var ErrEntradaInvalidaFeegow = errors.New("feegow: um ou mais parâmetros enviados não foram aceitos pela clínica; " +
	"revise os dados informados")

// SanitizeFeegowError is the single choke point every atendimento tool
// that can surface a raw Feegow error routes through before returning to
// its caller. It maps feegow.ConflictError (409, whose Content is Feegow's
// raw free-text error body) and feegow.ValidationError (422, whose Fields
// echoes back field names — both observed by the adversarial review to
// carry patient PII straight from the request, e.g. a name or CPF in a
// "pendência financeira" message) to the two fixed, PII-free sentinels
// above. The caller still learns *which* kind of problem happened — a
// business conflict vs. bad input — just never the Feegow body itself,
// which can and does end up in a public-facing agent transcript
// (ESPECIFICACAO.md §10; the same LGPD reasoning internal/feegow/errors.go
// already applies to 5xx bodies, extended here to 409/422).
//
// Every other error type — ArgumentError, ErrNaoLocalizado,
// feegow.CredentialError, feegow.InternalError,
// feegow.UnexpectedStatusError, a transport error, a decoding error,
// context.Canceled — passes through unchanged: none of those carry a
// Feegow-controlled free-text body, and disguising them would hide a real
// operational problem instead of a privacy one.
//
// A tool wires this in once, at its single outermost error-return point
// (see ConsultarAgenda, BuscarHorariosLivres), rather than case-by-case at
// every client.Call site — so a new Feegow call added later inside that
// function does not have to remember to sanitize anything itself; it
// already flows through this one call on the way out. This is what makes
// the discipline hard to regress, per the adversarial review that flagged
// this gap.
func SanitizeFeegowError(err error) error {
	if err == nil {
		return nil
	}
	var conflict *feegow.ConflictError
	if errors.As(err, &conflict) {
		return ErrConflitoFeegow
	}
	var validation *feegow.ValidationError
	if errors.As(err, &validation) {
		return ErrEntradaInvalidaFeegow
	}
	return err
}
