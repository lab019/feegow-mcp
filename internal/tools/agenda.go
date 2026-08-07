package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// Agendamento is the minimum a patient needs to see to situate themselves:
// when, with whom, for what, where, and its status. Deliberately excludes
// notas (free-text notes can carry clinical detail), agendado_por, valor,
// and every other field /appoints/search returns — see ESPECIFICACAO.md
// §10 ("tools de leitura retornam campos selecionados").
//
// AgendamentoID IS included — unlike every other field here, it is not
// patient content but the handle the Fase 3 write tools (cancelar,
// remarcar, confirmar) need: a caller can only tell this service which
// agendamento to act on by first learning its id from here. Exposing it
// costs nothing privacy-wise (it identifies a booking, not a person) and
// omitting it would make those write tools unreachable in a real
// conversation — there would be no way for an agent to ever learn a valid
// id to send them.
type Agendamento struct {
	AgendamentoID   int    `json:"agendamento_id"`
	Data            string `json:"data"` // ISO-8601 (YYYY-MM-DD)
	Horario         string `json:"horario"`
	ProfissionalID  int    `json:"profissional_id"`
	EspecialidadeID int    `json:"especialidade_id"`
	ProcedimentoID  int    `json:"procedimento_id"`
	UnidadeID       int    `json:"unidade_id"`
	StatusID        int    `json:"status_id"`
}

// ConsultarAgendaResult is consultar_agenda's result.
type ConsultarAgendaResult struct {
	Agendamentos []Agendamento `json:"agendamentos"`
}

// appointSearchEntry is the subset of one /appoints/search result entry
// this package maps into Agendamento. AgendamentoID also doubles as the
// posse (ownership) key resolveOwnedAgendamento (agendamento_escrita.go)
// matches a caller-supplied agendamento_id against — see that file's doc
// comment.
type appointSearchEntry struct {
	AgendamentoID   int    `json:"agendamento_id"`
	Data            string `json:"data"` // DD-MM-YYYY on the wire
	Horario         string `json:"horario"`
	ProfissionalID  int    `json:"profissional_id"`
	EspecialidadeID int    `json:"especialidade_id"`
	ProcedimentoID  int    `json:"procedimento_id"`
	UnidadeID       int    `json:"unidade_id"`
	StatusID        int    `json:"status_id"`
}

// ConsultarAgenda returns a patient's appointments. It never accepts a raw
// paciente_id: this service is stateless (no session, no login), so if it
// took an id directly, "só para paciente identificado" would be a promise
// this code doesn't actually keep — any caller could pass any id. Instead
// it takes the exact same two fact-pairs identificar_paciente does, resolves
// the identity itself (with the exact same uniform-failure behavior — see
// IdentificarPaciente), and only then queries /appoints/search — making the
// identification requirement structural, not a convention callers could
// bypass by skipping a step.
func ConsultarAgenda(ctx context.Context, client *feegow.Client, args IdentidadeArgs) (*ConsultarAgendaResult, error) {
	result, err := consultarAgenda(ctx, client, args)
	if err != nil {
		// Single choke point (see SanitizeFeegowError's doc): every error
		// path below funnels through here on the way out, so a raw Feegow
		// 409/422 body — which can carry patient PII — never reaches this
		// tool's caller.
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

// consultarAgenda is ConsultarAgenda's implementation, kept separate so
// every return path — however many client.Call sites it grows in the
// future — passes through ConsultarAgenda's single sanitizing wrapper
// above, instead of each one needing its own sanitize call.
func consultarAgenda(ctx context.Context, client *feegow.Client, args IdentidadeArgs) (*ConsultarAgendaResult, error) {
	identidade, err := IdentificarPaciente(ctx, client, args)
	if err != nil {
		return nil, err
	}

	resp, err := client.Call(ctx, "appoints.search", feegow.Request{
		Params: map[string]any{"paciente_id": identidade.PacienteID},
	})
	if err != nil {
		return nil, err
	}

	var entries []appointSearchEntry
	if err := json.Unmarshal(resp.Content, &entries); err != nil {
		return nil, fmt.Errorf("tools: decoding appoints/search response: %w", err)
	}

	agendamentos := make([]Agendamento, 0, len(entries))
	for _, e := range entries {
		// doc.txt's /appoints/search example shows "data": "07-08-2024"
		// (DD-MM-YYYY) — converted to this service's canonical ISO-8601. A
		// date that doesn't parse as DD-MM-YYYY is passed through verbatim
		// rather than dropping the whole appointment: better an
		// unconverted date the caller can still read than a silently
		// missing agendamento.
		data := e.Data
		if t, err := time.Parse(feegow.DateBR, e.Data); err == nil {
			data = t.Format(feegow.ISO8601)
		}
		agendamentos = append(agendamentos, Agendamento{
			AgendamentoID:   e.AgendamentoID,
			Data:            data,
			Horario:         e.Horario,
			ProfissionalID:  e.ProfissionalID,
			EspecialidadeID: e.EspecialidadeID,
			ProcedimentoID:  e.ProcedimentoID,
			UnidadeID:       e.UnidadeID,
			StatusID:        e.StatusID,
		})
	}

	return &ConsultarAgendaResult{Agendamentos: agendamentos}, nil
}
