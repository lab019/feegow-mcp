package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// HorariosLivresArgs is buscar_horarios_livres's argument shape, mirroring
// /appoints/available-schedule's query params (ESPECIFICACAO.md §8: "tipo"
// is documented as numeric but the real values are "E"/"P").
//
// UnidadeID is a *int, not an int, specifically to keep "0" and "not
// provided" distinguishable all the way to the wire request: per doc.txt,
// unidade_id=0 means "unidade principal" and an absent unidade_id means
// "todas as unidades" — two different queries. A plain int can't represent
// that distinction (its zero value IS 0), which is exactly the trap
// ESPECIFICACAO.md §7.3 calls out by name.
type HorariosLivresArgs struct {
	Tipo            string `json:"tipo" jsonschema:"\"E\" para buscar por especialidade ou \"P\" para buscar por procedimento."`
	EspecialidadeID *int   `json:"especialidade_id,omitempty" jsonschema:"ID da especialidade. Obrigatório quando tipo=E."`
	ProcedimentoID  *int   `json:"procedimento_id,omitempty" jsonschema:"ID do procedimento. Obrigatório quando tipo=P."`
	DataInicio      string `json:"data_inicio" jsonschema:"Data inicial da busca, ISO-8601 (YYYY-MM-DD)."`
	DataFim         string `json:"data_fim" jsonschema:"Data final da busca, ISO-8601 (YYYY-MM-DD)."`
	UnidadeID       *int   `json:"unidade_id,omitempty" jsonschema:"ID da unidade. 0 = unidade principal; se omitido, busca em todas as unidades — são consultas diferentes, não escolha um valor default."`
	ProfissionalID  *int   `json:"profissional_id,omitempty" jsonschema:"Filtra por um profissional específico."`
	ConvenioID      *int   `json:"convenio_id,omitempty" jsonschema:"Filtra por um convênio específico."`
}

// HorarioLivre is one open slot: who, where, when. Flattened out of
// /appoints/available-schedule's nested profissional_id -> local_id -> data
// -> [horarios] shape because a flat list is what a caller actually offers
// a patient, and because a flat shape is what makes "this slot was removed
// because it's blocked" a simple list-membership fact instead of a nested
// tree edit.
type HorarioLivre struct {
	ProfissionalID int    `json:"profissional_id"`
	LocalID        int    `json:"local_id"`
	Data           string `json:"data"`
	Horario        string `json:"horario"`
}

// HorariosLivresResult is buscar_horarios_livres's result.
type HorariosLivresResult struct {
	Horarios []HorarioLivre `json:"horarios"`
}

// availableScheduleContent mirrors /appoints/available-schedule's success
// content shape.
type availableScheduleContent struct {
	ProfissionalID map[string]struct {
		LocalID map[string]map[string][]string `json:"local_id"`
	} `json:"profissional_id"`
}

// lockEntry mirrors one /lock/list result entry.
type lockEntry struct {
	DateStart      string   `json:"date_start"` // YYYY-MM-DD per lock.list's descriptor
	DateEnd        string   `json:"date_end"`
	TimeStart      string   `json:"time_start"` // HH:MM:SS
	TimeEnd        string   `json:"time_end"`
	ProfessionalID int      `json:"professional_id"`
	WeekDay        []string `json:"week_day"`
	Units          []string `json:"units"`
}

// BuscarHorariosLivres combines /appoints/available-schedule with
// /lock/list and subtracts every slot a matching bloqueio covers: a
// time Feegow reports as free but that is under a clinic-side lock cannot
// be offered to the patient (ESPECIFICACAO.md §6) — offering it would just
// move the 409 "horário ocupado"-style conflict from this tool to the
// agendar tool one step later.
func BuscarHorariosLivres(ctx context.Context, client *feegow.Client, args HorariosLivresArgs) (*HorariosLivresResult, error) {
	wire, err := buildAvailableScheduleParams(args)
	if err != nil {
		return nil, err
	}

	dataInicio := args.DataInicio
	dataFim := args.DataFim
	availResp, err := client.Call(ctx, "appoints.available_schedule", feegow.Request{
		Params:    wire,
		DateStart: &dataInicio,
		DateEnd:   &dataFim,
	})
	if err != nil {
		return nil, err
	}

	var content availableScheduleContent
	if err := json.Unmarshal(availResp.Content, &content); err != nil {
		return nil, fmt.Errorf("tools: decoding available-schedule response: %w", err)
	}

	locks, err := fetchLocks(ctx, client, args, dataInicio, dataFim)
	if err != nil {
		return nil, err
	}

	horarios := make([]HorarioLivre, 0)
	for profStr, profData := range content.ProfissionalID {
		profID, err := strconv.Atoi(profStr)
		if err != nil {
			continue // unexpected shape from Feegow; skip rather than guess
		}
		for localStr, dates := range profData.LocalID {
			localID, err := strconv.Atoi(localStr)
			if err != nil {
				continue
			}
			for date, times := range dates {
				for _, horario := range times {
					if isBlocked(locks, profID, args.UnidadeID, date, horario) {
						continue
					}
					horarios = append(horarios, HorarioLivre{
						ProfissionalID: profID,
						LocalID:        localID,
						Data:           date,
						Horario:        horario,
					})
				}
			}
		}
	}

	sort.Slice(horarios, func(i, j int) bool {
		a, b := horarios[i], horarios[j]
		if a.Data != b.Data {
			return a.Data < b.Data
		}
		if a.Horario != b.Horario {
			return a.Horario < b.Horario
		}
		if a.ProfissionalID != b.ProfissionalID {
			return a.ProfissionalID < b.ProfissionalID
		}
		return a.LocalID < b.LocalID
	})

	return &HorariosLivresResult{Horarios: horarios}, nil
}

