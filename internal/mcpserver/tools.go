// This file is the only place internal/tools' pure functions get wrapped
// into actual MCP tools (see internal/tools' package doc comment on why
// that package itself never imports modelcontextprotocol/go-sdk). Each
// register* function below does the same three things: describe the tool,
// unmarshal its typed args (already handled by mcp.AddTool before the
// handler runs), call the matching internal/tools function against client,
// and hand back its typed result or error.
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lab019/feegow-mcp/internal/feegow"
	"github.com/lab019/feegow-mcp/internal/tools"
)

// registerListarCatalogo registers listar_catalogo: one tool over ~14
// catalog endpoints (unidades, especialidades, convênios, procedimentos,
// profissionais, canais, motivos, status...) instead of one tool per
// endpoint — see internal/tools/catalogo.go and ESPECIFICACAO.md §6 for
// why. No patient data is ever in scope here.
func registerListarCatalogo(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "listar_catalogo",
		Description: "Lista um catálogo de informações da clínica (unidades, locais, especialidades, " +
			"convênios, procedimentos, tipos/grupos/pacotes de procedimento, profissionais, canais de " +
			"agendamento, motivos de cancelamento/reagendamento ou status de agendamento). Não retorna " +
			"nenhum dado de paciente.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.CatalogoArgs) (*mcp.CallToolResult, tools.CatalogoResult, error) {
		result, err := tools.ListarCatalogo(ctx, client, args)
		if err != nil {
			return nil, tools.CatalogoResult{}, tools.SanitizeFeegowError(err)
		}
		return nil, *result, nil
	})
}

// registerBuscarHorariosLivres registers buscar_horarios_livres.
func registerBuscarHorariosLivres(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "buscar_horarios_livres",
		Description: "Busca horários disponíveis para agendamento por especialidade ou procedimento, num " +
			"intervalo de datas. Já remove os horários que estão sob bloqueio da clínica — nunca oferece " +
			"um horário indisponível. Atenção: unidade_id=0 é a unidade principal; omitir unidade_id " +
			"busca em todas as unidades — são consultas diferentes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.HorariosLivresArgs) (*mcp.CallToolResult, tools.HorariosLivresResult, error) {
		result, err := tools.BuscarHorariosLivres(ctx, client, args)
		if err != nil {
			return nil, tools.HorariosLivresResult{}, tools.SanitizeFeegowError(err)
		}
		return nil, *result, nil
	})
}

// registerIdentificarPaciente registers identificar_paciente — the tool
// that turns a paciente's self-declared identity (never verified: this is
// an unauthenticated, public channel — see internal/tools/paciente.go) into
// a paciente_id, requiring two independent facts that must both match.
func registerIdentificarPaciente(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "identificar_paciente",
		Description: "Identifica um paciente já cadastrado a partir de DOIS fatos que precisam bater: " +
			"cpf+data_nascimento OU telefone+nome_completo — nunca um fato isolado. Retorna apenas o " +
			"identificador interno do paciente, nunca nome, CPF, endereço ou qualquer outro dado " +
			"pessoal. Se o cadastro não existir OU se o segundo fato não bater, a resposta é a mesma " +
			"mensagem genérica de \"não localizado\" nos dois casos.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.IdentidadeArgs) (*mcp.CallToolResult, tools.IdentificarPacienteResult, error) {
		result, err := tools.IdentificarPaciente(ctx, client, args)
		if err != nil {
			return nil, tools.IdentificarPacienteResult{}, tools.SanitizeFeegowError(err)
		}
		return nil, *result, nil
	})
}

// registerConsultarAgenda registers consultar_agenda. Its argument shape is
// intentionally identical to identificar_paciente's — cpf+data_nascimento
// OU telefone+nome_completo — and deliberately has NO paciente_id field:
// see internal/tools/agenda.go's ConsultarAgenda doc comment for why a raw
// id is never accepted here.
func registerConsultarAgenda(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "consultar_agenda",
		Description: "Consulta os agendamentos de um paciente já cadastrado. Identifica o paciente a " +
			"partir de DOIS fatos que precisam bater — cpf+data_nascimento OU telefone+nome_completo — " +
			"e devolve data, horário, profissional, especialidade/procedimento, unidade e status de " +
			"cada agendamento. Nunca aceita um identificador de paciente diretamente, e nunca retorna " +
			"dado clínico.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.IdentidadeArgs) (*mcp.CallToolResult, tools.ConsultarAgendaResult, error) {
		result, err := tools.ConsultarAgenda(ctx, client, args)
		if err != nil {
			return nil, tools.ConsultarAgendaResult{}, tools.SanitizeFeegowError(err)
		}
		return nil, *result, nil
	})
}
