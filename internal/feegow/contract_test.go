//go:build contract

// ============================================================================
// !!! ATENÇÃO — LEIA ANTES DE RODAR !!!
//
// Este arquivo faz chamadas HTTP REAIS contra a API da Feegow, usando um
// token de uma licença de verdade (FEEGOW_CONTRACT_TOKEN). Ele NUNCA executa
// escrita de propósito, mas depende de você não apontar essa credencial para
// uma licença de PRODUÇÃO.
//
// O Registry (internal/feegow/registry.go) contém descritores de endpoints
// como DELETE /core/financial/invoice/remove, /core/financial/payment/remove,
// POST /medical-reports/create e POST /patient/upload-base64. Uma suíte que
// "verificasse todos os endpoints" ingenuamente, chamando cada um com dados
// plausíveis, apagaria registro financeiro ou clínico real de um cliente se
// rodada por engano contra produção.
//
// Regras não negociáveis deste arquivo:
//
//  1. Endpoints com Method POST/PUT/DELETE são PULADOS por padrão. Só GET é
//     exercitado na corrida normal (TestContract_Liveness / TestContract_Shape).
//  2. Opt-in explícito via FEEGOW_CONTRACT_PROBE_WRITES=true habilita
//     TestContract_WriteProbe, que sonda POST/PUT com CORPO VAZIO apenas —
//     isso devolve um 4xx nomeando os campos obrigatórios, sem mutar nada.
//     NUNCA envie aqui um payload plausível/preenchido.
//  3. DELETE nunca é sondado, com ou sem opt-in — não existe uma sondagem seg-
//     ura para um método cujo único efeito possível é apagar um registro real.
//  4. O token vem SÓ de env (FEEGOW_CONTRACT_TOKEN) — nunca de um arquivo
//     deste repo — e nunca é logado (nem em erro, nem em -v).
//
// Como rodar:
//
//	export FEEGOW_CONTRACT_TOKEN='<jwt da clínica>'
//	go test -tags=contract ./internal/feegow/... -run TestContract -v
//
//	# opcional, sonda POST/PUT com corpo vazio (ainda sem payload/mutação):
//	export FEEGOW_CONTRACT_PROBE_WRITES=true
//
// Build tag `contract`: este arquivo é invisível para `go build`, `go vet` e
// `go test` normais (e portanto para o CI) — só existe sob
// `-tags=contract`, e mesmo assim faz nada sem o token acima (t.Skip).
// ============================================================================

// Package feegow_test holds the contract test suite: an OPTIONAL suite that
// runs against the real Feegow API and checks that Registry still describes
// reality.
//
// This exists because of three real incidents where the httptest-mocked
// suite stayed green over a false premise, because the same person who
// wrote the assumption also wrote the mock that "confirmed" it:
//
//  1. /patient/search does not return an "id" field — identification would
//     never have found anyone in production, but every mocked test passed.
//  2. "not found" is HTTP 200 with an empty array, not an error — a mock
//     that returned a 404/409 for "not found" tested a shape Feegow never
//     actually sends.
//  3. /appoints/search requires data_start/data_end to be saved alongside
//     agendamento_id — a mocked "ownership check" that never sent real
//     dates would pass 100% of the time while failing every real call.
//
// In all three, only a request against the real API exposed the problem.
// This suite is the net that was missing: it never replaces the fast,
// network-free suite (`go test ./...`), it exists to catch the next
// divergence between what Registry claims and what Feegow actually does.
//
// Two layers of verification:
//
//   - Camera 1 (TestContract_Liveness): calls every GET endpoint in Registry
//     with no parameters and checks the response isn't the fingerprint of a
//     route that no longer exists (422 with an empty message) — the single
//     cheapest, highest-value signal, since it needs no example parameters
//     at all to catch a moved/renamed/removed route.
//   - Camera 2 (TestContract_Shape): for endpoints this file knows a real,
//     working example call for (see shapeCases below — every entry traces
//     back to a specific confirmed call in Registry's own Notes or in the
//     Fase 0 smoke-test report, never invented by analogy), checks that
//     Client.Call — built purely from the Registry descriptor — still
//     succeeds against the real API. Since Client.Call itself gates success
//     on the descriptor's declared SuccessStatus and unwraps the declared
//     Envelope, a successful call here is already proof those two, plus the
//     date format and pagination wire names, are still correct; a failure
//     is proof they no longer are.
//
// Severity follows Registry's own Verified flag: a Verified=true entry that
// no longer matches is a test failure (we asserted a fact and it changed);
// a Verified=false entry that doesn't match is reported, not failed (we
// already declared uncertainty); a Verified=false entry that matches
// consistently is reported as a promotion candidate — this suite is meant
// to be the tool that pays down that declared debt, not just a gate.
package feegow_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lab019/feegow-mcp/internal/auth"
	"github.com/lab019/feegow-mcp/internal/feegow"
)

