// Package mcpserver is the single place where this service talks to the
// modelcontextprotocol/go-sdk API. If that SDK's surface changes, this
// file (and its tests) is the only place that should need to change; tool
// logic itself will live in internal/tools (Fases 2-4), independent of the
// SDK.
//
// This service exposes two distinct MCP servers — "atendimento" (customer
// service) and "admin" — because the toolset itself is the security
// boundary between them: the only robust way to keep an atendimento agent
// from calling an admin-only tool is for that tool to not exist in its
// tools/list at all. See ESPECIFICACAO.md §3.
package mcpserver

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lab019/feegow-mcp/internal/auth"
	"github.com/lab019/feegow-mcp/internal/feegow"
)

const (
	atendimentoServerName = "feegow-mcp"
	adminServerName       = "feegow-mcp-admin"
	serverVersion         = "0.1.0"
)

// newAtendimento builds the MCP server for the atendimento (customer
// service) profile: the tool surface reachable via POST /mcp.
func newAtendimento() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    atendimentoServerName,
		Version: serverVersion,
	}, &mcp.ServerOptions{
		Instructions: "Thin, stateless proxy over the Feegow Clinic ERP API, " +
			"atendimento (customer service) profile. Every call requires the " +
			"caller to forward the clinic's Feegow token as " +
			"\"Authorization: Bearer <token>\"; this server performs no " +
			"authentication of its own and stores no tokens.",
	})
	registerAtendimentoTools(s, feegow.NewFromEnv())
	return s
}

// newAdmin builds the MCP server for the admin profile: the superset tool
// surface reachable via POST /mcp/admin. Its existence — like
// atendimento's — is gated entirely upstream, by whether the tenant's
// agent-runtime AgentSpec grants a specialist the "feegow-admin" server and
// whether the tenant ever stored a feegow_admin secret (see
// ESPECIFICACAO.md §3). This service applies no policy of its own beyond
// the fail-closed bearer-token check every request goes through.
func newAdmin() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    adminServerName,
		Version: serverVersion,
	}, &mcp.ServerOptions{
		Instructions: "Thin, stateless proxy over the Feegow Clinic ERP API, " +
			"admin profile (superset of the atendimento profile, including " +
			"financial, inventory and write operations). Every call requires " +
			"the caller to forward the clinic's Feegow admin token as " +
			"\"Authorization: Bearer <token>\"; this server performs no " +
			"authentication of its own and stores no tokens.",
	})
	registerAdminTools(s, feegow.NewFromEnv())
	return s
}

// newAtendimentoWithClient is like newAtendimento but takes an explicit
// *feegow.Client instead of building one from the process environment.
// Tests use this to point every tool call at an httptest.Server instead of
// Feegow's real hosts, without relying on FEEGOW_HOST_OVERRIDE / t.Setenv.
func newAtendimentoWithClient(client *feegow.Client) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    atendimentoServerName,
		Version: serverVersion,
	}, nil)
	registerAtendimentoTools(s, client)
	return s
}

// registerAtendimentoTools registers every tool exposed on the atendimento
// profile against client. This is the single, obvious place atendimento
// tools get wired in — Fase 3 adds writes on top of Fase 2's reads. See
// tools.go for the actual tool definitions; this file only wires them to
// an *mcp.Server.
func registerAtendimentoTools(s *mcp.Server, client *feegow.Client) {
	registerListarCatalogo(s, client)
	registerBuscarHorariosLivres(s, client)
	registerIdentificarPaciente(s, client)
	registerConsultarAgenda(s, client)
	// Fase 3: escritas de atendimento + as guardas de negócio do §7.
}

// registerAdminTools registers every tool exposed on the admin profile: the
// atendimento set plus everything admin-only. Composing it this way — by
// calling registerAtendimentoTools first — is what makes the "admin is a
// superset of atendimento" contract in ESPECIFICACAO.md §3 structural
// rather than something that can drift out of sync by hand.
func registerAdminTools(s *mcp.Server, client *feegow.Client) {
	registerAtendimentoTools(s, client)
	// Fase 4: tools exclusivas do perfil admin (financeiro, estoque,
	// propostas, laudos, faturamento, relatórios, funcionários, escritas de
	// cartão de benefício).
}

// newStreamableHandler wraps an *mcp.Server in the SDK's streamable-HTTP
// handler, run in "stateless" mode, itself wrapped in the fail-closed
// bearer-token auth middleware.
//
// Stateless mode is required, not just a preference — the same reasoning
// as agent-mcp-google's Handler(): in the SDK's default *stateful* mode, an
// MCP session is connected exactly once (on the request carrying
// "initialize") using that request's context.Context, and every later
// tool call on the same Mcp-Session-Id reuses that original, frozen
// context — including whichever bearer token the auth middleware injected
// into it at connect time. Since every session here shares the same
// underlying *mcp.Server (tool registration is stateless, so the server
// passed in is always the same instance), that would mean a tool call's
// Feegow request uses whichever token happened to be present when the
// session was created, not the token on the request actually making the
// call. That breaks token rotation mid-session and, more seriously, would
// leak one clinic's token to another's requests if an Mcp-Session-Id were
// ever reused across tenants.
//
// In stateless mode, the SDK never reuses a session across requests: every
// HTTP request gets server.Connect called with *that* request's own
// context.Context, and the session is closed again immediately after. That
// fresh session's context is a descendant of that request's
// http.Request.Context(), so auth.TokenFromContext inside a tool handler
// (once tools exist, from Fase 2 on) always resolves to the bearer token
// that specific HTTP request carried — never a stale one from a previous
// request on the same session. It also means the handler's internal
// session map never grows, so there is no idle-session leak either.
func newStreamableHandler(server *mcp.Server) http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})
	return auth.Middleware(h)
}

// Handler returns the /mcp HTTP handler for the atendimento profile: the
// SDK's streamable-HTTP handler (stateless) wrapped in the fail-closed
// bearer-token middleware.
func Handler() http.Handler {
	return newStreamableHandler(newAtendimento())
}

// AdminHandler returns the /mcp/admin HTTP handler for the admin profile,
// built the same way as Handler but with the admin tool superset.
func AdminHandler() http.Handler {
	return newStreamableHandler(newAdmin())
}
