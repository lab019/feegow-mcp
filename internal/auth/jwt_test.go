package auth

import (
	"strings"
	"testing"
	"time"
)

func TestDecodeJWTClaims_ValidToken(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	tok := testJWT(t, map[string]any{"exp": future, "sub": "clinica-42"})

	claims, err := DecodeJWTClaims(tok)
	if err != nil {
		t.Fatalf("DecodeJWTClaims: %v", err)
	}
	if claims.Expired() {
		t.Fatalf("claims with future exp reported as expired")
	}
	if got := claims.Identity(); got != "clinica-42" {
		t.Fatalf("Identity() = %q, want %q", got, "clinica-42")
	}
}

func TestDecodeJWTClaims_ExpiredToken(t *testing.T) {
	past := time.Now().Add(-time.Hour).Unix()
	tok := testJWT(t, map[string]any{"exp": past})

	claims, err := DecodeJWTClaims(tok)
	if err != nil {
		t.Fatalf("DecodeJWTClaims: %v", err)
	}
	if !claims.Expired() {
		t.Fatalf("claims with past exp not reported as expired")
	}
}

func TestDecodeJWTClaims_NoExpClaim_NeverExpired(t *testing.T) {
	tok := testJWT(t, map[string]any{"sub": "clinica-1"})

	claims, err := DecodeJWTClaims(tok)
	if err != nil {
		t.Fatalf("DecodeJWTClaims: %v", err)
	}
	if claims.Expired() {
		t.Fatalf("claims with no exp claim reported as expired")
	}
}

func TestDecodeJWTClaims_NotAJWT(t *testing.T) {
	cases := []string{
		"opaque-feegow-api-token",
		"only.two-parts",
		"a.b.c.d",
		"",
	}
	for _, tok := range cases {
		if _, err := DecodeJWTClaims(tok); err == nil {
			t.Errorf("DecodeJWTClaims(%q): got nil error, want an error (not a decodable JWT)", tok)
		}
	}
}

func TestDecodeJWTClaims_MalformedBase64Segment(t *testing.T) {
	if _, err := DecodeJWTClaims("header.not-valid-base64!!!.sig"); err == nil {
		t.Fatalf("DecodeJWTClaims with malformed base64 payload: got nil error, want an error")
	}
}

func TestDecodeJWTClaims_PayloadNotJSON(t *testing.T) {
	// "not json" base64url-encoded, so the payload segment decodes fine as
	// bytes but fails to unmarshal as JSON.
	tok := "header.bm90IGpzb24.sig"
	if _, err := DecodeJWTClaims(tok); err == nil {
		t.Fatalf("DecodeJWTClaims with non-JSON payload: got nil error, want an error")
	}
}

func TestClaims_Identity_FallsBackToUnknown(t *testing.T) {
	claims := &Claims{Raw: map[string]any{"unrelated": "field"}}
	if got := claims.Identity(); got != "unknown" {
		t.Fatalf("Identity() = %q, want %q", got, "unknown")
	}
}

func TestClaims_Identity_NilClaimsIsUnknown(t *testing.T) {
	var claims *Claims
	if got := claims.Identity(); got != "unknown" {
		t.Fatalf("Identity() on nil claims = %q, want %q", got, "unknown")
	}
}

func TestClaims_Expired_NilClaimsIsFalse(t *testing.T) {
	var claims *Claims
	if claims.Expired() {
		t.Fatalf("Expired() on nil claims = true, want false")
	}
}

// TestClaims_Identity_TruncatesOversizedClaim is the regression test for
// achado 1 in the Fase 1a review: a forged token's "sub" (or any other
// identity claim — the signature is never verified, see DecodeJWTClaims)
// with an unbounded length must not be logged verbatim, or a single
// request can blow up the audit log.
func TestClaims_Identity_TruncatesOversizedClaim(t *testing.T) {
	huge := strings.Repeat("a", 10_000)
	claims := &Claims{Raw: map[string]any{"sub": huge}}

	got := claims.Identity()

	gotRunes := []rune(got)
	if len(gotRunes) != identityMaxRunes+1 { // +1 for the trailing ellipsis rune
		t.Fatalf("Identity() length = %d runes, want %d (identityMaxRunes + ellipsis)", len(gotRunes), identityMaxRunes+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("Identity() = %q, want a trailing ellipsis marking truncation", got)
	}
	wantPrefix := huge[:identityMaxRunes]
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("Identity() did not preserve the claim's first %d chars", identityMaxRunes)
	}
}

// TestClaims_Identity_ShortClaimUntouched proves truncation only kicks in
// past the cap — a normal-length identity claim must come back byte-for-
// byte identical, with no ellipsis appended.
func TestClaims_Identity_ShortClaimUntouched(t *testing.T) {
	claims := &Claims{Raw: map[string]any{"sub": "clinica-42"}}
	if got := claims.Identity(); got != "clinica-42" {
		t.Fatalf("Identity() = %q, want %q (unmodified)", got, "clinica-42")
	}
}

// TestClaims_Identity_TruncatesOnRunesNotBytes guards against splitting a
// multi-byte UTF-8 character at the truncation boundary, which would
// produce invalid UTF-8 in the audit log line.
func TestClaims_Identity_TruncatesOnRunesNotBytes(t *testing.T) {
	// "á" is 2 bytes in UTF-8 but 1 rune; repeating it identityMaxRunes+50
	// times means a byte-based cut at identityMaxRunes bytes would land
	// mid-character, while a rune-based cut never does.
	huge := strings.Repeat("á", identityMaxRunes+50)
	claims := &Claims{Raw: map[string]any{"sub": huge}}

	got := claims.Identity()

	if !strings.HasSuffix(got, "…") {
		t.Fatalf("Identity() = %q, want a trailing ellipsis", got)
	}
	trimmed := strings.TrimSuffix(got, "…")
	if !isValidUTF8(trimmed) {
		t.Fatalf("Identity() produced invalid UTF-8 by cutting mid-rune: %q", trimmed)
	}
	if got := len([]rune(trimmed)); got != identityMaxRunes {
		t.Fatalf("truncated identity has %d runes, want %d", got, identityMaxRunes)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

// TestAuditLog_GatedByLogLevel is the regression test for achado 2 in the
// Fase 1a review: LOG_LEVEL must actually control something. At a
// non-verbose level, the audit line Middleware emits on successful auth
// must not be written at all.
func TestAuditLog_GatedByLogLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "WARN")
	logOut := captureAuditLog(t)

	auditLog("feegow-mcp: authenticated request, identity=%q", "clinica-42")

	if got := logOut.String(); got != "" {
		t.Fatalf("auditLog wrote output with LOG_LEVEL=WARN: %q, want nothing", got)
	}
}

// TestAuditLog_VerboseByDefault is the complement of
// TestAuditLog_GatedByLogLevel: with LOG_LEVEL unset (the documented
// default) or explicitly "INFO"/"DEBUG", the audit line is still emitted.
func TestAuditLog_VerboseByDefault(t *testing.T) {
	for _, level := range []string{"", "INFO", "DEBUG"} {
		t.Run("LOG_LEVEL="+level, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", level)
			logOut := captureAuditLog(t)

			auditLog("feegow-mcp: authenticated request, identity=%q", "clinica-42")

			if got := logOut.String(); got == "" {
				t.Fatalf("auditLog wrote nothing with LOG_LEVEL=%q, want a log line", level)
			}
		})
	}
}
