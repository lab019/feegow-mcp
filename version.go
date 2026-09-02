package main

import "runtime/debug"

// buildVersion can be stamped at build time with
// -ldflags="-X main.buildVersion=1.2.3". When it is empty (the plain
// `go build ./...` and `go run .` case), version() falls back to whatever
// the Go toolchain recorded in the binary itself.
var buildVersion string

// version returns the version string this binary reports on startup and to
// `--version`.
//
// It deliberately does NOT hard-code a literal: a constant in the source
// is a second place to remember to bump on every release, and the one
// nobody remembers. `go install github.com/lab019/feegow-mcp@v1.2.3`
// records "v1.2.3" in the build info on its own, so the honest answer is
// already in the binary; a release build can override it with -ldflags.
//
// "dev" is the last resort, for a local `go run .` where neither is
// available — better an obviously-not-a-release marker than a stale
// number that looks official.
func version() string {
	if buildVersion != "" {
		return buildVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