const (
	tokenEnv       = "FEEGOW_CONTRACT_TOKEN"
	probeWritesEnv = "FEEGOW_CONTRACT_PROBE_WRITES"

	// callSpacing keeps calls at least this far apart so this suite doesn't
	// look like abuse to whatever rate limiting the real API has.
	callSpacing = 200 * time.Millisecond
	// callTimeout bounds a single HTTP call; it is deliberately tighter
	// than Client's own 30s http.Client timeout so a stuck call surfaces as
	// "this call timed out" (network signal, not contract signal — see
	// classify's kindNetwork) well before the whole test run could hang.
	callTimeout = 20 * time.Second
)

var sharedClient = feegow.New(nil, "") // no host override: the real API

// ----------------------------------------------------------------------
// Plumbing: token, pacing, connectivity canary.
// ----------------------------------------------------------------------

// requireToken is the fail-closed entry point every Test function calls
// first. No token, no HTTP call — ever.
func requireToken(t *testing.T) string {
	t.Helper()
	tok := os.Getenv(tokenEnv)
	if tok == "" {
		t.Skipf(
			"pulado: %s não está setado.\n\n"+
				"Este é o teste de contrato — roda contra a API REAL da Feegow (leia o "+
				"cabeçalho de contract_test.go antes de rodar). Para executar:\n\n"+
				"  export %s='<jwt da clínica>'\n"+
				"  go test -tags=contract ./internal/feegow/... -run TestContract -v\n",
			tokenEnv, tokenEnv,
		)
	}
	return tok
}

var (
	paceMu   sync.Mutex
	lastCall time.Time
)

// pace enforces callSpacing between consecutive real HTTP calls, across
// every Test function in this file (they run sequentially, never in
// parallel — no t.Parallel() anywhere in this file, on purpose).
func pace() {
	paceMu.Lock()
	defer paceMu.Unlock()
	if !lastCall.IsZero() {
		if wait := callSpacing - time.Since(lastCall); wait > 0 {
			time.Sleep(wait)
		}
	}
	lastCall = time.Now()
}

// callEndpoint is the single choke point every real call in this file goes
// through: it paces, bounds the call with callTimeout, and attaches the
// bearer token the same way auth.Middleware does for a real request.
func callEndpoint(tok string, id feegow.EndpointID, req feegow.Request) (*feegow.Response, error) {
	pace()
	ctx, cancel := context.WithTimeout(auth.WithToken(context.Background(), tok), callTimeout)
	defer cancel()
	return sharedClient.Call(ctx, id, req)
}

var (
	connOnce   sync.Once
	connOK     bool
	connDetail string
)

// checkConnectivity is a canary, run once per test binary invocation
// (sync.Once), against a lightweight, always-alive, parameterless GET
// (company.list_unity). If even that fails at the network level, the
// problem is this environment's connectivity, not Feegow's contract — every
// Test function in this file calls this first and skips outright rather
// than let 80-odd calls each burn their own timeout for the same reason
// (the "o teste inteiro não pode pendurar" requirement).
func checkConnectivity(t *testing.T, tok string) {
	t.Helper()
	connOnce.Do(func() {
		_, err := callEndpoint(tok, "company.list_unity", feegow.Request{})
		kind, detail := classify(err)
		connOK = kind != kindNetwork
		connDetail = detail
	})
	if !connOK {
		t.Skipf(
			"sem conectividade com a API da Feegow (canário company.list_unity falhou: %s) — "+
				"isto é uma falha de REDE do ambiente, não um sinal de contrato (ver Cuidados no "+
				"prompt desta fase); corrija a conectividade e rode de novo antes de confiar em "+
				"qualquer outro resultado deste pacote",
			connDetail,
		)
	}
}

