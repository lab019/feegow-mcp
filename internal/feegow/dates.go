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
