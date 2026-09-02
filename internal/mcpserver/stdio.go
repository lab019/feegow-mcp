package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lab019/feegow-mcp/internal/auth"
)

// Profile names one of the two tool surfaces this service exposes. Over
// HTTP the profile is picked by route (POST /mcp vs POST /mcp/admin); over
// a single-session transport such as stdio there is no route, so the
// operator picks it explicitly when launching the process.
type Profile string

const (
	// ProfileAtendimento is the customer-service surface: the toolset a
	// public, patient-facing agent gets (same as POST /mcp).
	ProfileAtendimento Profile = "atendimento"
	// ProfileAdmin is the superset surface, including financial, inventory
	// and irreversible write operations (same as POST /mcp/admin).
	ProfileAdmin Profile = "admin"
)

// ParseProfile maps the operator-facing spelling of a profile to a Profile,
// rejecting anything else so a typo never silently falls back to a surface
// the operator did not ask for.
func ParseProfile(s string) (Profile, error) {
	switch Profile(s) {
	case ProfileAtendimento, ProfileAdmin:
		return Profile(s), nil
	}
	return "", fmt.Errorf("perfil desconhecido %q: use %q ou %q", s, ProfileAtendimento, ProfileAdmin)
}

// RunStdio serves exactly one MCP session over the process's stdin/stdout,
// exposing the given profile's toolset, and returns when the client
// disconnects or ctx is cancelled.
//
// This is the transport a local MCP client (Claude Code, Claude Desktop,
// Cursor, an SDK script) uses when it spawns the binary itself. There is no
// HTTP request to carry a bearer header on, so the clinic's Feegow token
// comes from the operator that launched the process, and this function
// stamps it on the session's context — the same auth.WithToken slot the
// HTTP middleware fills per request. From there, every tool call resolves
// it via auth.TokenFromContext exactly as it would over HTTP: the SDK
// derives each handler's context from the context Run was called with, so
// a single stamp at connect time covers the whole (single-tenant, single-
// client) session. Nothing else about the tools or the Feegow client
// changes between the two transports.
//
// It fails closed on an empty token: a stdio session with no token would
// otherwise answer tools/list happily and only fail on the first tool call,
// with a less readable error.
func RunStdio(ctx context.Context, profile Profile, token string) error {
	var server *mcp.Server
	switch profile {
	case ProfileAtendimento:
		server = newAtendimento()
	case ProfileAdmin:
		server = newAdmin()
	default:
		return fmt.Errorf("perfil desconhecido %q", profile)
	}
	return runSingleSession(ctx, server, token, &mcp.StdioTransport{})
}

// runSingleSession is RunStdio with the server and transport injected, so
// tests can drive it over an in-memory transport with a probe server
// instead of the real stdio pair and the real tool surface.
func runSingleSession(ctx context.Context, server *mcp.Server, token string, t mcp.Transport) error {
	if token == "" {
		return errors.New("token da clínica ausente: a sessão stdio não abre sem FEEGOW_TOKEN")
	}
	return server.Run(auth.WithToken(ctx, token), t)
}
