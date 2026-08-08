package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds gerenciar_faturamento, covering the three "Faturamento"
// endpoints ESPECIFICACAO.md §13 documents — buscar, editar, inserir_guia —
// all three living under the SAME wire path, /billing/insurances-billing,
// distinguished only by HTTP method (GET/PUT/POST respectively). This is
// the only path in the whole registry that serves three different
// operations this way (see EndpointDescriptor.Validate's doc comment on
// why PUT had to join the Method allow-list).
//
// Por instrução EXPLÍCITA desta fase, NENHUMA operação real foi exercitada
// além do 422 de corpo vazio: o faturamento (billing/guias de convênio) de
// uma licença de teste não tem endpoint de limpeza/reversão documentado.
// Isso significa que, para as três ações:
//
//   - os NOMES dos campos obrigatórios são confirmados (a Feegow os nomeou
//     na validação 422);
//   - o TIPO exato de cada campo (int vs string), formato e quaisquer
//     campos opcionais adicionais NÃO foram sondados.
//
// acao=inserir_guia em particular expõe seus 16 campos obrigatórios
// individualmente (para a tool ficar autoexplicativa), mas cada um tipado
// como valor livre (any) — nunca int/string assumido sem confirmação, o
// mesmo cuidado que gerenciar_conta já aplica a account/items/installments
// de financial.invoice_create. acao=buscar's campo "billing" segue a mesma
// lógica: não tem sufixo "_id" como billing_type_id/billing_id (usados nas
// outras duas ações), então seu tipo não pôde ser inferido com segurança.

const (
	acaoFaturamentoBuscar      = "buscar"
	acaoFaturamentoEditar      = "editar"
	acaoFaturamentoInserirGuia = "inserir_guia"
)

// GerenciarFaturamentoAcoes returns every valid `acao` value, sorted, for
// use in the MCP tool's description/schema and in tests.
func GerenciarFaturamentoAcoes() []string {
	return []string{acaoFaturamentoBuscar, acaoFaturamentoEditar, acaoFaturamentoInserirGuia}
}

// GerenciarFaturamentoArgs is gerenciar_faturamento's argument shape: one
// acao selects which endpoint/method answers the call, and every field
// below is meaningful only for the acao(s) noted in its own description.
type GerenciarFaturamentoArgs struct {
	Acao string `json:"acao" jsonschema:"O que fazer: buscar, editar, inserir_guia."`

	// buscar (GET) — billing_type_id também usado por editar.
	BillingTypeID *int `json:"billing_type_id,omitempty" jsonschema:"ID do tipo de faturamento. Obrigatório com acao=buscar e acao=editar."`
	Billing       any  `json:"billing,omitempty" jsonschema:"Identificador/filtro da guia para busca — tipo exato NÃO confirmado contra a API real (sem sufixo \"_id\" como os demais identificadores desta tool). Obrigatório com acao=buscar."`

	// editar (PUT)
	BillingID any `json:"billing_id,omitempty" jsonschema:"ID da guia a editar. Obrigatório com acao=editar."`

	// inserir_guia (POST) — os 16 campos que a validação da própria Feegow
	// nomeou como obrigatórios contra um corpo vazio; nenhum foi sondado
	// além do nome (ver o doc comment deste arquivo), por isso todos são
	// valores livres (any).
	UnitID                         any `json:"unit_id,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	InsuranceID                    any `json:"insurance_id,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	InsurancePlanID                any `json:"insurance_plan_id,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	ApplicantProfessionalCouncilID any `json:"applicant_professional_council_id,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	NumberOnTheRequestingCouncil   any `json:"number_on_the_requesting_council,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	UFRequestingCouncil            any `json:"UF_Requesting_Council,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	RequestingCBOCode              any `json:"requesting_CBO_code,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	Hired                          any `json:"hired,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	CarrierCode                    any `json:"carrier_code,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	HiredRequesterID               any `json:"hired_requester_ID,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	HiredRequesterCodeAtCarrier    any `json:"hired_requester_code_at_carrier,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	ANSRegistry                    any `json:"ANS_registry,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	CNESCode                       any `json:"CNES_code,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	RequestingProfessionalID       any `json:"requesting_professional_ID,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`
	RequestDate                    any `json:"request_date,omitempty" jsonschema:"Obrigatório com acao=inserir_guia."`

	Confirmacao bool `json:"confirmacao" jsonschema:"Confirmação EXPLÍCITA do OPERADOR da clínica (nunca assumida pelo agente/modelo). Obrigatória para acao=editar e acao=inserir_guia; ignorada (não exigida) para acao=buscar, que é uma leitura."`
}

// GerenciarFaturamentoResult is gerenciar_faturamento's result. Itens
// carries acao=buscar's content; Sucesso is set for acao=editar/inserir_guia
// — same "business blob" choice as GerenciarContaResult/GerenciarPropostasResult.
type GerenciarFaturamentoResult struct {
	Itens   any  `json:"itens,omitempty"`
	Sucesso bool `json:"sucesso,omitempty"`
}

