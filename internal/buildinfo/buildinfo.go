// Package buildinfo resolves the single version string this build reports
// everywhere it identifies itself: the startup log, the --version flag, and
// the "serverInfo.version" an MCP client sees in the initialize handshake.
//
// One resolver, one answer, deliberately. A version literal in the source
// is a second place to remember to bump on every release, and it is the one
// nobody remembers — this service shipped with a hard-coded "0.1.0" in the
// MCP handshake while its releases were already automated SEMVER, so every
// client that ever connected was told the wrong number.
package buildinfo

import "runtime/debug"

// version is stamped at link time with
//
//	-ldflags="-X github.com/lab019/feegow-mcp/internal/buildinfo.version=<v>"
//
// It is how the container image reports itself: the image is built from a
// context with no .git (see .dockerignore), and its own release tag does
// not exist yet at build time — the release workflow computes the SEMVER
// from Conventional Commits *after* this push's image already exists, then
// retags it without rebuilding. So the build stamps the one identifier it
// does have and that is stable forever: the commit, spelled exactly as the
// image tag that carries it (see the Dockerfile and build-push.yml).
var version string

// Version returns what this build calls itself, in descending order of how
// much the answer can be trusted:
//
//  1. the link-time stamp, when the build set one;
//  2. the module version the Go toolchain recorded — `go install
//     github.com/lab019/feegow-mcp@v1.2.3` writes "v1.2.3" here on its own,
//     which is what an end user installing a release gets;
//  3. "dev".
//
// "dev" is a last resort for a plain local `go build` / `go run .`, and it
// is deliberately not a number: an obviously-not-a-release marker beats a
// stale version that reads as official.
func Version() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		// "(devel)" is what the toolchain records for a build that is not
		// from a tagged module version — it carries no more information
		// than the "dev" fallback, and reads worse.
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
