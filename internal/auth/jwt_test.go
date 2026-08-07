package auth

import (
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
