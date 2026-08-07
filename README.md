# feegow-mcp

MCP server (streamable-HTTP transport) exposing the Feegow Clinic ERP as
tools for the `agent-runtime`. It is a thin, **stateless** proxy: it never
authenticates on its own and never stores a token. See `ESPECIFICACAO.md`
for the full design (normalization contract, tool cut, phases).

## Auth contract

The caller — the `agent-runtime` — resolves the clinic's Feegow token from
`agent-secrets` by `(provider, org_id)` and forwards it on every request as
a bearer token:

```
Authorization: Bearer <feegow-clinic-jwt>
```

This server extracts that token per-request (via HTTP middleware), puts it
on the request's `context.Context`, and forwards it verbatim as Feegow's
`x-access-token` header (in `internal/feegow`, a later phase). Nothing is
cached or persisted across requests — org/tenant isolation lives entirely
in the runtime's `agent-secrets` resolution, not here (see
`ESPECIFICACAO.md` §2).

Requests without a valid `Authorization: Bearer <token>` header are
rejected with `401` before ever reaching MCP dispatch (fail closed).

The token is a JWT. This service decodes **only its payload** (never the
signature — we are not the issuer and don't hold Feegow's signing secret,
so Feegow alone is the authority over the token) to:

1. reject an already-expired token with a readable message
   (`"credencial da clínica expirada, recadastre"`) instead of relaying an
   opaque `401` from Feegow — a UX optimization, not a security control;
2. extract a best-effort identity for audit logging, since this service has
   no platform `org_id` of its own.

A bearer token that isn't a decodable JWT is **not** rejected — it is
forwarded as-is and Feegow decides. The token is never logged, in whole or
in part.

## Profiles

Two independent MCP servers, gated only by which secret the tenant stored
in `agent-secrets` (never by env var or config — see `ESPECIFICACAO.md`
§3):

| Route | Toolset |
| --- | --- |
| `POST /mcp` | atendimento (customer service) |
| `POST /mcp/admin` | admin (superset) |

No tools are registered yet (Fases 2–4). `tools/list` on `/mcp` returns an
empty list today; `internal/mcpserver/server.go` is where both profiles'
tool registration points live.

## Running

```sh
go run .            # listens on :8087 by default
PORT=9000 go run .  # override the port
```

- `GET /healthz` → `200 ok`
- `POST /mcp` / `POST /mcp/admin` → MCP streamable-HTTP endpoints (require
  the bearer header above)

Env vars:

| Variable | Default | Para quê |
| --- | --- | --- |
| `PORT` | `8087` | porta HTTP |
| `LOG_LEVEL` | `INFO` | verbosidade (logado no startup) |

## Testing

```sh
gofmt -l .     # should print nothing
go vet ./...
go build ./...
go test ./...
```

All tests are network-free (`httptest` stands in for both the HTTP layer
and, at the auth layer, for Feegow itself).

## Layout

- `internal/auth` — bearer-token extraction, fail-closed HTTP middleware,
  context plumbing, JWT payload decode (`exp` check + audit identity).
- `internal/mcpserver` — the only package that talks to
  `github.com/modelcontextprotocol/go-sdk/mcp`: the two profiles
  (atendimento/admin), their tool registration points, and the
  streamable-HTTP handlers (stateless mode — see the doc comment on
  `newStreamableHandler` for why that's load-bearing, not a preference).
- `main.go` — process wiring: HTTP server, routing, graceful shutdown.

Not yet present (later phases, see `ESPECIFICACAO.md` §12):
`internal/feegow` (client + normalization), `internal/tools` (pure tool
logic), any actual tool, `Dockerfile`, release workflow.