func sortedIDs(reg map[feegow.EndpointID]feegow.EndpointDescriptor) []feegow.EndpointID {
	ids := make([]feegow.EndpointID, 0, len(reg))
	for id := range reg {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func strp(s string) *string { return &s }

// ----------------------------------------------------------------------
// Error classification: turns whatever Client.Call returned into a coarse
// bucket this file can reason about and report on.
// ----------------------------------------------------------------------

type outcomeKind int

const (
	kindSuccess outcomeKind = iota
	kindValidation
	kindConflict
	kindCredential
	kindInternal
	kindRouteNotFound
	kindUnexpectedStatus
	// kindNetwork is anything that isn't one of feegow's typed errors — a
	// transport failure, a context deadline, an unknown-endpoint-id bug in
	// this test file, etc. Never a contract signal on its own; see the
	// "Falha de rede é t.Skip" rule.
	kindNetwork
)

func (k outcomeKind) String() string {
	switch k {
	case kindSuccess:
		return "success"
	case kindValidation:
		return "validation(422)"
	case kindConflict:
		return "conflict(409)"
	case kindCredential:
		return "credential(401/403)"
	case kindInternal:
		return "internal(5xx)"
	case kindRouteNotFound:
		return "route-not-found(422 vazio)"
	case kindUnexpectedStatus:
		return "unexpected-status(genérico)"
	default:
		return "network/outro"
	}
}

// classify maps a Client.Call error to one of the buckets above. It never
// inspects PII: every branch here reads only the typed error's own fields
// (status codes, field NAMES from ValidationError, static message text) —
// see each error type's doc comment in errors.go for why none of that is
// patient data.
func classify(err error) (outcomeKind, string) {
	if err == nil {
		return kindSuccess, "200/success"
	}
	var validationErr *feegow.ValidationError
	var conflictErr *feegow.ConflictError
	var credentialErr *feegow.CredentialError
	var internalErr *feegow.InternalError
	var routeNotFoundErr *feegow.RouteNotFoundError
	var unexpectedErr *feegow.UnexpectedStatusError

	switch {
	case errors.As(err, &routeNotFoundErr):
		return kindRouteNotFound, err.Error()
	case errors.As(err, &validationErr):
		return kindValidation, err.Error()
	case errors.As(err, &conflictErr):
		return kindConflict, err.Error()
	case errors.As(err, &credentialErr):
		return kindCredential, err.Error()
	case errors.As(err, &internalErr):
		return kindInternal, err.Error()
	case errors.As(err, &unexpectedErr):
		return kindUnexpectedStatus, err.Error()
	default:
		return kindNetwork, err.Error()
	}
}

// ----------------------------------------------------------------------
// Report: accumulated across every Test function, printed once at the end
// by TestMain regardless of pass/fail — this is the deliverable, not just
// a pass/fail gate.
// ----------------------------------------------------------------------

type record struct {
	id       feegow.EndpointID
	verified bool
	kind     outcomeKind
	detail   string
	basis    string
}

type contractReport struct {
	mu       sync.Mutex
	liveness []record
	shape    []record
	writes   []record
	dead     []record
}

var globalReport contractReport

func (r *contractReport) addLiveness(rec record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.liveness = append(r.liveness, rec)
}

func (r *contractReport) addShape(rec record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shape = append(r.shape, rec)
}

func (r *contractReport) addWrite(rec record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, rec)
}

func (r *contractReport) addDead(rec record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dead = append(r.dead, rec)
}

// print renders the human-readable summary. Deliberately plain fmt.Println
// (not t.Log): it runs from TestMain, after every *testing.T has already
// finished, and it must show up even when every individual test was
// skipped so a human sees WHY nothing ran.
func (r *contractReport) print() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.liveness) == 0 && len(r.shape) == 0 && len(r.dead) == 0 && len(r.writes) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("======================================================================")
	fmt.Println(" feegow-mcp — RELATÓRIO DO TESTE DE CONTRATO (dados reais, sem PII)")
	fmt.Println("======================================================================")

	// --- Camera 1: liveness -------------------------------------------------
	var aliveVerified, aliveUnverified int
	var routeGoneVerified, routeGoneUnverified []feegow.EndpointID
	var unclassified []string
	for _, rec := range r.liveness {
		switch rec.kind {
		case kindRouteNotFound:
			if rec.verified {
				routeGoneVerified = append(routeGoneVerified, rec.id)
			} else {
				routeGoneUnverified = append(routeGoneUnverified, rec.id)
			}
		case kindNetwork:
			// excluded from the alive/gone counts entirely — not a signal.
		default:
			if rec.verified {
				aliveVerified++
			} else {
				aliveUnverified++
			}
			if rec.kind == kindUnexpectedStatus {
				unclassified = append(unclassified, fmt.Sprintf("  - %s: %s", rec.id, rec.detail))
			}
		}
	}
	fmt.Printf("\nCamada 1 — liveness (%d endpoints GET testados):\n", len(r.liveness))
	fmt.Printf("  vivos: %d verified, %d unverified\n", aliveVerified, aliveUnverified)
	if len(routeGoneVerified) > 0 {
		fmt.Printf("  ROTA SUMIU (Verified=true, FALHOU o teste): %v\n", routeGoneVerified)
	}
	if len(routeGoneUnverified) > 0 {
		fmt.Printf("  rota sumiu (Verified=false, só reportado): %v\n", routeGoneUnverified)
	}
	if len(unclassified) > 0 {
		fmt.Printf("  formatos de erro NÃO reconhecidos por classify*() (%d) — candidato a novo case:\n%s\n",
			len(unclassified), strings.Join(unclassified, "\n"))
	}

	// --- Camera 2: shape ------------------------------------------------
	var shapeOK int
	var mismatches, promotions []string
	for _, rec := range r.shape {
		if rec.kind == kindNetwork {
			continue
		}
		if rec.kind == kindSuccess {
			shapeOK++
			if !rec.verified {
				promotions = append(promotions, fmt.Sprintf("  - %s (%s)", rec.id, rec.basis))
			}
			continue
		}
		tag := "report, Verified=false"
		if rec.verified {
			tag = "FALHOU, Verified=true"
		}
		mismatches = append(mismatches, fmt.Sprintf("  - %s [%s]: chamada de exemplo (%s) devolveu %s: %s",
			rec.id, tag, rec.basis, rec.kind, rec.detail))
	}
	fmt.Printf("\nCamada 2 — shape (%d casos de exemplo conhecidos):\n", len(r.shape))
	fmt.Printf("  bateram com o descritor: %d\n", shapeOK)
	if len(mismatches) > 0 {
		fmt.Printf("  divergências:\n%s\n", strings.Join(mismatches, "\n"))
	}
	if len(promotions) > 0 {
		fmt.Printf("  candidatos a promoção (Verified=false -> true, bateram de forma consistente):\n%s\n",
			strings.Join(promotions, "\n"))
	}

	// --- Known-dead endpoints -------------------------------------------
	var revived []feegow.EndpointID
	for _, rec := range r.dead {
		if rec.kind == kindSuccess {
			revived = append(revived, rec.id)
		}
	}
	if len(r.dead) > 0 {
		fmt.Printf("\nEndpoints conhecidamente mortos (%d checados):\n", len(r.dead))
		if len(revived) > 0 {
			fmt.Printf("  RESSUSCITARAM (reavaliar Registry): %v\n", revived)
		} else {
			fmt.Println("  continuam todos mortos, como o esperado")
		}
	}

	// --- Write probe (opt-in) --------------------------------------------
	if len(r.writes) > 0 {
		var writeRouteGone []feegow.EndpointID
		for _, rec := range r.writes {
			if rec.kind == kindRouteNotFound {
				writeRouteGone = append(writeRouteGone, rec.id)
			}
		}
		fmt.Printf("\nSondagem de escrita, corpo vazio (%d endpoints POST/PUT, opt-in):\n", len(r.writes))
		if len(writeRouteGone) > 0 {
			fmt.Printf("  rota sumiu: %v\n", writeRouteGone)
		} else {
			fmt.Println("  todas as rotas responderam (nenhuma sumiu)")
		}
	} else {
		fmt.Printf("\nSondagem de escrita: PULADA (defina %s=true para habilitar; DELETE nunca é sondado)\n", probeWritesEnv)
	}

	fmt.Println("======================================================================")
}

