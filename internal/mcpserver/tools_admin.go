// This file is the admin profile's counterpart to tools.go: the only place
// internal/tools' admin-only pure functions get wrapped into actual MCP
// tools. See tools.go's package doc for why the SDK stays out of
// internal/tools itself.
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lab019/feegow-mcp/internal/feegow"
	"github.com/lab019/feegow-mcp/internal/tools"
)

// registerAdminOnlyTools registers every tool exposed ONLY on the admin
// profile — Fase 4a's paciente/agenda surface. See server.go's
// registerAdminTools for why this is composed on top of
// registerAtendimentoTools rather than listed as a parallel, independently
// maintained set.
//
// None of these apply atendimento's public-channel restrictions (two-fact
// identification, minimized results, uniform "não localizado" errors) —
// the admin caller is the clínica itself, already authenticated by its own
// Feegow token before the call ever reaches this service (see server.go's
// newAdmin doc comment and ESPECIFICACAO.md §3). Every write here still
// requires explicit confirmation from that caller — the clínica's own
// operador — via requireConfirmacaoOperador (internal/tools/guardas.go),
// the same "escritas com confirmação explícita" discipline Fase 3's
// atendimento writes already follow.
func registerAdminOnlyTools(s *mcp.Server, client *feegow.Client) {
	registerBuscarPacientes(s, client)
	registerObterPaciente(s, client)
	registerConsultarPacienteClinico(s, client)
	registerAtualizarPaciente(s, client)
	registerAnexarAoProntuario(s, client)
	registerAtualizarStatusAgendamento(s, client)
	registerGerarSenhaAtendimento(s, client)
	registerConsultarFinanceiro(s, client)
	registerGerenciarConta(s, client)
	registerGerenciarVoucher(s, client)
	registerRemoverRegistroFinanceiro(s, client)
	registerConsultarEstoque(s, client)
	registerMovimentarEstoque(s, client)
}

// registerBuscarPacientes registers buscar_pacientes: free-form patient
// search over /patient/list, with no two-fact identification requirement
// (unlike the atendimento profile's identificar_paciente) and a hard cap on
// page size (see internal/tools/paciente_admin.go's buscarPacientesMaxLimit)
// so a single call can never hand back the clinic's entire patient base.
func registerBuscarPacientes(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "buscar_pacientes",
		Description: "Busca pacientes cadastrados por qualquer combinação de filtros (cpf, telefone, " +
			"data_aniversario, origem_id, alterado_em, programa_saude), com paginação. Uso administrativo: " +
			"não exige dois fatos de identificação como o perfil de atendimento — quem chama esta tool já " +
			"está autenticado pelo próprio token Feegow da clínica. Sempre paginado (limit/offset); nunca " +
			"retorna a base inteira de pacientes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.BuscarPacientesArgs) (*mcp.CallToolResult, tools.BuscarPacientesResult, error) {
		result, err := tools.BuscarPacientes(ctx, client, args)
		if err != nil {
			return nil, tools.BuscarPacientesResult{}, err
		}
		return nil, *result, nil
	})
}

// registerObterPaciente registers obter_paciente: the full cadastro for a
// known paciente_id (endereço, documentos, convênios, ...) via
// /patient/search. ATENÇÃO: este endpoint devolve "nascimento" em
// DD-MM-YYYY, diferente de buscar_pacientes, que usa ISO-8601 — ver
// ObterPacienteResult (internal/tools/paciente_admin.go).
func registerObterPaciente(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "obter_paciente",
		Description: "Retorna o cadastro completo de um paciente por paciente_id: endereço, documentos, " +
			"convênios, programas de saúde. ATENÇÃO: o campo \"nascimento\" desta tool vem em DD-MM-YYYY, " +
			"diferente de buscar_pacientes (que usa ISO-8601 YYYY-MM-DD) — a própria API Feegow diverge " +
			"entre os dois endpoints.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.ObterPacienteArgs) (*mcp.CallToolResult, tools.ObterPacienteResult, error) {
		result, err := tools.ObterPaciente(ctx, client, args)
		if err != nil {
			return nil, tools.ObterPacienteResult{}, err
		}
		return nil, *result, nil
	})
}