// GerenciarFaturamento dispatches args.Acao to the matching Feegow endpoint.
func GerenciarFaturamento(ctx context.Context, client *feegow.Client, args GerenciarFaturamentoArgs) (*GerenciarFaturamentoResult, error) {
	result, err := gerenciarFaturamento(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func gerenciarFaturamento(ctx context.Context, client *feegow.Client, args GerenciarFaturamentoArgs) (*GerenciarFaturamentoResult, error) {
	switch args.Acao {
	case acaoFaturamentoBuscar:
		return faturamentoBuscar(ctx, client, args)
	case acaoFaturamentoEditar:
		return faturamentoEditar(ctx, client, args)
	case acaoFaturamentoInserirGuia:
		return faturamentoInserirGuia(ctx, client, args)
	default:
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"acao %q não é reconhecida; valores aceitos: %v", args.Acao, GerenciarFaturamentoAcoes(),
		)}
	}
}

func faturamentoBuscar(ctx context.Context, client *feegow.Client, args GerenciarFaturamentoArgs) (*GerenciarFaturamentoResult, error) {
	if args.BillingTypeID == nil {
		return nil, &ArgumentError{Msg: "billing_type_id é obrigatório para acao=buscar"}
	}
	if args.Billing == nil {
		return nil, &ArgumentError{Msg: "billing é obrigatório para acao=buscar"}
	}

	resp, err := client.Call(ctx, "billing.search_guide", feegow.Request{
		// billing_type_id is dereferenced to a plain int, never passed as
		// *int: this is a GET call, and Client.buildRequest's query-string
		// path (queryValue) has no case for *int — it would reject the
		// whole request with "unsupported query value type *int" if the
		// pointer leaked through here.
		Params: map[string]any{
			"billing_type_id": *args.BillingTypeID,
			"billing":         args.Billing,
		},
	})
	if err != nil {
		return nil, err
	}
	var itens any
	if err := json.Unmarshal(resp.Content, &itens); err != nil {
		return nil, fmt.Errorf("tools: decoding billing/insurances-billing (GET) response: %w", err)
	}
	return &GerenciarFaturamentoResult{Itens: itens}, nil
}

func faturamentoEditar(ctx context.Context, client *feegow.Client, args GerenciarFaturamentoArgs) (*GerenciarFaturamentoResult, error) {
	if err := requireConfirmacaoOperador(args.Confirmacao, "gerenciar_faturamento (acao=editar)"); err != nil {
		return nil, err
	}
	if args.BillingID == nil {
		return nil, &ArgumentError{Msg: "billing_id é obrigatório para acao=editar"}
	}
	if args.BillingTypeID == nil {
		return nil, &ArgumentError{Msg: "billing_type_id é obrigatório para acao=editar"}
	}

	if _, err := client.Call(ctx, "billing.edit_guide", feegow.Request{
		Params: map[string]any{
			"billing_id":      args.BillingID,
			"billing_type_id": *args.BillingTypeID,
		},
	}); err != nil {
		return nil, err
	}

	auditAdminWrite("gerenciar_faturamento:editar", 0, 0)
	return &GerenciarFaturamentoResult{Sucesso: true}, nil
}

func faturamentoInserirGuia(ctx context.Context, client *feegow.Client, args GerenciarFaturamentoArgs) (*GerenciarFaturamentoResult, error) {
	if err := requireConfirmacaoOperador(args.Confirmacao, "gerenciar_faturamento (acao=inserir_guia)"); err != nil {
		return nil, err
	}

	// billing_type_id is checked and dereferenced separately from the rest
	// (a *int, not an any): storing a typed-nil *int straight into a
	// map[string]any and comparing it to nil would NOT catch a caller who
	// omitted it — a nil *int boxed into an any interface value is never
	// == nil in Go, since the interface still carries a non-nil *int type
	// descriptor. Every other field below is already declared `any` in
	// GerenciarFaturamentoArgs, so an absent one really does decode to a
	// literal nil interface and the v == nil check below is correct for
	// all of them.
	var faltando []string
	if args.BillingTypeID == nil {
		faltando = append(faltando, "billing_type_id")
	}

	campos := map[string]any{
		"unit_id":                           args.UnitID,
		"insurance_id":                      args.InsuranceID,
		"insurance_plan_id":                 args.InsurancePlanID,
		"applicant_professional_council_id": args.ApplicantProfessionalCouncilID,
		"number_on_the_requesting_council":  args.NumberOnTheRequestingCouncil,
		"UF_Requesting_Council":             args.UFRequestingCouncil,
		"requesting_CBO_code":               args.RequestingCBOCode,
		"hired":                             args.Hired,
		"carrier_code":                      args.CarrierCode,
		"hired_requester_ID":                args.HiredRequesterID,
		"hired_requester_code_at_carrier":   args.HiredRequesterCodeAtCarrier,
		"ANS_registry":                      args.ANSRegistry,
		"CNES_code":                         args.CNESCode,
		"requesting_professional_ID":        args.RequestingProfessionalID,
		"request_date":                      args.RequestDate,
	}
	for campo, v := range campos {
		if v == nil {
			faltando = append(faltando, campo)
		}
	}
	if len(faltando) > 0 {
		sort.Strings(faltando)
		return nil, &ArgumentError{Msg: fmt.Sprintf(
			"campo(s) obrigatório(s) ausente(s) para acao=inserir_guia: %v", faltando,
		)}
	}
	campos["billing_type_id"] = *args.BillingTypeID

	if _, err := client.Call(ctx, "billing.insert_guide", feegow.Request{Params: campos}); err != nil {
		return nil, err
	}

	auditAdminWrite("gerenciar_faturamento:inserir_guia", 0, 0)
	return &GerenciarFaturamentoResult{Sucesso: true}, nil
}
