package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Claims is the subset of a Feegow bearer token's JWT payload this service
// cares about. Raw carries the full decoded claim set so callers can pull
// an audit-log identity out of whichever field Feegow happens to populate,
// without this package hard-coding Feegow's exact claim names.
type Claims struct {
	// exp is the JWT "exp" claim (Unix seconds since epoch), if present and
	// numeric. nil means the claim was absent or not a number.
	exp *int64
	// Raw is the full decoded JSON claim set.
	Raw map[string]any
}

// DecodeJWTClaims decodes the payload (the second, dot-separated segment)
// of a JWT bearer token.
//
// It NEVER verifies the token's signature. This service is not the issuer
// of Feegow tokens and does not hold Feegow's signing secret — the
// authority over whether a token is genuine and current is Feegow's alone,
// exercised the moment we forward it as "x-access-token" on the actual API
// call. Decoding here is a read of unauthenticated, self-reported claims.
//
// Consequently, the "exp" check built on top of this (see Middleware and
// Claims.Expired) is a UX optimization, never a security control: it only
// lets us return a readable "credencial da clínica expirada" message
// instead of relaying an opaque 401 from Feegow. A malicious or forged
// token stamped with an "exp" far in the future sails straight through
// this check with zero resistance — it gains nothing from doing so, because
// Feegow still rejects it (or accepts it, if it's genuinely a live Feegow
// token) based on its own signature verification, not on anything decided
// here.
//
// If the token is not a well-formed, JSON-payload JWT, DecodeJWTClaims
// returns an error and the caller (Middleware) must fail OPEN on that
// error — i.e. let the request proceed — rather than reject it. Not every
// valid Feegow bearer token needs to be shaped like a JWT for this
// service's purposes; only the exp/identity conveniences degrade if it
// isn't. Rejecting a non-JWT bearer here would make this service the
// authority on token shape, which is exactly the responsibility it does
// not have.
func DecodeJWTClaims(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("not a JWT: expected 3 dot-separated segments, got %d", len(parts))
	}

	// JWT uses base64url without padding (RFC 7519), but tolerate a padded
	// encoding too since it costs nothing and some issuers get this wrong.
	seg := parts[1]
	payload, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(seg)
		if err != nil {
			return nil, fmt.Errorf("decoding JWT payload segment: %w", err)
		}
	}

	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("JWT payload is not a JSON object: %w", err)
	}

	claims := &Claims{Raw: raw}
	if v, ok := raw["exp"].(float64); ok {
		e := int64(v)
		claims.exp = &e
	}
	return claims, nil
}

// Expired reports whether the claims carry an "exp" claim that is in the
// past. Claims with no "exp" claim at all are never considered expired by
// this method — Feegow decides what an absent exp means.
func (c *Claims) Expired() bool {
	if c == nil || c.exp == nil {
		return false
	}
	return time.Unix(*c.exp, 0).Before(time.Now())
}

// identityClaimKeys lists the JWT claim names checked, in order, to build
// an audit-log identity string. This service has no platform org_id of its
// own, so this best-effort identity — whichever of these fields Feegow's
// token happens to carry — is what audit logging has to work with.
var identityClaimKeys = []string{"sub", "user", "usuario", "licenca", "license", "name", "email"}

// Identity returns a best-effort human-readable identity for audit
// logging, pulled from whichever recognized claim is present. It never
// returns the token itself, and it deliberately does not fall back to
// dumping the whole raw claim set, since we don't control what Feegow puts
// in there.
func (c *Claims) Identity() string {
	if c == nil {
		return "unknown"
	}
	for _, key := range identityClaimKeys {
		if v, ok := c.Raw[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return "unknown"
}

// auditLog is the seam tests use to capture (and assert the absence of any
// token in) the audit log line emitted on a successful auth. It defers to
// the standard log package, matching this service's plain stdlib logging
// elsewhere.
func auditLog(format string, args ...any) {
	log.Printf(format, args...)
}
