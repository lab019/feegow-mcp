// Package tools holds the atendimento (customer-service) tool logic this
// service exposes: pure functions of (context, *feegow.Client, args) that
// return typed results or errors, with zero dependency on
// modelcontextprotocol/go-sdk. internal/mcpserver is the only package that
// wires these into actual MCP tools — see its package doc comment. Keeping
// the SDK out of this package is what lets every test here run without
// network and without spinning up an MCP session (ESPECIFICACAO.md §4).
package tools

import "errors"

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
