package feegow

import (
	"strconv"
	"testing"
)

// TestEncodePagination_AppointsSearch_OffsetIsPageSize is acceptance
// criterion 3's core claim: /appoints/search's wire "offset" is a page
// size, and the real deslocamento goes out as "start". A canonical
// Pagination{Limit: 20, Offset: 40} (skip 40, take 20) must NOT produce
// wire offset=40 — that would silently turn into "40 results per page"
// on Feegow's side, the exact bug the spec calls out.
func TestEncodePagination_AppointsSearch_OffsetIsPageSize(t *testing.T) {
	d := Registry["appoints.search"]
	got, err := EncodePagination(d, Pagination{Limit: 20, Offset: 40})
	if err != nil {
		t.Fatalf("EncodePagination: %v", err)
	}
	if got["start"] != "40" {
		t.Errorf(`wire["start"] = %v, want "40" (the real deslocamento, from canonical Offset)`, got["start"])
	}
	if got["offset"] != "20" {
		t.Errorf(`wire["offset"] = %v, want "20" (Feegow's page size, from canonical Limit)`, got["offset"])
	}
}

// TestEncodePagination_FinancialDmed_OffsetIsRealDeslocamento is the
// direct contrast case for the test above: /financial/dmed's wire
// "offset" really is the deslocamento — the same canonical Pagination
// value must produce a DIFFERENT wire encoding than appoints.search's,
// proving the two are not treated as the same scheme.
func TestEncodePagination_FinancialDmed_OffsetIsRealDeslocamento(t *testing.T) {
	d := Registry["financial.dmed"]
	got, err := EncodePagination(d, Pagination{Limit: 20, Offset: 40})
	if err != nil {
		t.Fatalf("EncodePagination: %v", err)
	}
	if got["limit"] != "20" {
		t.Errorf(`wire["limit"] = %v, want "20"`, got["limit"])
	}
	if got["offset"] != "40" {
		t.Errorf(`wire["offset"] = %v, want "40" (true deslocamento here, unlike appoints.search)`, got["offset"])
	}
}

// TestEncodePagination_SameCanonicalValueDiffersByEndpoint locks in the
// contrast directly: the exact same canonical Pagination value, encoded
// against two different registry entries, must not produce the same
// wire["offset"] value — because "offset" means opposite things on the
// two endpoints. If this test ever passes with equal values, the two
// schemes have been collapsed into one and the spec's central bug has
// been reintroduced.
func TestEncodePagination_SameCanonicalValueDiffersByEndpoint(t *testing.T) {
	p := Pagination{Limit: 20, Offset: 40}

	searchWire, err := EncodePagination(Registry["appoints.search"], p)
	if err != nil {
		t.Fatalf("EncodePagination(appoints.search): %v", err)
	}
	dmedWire, err := EncodePagination(Registry["financial.dmed"], p)
	if err != nil {
		t.Fatalf("EncodePagination(financial.dmed): %v", err)
	}

	if searchWire["offset"] == dmedWire["offset"] {
		t.Fatalf(
			"appoints.search and financial.dmed both encoded canonical Offset=%d as wire offset=%v — "+
				"they must differ, since appoints.search's wire \"offset\" is a page size and financial.dmed's is a real deslocamento",
			p.Offset, searchWire["offset"],
		)
	}
}

// TestEncodePagination_PagePerPage_ComputesPageNumber covers the third
// scheme: cartao-beneficios's page+perPage, 1-indexed, computed from a
// canonical (limit, offset) pair that lands on a page boundary.
func TestEncodePagination_PagePerPage_ComputesPageNumber(t *testing.T) {
	d := Registry["benefit.plan_datagrid"]

	cases := []struct {
		limit, offset int
		wantPage      string
	}{
		{limit: 10, offset: 0, wantPage: "1"},
		{limit: 10, offset: 10, wantPage: "2"},
		{limit: 10, offset: 30, wantPage: "4"},
		{limit: 1, offset: 0, wantPage: "1"},
	}
	for _, c := range cases {
		got, err := EncodePagination(d, Pagination{Limit: c.limit, Offset: c.offset})
		if err != nil {
			t.Fatalf("EncodePagination(limit=%d, offset=%d): %v", c.limit, c.offset, err)
		}
		if got["page"] != c.wantPage {
			t.Errorf("EncodePagination(limit=%d, offset=%d)[page] = %v, want %q", c.limit, c.offset, got["page"], c.wantPage)
		}
		if got["perPage"] != strconv.Itoa(c.limit) {
			t.Errorf("EncodePagination(limit=%d, offset=%d)[perPage] = %v, want %q", c.limit, c.offset, got["perPage"], strconv.Itoa(c.limit))
		}
	}
}

