package feegow

import (
	"fmt"
	"sort"
	"strings"
)

// RenderMarkdownDocs renders Registry as the Markdown translation table
// this package's doc comment promises: one row per endpoint, generated
// straight from the same data structure Client.Call executes against, so
// docs/feegow-api.md can never quietly drift out of sync with the code —
// docgen_test.go asserts the committed file equals this function's output
// byte for byte.
func RenderMarkdownDocs() string {
	ids := make([]string, 0, len(Registry))
	for id := range Registry {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)

	var b strings.Builder
	b.WriteString("# Tabela de tradução — internal/feegow\n\n")
	b.WriteString("Gerado a partir de `internal/feegow.Registry` (ver `internal/feegow/docgen.go`).\n")
	b.WriteString("`docgen_test.go` falha o build se este arquivo divergir do registry — não edite à mão.\n\n")
	b.WriteString("| ID | Host | Método | Path | Datas (papel: nome-na-API, formato) | Paginação | Envelope | Verificado | Notas |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")

	for _, id := range ids {
		d := Registry[EndpointID(id)]
		row := []string{
			escapeCell(id),
			escapeCell(string(d.Host)),
			d.Method,
			escapeCell(d.Path),
			escapeCell(dateParamsLabel(d.DateParams)),
			escapeCell(paginationLabel(d.Pagination)),
			envelopeLabel(d.Envelope),
			verifiedLabel(d.Verified),
			escapeCell(d.Notes),
		}
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}

	return b.String()
}

func escapeCell(s string) string {
	if s == "" {
		return "—"
	}
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func dateFormatLabel(format string) string {
	switch format {
	case ISO8601:
		return "YYYY-MM-DD"
	case DateBR:
		return "DD-MM-YYYY"
	default:
		return format
	}
}

func dateParamsLabel(params []DateParam) string {
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, 0, len(params))
	for _, dp := range params {
		parts = append(parts, fmt.Sprintf("%s: `%s` (%s)", dp.Role, dp.WireName, dateFormatLabel(dp.Format)))
	}
	return strings.Join(parts, "; ")
}

func paginationLabel(spec PaginationSpec) string {
	switch spec.Kind {
	case PaginationNone:
		return ""
	case PaginationStartOffset:
		return fmt.Sprintf("`%s`+`%s` (armadilha: `%s` é tamanho de página, `%s` é o deslocamento real)",
			spec.OffsetParam, spec.LimitParam, spec.LimitParam, spec.OffsetParam)
	case PaginationLimitOffset:
		return fmt.Sprintf("`%s`+`%s` (deslocamento real)", spec.LimitParam, spec.OffsetParam)
	case PaginationPagePerPage:
		return fmt.Sprintf("`%s`+`%s` (1-indexado)", spec.OffsetParam, spec.LimitParam)
	default:
		return "desconhecido"
	}
}

func envelopeLabel(kind EnvelopeKind) string {
	switch kind {
	case EnvelopeStandard:
		return "`{success,content}`"
	case EnvelopeNone:
		return "sem envelope"
	default:
		return "desconhecido"
	}
}

func verifiedLabel(verified bool) string {
	if verified {
		return "✅"
	}
	return "⚠️ não verificado"
}
