package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds registrar_laudo, deliberately its OWN tool rather than
// one more acao inside consultar_laudos — it writes a real clinical record
// (laudo) into a paciente's prontuário via /medical-reports/create, the
// same "write deserves its own confirmação gate, never bundled behind a
// read-shaped acao switch" separation gerenciar_conta/gerenciar_voucher
// already establish relative to consultar_financeiro.
//
// LAUDO É DADO CLÍNICO — laudo_base64 carries the actual clinical file
// content (base64-encoded). This tool NEVER logs it, and never logs
// Feegow's raw response body either (which, on failure, has been observed
// to echo an internal PHP error string — not laudo content, but still not
// something that belongs in a log line). The result this tool returns is
// deliberately minimal (a bare boolean), not Feegow's message field —
// "retorna o mínimo útil", per this fase's own instruction.
//
// NEVER exercised end-to-end by the Fase 4c smoke test, by explicit
// instruction: the prontuário of a test license has no cleanup endpoint.
// Only agendamento_id and laudo_base64 are exposed as required — the two
// fields the empty-body 422 confirmed (see medical_reports.create's Notes
// in internal/feegow/registry.go) — any additional optional field Feegow
// might accept was not sounded and is not modeled here.

// RegistrarLaudoArgs is registrar_laudo's argument shape.
type RegistrarLaudoArgs struct {
	AgendamentoID *int   `json:"agendamento_id" jsonschema:"ID do agendamento ao qual o laudo se refere. Obrigatório."`
	LaudoBase64   string `json:"laudo_base64" jsonschema:"Conteúdo do laudo em base64. Obrigatório. Dado clínico — nunca é logado por esta tool."`

	Confirmacao bool `json:"confirmacao" jsonschema:"Confirmação EXPLÍCITA do OPERADOR da clínica (nunca assumida pelo agente/modelo) de que deseja registrar este laudo no prontuário do paciente."`
}

// RegistrarLaudoResult is registrar_laudo's result — deliberately minimal
// (see this file's doc comment): never Feegow's raw message field.
type RegistrarLaudoResult struct {
	Registrado bool `json:"registrado"`
}

// registrarLaudoBody mirrors /medical-reports/create's whole response body
// — EnvelopeNone (see medical_reports.create's Notes in
// internal/feegow/registry.go): the real success shape was never observed
// by any phase's smoke test, and the endpoint does NOT use the
// {success,content} envelope (its error responses use "message", not
// "content" — confirmed on both the 422 and the one 200 success:false
// response observed).
type registrarLaudoBody struct {
	Success bool `json:"success"`
}

// RegistrarLaudo registers a laudo against an agendamento's prontuário via
// /medical-reports/create.
func RegistrarLaudo(ctx context.Context, client *feegow.Client, args RegistrarLaudoArgs) (*RegistrarLaudoResult, error) {
	result, err := registrarLaudo(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func registrarLaudo(ctx context.Context, client *feegow.Client, args RegistrarLaudoArgs) (*RegistrarLaudoResult, error) {
	if err := requireConfirmacaoOperador(args.Confirmacao, "registrar_laudo"); err != nil {
		return nil, err
	}
	if args.AgendamentoID == nil || *args.AgendamentoID <= 0 {
		return nil, &ArgumentError{Msg: "agendamento_id é obrigatório e deve ser um identificador positivo"}
	}
	if args.LaudoBase64 == "" {
		return nil, &ArgumentError{Msg: "laudo_base64 é obrigatório"}
	}

	resp, err := client.Call(ctx, "medical_reports.create", feegow.Request{
		Params: map[string]any{
			"agendamento_id": *args.AgendamentoID,
			"laudo_base64":   args.LaudoBase64,
		},
	})
	if err != nil {
		return nil, err
	}

	var body registrarLaudoBody
	if err := json.Unmarshal(resp.Content, &body); err != nil {
		return nil, fmt.Errorf("tools: decoding medical-reports/create response: %w", err)
	}
	if err := checkEnvelopeNoneSuccess(body.Success); err != nil {
		return nil, err
	}

	auditAdminWrite("registrar_laudo", 0, *args.AgendamentoID)
	return &RegistrarLaudoResult{Registrado: true}, nil
}
