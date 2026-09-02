package buildinfo

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestDockerfileStampsTheRightSymbol guards the one failure mode the
// version stamp has that nothing else can catch: a wrong `-X` symbol path.
//
// `go build -ldflags "-X wrong/path.version=v"` is NOT an error. The linker
// silently ignores a stamp it cannot resolve, the build succeeds, and the
// binary reports "dev" — which is exactly the class of silent-wrong-version
// defect this package exists to end. Neither `go build` nor any test that
// sets `version` from inside the package can see it, because the string
// only exists in the Dockerfile.
//
// So the assertion is made where the truth lives: this package's own import
// path, derived at runtime rather than typed as a literal, must appear in
// the Dockerfile's ldflags. Move or rename the package and this fails.
func TestDockerfileStampsTheRightSymbol(t *testing.T) {
	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("reading Dockerfile: %v", err)
	}

	// PkgPath of a type declared here is this package's import path,
	// whatever it currently is — no literal to drift.
	pkg := reflect.TypeOf(struct{}{}).PkgPath()
	if pkg == "" {
		pkg = "github.com/lab019/feegow-mcp/internal/buildinfo"
	}
	want := "-X " + pkg + ".version="

	if !strings.Contains(string(dockerfile), want) {
		t.Fatalf("Dockerfile does not stamp %q.\n"+
			"A wrong -X path is silently ignored by the linker: the image would build fine "+
			"and report %q forever.", want, "dev")
	}
}

// TestDockerfileDeclaresTheBuildArg pins the other half: the ldflags line
// interpolates ${VERSION}, which is empty unless an ARG declares it in this
// build stage. An ARG in the wrong stage yields an empty stamp — again with
// no build error.
func TestDockerfileDeclaresTheBuildArg(t *testing.T) {
	b, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("reading Dockerfile: %v", err)
	}
	content := string(b)

	argAt := strings.Index(content, "ARG VERSION")
	if argAt < 0 {
		t.Fatal("Dockerfile has no `ARG VERSION`; ${VERSION} in the ldflags would always be empty")
	}
	stampAt := strings.Index(content, "${VERSION}")
	if stampAt < 0 {
		t.Fatal("Dockerfile never interpolates ${VERSION} into the build")
	}
	if argAt > stampAt {
		t.Fatal("`ARG VERSION` appears after the RUN that uses ${VERSION}: the stamp would be empty")
	}
}
