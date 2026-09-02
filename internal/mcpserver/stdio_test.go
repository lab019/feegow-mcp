package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// runOverMemory starts runSingleSession for server in the background over
// an in-memory transport pair and returns the client-side transport plus a
// channel that yields Run's result once the session ends. Cancelling ctx
// tears the session down.
func runOverMemory(t *testing.T, ctx context.Context, server *mcp.Server, token string) (*mcp.InMemoryTransport, <-chan error) {
	t.Helper()
	serverT, clientT := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- runSingleSession(ctx, server, token, serverT) }()
	return clientT, done
}

func connectClient(t *testing.T, ctx context.Context, clientT *mcp.InMemoryTransport) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	return session
}

func waitRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("runSingleSession did not return after the client disconnected")
		return nil
	}
}

// TestRunSingleSession_TokenReachesEveryToolCall is the stdio counterpart
// of the streamable-HTTP token-isolation test: with no HTTP request to
// carry a bearer header, the token stamped once at connect time must be
// what auth.TokenFromContext resolves to inside every tool call of the
// session — not just the first.
func TestRunSingleSession_TokenReachesEveryToolCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const token = "tok-stdio-clinica"
	clientT, done := runOverMemory(t, ctx, newProbeServer(), token)
	session := connectClient(t, ctx, clientT)

	for i := 0; i < 3; i++ {
		if got := callProbe(t, ctx, session); got != token {
			t.Fatalf("call %d: tool saw token %q, want %q", i+1, got, token)
		}
	}

	if err := session.Close(); err != nil {
		t.Fatalf("closing client session: %v", err)
	}
	if err := waitRun(t, done); err != nil {
		t.Fatalf("runSingleSession returned error after clean client disconnect: %v", err)
	}
}

// TestRunSingleSession_FailsClosedOnEmptyToken: an empty token must be
// rejected before the transport is even connected, so a misconfigured
// launch fails at startup with a readable message instead of answering
// tools/list and then failing on the first real call.
func TestRunSingleSession_FailsClosedOnEmptyToken(t *testing.T) {
	serverT, _ := mcp.NewInMemoryTransports()
	err := runSingleSession(context.Background(), newProbeServer(), "", serverT)
	if err == nil {
		t.Fatal("expected an error for an empty token, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Fatalf("error should say a token is missing, got: %v", err)
	}
}

// TestRunSingleSession_StopsOnContextCancel: a stdio process ends on
// SIGINT/SIGTERM by cancelling the context; Run must actually return then,
// otherwise the process would hang with the client already gone.
func TestRunSingleSession_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clientT, done := runOverMemory(t, ctx, newProbeServer(), "tok")
	session := connectClient(t, ctx, clientT)
	defer session.Close()

	cancel()
	err := waitRun(t, done)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run after cancel: got %v, want context.Canceled", err)
	}
}

// TestRunStdio_ProfilesExposeTheRightSurface pins the contract that picking
// a profile on a single-session transport yields the same toolset the
// corresponding HTTP route does: admin is a strict superset of atendimento,
// and the irreversible admin-only tools never appear on atendimento.
func TestRunStdio_ProfilesExposeTheRightSurface(t *testing.T) {
	listTools := func(t *testing.T, server *mcp.Server) map[string]bool {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		clientT, done := runOverMemory(t, ctx, server, "tok")
		session := connectClient(t, ctx, clientT)
		res, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		names := make(map[string]bool, len(res.Tools))
		for _, tool := range res.Tools {
			names[tool.Name] = true
		}
		session.Close()
		waitRun(t, done)
		return names
	}

	// Goes through serverForProfile — the mapping the --profile flag
	// actually performs — instead of calling newAtendimento()/newAdmin()
	// directly. Driving the constructors directly is what let an earlier
	// version of this test pass while the switch in RunStdio was free to
	// be backwards.
	serverFor := func(t *testing.T, p Profile) *mcp.Server {
		t.Helper()
		s, err := serverForProfile(p)
		if err != nil {
			t.Fatalf("serverForProfile(%q): %v", p, err)
		}
		return s
	}
	atendimento := listTools(t, serverFor(t, ProfileAtendimento))
	admin := listTools(t, serverFor(t, ProfileAdmin))

	if len(atendimento) == 0 {
		t.Fatal("atendimento profile exposes no tools")
	}
	for name := range atendimento {
		if !admin[name] {
			t.Errorf("tool %q is on atendimento but missing from admin: admin must be a superset", name)
		}
	}
	if len(admin) <= len(atendimento) {
		t.Fatalf("admin (%d tools) should be strictly larger than atendimento (%d tools)", len(admin), len(atendimento))
	}
	for _, adminOnly := range []string{"remover_registro_financeiro", "buscar_pacientes", "obter_paciente"} {
		if atendimento[adminOnly] {
			t.Errorf("admin-only tool %q leaked into the atendimento profile", adminOnly)
		}
		if !admin[adminOnly] {
			t.Errorf("expected admin-only tool %q on the admin profile", adminOnly)
		}
	}
	for _, shared := range []string{"agendar", "identificar_paciente", "listar_catalogo"} {
		if !atendimento[shared] {
			t.Errorf("expected atendimento tool %q on the atendimento profile", shared)
		}
	}
}

