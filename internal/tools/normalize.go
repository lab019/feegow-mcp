package tools

import "strings"

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

// normalizeName folds a full name for comparison: trims, collapses internal
// whitespace runs to a single space, and upper-cases. It intentionally does
// NOT strip accents — Feegow's own examples ("JOSE RENATO BARONI") are
// already unaccented uppercase, and silently folding "É"/"E" together would
// widen the second-fact match beyond what the caller actually typed, which
// is the wrong direction for a security check meant to narrow, not widen.
func normalizeName(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}
