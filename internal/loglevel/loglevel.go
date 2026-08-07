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
