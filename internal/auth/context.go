// Package auth implements the auth contract for this server: it is a
// stateless translator between the agent-runtime and the Feegow API, never
// an authority on its own. The agent-runtime resolves the clinic's Feegow
// token from agent-secrets (by (provider, org_id) — org isolation lives
// there, not here) and forwards it on every request as a plain bearer
// token in the Authorization header. This package extracts that
// per-request bearer token, makes it available to tool handlers via
// context.Context, and forwards it verbatim as Feegow's "x-access-token"
// header (in internal/feegow, a later phase). It never persists a token
// across requests.
//
// The Feegow token is a JWT issued by Feegow to the clinic's license. This
// package decodes its payload — never verifies its signature, see
// DecodeJWTClaims — purely to (a) short-circuit an obviously expired
// credential with a readable message, and (b) extract an identity for
// audit logging, since this service has no platform org_id of its own.
package auth

import (
	"context"
	"net/http"
	"strings"
)

// tokenContextKey is an unexported type so external packages cannot collide
// with, or forge, our context key.
type tokenContextKey struct{}

// WithToken returns a copy of ctx carrying the given bearer token.
func WithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenContextKey{}, token)
}

// TokenFromContext returns the bearer token stored in ctx, if any. The
// second return value is false when no token is present (or it was empty),
// which callers must treat as "unauthenticated" and fail closed.
func TokenFromContext(ctx context.Context) (string, bool) {
	tok, _ := ctx.Value(tokenContextKey{}).(string)
	if tok == "" {
		return "", false
	}
	return tok, true
}

// ExtractBearer parses the standard "Authorization: Bearer <token>" header.
// It returns false if the header is missing, malformed, or the token is
// empty.
func ExtractBearer(h http.Header) (string, bool) {
	v := h.Get("Authorization")
	if v == "" {
		return "", false
	}
	const prefix = "Bearer "
	// Case-insensitive match on the scheme, per RFC 6750/7235.
	if len(v) <= len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(v[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

// Middleware wraps next with fail-closed bearer-token extraction: requests
// without a valid "Authorization: Bearer <token>" header are rejected with
// 401 before ever reaching the MCP handler. Requests that do carry a token
// have it injected into the request's context, where tool handlers can
// read it via TokenFromContext.
//
// It also makes a best-effort decode of the token as a JWT (see
// DecodeJWTClaims): if it decodes and carries an "exp" claim in the past,
// the request is rejected with a readable "credencial da clínica expirada"
// message *before* Feegow is ever called. This check is UX, not security —
// see DecodeJWTClaims' doc comment. If the token does not decode as a JWT
// at all, that is not an error: the middleware still lets the request
// through and leaves the verdict to Feegow, which is the actual authority
// over the token's validity and shape.
//
// The token itself is never logged, in this function or in AuditIdentity.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := ExtractBearer(r.Header)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="feegow-mcp"`)
			http.Error(w, "missing or malformed bearer token: expected \"Authorization: Bearer <feegow-clinic-token>\"", http.StatusUnauthorized)
			return
		}

		if claims, err := DecodeJWTClaims(tok); err == nil {
			if claims.Expired() {
				http.Error(w, "credencial da clínica expirada, recadastre", http.StatusUnauthorized)
				return
			}
			auditLog("feegow-mcp: authenticated request, identity=%q", claims.Identity())
		}
		// err != nil means the bearer wasn't a decodable JWT at all. That is
		// fine: we are not the authority on token shape, only Feegow is —
		// fall through and let the request proceed with no exp check and no
		// identity to log.

		ctx := WithToken(r.Context(), tok)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
