package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log"
	"testing"
)

// testJWT builds a syntactically valid (but unsigned/fake-signed) JWT
// carrying the given claims as its payload, for use as test fixtures. The
// signature segment is never verified by this package (see
// DecodeJWTClaims), so its content is irrelevant here.
func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshaling test claims: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := base64.RawURLEncoding.EncodeToString([]byte("test-signature-not-verified"))
	return header + "." + payload + "." + sig
}

// captureAuditLog redirects the standard "log" package's output (used by
// auditLog) to a buffer for the duration of the test, restoring the
// previous output on cleanup, and returns that buffer.
func captureAuditLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	})
	return &buf
}
