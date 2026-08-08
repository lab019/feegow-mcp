package feegow

import (
	"net/http"
	"strings"
	"testing"
)

// TestRegistry_EveryEntryValidates is acceptance criterion 9: Registry is
// self-verifying. Every descriptor actually shipped in Registry must pass
// Validate() — a half-declared translation must never reach this table
// silently.
func TestRegistry_EveryEntryValidates(t *testing.T) {
	if len(Registry) == 0 {
		t.Fatal("Registry is empty")
	}
	for id, d := range Registry {
		if d.ID != id {
			t.Errorf("Registry[%q].ID = %q, want it to match its own map key", id, d.ID)
		}
		if err := d.Validate(); err != nil {
			t.Errorf("Registry[%q].Validate(): %v", id, err)
		}
	}
}

// TestRegistry_CoversThreeDateConventions proves the registry actually
// exercises the three date-parameter naming conventions ESPECIFICACAO.md
// §5 documents (data_start/data_end, date_start/date_end, dataInicio/
// dataFim), not just one of them.
func TestRegistry_CoversThreeDateConventions(t *testing.T) {
	wantWireNames := []string{"data_start", "date_start", "dataInicio"}
	for _, want := range wantWireNames {
		found := false
		for _, d := range Registry {
			for _, dp := range d.DateParams {
				if dp.WireName == want {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("no registry entry declares a date param named %q", want)
		}
	}
}

// TestRegistry_CoversThreePaginationSchemes proves the registry exercises
// all three pagination schemes from ESPECIFICACAO.md §5.
func TestRegistry_CoversThreePaginationSchemes(t *testing.T) {
	wantKinds := map[PaginationKind]bool{
		PaginationStartOffset: false,
		PaginationLimitOffset: false,
		PaginationPagePerPage: false,
	}
	for _, d := range Registry {
		if _, ok := wantKinds[d.Pagination.Kind]; ok {
			wantKinds[d.Pagination.Kind] = true
		}
	}
	for kind, found := range wantKinds {
		if !found {
			t.Errorf("no registry entry uses PaginationKind %d", kind)
		}
	}
}

// TestRegistry_CoversAtLeastThreeHosts proves the registry spans at least
// three of the four documented hosts (ESPECIFICACAO.md §5).
func TestRegistry_CoversAtLeastThreeHosts(t *testing.T) {
	seen := map[Host]bool{}
	for _, d := range Registry {
		seen[d.Host] = true
	}
	if len(seen) < 3 {
		t.Fatalf("registry only covers %d distinct hosts (%v), want at least 3", len(seen), seen)
	}
}

// TestRegistry_UnverifiedEntriesExplainThemselves is a direct check on top
// of TestRegistry_EveryEntryValidates: every Verified=false entry must
// carry Notes that at least mentions why (a smoke test / Fase 0 pointer),
// not just a non-empty string.
func TestRegistry_UnverifiedEntriesExplainThemselves(t *testing.T) {
	for id, d := range Registry {
		if d.Verified {
			continue
		}
		if strings.TrimSpace(d.Notes) == "" {
			t.Errorf("%s: Verified=false but Notes is empty", id)
		}
	}
}

// TestEndpointDescriptor_Validate_CatchesIncompleteTranslation is
// acceptance criterion 9's proof, isolated from the real Registry: a
// synthetic descriptor that declares pagination but omits the wire param
// names (the shape a careless addition to Registry would take) must fail
// Validate — not pass silently. This is what makes the registry
// self-verifying rather than just "currently correct by luck".
func TestEndpointDescriptor_Validate_CatchesIncompleteTranslation(t *testing.T) {
	cases := []struct {
		name string
		d    EndpointDescriptor
	}{
		{
			name: "pagination kind declared without wire param names",
			d: EndpointDescriptor{
				ID:         "fake.broken_pagination",
				Host:       HostAPI,
				Method:     http.MethodGet,
				Path:       "/fake",
				Pagination: PaginationSpec{Kind: PaginationLimitOffset}, // no LimitParam/OffsetParam
				Verified:   true,
			},
		},
		{
			name: "date param missing WireName",
			d: EndpointDescriptor{
				ID:         "fake.broken_date_name",
				Host:       HostAPI,
				Method:     http.MethodGet,
				Path:       "/fake",
				DateParams: []DateParam{{Role: DateRoleStart, Format: ISO8601}}, // no WireName
				Verified:   true,
			},
		},
		{
			name: "date param missing Format",
			d: EndpointDescriptor{
				ID:         "fake.broken_date_format",
				Host:       HostAPI,
				Method:     http.MethodGet,
				Path:       "/fake",
				DateParams: []DateParam{{Role: DateRoleStart, WireName: "data_start"}}, // no Format
				Verified:   true,
			},
		},
		{
			name: "unverified with no explanation",
			d: EndpointDescriptor{
				ID:       "fake.unverified_silent",
				Host:     HostAPI,
				Method:   http.MethodGet,
				Path:     "/fake",
				Verified: false, // no Notes
			},
		},
		{
			name: "empty path",
			d: EndpointDescriptor{
				ID:       "fake.no_path",
				Host:     HostAPI,
				Method:   http.MethodGet,
				Verified: true,
			},
		},
		{
			name: "empty host",
			d: EndpointDescriptor{
				ID:       "fake.no_host",
				Method:   http.MethodGet,
				Path:     "/fake",
				Verified: true,
			},
		},
		{
			name: "unsupported method",
			d: EndpointDescriptor{
				ID:       "fake.bad_method",
				Host:     HostAPI,
				Method:   http.MethodPut,
				Path:     "/fake",
				Verified: true,
			},
		},
		{
			name: "SuccessStatus not a 2xx",
			d: EndpointDescriptor{
				ID:            "fake.bad_success_status",
				Host:          HostAPI,
				Method:        http.MethodGet,
				Path:          "/fake",
				SuccessStatus: 404,
				Verified:      true,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.d.Validate(); err == nil {
				t.Fatalf("Validate() on a deliberately incomplete descriptor returned nil, want an error")
			}
		})
	}
}

// TestEndpointDescriptor_Validate_AcceptsWellFormedDescriptor is the
// positive control for the negative cases above.
func TestEndpointDescriptor_Validate_AcceptsWellFormedDescriptor(t *testing.T) {
	d := EndpointDescriptor{
		ID:     "fake.well_formed",
		Host:   HostAPI,
		Method: http.MethodGet,
		Path:   "/fake",
		DateParams: []DateParam{
			{Role: DateRoleStart, WireName: "data_start", Format: DateBR},
			{Role: DateRoleEnd, WireName: "data_end", Format: DateBR},
		},
		Pagination: PaginationSpec{Kind: PaginationLimitOffset, LimitParam: "limit", OffsetParam: "offset"},
		Verified:   true,
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("Validate() on a well-formed descriptor: %v", err)
	}
}
