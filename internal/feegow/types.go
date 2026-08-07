// Package feegow is the normalization layer this service exists for (see
// ESPECIFICACAO.md §5): a multi-host HTTP client for the Feegow Clinic ERP
// REST API whose public surface always speaks one convention — ISO-8601
// dates (YYYY-MM-DD), limit/offset pagination with true "records skipped"
// semantics, and a single decoded response shape — no matter how many
// different conventions the underlying endpoint actually uses.
//
// The translation itself is DATA, not scattered "if endpoint == ..."
// branches: EndpointDescriptor describes one endpoint's host, path,
// method, date parameter names/formats, pagination scheme and known
// quirks, and Registry is the map keyed by a stable EndpointID that holds
// every descriptor this phase covers. Client.Call is the single place that
// interprets a descriptor to build and execute a request; it never
// branches on an endpoint's identity, only on the descriptor's declared
// shape. That is what makes docs/feegow-api.md (generated from Registry,
// see RenderMarkdownDocs) a verifiable contract instead of prose: every
// entry in the table is backed by an EndpointDescriptor a test iterates
// over, and a descriptor with an incomplete translation fails
// EndpointDescriptor.Validate — see registry_test.go.
package feegow

import (
	"fmt"
	"net/http"
	"strings"
)

// EndpointID is the stable key this package and its callers use to refer
// to one Feegow endpoint. It is deliberately not the wire path, so the
// path can be corrected (see the doc bugs in ESPECIFICACAO.md §8) without
// changing every caller.
type EndpointID string

// Host is the scheme-less host (and, for api.feegow.com, its versioned
// path prefix) a group of endpoints lives under. It is a property of the
// endpoint, declared in Registry — never an environment variable. See
// ESPECIFICACAO.md §5 "Hosts" and Client.baseURL for the one escape hatch
// (FEEGOW_HOST_OVERRIDE) that exists purely for tests/CI.
type Host string

// The four hosts documented in ESPECIFICACAO.md §5. The same
// "financial2/external" path prefix appears under both HostCore and
// HostCoreBR in the official docs — ESPECIFICACAO.md §8 flags this as an
// unresolved doc bug pending a Fase 0 smoke test, not a typo introduced
// here.
const (
	HostAPI     Host = "api.feegow.com/v1/api"
	HostBenefit Host = "cartao-beneficios.feegow.com"
	HostCoreBR  Host = "core.feegow.com.br"
	HostCore    Host = "core.feegow.com"
)

// DateRole names which slot of the canonical Request a DateParam's value
// comes from. An endpoint declares at most the roles it actually accepts
// (zero, one, or the Start+End pair) — never more than what the doc
// documents for that endpoint.
type DateRole string

const (
	// DateRoleStart is the beginning of a date range (Request.DateStart).
	DateRoleStart DateRole = "start"
	// DateRoleEnd is the end of a date range (Request.DateEnd).
	DateRoleEnd DateRole = "end"
	// DateRoleSingle is a lone date, not part of a range (Request.Date) —
	// e.g. /appoints/new-appoint's "data".
	DateRoleSingle DateRole = "date"
)

// Date layouts, expressed as Go reference-time format strings. ISO8601 is
// the one and only format this package's public API accepts on the way
// in; BR is the "DD-MM-YYYY" convention several endpoints require on the
// way out.
const (
	ISO8601 = "2006-01-02" // YYYY-MM-DD
	DateBR  = "02-01-2006" // DD-MM-YYYY
)

// DateParam declares that a wire request carries a date under WireName,
// formatted as Format, sourced from the canonical Request field Role
// names. Role, WireName and Format are all mandatory once declared — see
// EndpointDescriptor.Validate.
type DateParam struct {
	Role     DateRole
	WireName string
	Format   string // a layout from the constants above (or an equivalent Go time layout)
}

