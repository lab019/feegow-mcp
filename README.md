# feegow-mcp

An [MCP](https://modelcontextprotocol.io) server that puts the **Feegow
Clinic** ERP — the Brazilian clinic-management system — in reach of an AI
agent: scheduling, patients, financials, inventory, reports.

It is a stateless translator. No database, no cache, no OAuth, no session.
The clinic's own Feegow token comes in, a normalized result goes out.

```
MCP client (Claude Code, Cursor, your agent)
        │  token da clínica
        ▼
   feegow-mcp  ──►  api.feegow.com
```

## Why this is not a thin wrapper

The proxying is the easy half. The real work is **normalization**.

Feegow's REST API uses three parameter-naming conventions, three pagination
schemes and two date formats, varying *per endpoint within the same group*.
A model does not get that right reliably, and each miss becomes a `422` in
the middle of a conversation with a patient. So the tools expose one
coherent surface — ISO-8601 dates, limit/offset pagination with true offset
semantics — and a per-endpoint translation table does the rest.

That table is [`internal/feegow/registry.go`](internal/feegow/registry.go),
an executable map keyed by a stable `EndpointID` covering 85 endpoints.
[`docs/feegow-api.md`](docs/feegow-api.md) is generated from it, and a test
fails the build if the two drift apart.

The tools are also cut **by task, not by endpoint**. `listar_catalogo(tipo)`
is one tool over ~14 endpoints. A 1:1 mapping would produce a catalog too
large for a model to navigate.

## Two profiles

The toolset itself is the security boundary, so there are two servers, not
one server with a permission flag. The only robust way to stop a
customer-service agent from calling `remover_registro_financeiro` is for
that tool to not exist in its `tools/list` at all.

| Profile | Toolset | For |
| --- | --- | --- |
| `atendimento` | 9 tools | patient-facing scheduling and self-service |
| `admin` | 29 tools (superset) | clinic staff: financials, inventory, reports, records |

**atendimento** — `listar_catalogo`, `buscar_horarios_livres`,
`identificar_paciente`, `consultar_agenda`, `agendar`, `cancelar`,
`remarcar`, `confirmar`, `criar_paciente`.

**admin** — everything above, plus `buscar_pacientes`, `obter_paciente`,
`consultar_paciente_clinico`, `atualizar_paciente`, `anexar_ao_prontuario`,
`atualizar_status_agendamento`, `gerar_senha_atendimento`,
`consultar_financeiro`, `gerenciar_conta`, `gerenciar_voucher`,
`remover_registro_financeiro`, `consultar_estoque`, `movimentar_estoque`,
`gerenciar_propostas`, `consultar_laudos`, `registrar_laudo`,
`gerenciar_faturamento`, `listar_relatorios`, `gerar_relatorio`,
`listar_funcionarios`.

Tool names, descriptions and error messages are in **Portuguese**,
deliberately: the operators and patients on the other end of these
conversations are Brazilian, and so is the ERP.

## Getting the token

Feegow authenticates with an `x-access-token` header. The token is a JWT
issued by the license's **master user**, from the Feegow Clinic web
interface. It carries that user's identity and permissions inside the ERP,
and Feegow — not this server — decides what it may do.

Issue it from a **Feegow user dedicated to the integration**, never someone's
personal login. The ERP's own audit trail then distinguishes what the agent
did from what the receptionist did, and turning the integration off does not
mean disabling a person.

The admin profile is not a setting here: it is simply what a token with
those ERP permissions can reach. A token without financial permissions gets
errors from Feegow on the financial tools, whichever profile it is on.

## Install

```bash
go install github.com/lab019/feegow-mcp@latest
```

Or build from source with `go build .` (Go 1.24+). A container image is
published to `ghcr.io/lab019/feegow-mcp`.

## Run it locally (stdio)

For a single clinic on your own machine, the MCP client spawns the binary
and talks over stdin/stdout. There is no HTTP request to carry a header, so
the token comes from the environment.

```bash
FEEGOW_TOKEN='<jwt da clínica>' feegow-mcp --stdio --profile=atendimento
```

**Claude Code:**

```bash
claude mcp add feegow --env FEEGOW_TOKEN=<jwt da clínica> -- feegow-mcp --stdio
```

**Cursor / Claude Desktop** — in `mcp.json` (or
`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "feegow": {
      "command": "feegow-mcp",
      "args": ["--stdio", "--profile=atendimento"],
      "env": { "FEEGOW_TOKEN": "<jwt da clínica>" }
    }
  }
}
```

Use `--profile=admin` for the full surface. The process serves one session
and exits when the client disconnects. It fails at startup, with a readable
message, if `FEEGOW_TOKEN` is missing.

On this transport **stdout is the JSON-RPC stream** — every log line goes to
stderr, and nothing else may be printed to stdout.

## Run it as a service (HTTP)

For a shared, multi-tenant deployment, run one process and let each caller
bring its own clinic's token. Both profiles are served at once, as routes.

```bash
feegow-mcp            # listens on :8087
PORT=9000 feegow-mcp
```

| Route | |
| --- | --- |
| `POST /mcp` | atendimento profile (streamable-HTTP) |
| `POST /mcp/admin` | admin profile (streamable-HTTP) |
| `GET /healthz` | `200 ok` |

Every request must carry the clinic's token:

```
Authorization: Bearer <jwt da clínica>
```

Requests without a valid bearer are rejected with `401` before reaching MCP
dispatch (fail closed). The token is forwarded verbatim as Feegow's
`x-access-token`. Nothing is cached or persisted across requests — the
handler runs in the SDK's **stateless** mode specifically so that each
request's own token is the one its tool calls use, never one frozen from
whenever the session was created. See the comment on `newStreamableHandler`
in [`internal/mcpserver/server.go`](internal/mcpserver/server.go); it is
load-bearing, not a preference.

There is no per-clinic configuration, and there is no allowlist. Default
unit and default specialty are not settings here: discovery tools resolve
name→id, and the default belongs in your agent's prompt. The clinic's
identity comes from the token itself.

| Variable | Default | |
| --- | --- | --- |
| `FEEGOW_TOKEN` | — | clinic token, `--stdio` only |
| `PORT` | `8087` | HTTP port |
| `LOG_LEVEL` | `INFO` | `DEBUG`/`INFO` enable the audit line and the per-Feegow-call request log; anything else silences both |
| `FEEGOW_HOST_OVERRIDE` | _(empty)_ | redirects **all four** Feegow hosts to this value — test/CI only |

## What this server does with your token

It decodes the JWT's **payload only** — never the signature. This service is
not the issuer and does not hold Feegow's signing secret, so Feegow alone is
the authority on whether a token is genuine. Decoding buys two things:

1. an expired token gets `credencial da clínica expirada, recadastre`
   instead of an opaque `401` relayed from Feegow;
2. a best-effort identity for the audit log.

Both are conveniences, **never security controls** — a forged token with a
far-future `exp` sails through and is then rejected by Feegow itself. A
bearer that is not a decodable JWT is not rejected either; it is forwarded
and Feegow decides.

The token is never logged, in whole or in part.

## Safety properties worth knowing before you point an agent at it

These are decisions baked into the tools, not options:

- **Two facts, never one.** `identificar_paciente` and every atendimento
  tool built on it require CPF+data de nascimento *or* telefone+nome
  completo, both matching. A single fact is not an identity check on a
  public surface: CPF alone is not a secret, and accepting it alone would
  make this a lookup oracle for anyone holding a leaked list. Raw
  `paciente_id` is not accepted on the atendimento profile at all.
- **Writes need explicit human confirmation**, and the guard says whose:
  the patient on atendimento, the clinic's operator on admin. Confirmation
  manufactured by the model does not count.
- **Business-rule guards run before any Feegow call:** no retroactive
  scheduling, `valor` in cents with the conditional `plano=1 → valor=0`
  rule, `unidade_id=0` ("main unit") distinguished from omitted ("all
  units"), and the `agendar` race (`available-schedule` says free,
  `new-appoint` returns `409`) surfaced as a recoverable state rather than a
  failure.
- **Results are minimal by design.** `identificar_paciente` returns which
  cadastro matched and nothing that identifies who it belongs to. Audit
  lines carry the tool name and internal ids only — never CPF, telefone,
  nome or data de nascimento — and they say explicitly that the identity
  behind a write was *claimed*, never verified.

The multi-tenant isolation story is worth stating plainly rather than
overselling: over HTTP this server has no `org_id` and cannot detect a
caller routing the wrong clinic's token. It trusts the caller. What it does
guarantee, and tests, is statelessness — nothing from one request survives
into the next.

## Development

```sh
gofmt -l .   # should print nothing
go vet ./...
go build ./...
go test ./...
```

All tests are network-free; `httptest` stands in for Feegow.

### Contract test (optional, hits the real Feegow API)

[`internal/feegow/contract_test.go`](internal/feegow/contract_test.go)
(build tag `contract`) is an opt-in suite that calls the **real** Feegow API
and checks that `Registry` still describes reality.

It exists because of three real incidents where the mocked suite stayed
green over a false premise — the same person who wrote the assumption also
wrote the mock that "confirmed" it. `/patient/search` never returned an
`id` (identification would never have found anyone); "not found" turned out
to be HTTP 200 with an empty array, not an error; and `/appoints/search`
silently required `data_start`/`data_end` alongside `agendamento_id`
(ownership checks would have failed 100% of the time). Only a real request
exposed each one.

**⚠️ Read the warning block at the top of that file first** — it calls a
real license, and `Registry` includes destructive endpoints. By default only
`GET` endpoints are called; `POST`/`PUT` are probed with an empty body only
under an explicit opt-in, and `DELETE` is **never** probed, under any flag.

```sh
export FEEGOW_CONTRACT_TOKEN='<jwt da clínica>'   # never in a file
go test -tags=contract ./internal/feegow/... -run TestContract -v
```

With no `FEEGOW_CONTRACT_TOKEN`, every test in it skips.

### Layout

| | |
| --- | --- |
| `main.go`, `version.go` | process wiring: flags, both transports, graceful shutdown |
| `internal/mcpserver` | the only package that touches the MCP SDK: the two profiles, tool registration, the HTTP and stdio entry points |
| `internal/tools` | pure tool logic and the business-rule guards |
| `internal/feegow` | the normalization layer: multi-host client + `Registry` |
| `internal/auth` | bearer extraction, fail-closed middleware, context plumbing, JWT payload decode |
| `internal/loglevel` | makes `LOG_LEVEL` control something real |
| `docs/feegow-api.md` | generated from `Registry` |
| `ESPECIFICACAO.md` | the full design, in Portuguese |

## Appendix: the Lab019 platform deployment

This server was built for, and is deployed as part of, the Lab019 agent
platform. Nothing in this section is required to use it anywhere else — it
is one deployment of the generic HTTP mode described above.

There, `feegow-mcp` runs as a single shared container reachable only on the
internal network, registered as two **global** MCP servers on
`agent-runtime` with `auth: byok:feegow` and `auth: byok:feegow_admin`. On
each call the runtime resolves the clinic's token from `agent-secrets` by
`(provider, org_id)` and forwards it as the bearer header this server
expects. Multi-tenant isolation therefore lives in that resolution, not
here.

The admin profile is gated by **data, not configuration**: a tenant that
never stored a `feegow_admin` secret makes the runtime fail closed before
this service is reached. There is no policy env var and no `org_id`
allowlist, because a per-customer knob in env would require an operator
redeploy per clinic and would not scale. Which specialist carries which
server is `tools=["feegow"]` vs `tools=["feegow-admin"]` in the tenant's own
AgentSpec.

`ESPECIFICACAO.md` §2, §3 and §11 document that integration in full.

## License

Copyright 2026 Lab019.

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