// TestEncodePagination_PagePerPage_RejectsNonBoundaryOffset proves the
// page-based scheme refuses to silently round an offset that doesn't fall
// on a page boundary, rather than producing a page number that doesn't
// actually skip the requested number of records.
func TestEncodePagination_PagePerPage_RejectsNonBoundaryOffset(t *testing.T) {
	d := Registry["benefit.plan_datagrid"]
	if _, err := EncodePagination(d, Pagination{Limit: 10, Offset: 15}); err == nil {
		t.Fatalf("EncodePagination(limit=10, offset=15): got nil error, want a rejection (15 is not a multiple of 10)")
	}
}

// TestEncodePagination_PagePerPage_RequiresPositiveLimit proves a caller
// can't ask for page-based pagination with no page size at all — there is
// no sane page number to compute without one.
func TestEncodePagination_PagePerPage_RequiresPositiveLimit(t *testing.T) {
	d := Registry["benefit.plan_datagrid"]
	if _, err := EncodePagination(d, Pagination{Limit: 0, Offset: 0}); err == nil {
		t.Fatalf("EncodePagination(limit=0): got nil error, want a rejection")
	}
}

// TestEncodePagination_LimitOffsetIdentity_PatientList is the identity
// case: patient.list's wire limit/offset already ARE canonical
// deslocamento semantics, so the "translation" is just passing the values
// through unchanged.
func TestEncodePagination_LimitOffsetIdentity_PatientList(t *testing.T) {
	d := Registry["patient.list"]
	got, err := EncodePagination(d, Pagination{Limit: 50, Offset: 5})
	if err != nil {
		t.Fatalf("EncodePagination: %v", err)
	}
	if got["limit"] != "50" || got["offset"] != "5" {
		t.Fatalf("got %v, want limit=50 offset=5 unchanged", got)
	}
}

// TestEncodePagination_NoneKindRejectsPagination proves an endpoint that
// declares no pagination at all refuses a Pagination request outright,
// rather than silently dropping it (which would make a caller believe
// their limit/offset were honored when they were not).
func TestEncodePagination_NoneKindRejectsPagination(t *testing.T) {
	d := Registry["company.list_unity"]
	if _, err := EncodePagination(d, Pagination{Limit: 10, Offset: 0}); err == nil {
		t.Fatalf("EncodePagination on a PaginationNone endpoint: got nil error, want a rejection")
	}
}

// TestEncodePagination_NegativeValuesRejected guards against nonsensical
// input reaching the wire.
func TestEncodePagination_NegativeValuesRejected(t *testing.T) {
	d := Registry["financial.dmed"]
	cases := []Pagination{
		{Limit: -1, Offset: 0},
		{Limit: 10, Offset: -1},
	}
	for _, p := range cases {
		if _, err := EncodePagination(d, p); err == nil {
			t.Fatalf("EncodePagination(%+v): got nil error, want a rejection", p)
		}
	}
}

// TestApplyPagination_NilPaginationIsANoop proves a nil Request.Pagination
// is always valid — even for endpoints that do support pagination — and
// leaves wire untouched.
func TestApplyPagination_NilPaginationIsANoop(t *testing.T) {
	d := Registry["financial.dmed"]
	wire := map[string]any{}
	if err := applyPagination(d, Request{}, wire); err != nil {
		t.Fatalf("applyPagination with nil Pagination: %v", err)
	}
	if len(wire) != 0 {
		t.Fatalf("wire = %v, want empty", wire)
	}
}