// registerConsultarPacienteClinico registers consultar_paciente_clinico:
// dependentes, pedidos de exame, programas de saúde, linha do tempo,
// elegibilidade, origens de cadastro e tabelas particulares num só lugar —
// see internal/tools/paciente_admin.go's ConsultarPacienteClinicoTipos for
// the exact tipo values and which fields each one uses.
func registerConsultarPacienteClinico(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "consultar_paciente_clinico",
		Description: "Consulta informações administrativas/clínicas associadas a um paciente (ou a " +
			"catálogos da clínica), escolhidas por tipo: dependentes, pedidos_exame, programas_saude, " +
			"linha_tempo, elegibilidade, origens, tabelas_particulares. paciente_id é obrigatório para " +
			"dependentes/pedidos_exame/linha_tempo/elegibilidade; programas_saude/origens/" +
			"tabelas_particulares são catálogos da clínica e ignoram paciente_id. pedidos_exame também " +
			"exige data_inicio, data_fim e tipo_pedido.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.ConsultarPacienteClinicoArgs) (*mcp.CallToolResult, tools.ConsultarPacienteClinicoResult, error) {
		result, err := tools.ConsultarPacienteClinico(ctx, client, args)
		if err != nil {
			return nil, tools.ConsultarPacienteClinicoResult{}, err
		}
		return nil, *result, nil
	})
}

// registerAtualizarPaciente registers atualizar_paciente: edits an existing
// paciente's cadastro via /patient/edit. Requires confirmacao=true from the
// operador (never assumed by the agent) before any Feegow call is made —
// see requireConfirmacaoOperador.
func registerAtualizarPaciente(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "atualizar_paciente",
		Description: "Edita o cadastro de um paciente existente (nome, cpf, contato, endereço, tabela " +
			"particular, ...) via paciente_id. Exige confirmação EXPLÍCITA do OPERADOR da clínica " +
			"(confirmacao=true) — nunca assumida pelo agente/modelo.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.AtualizarPacienteArgs) (*mcp.CallToolResult, tools.AtualizarPacienteResult, error) {
		result, err := tools.AtualizarPaciente(ctx, client, args)
		if err != nil {
			return nil, tools.AtualizarPacienteResult{}, err
		}
		return nil, *result, nil
	})
}

// registerAnexarAoProntuario registers anexar_ao_prontuario: attaches a
// base64-encoded file to a paciente's prontuário via
// /patient/upload-base64. Requires confirmacao=true from the operador.
func registerAnexarAoProntuario(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "anexar_ao_prontuario",
		Description: "Anexa um arquivo (base64) ao prontuário de um paciente, identificado por " +
			"paciente_id OU por cpf+nascimento. Exige confirmação EXPLÍCITA do OPERADOR da clínica " +
			"(confirmacao=true) — nunca assumida pelo agente/modelo.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.AnexarAoProntuarioArgs) (*mcp.CallToolResult, tools.AnexarAoProntuarioResult, error) {
		result, err := tools.AnexarAoProntuario(ctx, client, args)
		if err != nil {
			return nil, tools.AnexarAoProntuarioResult{}, err
		}
		return nil, *result, nil
	})
}

// registerAtualizarStatusAgendamento registers atualizar_status_agendamento:
// updates an agendamento's status via /appoints/statusUpdate. Requires
// confirmacao=true from the operador — no posse (ownership) check is
// needed here, unlike Fase 3's cancelar/remarcar/confirmar: see this
// file's package doc.
func registerAtualizarStatusAgendamento(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "atualizar_status_agendamento",
		Description: "Atualiza o status de um agendamento (ver listar_catalogo tipo=status_agendamento " +
			"para os valores de status_id) e opcionalmente registra uma observação. Exige confirmação " +
			"EXPLÍCITA do OPERADOR da clínica (confirmacao=true) — nunca assumida pelo agente/modelo.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.AtualizarStatusAgendamentoArgs) (*mcp.CallToolResult, tools.AtualizarStatusAgendamentoResult, error) {
		result, err := tools.AtualizarStatusAgendamento(ctx, client, args)
		if err != nil {
			return nil, tools.AtualizarStatusAgendamentoResult{}, err
		}
		return nil, *result, nil
	})
}