func TestMain(m *testing.M) {
	code := m.Run()
	globalReport.print()
	os.Exit(code)
}

// ----------------------------------------------------------------------
// Camera 1 — liveness + error classification, every GET endpoint.
// ----------------------------------------------------------------------

// knownDeadIDs lists endpoints Registry's own Notes already document as
// confirmed dead in this environment (wrong/unreachable host, 404 route).
// They are excluded from the generic liveness loop below and get their own
// check (TestContract_KnownDeadStayDead) with the OPPOSITE polarity: for
// these, an error is the healthy, expected outcome and a SUCCESS is the
// noteworthy one.
//
// The four HostCoreBR entries here are POST — normally excluded from any
// unprobed call by the write-safety rule — but calling them is safe
// regardless of method specifically BECAUSE core.feegow.com.br does not
// resolve in this environment (confirmed by Fase 0): DNS resolution fails
// before a single byte is sent, so there is no server on the other end to
// mutate. If that host ever starts resolving, this call would then reach
// a real POST endpoint — which is exactly the "ressuscitou" signal this
// check exists to catch, surfaced as a report, not a silent write.
var knownDeadIDs = map[feegow.EndpointID]bool{
	"financial.dmed":               true, // GET, HostAPI — 404 in this env (route disabled).
	"financial.private_table_list": true, // GET, HostCore — 404 in this env (route disabled).
	"stock.location_list":          true, // POST, HostCoreBR — host does not resolve.
	"stock.product_entry":          true, // POST, HostCoreBR — host does not resolve.
	"stock.product_movement":       true, // POST, HostCoreBR — host does not resolve.
	"stock.product_exit":           true, // POST, HostCoreBR — host does not resolve.
}

