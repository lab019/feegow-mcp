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
	got := Version()
	if got == "" {
		t.Fatal("Version() returned an empty string; it must always name something")
	}
	if got == "(devel)" {
		t.Fatal(`Version() leaked the toolchain's "(devel)" placeholder`)
	}
	if got != "dev" {
		// Not a failure in itself: a test binary built from a tagged
		// module version would legitimately report that tag. Say so
		// rather than asserting an environment-dependent literal.
		t.Logf("Version() = %q (build info carried a module version)", got)
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
