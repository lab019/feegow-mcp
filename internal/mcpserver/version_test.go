package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lab019/feegow-mcp/internal/buildinfo"
)

// handshakeVersion connects a client to server over an in-memory transport
// and returns the "serverInfo.version" the initialize handshake reported —
// i.e. exactly the string a real MCP client is told, read back off the
// wire rather than off the constant that produced it.
func handshakeVersion(t *testing.T, server *mcp.Server) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverT, clientT := mcp.NewInMemoryTransports()
	go server.Run(ctx, serverT)

	client := mcp.NewClient(&mcp.Implementation{Name: "version-probe", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	res := session.InitializeResult()
	if res == nil || res.ServerInfo == nil {
		t.Fatal("initialize returned no serverInfo")
	}
	return res.ServerInfo.Version
}

// TestHandshakeVersion_ComesFromTheBuild is the regression guard for the
// defect this file exists because of: both profiles announced a hard-coded
// "0.1.0" to every client that ever connected, while the repo's releases
// were already automated SEMVER — so the number on the wire was simply
// wrong, and silently so.
//
// It asserts the wire value equals what the build itself reports, rather
// than any literal. A literal here would recreate exactly the problem: a
// second place to bump, which nobody bumps.
func TestHandshakeVersion_ComesFromTheBuild(t *testing.T) {
	want := buildinfo.Version()
	if want == "" {
		t.Fatal("buildinfo.Version() is empty; serverInfo.version would be too")
	}

	for _, tc := range []struct {
		profile string
		server  *mcp.Server
	}{
		{"atendimento", newAtendimento()},
		{"admin", newAdmin()},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			if got := handshakeVersion(t, tc.server); got != want {
				t.Fatalf("serverInfo.version = %q, want %q (the build's own version)", got, want)
			}
		})
	}
}

// TestHandshakeVersion_NoHardCodedLiteral pins the property directly: if
// someone reintroduces a literal, serverVersion() stops tracking
// buildinfo.Version() and this fails without needing to guess which
// literal they picked.
func TestHandshakeVersion_NoHardCodedLiteral(t *testing.T) {
	if serverVersion() != buildinfo.Version() {
		t.Fatalf("serverVersion() = %q but the build reports %q: the handshake must not carry its own version literal",
			serverVersion(), buildinfo.Version())
	}
	if serverVersion() == "0.1.0" && buildinfo.Version() != "0.1.0" {
		t.Fatal(`serverVersion() is back to the hard-coded "0.1.0"`)
	}
}

// TestBothProfilesReportTheSameVersion: the two profiles are one binary, so
// a client must never be able to tell them apart by version.
func TestBothProfilesReportTheSameVersion(t *testing.T) {
	if a, b := handshakeVersion(t, newAtendimento()), handshakeVersion(t, newAdmin()); a != b {
		t.Fatalf("atendimento reports %q but admin reports %q; same binary, same version", a, b)
	}
}
