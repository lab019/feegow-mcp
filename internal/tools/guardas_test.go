package tools

import (
	"errors"
	"testing"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// TestValidateNotPast_AcceptsYesterdayUTC is the regression test for the
// "todo dia às 21h-23h59 de Brasília" false rejection: at that local time,
// UTC has already rolled to tomorrow while it is still "hoje" for a patient
// in a timezone behind UTC (e.g. America/Sao_Paulo, UTC-3). A date that is
// exactly one day before UTC "hoje" is the boundary this guard must NEVER
// reject — that is precisely the patient's own today during that window —
// and Feegow itself is the authority that enforces the real boundary
// server-side. Fails under the old strict `t.Before(todayUTC)` guard, which
// rejected this every single day.
func TestValidateNotPast_AcceptsYesterdayUTC(t *testing.T) {
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format(feegow.ISO8601)
	if err := validateNotPast("data", yesterday); err != nil {
		t.Fatalf("validateNotPast(%q) = %v, want nil (boundary day must be accepted, Feegow enforces the real cutoff)", yesterday, err)
	}
}

// TestValidateNotPast_AcceptsTodayAndFuture proves the guard still accepts
// the unambiguous non-past cases.
func TestValidateNotPast_AcceptsTodayAndFuture(t *testing.T) {
	for _, offset := range []int{0, 1, 30} {
		date := time.Now().UTC().AddDate(0, 0, offset).Format(feegow.ISO8601)
		if err := validateNotPast("data", date); err != nil {
			t.Fatalf("validateNotPast(%q) (offset %d) = %v, want nil", date, offset, err)
		}
	}
}

// TestValidateNotPast_RejectsUnambiguouslyPastDate proves the guard still
// catches its actual purpose — a gross caller mistake, unambiguously past in
// every timezone (more than one full day before UTC "hoje") — even after
// being loosened for the one-day boundary above.
func TestValidateNotPast_RejectsUnambiguouslyPastDate(t *testing.T) {
	for _, offset := range []int{-2, -30, -400} {
		date := time.Now().UTC().AddDate(0, 0, offset).Format(feegow.ISO8601)
		err := validateNotPast("data", date)
		var argErr *ArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("validateNotPast(%q) (offset %d) = %v (%T), want *ArgumentError", date, offset, err, err)
		}
	}
}

func TestValidateNotPast_RejectsMalformedDate(t *testing.T) {
	err := validateNotPast("data", "not-a-date")
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("validateNotPast(malformed) = %v (%T), want *ArgumentError", err, err)
	}
}
