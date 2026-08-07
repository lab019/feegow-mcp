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
// wider than the endpoint's declared MaxRangeDays — checked before any
// HTTP request is built, exactly like InvalidDateError, so a too-wide
// window never turns into Feegow's undocumented 409 "Intervalo de data
// deve ser menor que 6 meses." (confirmed against the real API for
// /appoints/search). The limit is tracked and enforced in DAYS, not
// months — see MaxRangeDays' doc comment in types.go for the empirical
// measurement backing that. A 409 sanitized by tools.SanitizeFeegowError
// reads as an opaque "conflito ao processar a solicitação"; this error
// instead names the actual, fixable problem, in Portuguese, before any
// network call is made.
type DateRangeTooWideError struct {
	MaxRangeDays int
}

func (e *DateRangeTooWideError) Error() string {
	return fmt.Sprintf(
		"feegow: o período consultado precisa ter no máximo %d dias corridos "+
			"(a Feegow relata isso como \"menor que 6 meses\", mas o limite real, medido "+
			"contra a API, é em dias corridos — ver MaxRangeDays em types.go)",
		e.MaxRangeDays,
	)
}

// DateRangeInvertedError is returned when a caller's Start–End window has
// End before Start — a caller bug (e.g. swapped arguments) that would
// otherwise sail through validateDateRange's width check (any inverted
// window is, trivially, "narrower" than any positive limit) and reach
// Feegow as a nonsensical request. Checked before any HTTP request is
// built, exactly like DateRangeTooWideError.
type DateRangeInvertedError struct {
	Start, End string // the original ISO-8601 strings, for a readable message
}

func (e *DateRangeInvertedError) Error() string {
	return fmt.Sprintf(
		"feegow: data de início (%s) é posterior à data de fim (%s) — período invertido",
		e.Start, e.End,
	)
}

// validateDateRange enforces d.MaxRangeDays against req's DateStart/
// DateEnd pair before any wire translation happens. Declarative, not
// endpoint-specific — see MaxRangeDays' doc comment in types.go: it only
// fires when both dates are supplied and the descriptor declares a
// limit, and is silent (nil) otherwise, matching applyDates' "dates are
// always optional at the transport level" contract.
//
// The width check is plain day-count subtraction (end.Sub(start)), not
// calendar-field arithmetic: parseISODate always produces UTC midnight
// values for date-only ISO-8601 input, so the duration in hours divides
// evenly into days with no DST or leap-year special-casing needed — the
// exact class of calendar edge case (month-end overflow via AddDate) that
// made the previous month-based guard wrong. See MaxRangeDays in
// types.go for the measurements this cutoff is based on.
func validateDateRange(d EndpointDescriptor, req Request) error {
	if d.MaxRangeDays <= 0 || req.DateStart == nil || req.DateEnd == nil {
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
	if end.Before(start) {
		return &DateRangeInvertedError{Start: *req.DateStart, End: *req.DateEnd}
	}
	days := int(end.Sub(start).Hours() / 24)
	if days > d.MaxRangeDays {
		return &DateRangeTooWideError{MaxRangeDays: d.MaxRangeDays}
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
