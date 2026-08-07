package feegow

import (
	"sort"
	"testing"
)

// TestApplyDates_RegistryDriven is acceptance criterion 1: for every
// endpoint declared in Registry with date params, an ISO-8601 input on the
// canonical Request must come out the other side under the endpoint's own
// wire name, in the endpoint's own wire format — DD-MM-YYYY where that's
// what's required, YYYY-MM-DD where that's what's required. It iterates
// Registry directly rather than hand-listing endpoint IDs, so a new
// endpoint added with DateParams automatically gets a case here too.
func TestApplyDates_RegistryDriven(t *testing.T) {
	// Deterministic order for readable failures.
	ids := make([]string, 0, len(Registry))
	for id := range Registry {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)

	for _, id := range ids {
		d := Registry[EndpointID(id)]
		if len(d.DateParams) == 0 {
			continue
		}
		t.Run(id, func(t *testing.T) {
			req := Request{}
			isoStart := "2024-01-05"
			isoEnd := "2024-01-20"
			isoSingle := "2024-03-09"

			for _, dp := range d.DateParams {
				switch dp.Role {
				case DateRoleStart:
					req.DateStart = &isoStart
				case DateRoleEnd:
					req.DateEnd = &isoEnd
				case DateRoleSingle:
					req.Date = &isoSingle
				}
			}

			wire := map[string]any{}
			if err := applyDates(d, req, wire); err != nil {
				t.Fatalf("applyDates: %v", err)
			}

			for _, dp := range d.DateParams {
				var wantISO string
				switch dp.Role {
				case DateRoleStart:
					wantISO = isoStart
				case DateRoleEnd:
					wantISO = isoEnd
				case DateRoleSingle:
					wantISO = isoSingle
				}
				wantWire := reformat(t, wantISO, dp.Format)

				got, ok := wire[dp.WireName]
				if !ok {
					t.Fatalf("wire param %q not set for role %q", dp.WireName, dp.Role)
				}
				if got != wantWire {
					t.Fatalf("wire[%q] = %v, want %q (format %q)", dp.WireName, got, wantWire, dp.Format)
				}
			}
		})
	}
}

func reformat(t *testing.T, iso, format string) string {
	t.Helper()
	tm, err := parseISODate(DateRoleSingle, iso)
	if err != nil {
		t.Fatalf("reformat: parsing fixture %q: %v", iso, err)
	}
	return tm.Format(format)
}

// TestApplyDates_BRFormatIsDDMMYYYY pins the exact BR conversion with a
// concrete, hand-checked example (rather than only the round-trip check
// above), so a Go time-layout mistake (e.g. swapping day/month) can't
// silently cancel itself out.
func TestApplyDates_BRFormatIsDDMMYYYY(t *testing.T) {
	d := Registry["appoints.search"]
	iso := "2018-08-05" // 5th of August 2018
	req := Request{DateStart: &iso}

	wire := map[string]any{}
	if err := applyDates(d, req, wire); err != nil {
		t.Fatalf("applyDates: %v", err)
	}
	if got := wire["data_start"]; got != "05-08-2018" {
		t.Fatalf("data_start = %v, want %q", got, "05-08-2018")
	}
}

// TestApplyDates_ISOFormatStaysYYYYMMDD is the complement: an endpoint
// whose wire format is already ISO-8601 must not be mangled by a
// round-trip through applyDates.
func TestApplyDates_ISOFormatStaysYYYYMMDD(t *testing.T) {
	d := Registry["financial.dmed"]
	iso := "2017-02-01"
	req := Request{DateStart: &iso}

	wire := map[string]any{}
	if err := applyDates(d, req, wire); err != nil {
		t.Fatalf("applyDates: %v", err)
	}
	if got := wire["dataInicio"]; got != "2017-02-01" {
		t.Fatalf("dataInicio = %v, want %q", got, "2017-02-01")
	}
}

// TestApplyDates_InvalidDateRejectedBeforeRequest is acceptance criterion
// 2: a malformed date must fail translation — and by extension never
// leave this process as part of a request — rather than being forwarded
// as garbage for Feegow to reject.
func TestApplyDates_InvalidDateRejectedBeforeRequest(t *testing.T) {
	d := Registry["appoints.search"]

	cases := []string{
		"05-08-2018", // BR format where ISO was expected
		"2018-13-40", // not a real date
		"2018/08/05", // wrong separator
		"not-a-date",
		"",
	}
	for _, bad := range cases {
		t.Run(bad, func(t *testing.T) {
			val := bad
			req := Request{DateStart: &val}
			wire := map[string]any{}
			err := applyDates(d, req, wire)
			if err == nil {
				t.Fatalf("applyDates(%q): got nil error, want a rejection", bad)
			}
			if len(wire) != 0 {
				t.Fatalf("applyDates(%q): wire was populated (%v) despite the error — invalid date leaked into request params", bad, wire)
			}
		})
	}
}

// TestApplyDates_RoleNotDeclaredByEndpointIsRejected proves a caller can't
// smuggle a date into an endpoint that doesn't document accepting one for
// that role — e.g. asking /company/list-unity (no DateParams at all) for
// a DateStart.
func TestApplyDates_RoleNotDeclaredByEndpointIsRejected(t *testing.T) {
	d := Registry["company.list_unity"]
	val := "2024-01-01"
	req := Request{DateStart: &val}

	wire := map[string]any{}
	if err := applyDates(d, req, wire); err == nil {
		t.Fatalf("applyDates: got nil error for a role the endpoint does not declare, want a rejection")
	}
}

// TestApplyDates_OmittedDateIsOmittedFromWire proves dates are optional at
// the transport level: not supplying one must not add anything to wire,
// never an empty-string placeholder.
func TestApplyDates_OmittedDateIsOmittedFromWire(t *testing.T) {
	d := Registry["appoints.search"]
	wire := map[string]any{}
	if err := applyDates(d, Request{}, wire); err != nil {
		t.Fatalf("applyDates with no dates set: %v", err)
	}
	if len(wire) != 0 {
		t.Fatalf("wire = %v, want empty (no date fields supplied)", wire)
	}
}