func TestRunStdio_RejectsUnknownProfile(t *testing.T) {
	err := RunStdio(context.Background(), Profile("gerente"), "tok")
	if err == nil || !strings.Contains(err.Error(), "gerente") {
		t.Fatalf("expected an error naming the unknown profile, got: %v", err)
	}
}

func TestParseProfile(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Profile
		ok   bool
	}{
		{"atendimento", ProfileAtendimento, true},
		{"admin", ProfileAdmin, true},
		{"", "", false},
		{"Admin", "", false},
		{"superuser", "", false},
	} {
		got, err := ParseProfile(tc.in)
		if tc.ok && (err != nil || got != tc.want) {
			t.Errorf("ParseProfile(%q) = %q, %v; want %q, nil", tc.in, got, err, tc.want)
		}
		if !tc.ok && err == nil {
			t.Errorf("ParseProfile(%q) accepted, want error", tc.in)
		}
	}
}

// TestServerForProfile_MappingIsNotBackwards is the direct guard on the
// one thing --profile does. It pins each profile to a tool that exists on
// ONLY that surface, so swapping the two arms of the switch fails here
// instead of silently handing a patient-facing agent the irreversible
// financial removals.
func TestServerForProfile_MappingIsNotBackwards(t *testing.T) {
	adminOnly := "remover_registro_financeiro"

	atendimento := listToolsOf(t, ProfileAtendimento)
	if atendimento[adminOnly] {
		t.Fatalf("--profile=atendimento exposes %q: the profile switch is backwards", adminOnly)
	}
	if !atendimento["identificar_paciente"] {
		t.Error(`--profile=atendimento is missing "identificar_paciente"`)
	}

	admin := listToolsOf(t, ProfileAdmin)
	if !admin[adminOnly] {
		t.Fatalf("--profile=admin is missing %q: the profile switch is backwards", adminOnly)
	}
	if len(admin) <= len(atendimento) {
		t.Fatalf("--profile=admin has %d tools, atendimento %d: admin must be the superset",
			len(admin), len(atendimento))
	}
}

// listToolsOf resolves a profile the way RunStdio does and returns the
// names on that surface.
func listToolsOf(t *testing.T, p Profile) map[string]bool {
	t.Helper()
	server, err := serverForProfile(p)
	if err != nil {
		t.Fatalf("serverForProfile(%q): %v", p, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientT, done := runOverMemory(t, ctx, server, "tok")
	session := connectClient(t, ctx, clientT)
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	names := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	session.Close()
	waitRun(t, done)
	return names
}

// TestServerForProfile_RejectsUnknown keeps the default arm fail-closed:
// an unrecognised profile must never quietly resolve to a toolset.
func TestServerForProfile_RejectsUnknown(t *testing.T) {
	if s, err := serverForProfile(Profile("gerente")); err == nil || s != nil {
		t.Fatalf("serverForProfile(\"gerente\") = %v, %v; want nil, error", s, err)
	}
}
