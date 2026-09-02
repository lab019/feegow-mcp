package buildinfo

import "testing"

// withVersion swaps the link-time stamp for the duration of a test, so a
// test can exercise the stamped path without an actual -ldflags build.
func withVersion(t *testing.T, v string) {
	t.Helper()
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

func TestVersion_StampWins(t *testing.T) {
	withVersion(t, "sha-0f5a0f18ef288488d1991c5130528dfb222dad32")
	if got := Version(); got != "sha-0f5a0f18ef288488d1991c5130528dfb222dad32" {
		t.Fatalf("Version() = %q, want the link-time stamp", got)
	}
}

// TestVersion_FallsBackToDev pins the last resort. Under `go test` there is
// no stamp and the main module has no tagged version, so this is the path
// a plain local build takes.
func TestVersion_FallsBackToDev(t *testing.T) {
	withVersion(t, "")
	// A `go test` binary is never built from a tagged module version —
	// the toolchain records "(devel)" or nothing — so this is a hard
	// assertion, not an environment-dependent one. Asserting the literal
	// is the point: an earlier version of this test only logged when the
	// value differed, so renaming the fallback broke nothing.
	if got := Version(); got != "dev" {
		t.Fatalf("Version() = %q with no stamp, want %q", got, "dev")
	}
}

// TestVersion_NeverEmpty guards the property every caller depends on: the
// startup log, --version and the MCP handshake all interpolate this
// directly, and an empty serverInfo.version is a protocol-visible defect.
func TestVersion_NeverEmpty(t *testing.T) {
	for _, stamp := range []string{"", "1.2.3", "sha-abc123"} {
		withVersion(t, stamp)
		if Version() == "" {
			t.Fatalf("Version() is empty with stamp %q", stamp)
		}
	}
}