// registerGerarSenhaAtendimento registers gerar_senha_atendimento: issues a
// new queue ticket via /appoints/queue-position. No confirmação is
// required — see internal/tools/agenda_admin.go's GerarSenhaAtendimento doc
// comment for why this is treated as a read, not a write.
func registerGerarSenhaAtendimento(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "gerar_senha_atendimento",
		Description: "Gera uma nova senha de atendimento (posição na fila) para uma unidade e tipo de " +
			"senha (0=G, 1=P, 2=C, 3=E, 4=R).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.GerarSenhaAtendimentoArgs) (*mcp.CallToolResult, tools.GerarSenhaAtendimentoResult, error) {
		result, err := tools.GerarSenhaAtendimento(ctx, client, args)
		if err != nil {
			return nil, tools.GerarSenhaAtendimentoResult{}, err
		}
		return nil, *result, nil
	})
}

// registerConsultarFinanceiro registers consultar_financeiro: the read-side
// entry point into the Financeiro endpoint group (fornecedores, repasses,
// contas, vendas, bandeiras, contas correntes, centros de custo, plano de
// contas, invoice por nfse) — see internal/tools/financeiro_consulta.go for
// which tipos map to which endpoint, and why tabelas_privadas/dmed are
// recognized but always refused (endpoints confirmed dead by Fase 0).
func registerConsultarFinanceiro(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "consultar_financeiro",
		Description: "Consulta informações financeiras da clínica, escolhidas por tipo: fornecedores, " +
			"fornecedor (detalhe por fornecedor_id), repasses, contas, vendas, bandeiras, contas_correntes, " +
			"centros_custo, plano_contas, invoice_por_nfse. tabelas_privadas e dmed são reconhecidos mas " +
			"SEMPRE indisponíveis — os endpoints correspondentes responderam 404 real na sondagem contra a " +
			"API da Feegow (rota ausente nesta licença/ambiente).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.ConsultarFinanceiroArgs) (*mcp.CallToolResult, tools.ConsultarFinanceiroResult, error) {
		result, err := tools.ConsultarFinanceiro(ctx, client, args)
		if err != nil {
			return nil, tools.ConsultarFinanceiroResult{}, err
		}
		return nil, *result, nil
	})
}

// registerGerenciarConta registers gerenciar_conta: creates and settles
// "contas" (invoices) — criar, criar_por_agendamento, pagar,
// pagar_agendamento, atualizar_nfse. Requires confirmacao=true from the
// operador for every ação. associar_conta is recognized but always
// unavailable — see internal/tools/financeiro_conta.go's doc comment for
// why (the endpoint's body contract could not be confirmed against the
// sandbox). The two DELETE operations (remover fatura/pagamento) are
// deliberately NOT here — see registerRemoverRegistroFinanceiro.
func registerGerenciarConta(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "gerenciar_conta",
		Description: "Cria e liquida contas (invoices) financeiras da clínica: criar, " +
			"criar_por_agendamento, pagar, pagar_agendamento, atualizar_nfse. Exige confirmação EXPLÍCITA " +
			"do OPERADOR da clínica (confirmacao=true) — nunca assumida pelo agente/modelo. " +
			"associar_conta é reconhecido mas SEMPRE indisponível (contrato do corpo não confirmado " +
			"contra a API da Feegow). Para REMOVER uma fatura ou pagamento, use " +
			"remover_registro_financeiro — não esta tool.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.GerenciarContaArgs) (*mcp.CallToolResult, tools.GerenciarContaResult, error) {
		result, err := tools.GerenciarConta(ctx, client, args)
		if err != nil {
			return nil, tools.GerenciarContaResult{}, err
		}
		return nil, *result, nil
	})
}