func TestContract_Liveness(t *testing.T) {
	tok := requireToken(t)
	checkConnectivity(t, tok)

	tested := 0
	for _, id := range sortedIDs(feegow.Registry) {
		d := feegow.Registry[id]
		if knownDeadIDs[id] {
			continue // see TestContract_KnownDeadStayDead
		}
		if d.Method != http.MethodGet {
			continue // POST/PUT/DELETE: never probed here, see TestContract_WriteProbe
		}
		tested++

		t.Run(string(id), func(t *testing.T) {
			_, err := callEndpoint(tok, id, feegow.Request{})
			kind, detail := classify(err)
			globalReport.addLiveness(record{id: id, verified: d.Verified, kind: kind, detail: detail})

			switch kind {
			case kindNetwork:
				t.Skipf("falha de rede chamando %s — não é sinal de contrato: %s", id, detail)

			case kindRouteNotFound:
				msg := fmt.Sprintf(
					"%s: resposta bate o fingerprint de rota inexistente (422 corpo vazio) — "+
						"Host/Path/Method deste descritor pode ter mudado ou sumido", id)
				if d.Verified {
					t.Error(msg)
				} else {
					t.Logf("REPORT (Verified=false): %s", msg)
				}

			default:
				// Alive: success or ANY classified error proves the route
				// exists (only RouteNotFoundError means it doesn't).
				// kindUnexpectedStatus is logged, never failed here: some
				// endpoints have a Feegow-side quirk already documented in
				// Registry's Notes (e.g. procedures.groups' empty-catalog
				// SQL error, a real bug on Feegow's side, not ours) that
				// legitimately answers with a status classify*() has no
				// case for. Hard-failing on that would be exactly the
				// "mock encodes the author's own assumption" trap this
				// suite exists to avoid — it gets reported instead, so a
				// human decides whether classify*() needs a new case.
				if kind == kindUnexpectedStatus {
					t.Logf("REPORT: %s respondeu com um formato que classify*() não reconhece (%s): %s",
						id, kind, detail)
				}
			}
		})
	}

	if tested == 0 {
		t.Fatal("nenhum endpoint GET elegível foi testado — Registry mudou de forma inesperada?")
	}
}

// ----------------------------------------------------------------------
// Camera 2 — full shape, known example parameters.
// ----------------------------------------------------------------------

// shapeCase is one endpoint this file knows a real, currently-working
// example call for. Every entry's params trace back to a specific call the
// Fase 0 smoke test (or a later phase's Notes on the descriptor itself)
// actually made and got a 2xx from — never invented by analogy with a
// sibling endpoint, same discipline Registry's own doc comment demands of
// itself. This table lives here, not in Registry, because it is a TEST
// fixture (throwaway sandbox record ids), not part of the production
// contract.
//
// Deliberately excluded, with the reason:
//   - procedures.groups / procedures.bundles: no known-good call exists —
//     the sandbox has an empty catalog and Feegow's own backend answers
//     that with a raw SQL error (400), a real bug on Feegow's side per the
//     Fase 0 report, not something this table can route around honestly.
//   - financial.search_supplier: every fornecedor_id tried, including a
//     real one, hits Feegow's own "Undefined index: id" bug — there is no
//     example call that succeeds to assert on.
//   - proposal.list_dates, medical_reports.get_laudos_list,
//     medical_reports.search: Registry's own Notes say every parameter
//     combination tried landed on the same error — no known-good example.
type shapeCase struct {
	id    feegow.EndpointID
	req   feegow.Request
	basis string // which confirmed real call this reproduces
}

