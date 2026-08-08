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
| `LOG_LEVEL` | `INFO` | verbosidade — `DEBUG`/`INFO` (ou vazio) habilitam o log de auditoria (`internal/auth`) e o log de request por chamada à Feegow (`internal/feegow`); qualquer outro valor silencia os dois (ver `internal/loglevel`) |
| `FEEGOW_HOST_OVERRIDE` | _(vazio)_ | quando setado, redireciona **todos** os quatro hosts da Feegow (ver `internal/feegow`) para este valor — só teste/CI |

## Testing

```sh
gofmt -l .     # should print nothing
go vet ./...
go build ./...
go test ./...
```

All tests are network-free (`httptest` stands in for both the HTTP layer
and, at the auth layer, for Feegow itself).

### Contract test (optional, hits the real Feegow API)

`internal/feegow/contract_test.go` (`//go:build contract`) is a separate,
opt-in suite that calls the **real** Feegow API and checks that `Registry`
still describes reality. It exists because of three real incidents where the
`httptest`-mocked suite stayed green over a false premise — the same person
who wrote the assumption also wrote the mock that "confirmed" it:
`/patient/search` never returned an `id` (identification would never have
found anyone), "not found" turned out to be HTTP 200 with an empty array
(not an error), and `/appoints/search` silently required `data_start`/
`data_end` alongside `agendamento_id` (ownership checks would have failed
100% of the time). Only a real request exposed each one.

**⚠️ Read the warning block at the top of `contract_test.go` before running
this** — it calls a real license, and `Registry` includes destructive
endpoints (`DELETE .../invoice/remove`, `.../payment/remove`, etc.). By
default only `GET` endpoints are called (with no or empty parameters);
`POST`/`PUT` are only probed with an empty body under an explicit opt-in,
and `DELETE` is **never** probed, under any flag.

```sh
export FEEGOW_CONTRACT_TOKEN='<jwt da clínica>'   # never in a file
go test -tags=contract ./internal/feegow/... -run TestContract -v
```

Excluded from `go build`/`go vet`/`go test` (and therefore CI) by the build
tag; with no `FEEGOW_CONTRACT_TOKEN` set, every test in it just `t.Skip`s.
Prints a summary report at the end (routes that vanished, shape mismatches,
`Verified=false` entries that now pass consistently — promotion candidates
— and known-dead endpoints that came back to life).

## Layout

- `internal/auth` — bearer-token extraction, fail-closed HTTP middleware,
  context plumbing, JWT payload decode (`exp` check + audit identity).
- `internal/feegow` — the normalization layer this service exists for (see
  `ESPECIFICACAO.md` §5): a multi-host client for the Feegow API whose
  public surface always speaks one convention (ISO-8601 dates,
  limit/offset pagination with true offset semantics) no matter how many
  conventions the underlying endpoint actually uses. The translation table
  is `Registry` (`internal/feegow/registry.go`), an executable map keyed by
  a stable `EndpointID` — see the package doc comment for the full design.
  `docs/feegow-api.md` is generated from `Registry`
  (`internal/feegow/docgen.go`); `docgen_test.go` fails the build if the
  two drift apart. `contract_test.go` (build tag `contract`, see "Contract
  test" above) is the opt-in suite that checks `Registry` against the real
  API.
- `internal/loglevel` — makes `LOG_LEVEL` control something real (shared by
  `internal/auth`'s audit log and `internal/feegow`'s request log).
- `internal/mcpserver` — the only package that talks to
  `github.com/modelcontextprotocol/go-sdk/mcp`: the two profiles
  (atendimento/admin), their tool registration points, and the
  streamable-HTTP handlers (stateless mode — see the doc comment on
  `newStreamableHandler` for why that's load-bearing, not a preference).
- `main.go` — process wiring: HTTP server, routing, graceful shutdown.
- `docs/feegow-api.md` — the normalization contract as a table, generated
  from `internal/feegow.Registry`.

Not yet present (later phases, see `ESPECIFICACAO.md` §12):
`internal/tools` (pure tool logic), any actual tool, `Dockerfile`, release
workflow.