// PaginationKind names one of the three pagination conventions
// ESPECIFICACAO.md §5 documents. The names deliberately spell out the
// *wire* semantics, because that is exactly the thing that is not
// consistent across endpoints — two of these three "mean" limit/offset in
// completely different ways.
type PaginationKind int

const (
	// PaginationNone: the endpoint has no pagination at all. A caller
	// supplying Request.Pagination for such an endpoint gets an error.
	PaginationNone PaginationKind = iota

	// PaginationStartOffset is /appoints/search's scheme: the wire
	// parameter conventionally called "start" is the true offset
	// (records to skip), while the wire parameter Feegow calls "offset"
	// is actually the *page size* (default 50). This is the trap
	// ESPECIFICACAO.md §5 calls out by name: the word "offset" means the
	// opposite of deslocamento here.
	PaginationStartOffset

	// PaginationLimitOffset is the "boring", actually-standard scheme:
	// wire limit = canonical limit, wire offset = canonical offset
	// (records to skip). /financial/dmed and /patient/list both use
	// this — /financial/dmed is, per ESPECIFICACAO.md, "o único com
	// limit+offset de verdade".
	PaginationLimitOffset

	// PaginationPagePerPage is the cartao-beneficios datagrid scheme:
	// 1-indexed page number + a page size (perPage). Since a page
	// boundary can't express an arbitrary skip count, EncodePagination
	// requires canonical Offset to be an exact multiple of canonical
	// Limit and rejects anything else rather than silently rounding.
	PaginationPagePerPage
)

// PaginationSpec declares how an endpoint's pagination is encoded on the
// wire. LimitParam and OffsetParam are the wire parameter *names* — their
// meaning depends on Kind, documented on the PaginationKind constants
// above. Both are mandatory whenever Kind != PaginationNone.
type PaginationSpec struct {
	Kind        PaginationKind
	LimitParam  string
	OffsetParam string
}

// EnvelopeKind names the shape a *successful* (2xx) response body takes.
// Error responses (409/422/401/403/5xx) are dispatched by HTTP status
// code, independent of EnvelopeKind — see errors.go and Client.Call.
type EnvelopeKind int

const (
	// EnvelopeStandard is {"success": bool, "content": ...}. Success is
	// unwrapped and Content is returned verbatim as Response.Content. A
	// 200 response with success:false is treated as a ConflictError,
	// defensively — Feegow's docs show this shape used for at least one
	// 5xx example too, so a caller can't assume success:true just
	// because the transport-level status was 200.
	EnvelopeStandard EnvelopeKind = iota

	// EnvelopeNone: the response body itself *is* the content, with no
	// success/content wrapper at all. This is what every
	// cartao-beneficios datagrid endpoint and both core.feegow.com(.br)
	// endpoints in this registry actually return (a bare array, or a
	// {"data": [...], "count": ..., ...} object with no "success" key).
	EnvelopeNone
)

// EndpointDescriptor is the executable translation-table row for one
// Feegow endpoint: everything Client.Call needs to build a request and
// interpret its response, and everything docs/feegow-api.md needs to
// document it.
type EndpointDescriptor struct {
	ID     EndpointID
	Host   Host
	Method string // http.MethodGet or http.MethodPost
	Path   string // e.g. "/appoints/search" — appended verbatim to the resolved host

	DateParams []DateParam
	Pagination PaginationSpec
	Envelope   EnvelopeKind

	// MaxRangeMonths declares the widest Start–End window (in calendar
	// months) this endpoint tolerates before Feegow itself rejects the
	// request. Zero (the default) means no client-enforced limit. This
	// exists specifically for /appoints/search, whose real, undocumented
	// behavior (confirmed by the Fase 0 smoke test — see ESPECIFICACAO.md
	// Fase 0 RELATORIO.md item a.4) is a 409 "Intervalo de data deve ser
	// menor que 6 meses." for any wider window. Declared on the
	// descriptor — not hard-coded to an endpoint ID — so
	// validateDateRange (dates.go) stays as data-driven as applyDates and
	// EncodePagination: any future endpoint that turns out to share this
	// limit gets the same client-side guard for free.
	MaxRangeMonths int

	// Verified is false when doc.txt did not give this endpoint's
	// translation with enough clarity to trust without a real-license
	// smoke test (ESPECIFICACAO.md §0/§8). An unverified descriptor is
	// still wired up and testable — Verified is a documentation/trust
	// flag, not a functional gate — but it MUST carry a non-empty Notes
	// explaining what's uncertain. See EndpointDescriptor.Validate.
	Verified bool

	// Notes is free text surfaced in docs/feegow-api.md: known doc bugs,
	// why something is unverified, naming quirks tools built on top of
	// this endpoint need to know about (e.g. the "tipo" field documented
	// as numeric but really "E"/"P" on /appoints/available-schedule — see
	// ESPECIFICACAO.md §7.5). Mandatory when Verified is false.
	Notes string
}

