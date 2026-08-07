package feegow

import (
	"fmt"
	"strconv"
)

// Pagination is the canonical shape every caller of this package's public
// API uses, regardless of endpoint: Limit is a page size, Offset is the
// number of records to skip before the page starts (true deslocamento
// semantics) — never a page-size-in-disguise, never a 1-indexed page
// number. EncodePagination is what turns this into whatever a specific
// endpoint's wire format actually requires.
type Pagination struct {
	Limit  int
	Offset int
}

// PaginationError is returned when a Pagination value can't be honestly
// translated into an endpoint's wire scheme — e.g. the endpoint has no
// pagination at all, or a page-based endpoint is asked for an Offset that
// doesn't land on a page boundary. It is always returned before any HTTP
// request is built.
type PaginationError struct {
	EndpointID EndpointID
	Reason     string
}

func (e *PaginationError) Error() string {
	return fmt.Sprintf("feegow: paginação inválida para %q: %s", e.EndpointID, e.Reason)
}

// EncodePagination translates p into d's wire parameters, per d's declared
// PaginationSpec.Kind. It is the single place that knows the *meaning* of
// each scheme — see the PaginationKind constants in types.go for what each
// one actually does on the wire — so nothing else in this package (or its
// callers) needs to reason about which endpoint's "offset" is a lie.
func EncodePagination(d EndpointDescriptor, p Pagination) (map[string]any, error) {
	if p.Limit < 0 || p.Offset < 0 {
		return nil, &PaginationError{EndpointID: d.ID, Reason: "limit e offset não podem ser negativos"}
	}

	switch d.Pagination.Kind {
	case PaginationNone:
		return nil, &PaginationError{EndpointID: d.ID, Reason: "endpoint não suporta paginação"}

	case PaginationStartOffset:
		// Feegow's own "offset" wire param is the page size; its "start"
		// wire param is the real deslocamento. See the doc comment on
		// PaginationStartOffset.
		return map[string]any{
			d.Pagination.OffsetParam: strconv.Itoa(p.Offset), // wire "start" = canonical Offset
			d.Pagination.LimitParam:  strconv.Itoa(p.Limit),  // wire "offset" = canonical Limit
		}, nil

	case PaginationLimitOffset:
		return map[string]any{
			d.Pagination.LimitParam:  strconv.Itoa(p.Limit),
			d.Pagination.OffsetParam: strconv.Itoa(p.Offset),
		}, nil

	case PaginationPagePerPage:
		if p.Limit == 0 {
			return nil, &PaginationError{EndpointID: d.ID, Reason: "limit é obrigatório para paginação page/perPage"}
		}
		if p.Offset%p.Limit != 0 {
			return nil, &PaginationError{
				EndpointID: d.ID,
				Reason: fmt.Sprintf(
					"offset (%d) precisa ser múltiplo de limit (%d) — paginação page/perPage não expressa um deslocamento arbitrário",
					p.Offset, p.Limit,
				),
			}
		}
		page := p.Offset/p.Limit + 1
		return map[string]any{
			d.Pagination.LimitParam:  strconv.Itoa(p.Limit),
			d.Pagination.OffsetParam: strconv.Itoa(page),
		}, nil

	default:
		return nil, &PaginationError{EndpointID: d.ID, Reason: fmt.Sprintf("PaginationKind %d desconhecido", d.Pagination.Kind)}
	}
}

// applyPagination merges EncodePagination's result into wire, when req
// carries a Pagination request at all. A nil req.Pagination means "no
// pagination requested" and is always valid, even for endpoints that do
// support pagination — the caller just gets Feegow's own default page.
func applyPagination(d EndpointDescriptor, req Request, wire map[string]any) error {
	if req.Pagination == nil {
		return nil
	}
	encoded, err := EncodePagination(d, *req.Pagination)
	if err != nil {
		return err
	}
	for k, v := range encoded {
		wire[k] = v
	}
	return nil
}