func buildAvailableScheduleParams(args HorariosLivresArgs) (map[string]any, error) {
	wire := map[string]any{}

	switch args.Tipo {
	case "E":
		if args.EspecialidadeID == nil {
			return nil, &ArgumentError{Msg: "especialidade_id é obrigatório quando tipo=E"}
		}
		wire["tipo"] = "E"
		wire["especialidade_id"] = *args.EspecialidadeID
	case "P":
		if args.ProcedimentoID == nil {
			return nil, &ArgumentError{Msg: "procedimento_id é obrigatório quando tipo=P"}
		}
		wire["tipo"] = "P"
		wire["procedimento_id"] = *args.ProcedimentoID
	default:
		return nil, &ArgumentError{Msg: fmt.Sprintf(`tipo deve ser "E" (especialidade) ou "P" (procedimento), recebido %q`, args.Tipo)}
	}

	// unidade_id: only set on the wire when the caller actually provided
	// it — nil means "todas as unidades" and must stay entirely absent
	// from the request, never sent as 0. See the HorariosLivresArgs doc
	// comment.
	if args.UnidadeID != nil {
		wire["unidade_id"] = *args.UnidadeID
	}
	if args.ProfissionalID != nil {
		wire["profissional_id"] = *args.ProfissionalID
	}
	if args.ConvenioID != nil {
		wire["convenio_id"] = *args.ConvenioID
	}

	return wire, nil
}

// fetchLocks calls /lock/list for the same date range and, if requested, the
// same unidade. A 409 ("nenhum bloqueio encontrado" — this API's usual
// not-found shape) is treated as an empty lock list rather than propagated:
// having no blocks in the period is a completely normal outcome for this
// query, not an error condition for the caller to see.
func fetchLocks(ctx context.Context, client *feegow.Client, args HorariosLivresArgs, dataInicio, dataFim string) ([]lockEntry, error) {
	wire := map[string]any{}
	if args.UnidadeID != nil {
		wire["unidade_id"] = *args.UnidadeID
	}
	if args.ProfissionalID != nil {
		wire["profissional_id"] = *args.ProfissionalID
	}

	resp, err := client.Call(ctx, "lock.list", feegow.Request{
		Params:    wire,
		DateStart: &dataInicio,
		DateEnd:   &dataFim,
	})
	if err != nil {
		var conflict *feegow.ConflictError
		if errors.As(err, &conflict) {
			return nil, nil
		}
		return nil, err
	}

	var locks []lockEntry
	if err := json.Unmarshal(resp.Content, &locks); err != nil {
		return nil, fmt.Errorf("tools: decoding lock/list response: %w", err)
	}
	return locks, nil
}

// isBlocked reports whether any lock in locks covers (profID, date,
// horario) for the requested unidadeID. Every check that can't be resolved
// with confidence fails toward "blocked, don't offer it" rather than
// "free" — offering a slot that turns out to be locked is a 409 in front of
// the patient one step later (ESPECIFICACAO.md §7.4); silently hiding one
// that's actually free just means the patient sees one fewer option.
//
// week_day's numbering convention is not documented in doc.txt beyond the
// example values "1".."7"; this treats it as ISO-8601 (1=Monday..7=Sunday).
// lock.list is already Verified:false pending a Fase 0 smoke test (see
// internal/feegow/registry.go) — this assumption should be confirmed then.
func isBlocked(locks []lockEntry, profID int, unidadeID *int, date, horario string) bool {
	d, err := time.Parse(feegow.ISO8601, date)
	if err != nil {
		return true // can't evaluate this slot against blocks — don't offer it
	}

	for _, lk := range locks {
		if lk.ProfessionalID != 0 && lk.ProfessionalID != profID {
			continue
		}
		if unidadeID != nil && len(lk.Units) > 0 && !containsStr(lk.Units, strconv.Itoa(*unidadeID)) {
			continue
		}

		ds, errStart := time.Parse(feegow.ISO8601, lk.DateStart)
		de, errEnd := time.Parse(feegow.ISO8601, lk.DateEnd)
		if errStart != nil || errEnd != nil {
			continue // malformed lock entry: skip it, don't let it block everything
		}
		if d.Before(ds) || d.After(de) {
			continue
		}

		if len(lk.WeekDay) > 0 && !containsStr(lk.WeekDay, strconv.Itoa(isoWeekday(d))) {
			continue
		}

		if lk.TimeStart != "" && horario < lk.TimeStart {
			continue
		}
		if lk.TimeEnd != "" && horario > lk.TimeEnd {
			continue
		}

		return true
	}
	return false
}

// isoWeekday returns t's weekday as 1 (Monday) .. 7 (Sunday) — ISO-8601's
// convention — since Go's time.Weekday numbers Sunday as 0.
func isoWeekday(t time.Time) int {
	wd := int(t.Weekday())
	if wd == 0 {
		return 7
	}
	return wd
}

func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
