// Package loglevel makes the LOG_LEVEL environment variable actually
// control something, instead of being read once at startup and printed to
// a log line and nowhere else. It is intentionally tiny and dependency
// free so both internal/auth (audit logging) and internal/feegow (request
// logging) can share the exact same verbosity decision without importing
// each other.
//
// The contract (documented in ESPECIFICACAO.md and README.md): LOG_LEVEL
// defaults to "INFO". At "DEBUG" or "INFO" (case-insensitive; unset counts
// as the "INFO" default), the audit log line on successful auth and the
// per-request log line in internal/feegow are emitted. At any other value
// ("WARN", "ERROR", "SILENT", ...), both are suppressed. Neither line ever
// carries a token or patient payload regardless of level — this package
// only gates *whether* the (already-safe) line is written, never what
// goes into it.
package loglevel

import (
	"os"
	"strings"
)

// Verbose reports whether LOG_LEVEL, as currently set in the process
// environment, permits informational/audit logging. It re-reads the env
// var on every call (rather than caching it at process start) so tests can
// exercise both settings with t.Setenv without needing to reconstruct any
// package state.
func Verbose() bool {
	switch strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "", "INFO", "DEBUG":
		return true
	default:
		return false
	}
}

// WarnEnabled reports whether a WARN-severity line should be emitted, as
// currently configured via LOG_LEVEL. Unlike Verbose — which gates
// informational/audit logging and goes silent for anything other than
// "DEBUG"/"INFO" — a WARN-severity line is an operational alert (e.g. an
// upstream API responding in a shape this service no longer understands),
// so it stays visible at every level down to and including "WARN": the
// default ("INFO"), "DEBUG" and "WARN" itself all keep it on. Only an
// explicit stricter setting ("ERROR") turns it off, matching the usual
// meaning of raising a log threshold above WARN.
func WarnEnabled() bool {
	return strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) != "ERROR"
}
