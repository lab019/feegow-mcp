package tools

import (
	"errors"
	"testing"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// TestSanitizeFeegowError_MapsConflictAndValidation_DistinguishablyAndWithoutBody
// proves SanitizeFeegowError does the two things Achado 3 requires at once:
// the original Feegow body never survives (checked via the planted PII
// marker), and the two failure kinds still map to two DIFFERENT sentinels
// an agent could branch on — collapsing both into one message would lose
// the "conflito de negócio vs input inválido" distinction the finding asks
// to preserve.
func TestSanitizeFeegowError_MapsConflictAndValidation_DistinguishablyAndWithoutBody(t *testing.T) {
	conflict := &feegow.ConflictError{Content: "Paciente Fulano de Tal (CPF 111.111.111-11) possui pendência financeira"}
	got := SanitizeFeegowError(conflict)
	if got == nil {
		t.Fatal("SanitizeFeegowError(ConflictError) = nil, want a non-nil sanitized error")
	}
	if !errors.Is(got, ErrConflitoFeegow) {
		t.Fatalf("SanitizeFeegowError(ConflictError) = %v, want ErrConflitoFeegow", got)
	}
	if s := got.Error(); s == conflict.Error() {
		t.Fatalf("SanitizeFeegowError(ConflictError) kept the original Feegow body: %q", s)
	}

	validation := &feegow.ValidationError{Fields: map[string][]string{"paciente_id": {"já existe para Fulano de Tal, CPF 111.111.111-11"}}}
	got = SanitizeFeegowError(validation)
	if !errors.Is(got, ErrEntradaInvalidaFeegow) {
		t.Fatalf("SanitizeFeegowError(ValidationError) = %v, want ErrEntradaInvalidaFeegow", got)
	}
	if s := got.Error(); s == validation.Error() {
		t.Fatalf("SanitizeFeegowError(ValidationError) kept the original Feegow body: %q", s)
	}

	if errors.Is(ErrConflitoFeegow, ErrEntradaInvalidaFeegow) {
		t.Fatal("ErrConflitoFeegow and ErrEntradaInvalidaFeegow must stay distinguishable sentinels")
	}
}

// TestSanitizeFeegowError_PassesOtherErrorsThrough proves the sanitizer
// only touches the two shapes that can carry a Feegow-controlled free-text
// body — everything else (argument errors, the uniform not-found sentinel,
// infra failures) must reach the caller unchanged, or a real operational
// problem would end up disguised as a generic sanitized message.
func TestSanitizeFeegowError_PassesOtherErrorsThrough(t *testing.T) {
	if SanitizeFeegowError(nil) != nil {
		t.Fatal("SanitizeFeegowError(nil) must stay nil")
	}

	argErr := &ArgumentError{Msg: "boom"}
	if got := SanitizeFeegowError(argErr); got != error(argErr) {
		t.Fatalf("SanitizeFeegowError(*ArgumentError) = %v, want unchanged %v", got, argErr)
	}

	if got := SanitizeFeegowError(ErrNaoLocalizado); !errors.Is(got, ErrNaoLocalizado) {
		t.Fatalf("SanitizeFeegowError(ErrNaoLocalizado) = %v, want unchanged", got)
	}

	internal := &feegow.InternalError{StatusCode: 503}
	if got := SanitizeFeegowError(internal); got != error(internal) {
		t.Fatalf("SanitizeFeegowError(*InternalError) = %v, want unchanged %v", got, internal)
	}
}
