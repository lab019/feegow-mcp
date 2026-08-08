package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds listar_funcionarios, the Fase 4c "Funcionários" group —
// a single endpoint (/employee/list per ESPECIFICACAO.md §13), so a
// dedicated tool rather than a one-case acao/tipo switch, same reasoning as
// listar_relatorios/gerar_relatorio.

// ListarFuncionariosResult is listar_funcionarios' result: /employee/list's
// content passed through as-is — same "business blob" choice as every
// other admin catalog result in this package.
type ListarFuncionariosResult struct {
	Funcionarios any `json:"funcionarios"`
}

// ListarFuncionarios lists the clínica's employees via /employee/list — no
// parameters documented by the Fase 4c smoke test (a bare GET already
// returns the standard {success,content,total} envelope).
func ListarFuncionarios(ctx context.Context, client *feegow.Client) (*ListarFuncionariosResult, error) {
	result, err := listarFuncionarios(ctx, client)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func listarFuncionarios(ctx context.Context, client *feegow.Client) (*ListarFuncionariosResult, error) {
	resp, err := client.Call(ctx, "employee.list", feegow.Request{})
	if err != nil {
		return nil, err
	}
	var funcionarios any
	if err := json.Unmarshal(resp.Content, &funcionarios); err != nil {
		return nil, fmt.Errorf("tools: decoding employee/list response: %w", err)
	}
	return &ListarFuncionariosResult{Funcionarios: funcionarios}, nil
}