var shapeCases = []shapeCase{
	{"appoints.search", feegow.Request{
		DateStart:  strp("2026-01-01"),
		DateEnd:    strp("2026-03-01"),
		Params:     map[string]any{"list_procedures": 1},
		Pagination: &feegow.Pagination{Limit: 1, Offset: 0},
	}, "Fase 0: data_start=01-01-2026&data_end=01-03-2026&list_procedures=1&start=0&offset=1 -> 200"},

	{"appoints.available_schedule", feegow.Request{
		DateStart: strp("2026-08-08"),
		DateEnd:   strp("2026-08-10"),
		Params:    map[string]any{"tipo": "P", "procedimento_id": 1, "unidade_id": 0},
	}, "Fase 0: tipo=P&procedimento_id=1&unidade_id=0&data_start=08-08-2026&data_end=10-08-2026 -> 200"},

	{"appoints.status", feegow.Request{}, "Fase 0: sem parâmetros -> 200 (catálogo)"},
	{"appoints.motives", feegow.Request{}, "Fase 0: sem parâmetros -> 200 (catálogo)"},
	{"appoints.list_channel", feegow.Request{}, "Fase 0: sem parâmetros -> 200 (catálogo)"},

	{"appoints.queue_position", feegow.Request{
		Params: map[string]any{"unidade_id": 0, "tipo_senha": 1},
	}, "Registry Notes: unidade_id=0&tipo_senha=1 -> 200, content real (EnvelopeNone)"},

	{"lock.list", feegow.Request{
		DateStart: strp("2023-05-10"),
		DateEnd:   strp("2023-05-29"),
	}, "Fase 0: date_start=2023-05-10&date_end=2023-05-29 (YYYY-MM-DD) -> 200, content:[] (formato ainda inconclusivo, mas a chamada não erra)"},

	{"patient.search", feegow.Request{
		Params: map[string]any{"paciente_id": 3},
	}, "Fase 0: paciente_id=3 -> 200"},

	{"patient.list", feegow.Request{
		Pagination: &feegow.Pagination{Limit: 5, Offset: 0},
	}, "Fase 0: limit=5&offset=0 -> 200"},

	{"patient.list_dependents", feegow.Request{
		Params: map[string]any{"paciente_id": 1},
	}, "Registry Notes: paciente_id=1 -> 200"},

	{"patient.list_sources", feegow.Request{}, "Registry Notes: sem parâmetros -> 200, 11 origens reais"},
	{"patient.list_privates", feegow.Request{}, "Registry Notes: sem parâmetros -> 200"},
	{"patient.health_programs", feegow.Request{}, "Registry Notes: sem parâmetros -> 200"},

	{"patient.exam_requests", feegow.Request{
		Params: map[string]any{"paciente_id": 1},
	}, "Registry Notes/Fase 0: paciente_id=1 -> 200, content:[]"},

	{"medical_record.timeline", feegow.Request{
		Params: map[string]any{"paciente_id": 3},
	}, "Registry Notes: paciente_id=3 -> 200"},

	{"patient.check_eligibility", feegow.Request{
		Params: map[string]any{"paciente_id": 3},
	}, "Registry Notes: paciente_id=3 -> 200 {success,elegivel,term} (EnvelopeNone)"},

	{"company.list_unity", feegow.Request{}, "Fase 0: sem parâmetros -> 200"},
	{"company.list_local", feegow.Request{}, "Fase 0: sem parâmetros -> 200"},
	{"company.list_unity_v2", feegow.Request{}, "Registry Notes: sem parâmetros -> 200, mesmo shape do v1"},
	{"specialties.list", feegow.Request{}, "Fase 0: sem parâmetros -> 200"},
	{"insurance.list", feegow.Request{}, "Fase 0: sem parâmetros -> 200"},
	{"procedures.list", feegow.Request{}, "Fase 0: sem parâmetros -> 200"},
	{"procedures.types", feegow.Request{}, "Fase 0: sem parâmetros -> 200"},
	{"professional.list", feegow.Request{}, "Fase 0: sem parâmetros -> 200"},

	{"professional.search", feegow.Request{
		Params: map[string]any{"profissional_id": 1},
	}, "Fase 0: profissional_id=1 -> 200"},

	{"benefit.contract_datagrid", feegow.Request{
		Pagination: &feegow.Pagination{Limit: 10, Offset: 0},
	}, "Fase 0: page=1&perPage=10 -> 200, ecoado de volta em page/perPage/pages"},

	{"benefit.plan_datagrid", feegow.Request{
		Pagination: &feegow.Pagination{Limit: 10, Offset: 0},
	}, "Fase 0: page=1&perPage=10 -> 200"},

	{"financial.list_suppliers", feegow.Request{}, "Registry Notes (Fase 4b): sem parâmetros -> 200, content:[]"},

	{"financial.list_medical_transfer", feegow.Request{
		DateStart: strp("2024-01-01"),
		DateEnd:   strp("2026-12-31"),
	}, "Registry Notes (Fase 4b): data_start=01-01-2024&data_end=31-12-2026 -> 200, content:[]"},

	{"financial.list_invoice", feegow.Request{
		DateStart: strp("2024-01-01"),
		DateEnd:   strp("2026-12-31"),
		Params:    map[string]any{"tipo_transacao": "C", "unidade_id": 0},
	}, "Registry Notes (Fase 4b): data_start/data_end + tipo_transacao=C + unidade_id=0 -> 200 real"},

	{"financial.list_sales", feegow.Request{
		DateStart: strp("2024-01-01"),
		DateEnd:   strp("2026-12-31"),
		Params:    map[string]any{"unidade_id": 0},
	}, "Registry Notes (Fase 4b): date_start=2024-01-01&date_end=2026-12-31&unidade_id=0 -> 200 real"},

	{"financial.credit_card_flags", feegow.Request{}, "Registry Notes (Fase 4b): sem parâmetros -> 200, 31 bandeiras reais"},

	{"financial.voucher_list", feegow.Request{
		Pagination: &feegow.Pagination{Limit: 5, Offset: 5}, // -> wire limit=5&page=2
	}, "Registry Notes (Fase 4b): GET ?page=2&limit=5 -> 200, page/limit ecoados de volta"},

	{"proposal.list", feegow.Request{
		DateStart: strp("2026-01-01"),
		DateEnd:   strp("2026-02-01"),
	}, "Registry Notes (Fase 4c): data_inicio=2026-01-01&data_fim=2026-02-01 -> 200 real {success,content:[],total:0}"},

	{"proposal.proposal_url", feegow.Request{
		Params: map[string]any{"proposta_id": 1},
	}, "Registry Notes (Fase 4c): proposta_id=1 -> 200 real {success:true,content:false}"},

	{"medical_reports.get_labs_report_file", feegow.Request{
		Params: map[string]any{"lab_report_id": 1},
	}, "Registry Notes (Fase 4c): lab_report_id=1 -> 200 real {success,content:{status,msg},total}"},

	{"billing.search_guide", feegow.Request{
		Params: map[string]any{"billing_type_id": 2, "insurance_id": 1, "billing": 101010},
	}, "Fase 0: billing_type_id=2&insurance_id=1&billing=101010 -> 200 (Verified=false hoje — Fase 4c só sondou o 422 de corpo vazio, de propósito)"},

	{"reports.list", feegow.Request{}, "Registry Notes (Fase 4c): sem parâmetros -> 200, array JSON puro (EnvelopeNone)"},
	{"employee.list", feegow.Request{}, "Registry Notes (Fase 4c): sem parâmetros -> 200 {success,content:[],total:0}"},
}