// registerGerenciarVoucher registers gerenciar_voucher: cancelar e listar
// vouchers. acao=criar é reconhecida mas SEMPRE indisponível — ver
// internal/tools/financeiro_voucher.go's doc comment (o endpoint de criação
// devolveu erro 500 genérico em toda tentativa da Fase 4b, sem nomear
// nenhum campo). Não existe ação "editar": a API da Feegow não documenta
// nenhum endpoint de edição de voucher.
func registerGerenciarVoucher(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "gerenciar_voucher",
		Description: "Gerencia vouchers da clínica: cancelar (exige confirmação EXPLÍCITA do OPERADOR " +
			"via confirmacao=true) e listar (leitura, paginada). acao=criar é reconhecida mas SEMPRE " +
			"indisponível — o endpoint de criação de voucher da API da Feegow respondeu com erro interno " +
			"em toda tentativa de sondagem, sem confirmar seu contrato.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.GerenciarVoucherArgs) (*mcp.CallToolResult, tools.GerenciarVoucherResult, error) {
		result, err := tools.GerenciarVoucher(ctx, client, args)
		if err != nil {
			return nil, tools.GerenciarVoucherResult{}, err
		}
		return nil, *result, nil
	})
}

// registerRemoverRegistroFinanceiro registers remover_registro_financeiro:
// the two most destructive writes in the project (remove invoice / remove
// payment), deliberately isolated from gerenciar_conta's everyday
// operations — see internal/tools/financeiro_remocao.go's doc comment.
// Requires BOTH confirmacao=true AND ciente_irreversivel=true from the
// operador — a single confirmacao is not enough friction for an
// irreversible delete of a client's financial record.
func registerRemoverRegistroFinanceiro(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "remover_registro_financeiro",
		Description: "IRREVERSÍVEL — remove PERMANENTEMENTE um registro financeiro do cliente (fatura ou " +
			"pagamento), sem nenhuma forma de desfazer documentada pela API da Feegow. Exige DUAS " +
			"confirmações EXPLÍCITAS e independentes do OPERADOR da clínica (nunca assumidas pelo " +
			"agente/modelo): confirmacao=true E ciente_irreversivel=true.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.RemoverRegistroFinanceiroArgs) (*mcp.CallToolResult, tools.RemoverRegistroFinanceiroResult, error) {
		result, err := tools.RemoverRegistroFinanceiro(ctx, client, args)
		if err != nil {
			return nil, tools.RemoverRegistroFinanceiroResult{}, err
		}
		return nil, *result, nil
	})
}

// registerConsultarEstoque registers consultar_estoque: posição de produtos
// e lista de produtos, ambas paginadas com teto (default 20, máx 100).
func registerConsultarEstoque(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "consultar_estoque",
		Description: "Consulta o estoque de produtos da clínica, escolhido por tipo: posicao (posição de " +
			"produtos, filtros em português) ou lista_produtos (lista de produtos, filtros em inglês — os " +
			"nomes dos filtros genuinamente divergem entre os dois endpoints). Sempre paginado " +
			"(limit/offset, teto 100).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.ConsultarEstoqueArgs) (*mcp.CallToolResult, tools.ConsultarEstoqueResult, error) {
		result, err := tools.ConsultarEstoque(ctx, client, args)
		if err != nil {
			return nil, tools.ConsultarEstoqueResult{}, err
		}
		return nil, *result, nil
	})
}

// registerMovimentarEstoque registers movimentar_estoque: acao=inserir_produto
// é a ÚNICA ação que realmente chama a Feegow — entrada, saida e
// movimentacao são reconhecidas mas SEMPRE indisponíveis, porque os
// endpoints correspondentes vivem sob core.feegow.com.br, um host
// confirmado MORTO pela Fase 0 (não resolve). Exige confirmação EXPLÍCITA
// do OPERADOR da clínica (confirmacao=true).
func registerMovimentarEstoque(s *mcp.Server, client *feegow.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "movimentar_estoque",
		Description: "Movimenta o estoque de produtos da clínica. Só acao=inserir_produto está " +
			"disponível de fato (insere um novo produto no catálogo de estoque) — entrada, saida e " +
			"movimentacao são reconhecidas mas SEMPRE indisponíveis, pois os endpoints correspondentes " +
			"vivem sob um host (core.feegow.com.br) confirmado inacessível nesta licença/ambiente. Exige " +
			"confirmação EXPLÍCITA do OPERADOR da clínica (confirmacao=true) — nunca assumida pelo " +
			"agente/modelo.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.MovimentarEstoqueArgs) (*mcp.CallToolResult, tools.MovimentarEstoqueResult, error) {
		result, err := tools.MovimentarEstoque(ctx, client, args)
		if err != nil {
			return nil, tools.MovimentarEstoqueResult{}, err
		}
		return nil, *result, nil
	})
}