// Validate reports whether d is a well-formed, fully-declared translation.
// It is what makes Registry self-verifying (ESPECIFICACAO.md §5 "Regra",
// acceptance criterion 9): an endpoint appended to Registry with a
// half-declared date or pagination translation fails this check instead
// of silently shipping a broken conversion, and registry_test.go asserts
// Validate() == nil for every entry actually in Registry.
func (d EndpointDescriptor) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("endpoint descriptor: empty ID")
	}
	if d.Host == "" {
		return fmt.Errorf("%s: empty Host", d.ID)
	}
	if d.Method != http.MethodGet && d.Method != http.MethodPost {
		return fmt.Errorf("%s: Method must be GET or POST, got %q", d.ID, d.Method)
	}
	if !strings.HasPrefix(d.Path, "/") {
		return fmt.Errorf("%s: Path %q must start with \"/\"", d.ID, d.Path)
	}

	seenRoles := map[DateRole]bool{}
	for _, dp := range d.DateParams {
		switch dp.Role {
		case DateRoleStart, DateRoleEnd, DateRoleSingle:
		default:
			return fmt.Errorf("%s: date param has unknown Role %q", d.ID, dp.Role)
		}
		if seenRoles[dp.Role] {
			return fmt.Errorf("%s: date Role %q declared more than once", d.ID, dp.Role)
		}
		seenRoles[dp.Role] = true
		if dp.WireName == "" {
			return fmt.Errorf("%s: date param role %q has empty WireName", d.ID, dp.Role)
		}
		if dp.Format == "" {
			return fmt.Errorf("%s: date param role %q has empty Format", d.ID, dp.Role)
		}
	}
	if seenRoles[DateRoleSingle] && (seenRoles[DateRoleStart] || seenRoles[DateRoleEnd]) {
		return fmt.Errorf("%s: declares DateRoleSingle alongside a Start/End range, which is not a shape any real Feegow endpoint has", d.ID)
	}

	if d.MaxRangeMonths < 0 {
		return fmt.Errorf("%s: MaxRangeMonths must not be negative, got %d", d.ID, d.MaxRangeMonths)
	}

	switch d.Pagination.Kind {
	case PaginationNone:
		if d.Pagination.LimitParam != "" || d.Pagination.OffsetParam != "" {
			return fmt.Errorf("%s: PaginationNone must not declare wire param names", d.ID)
		}
	case PaginationStartOffset, PaginationLimitOffset, PaginationPagePerPage:
		if d.Pagination.LimitParam == "" || d.Pagination.OffsetParam == "" {
			return fmt.Errorf("%s: pagination kind %d missing LimitParam/OffsetParam", d.ID, d.Pagination.Kind)
		}
	default:
		return fmt.Errorf("%s: unknown PaginationKind %d", d.ID, d.Pagination.Kind)
	}

	if !d.Verified && strings.TrimSpace(d.Notes) == "" {
		return fmt.Errorf("%s: Verified=false requires a non-empty Notes explaining what is unverified (see ESPECIFICACAO.md Fase 0)", d.ID)
	}

	return nil
}