func TestContract_Shape(t *testing.T) {
	tok := requireToken(t)
	checkConnectivity(t, tok)

	if len(shapeCases) == 0 {
		t.Fatal("shapeCases está vazio")
	}

	seen := map[feegow.EndpointID]bool{}
	for _, c := range shapeCases {
		if seen[c.id] {
			t.Fatalf("shapeCases tem uma entrada duplicada para %q", c.id)
		}
		seen[c.id] = true

		d, ok := feegow.Registry[c.id]
		if !ok {
			t.Fatalf("shapeCases referencia %q, que não existe mais em Registry — corrija ou remova esta linha", c.id)
		}

		t.Run(string(c.id), func(t *testing.T) {
			resp, err := callEndpoint(tok, c.id, c.req)
			kind, detail := classify(err)
			globalReport.addShape(record{id: c.id, verified: d.Verified, kind: kind, detail: detail, basis: c.basis})

			if kind == kindNetwork {
				t.Skipf("falha de rede chamando %s: %s", c.id, detail)
			}

			if err != nil {
				msg := fmt.Sprintf(
					"%s: chamada de exemplo conhecida (%s) que antes tinha sucesso agora devolve %s: %v "+
						"— SuccessStatus/Envelope/formato de data/paginação do descritor pode ter divergido da API real",
					c.id, c.basis, kind, err)
				if d.Verified {
					t.Error(msg)
				} else {
					t.Logf("REPORT (Verified=false, não promovido): %s", msg)
				}
				return
			}

			// Success. Client.Call already gated this on SuccessStatus and
			// unwrapped it per the declared Envelope — a nil error here is
			// itself the shape confirmation for both of those, plus the
			// date format(s) and pagination wire names this case exercised.
			if !d.Verified {
				t.Logf("REPORT candidato a promoção (Verified=false hoje, bateu com a API real): %s (%s)", c.id, c.basis)
			}

			// Soft, informational-only heuristic: an EnvelopeNone endpoint
			// whose real body ALSO happens to carry both "success" and a
			// non-empty "content" may have migrated to the standard
			// envelope. Never fails — a false positive here (a body that
			// coincidentally has both field names for unrelated reasons)
			// is expected to happen, and is fine, since this is report-only.
			if d.Envelope == feegow.EnvelopeNone && resp != nil {
				var probe struct {
					Success *bool           `json:"success"`
					Content json.RawMessage `json:"content"`
				}
				if json.Unmarshal(resp.Content, &probe) == nil && probe.Success != nil && len(probe.Content) > 0 {
					t.Logf("REPORT: %s é EnvelopeNone, mas o corpo real também tem success+content — "+
						"pode ter migrado para o envelope padrão, vale conferir manualmente", c.id)
				}
			}
		})
	}
}

