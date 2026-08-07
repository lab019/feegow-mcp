package tools

import (
	"fmt"
	"strings"
)

// onlyDigits strips everything but ASCII digits from s. Every doc.txt
// example of a CPF or phone filter (cpf=22222222222, telefone=2155554321)
// is digits-only, so this is what a caller's punctuated input ("123.456.789-01",
// "(21) 5555-4321") gets normalized to before it ever becomes a Feegow query
// parameter.
func onlyDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// argumentDigitsField validates a caller-supplied field that must reduce to
// at least one digit via onlyDigits before it is usable on the wire. Two
// cases are NOT the same and must not be conflated:
//
//   - The caller didn't send this field at all (raw == "") — a normal,
//     silent no-op in a partial PATCH like atualizar_paciente, per its own
//     "send if set" contract.
//   - The caller DID send something, but it reduces to no digits at all
//     (e.g. cpf: "não sei") — a caller mistake, not a field to quietly
//     drop. Dropping it silently would make a result like
//     {"atualizado":true} lie about which of the caller's fields actually
//     made it into the request; rejecting it here means the caller finds
//     out about the mistake, instead of an operador believing a field was
//     saved that Feegow never even received.
func argumentDigitsField(field, raw string) (string, error) {
	digits := onlyDigits(raw)
	if raw != "" && digits == "" {
		return "", &ArgumentError{Msg: fmt.Sprintf("%s não contém nenhum dígito válido: %q", field, raw)}
	}
	return digits, nil
}

// normalizeName folds a full name for comparison: trims, collapses internal
// whitespace runs to a single space, and upper-cases. It intentionally does
// NOT strip accents — Feegow's own examples ("JOSE RENATO BARONI") are
// already unaccented uppercase, and silently folding "É"/"E" together would
// widen the second-fact match beyond what the caller actually typed, which
// is the wrong direction for a security check meant to narrow, not widen.
func normalizeName(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}
