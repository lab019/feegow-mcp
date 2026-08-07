package feegow

import (
	"fmt"
	"time"
)

// InvalidDateError is returned when a caller-supplied date string does not
// parse as ISO-8601 (YYYY-MM-DD) — the one and only format this package's
// public API accepts. It is returned before any HTTP request is built, so
// a bad date never reaches Feegow (acceptance criterion 2).
type InvalidDateError struct {
	Role  DateRole
	Value string
	Err   error
}

func (e *InvalidDateError) Error() string {
	return fmt.Sprintf("feegow: data inválida para %q (esperado ISO-8601 YYYY-MM-DD): %q: %v", e.Role, e.Value, e.Err)
}

func (e *InvalidDateError) Unwrap() error { return e.Err }

// parseISODate parses value as ISO-8601 (YYYY-MM-DD), wrapping any failure
// in an *InvalidDateError tagged with role so callers get a message that
// names which field was bad.
func parseISODate(role DateRole, value string) (time.Time, error) {
	t, err := time.Parse(ISO8601, value)
	if err != nil {
		return time.Time{}, &InvalidDateError{Role: role, Value: value, Err: err}
	}
	return t, nil
}

// DateRangeTooWideError is returned when a caller's Start–End window is
// wider than the endpoint's declared MaxRangeMonths — checked before any
// HTTP request is built, exactly like InvalidDateError, so a too-wide
// window never turns into Feegow's undocumented 409 "Intervalo de data
// deve ser menor que 6 meses." (confirmed against the real API for
// /appoints/search — see ESPECIFICACAO.md Fase 0 RELATORIO.md item a.4).
// A 409 sanitized by tools.SanitizeFeegowError reads as an opaque
// "conflito ao processar a solicitação"; this error instead names the
// actual, fixable problem, in Portuguese, before any network call is
// made.
type DateRangeTooWideError struct {
	MaxRangeMonths int
}

func (e *DateRangeTooWideError) Error() string {
	return fmt.Sprintf("feegow: o período consultado precisa ser menor que %d meses", e.MaxRangeMonths)
}

// validateDateRange enforces d.MaxRangeMonths against req's DateStart/
// DateEnd pair before any wire translation happens. Declarative, not
// endpoint-specific — see MaxRangeMonths' doc comment in types.go: it
// only fires when both dates are supplied and the descriptor declares a
// limit, and is silent (nil) otherwise, matching applyDates' "dates are
// always optional at the transport level" contract.
func validateDateRange(d EndpointDescriptor, req Request) error {
	if d.MaxRangeMonths <= 0 || req.DateStart == nil || req.DateEnd == nil {
		return nil
	}
	start, err := parseISODate(DateRoleStart, *req.DateStart)
	if err != nil {
		return err
	}
	end, err := parseISODate(DateRoleEnd, *req.DateEnd)
	if err != nil {
		return err
	}
	if end.After(start.AddDate(0, d.MaxRangeMonths, 0)) {
		return &DateRangeTooWideError{MaxRangeMonths: d.MaxRangeMonths}
	}
	return nil
}

// applyDates translates the date fields set on req (already validated as
// ISO-8601) into d's declared wire parameter names/formats, writing the
// results into wire. It is a pure function of the descriptor and request:
// it never special-cases an endpoint by ID, only by what DateParams the
// descriptor declares.
//
// A role present in req but not declared by d is rejected — that is a
// caller bug (asking an endpoint to accept a date it doesn't document),
// not a data problem, and it must never silently vanish from the request.
// A role declared by d but absent from req is simply omitted from wire:
// every date field this package models is optional at the transport
// level, matching doc.txt (fields are "(opcional)" throughout the
// endpoints in Registry).
func applyDates(d EndpointDescriptor, req Request, wire map[string]any) error {
	declared := map[DateRole]DateParam{}
	for _, dp := range d.DateParams {
		declared[dp.Role] = dp
	}

	provided := map[DateRole]*string{
		DateRoleStart:  req.DateStart,
		DateRoleEnd:    req.DateEnd,
		DateRoleSingle: req.Date,
	}

	for role, val := range provided {
		if val == nil {
			continue
		}
		dp, ok := declared[role]
		if !ok {
			return fmt.Errorf("feegow: endpoint %q does not accept a %q date parameter", d.ID, role)
		}
		t, err := parseISODate(role, *val)
		if err != nil {
			return err
		}
		wire[dp.WireName] = t.Format(dp.Format)
	}
	return nil
}