// ----------------------------------------------------------------------
// Known-dead endpoints: verify they stay dead.
// ----------------------------------------------------------------------

func TestContract_KnownDeadStayDead(t *testing.T) {
	tok := requireToken(t)
	checkConnectivity(t, tok)

	for _, id := range sortedIDs(feegow.Registry) {
		if !knownDeadIDs[id] {
			continue
		}
		t.Run(string(id), func(t *testing.T) {
			_, err := callEndpoint(tok, id, feegow.Request{})
			kind, detail := classify(err)
			globalReport.addDead(record{id: id, verified: feegow.Registry[id].Verified, kind: kind, detail: detail})

			if err == nil {
				t.Errorf(
					"RESSUSCITOU: %s respondeu com SUCESSO — Registry documenta este endpoint como "+
						"morto/inacessível neste ambiente (ver Notes); reavalie a Nota e o campo Verified", id)
				return
			}
			t.Logf("%s continua morto, como esperado (%s: %s)", id, kind, detail)
		})
	}
}

// ----------------------------------------------------------------------
// Write probe (opt-in): empty-body-only sondagem of POST/PUT. DELETE is
// never included here, with or without the opt-in — see the file header.
// ----------------------------------------------------------------------

func TestContract_WriteProbe(t *testing.T) {
	tok := requireToken(t)
	checkConnectivity(t, tok)

	if os.Getenv(probeWritesEnv) != "true" {
		t.Skipf(
			"pulado por padrão — sondagem de escrita (POST/PUT, corpo vazio) só roda com %s=true "+
				"(ver o cabeçalho deste arquivo). DELETE nunca é sondado, com ou sem esta flag.",
			probeWritesEnv,
		)
	}

	tested := 0
	for _, id := range sortedIDs(feegow.Registry) {
		d := feegow.Registry[id]
		if d.Method != http.MethodPost && d.Method != http.MethodPut {
			continue // GET: TestContract_Liveness/Shape. DELETE: never.
		}
		if knownDeadIDs[id] {
			continue // TestContract_KnownDeadStayDead already covers these.
		}
		tested++

		t.Run(string(id), func(t *testing.T) {
			// Request{} with no Params/DateStart/DateEnd/Pagination encodes
			// to an EMPTY JSON body for a POST/PUT — see
			// Client.buildRequest. This is the one safe probe this file
			// performs on a write endpoint: it either returns a 4xx naming
			// required fields, or — for the handful of POST endpoints that
			// are actually filterable reads (financial.current_accounts,
			// financial.cost_center, financial.financial_category,
			// stock.product_position, stock.product_list, per their own
			// Registry Notes) — the full, unfiltered, side-effect-free
			// listing. NEVER fill this in with a plausible payload.
			_, err := callEndpoint(tok, id, feegow.Request{})
			kind, detail := classify(err)
			globalReport.addWrite(record{id: id, verified: d.Verified, kind: kind, detail: detail})

			if kind == kindNetwork {
				t.Skipf("falha de rede chamando %s: %s", id, detail)
			}
			if kind == kindRouteNotFound {
				msg := fmt.Sprintf("%s: corpo vazio bate o fingerprint de rota inexistente", id)
				if d.Verified {
					t.Error(msg)
				} else {
					t.Logf("REPORT (Verified=false): %s", msg)
				}
			}
		})
	}

	if tested == 0 {
		t.Fatal("nenhum endpoint POST/PUT elegível foi testado — Registry mudou de forma inesperada?")
	}
}
