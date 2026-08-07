package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// This file holds the admin profile's patient WRITE tools —
// atualizar_paciente (/patient/edit) and anexar_ao_prontuario
// (/patient/upload-base64). Both require explicit confirmation from the
// clínica's own operador (requireConfirmacaoOperador, guardas.go) — the
// same "escritas com confirmação explícita" discipline Fase 3's atendimento
// writes already follow, generalized to who is actually confirming on this
// profile. Neither applies the atendimento posse/identity checks
// (resolveOwnedAgendamento and friends): those exist specifically because
// an unauthenticated public channel cannot otherwise tell who is really
// asking, and that risk does not exist here — see this package's paciente_
// admin.go doc comment.

// --- atualizar_paciente ---------------------------------------------------

// AtualizarPacienteArgs is atualizar_paciente's argument shape, mirroring
// every field /patient/edit documents. paciente_id is the only mandatory
// field beyond Confirmacao — every other field is send-if-set, exactly
// like /patient/edit's own "(opcional)" contract.
type AtualizarPacienteArgs struct {
	PacienteID     int    `json:"paciente_id" jsonschema:"ID do paciente a editar. Obrigatório."`
	NomeCompleto   string `json:"nome_completo,omitempty" jsonschema:"Nome completo (máx 200 caracteres)."`
	CPF            string `json:"cpf,omitempty" jsonschema:"CPF (apenas dígitos, máx 11)."`
	Email          string `json:"email,omitempty" jsonschema:"E-mail."`
	DataNascimento string `json:"data_nascimento,omitempty" jsonschema:"Data de nascimento, ISO-8601 (YYYY-MM-DD)."`
	Genero         string `json:"genero,omitempty" jsonschema:"\"M\" ou \"F\"."`
	Telefone       string `json:"telefone,omitempty" jsonschema:"Telefone (apenas dígitos, máx 20)."`
	Celular        string `json:"celular,omitempty" jsonschema:"Celular (apenas dígitos, máx 20)."`
	Telefone2      string `json:"telefone2,omitempty" jsonschema:"Segundo telefone (apenas dígitos, máx 20)."`
	Celular2       string `json:"celular2,omitempty" jsonschema:"Segundo celular (apenas dígitos, máx 20)."`
	TabelaID       *int   `json:"tabela_id,omitempty" jsonschema:"ID da tabela particular (ver consultar_paciente_clinico tipo=tabelas_particulares)."`
	CEP            string `json:"cep,omitempty" jsonschema:"CEP (máx 9 caracteres)."`
	Cidade         string `json:"cidade,omitempty"`
	Estado         string `json:"estado,omitempty"`
	Endereco       string `json:"endereco,omitempty"`
	Numero         string `json:"numero,omitempty"`
	Complemento    string `json:"complemento,omitempty"`
	Bairro         string `json:"bairro,omitempty"`
	NomeMae        string `json:"nome_mae,omitempty" jsonschema:"Nome da mãe (máx 200 caracteres)."`

	Confirmacao bool `json:"confirmacao" jsonschema:"Confirmação EXPLÍCITA do OPERADOR da clínica (nunca assumida pelo agente/modelo) de que deseja salvar estas alterações no cadastro do paciente."`
}

// AtualizarPacienteResult is atualizar_paciente's result.
type AtualizarPacienteResult struct {
	Atualizado bool `json:"atualizado"`
}

// AtualizarPaciente edits an existing paciente's cadastro via
// /patient/edit. A 200 response with success:false (doc.txt documents both
// {"success":true,"content":"Paciente atualizado"} and
// {"success":false,"content":"Paciente não atualizado"} as real outcomes)
// already surfaces as a feegow.ConflictError via parseSuccess — SanitizeFeegowError
// folds that into the admin-appropriate error the same way every other
// write tool in this package does.
func AtualizarPaciente(ctx context.Context, client *feegow.Client, args AtualizarPacienteArgs) (*AtualizarPacienteResult, error) {
	result, err := atualizarPaciente(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func atualizarPaciente(ctx context.Context, client *feegow.Client, args AtualizarPacienteArgs) (*AtualizarPacienteResult, error) {
	if err := requireConfirmacaoOperador(args.Confirmacao, "atualizar_paciente"); err != nil {
		return nil, err
	}
	if args.PacienteID <= 0 {
		return nil, &ArgumentError{Msg: "paciente_id é obrigatório"}
	}
	if args.DataNascimento != "" {
		if _, err := time.Parse(feegow.ISO8601, args.DataNascimento); err != nil {
			return nil, &ArgumentError{Msg: "data_nascimento deve estar em ISO-8601 (YYYY-MM-DD)"}
		}
	}
	if args.Genero != "" && args.Genero != "M" && args.Genero != "F" {
		return nil, &ArgumentError{Msg: `genero deve ser "M" ou "F"`}
	}

	cpf, err := argumentDigitsField("cpf", args.CPF)
	if err != nil {
		return nil, err
	}
	tel, err := argumentDigitsField("telefone", args.Telefone)
	if err != nil {
		return nil, err
	}
	cel, err := argumentDigitsField("celular", args.Celular)
	if err != nil {
		return nil, err
	}
	tel2, err := argumentDigitsField("telefone2", args.Telefone2)
	if err != nil {
		return nil, err
	}
	cel2, err := argumentDigitsField("celular2", args.Celular2)
	if err != nil {
		return nil, err
	}

	wire := map[string]any{"paciente_id": args.PacienteID}
	if args.NomeCompleto != "" {
		wire["nome_completo"] = args.NomeCompleto
	}
	if cpf != "" {
		wire["cpf"] = cpf
	}
	if args.Email != "" {
		wire["email"] = args.Email
	}
	if args.DataNascimento != "" {
		wire["data_nascimento"] = args.DataNascimento
	}
	if args.Genero != "" {
		wire["genero"] = args.Genero
	}
	if tel != "" {
		wire["telefone"] = tel
	}
	if cel != "" {
		wire["celular"] = cel
	}
	if tel2 != "" {
		wire["telefone2"] = tel2
	}
	if cel2 != "" {
		wire["celular2"] = cel2
	}
	if args.TabelaID != nil {
		wire["tabela_id"] = *args.TabelaID
	}
	if args.CEP != "" {
		wire["cep"] = args.CEP
	}
	if args.Cidade != "" {
		wire["cidade"] = args.Cidade
	}
	if args.Estado != "" {
		wire["estado"] = args.Estado
	}
	if args.Endereco != "" {
		wire["endereco"] = args.Endereco
	}
	if args.Numero != "" {
		wire["numero"] = args.Numero
	}
	if args.Complemento != "" {
		wire["complemento"] = args.Complemento
	}
	if args.Bairro != "" {
		wire["bairro"] = args.Bairro
	}
	if args.NomeMae != "" {
		wire["nome_mae"] = args.NomeMae
	}

	if _, err := client.Call(ctx, "patient.edit", feegow.Request{Params: wire}); err != nil {
		return nil, err
	}

	auditAdminWrite("atualizar_paciente", args.PacienteID, 0)
	return &AtualizarPacienteResult{Atualizado: true}, nil
}

// --- anexar_ao_prontuario -------------------------------------------------

// AnexarAoProntuarioArgs is anexar_ao_prontuario's argument shape, mirroring
// /patient/upload-base64: the paciente is identified either by paciente_id
// OR by cpf+nascimento together (doc.txt documents both as valid,
// mutually-substitutable ways to say which paciente this upload is for).
type AnexarAoProntuarioArgs struct {
	PacienteID       int    `json:"paciente_id,omitempty" jsonschema:"ID do paciente. Informe paciente_id OU (cpf + nascimento)."`
	CPF              string `json:"cpf,omitempty" jsonschema:"CPF do paciente (apenas dígitos). Use junto com nascimento quando não tiver paciente_id."`
	Nascimento       string `json:"nascimento,omitempty" jsonschema:"Data de nascimento do paciente, ISO-8601 (YYYY-MM-DD). Use junto com cpf quando não tiver paciente_id."`
	Base64File       string `json:"base64_file" jsonschema:"Conteúdo do arquivo em base64, incluindo o prefixo \"data:<content-type>;base64,<hash>\". Obrigatório."`
	ArquivoDescricao string `json:"arquivo_descricao,omitempty" jsonschema:"Descrição do arquivo."`
	ArquivoID        *int   `json:"arquivo_id,omitempty" jsonschema:"ID de um arquivo já existente a ser substituído."`

	Confirmacao bool `json:"confirmacao" jsonschema:"Confirmação EXPLÍCITA do OPERADOR da clínica (nunca assumida pelo agente/modelo) de que deseja anexar este arquivo ao prontuário."`
}

// AnexarAoProntuarioResult is anexar_ao_prontuario's result. FileID is
// omitted (zero value) when Feegow's response didn't carry a recognizable
// fileId — see anexarAoProntuario: unlike a shape this package can't trust
// for a not-yet-created resource, the upload itself already succeeded by
// the time that would happen, so this never turns into an error the way
// decodeCreatedPatientID's unrecognized shape does.
type AnexarAoProntuarioResult struct {
	Anexado bool `json:"anexado"`
	FileID  int  `json:"file_id,omitempty"`
}

// uploadBase64Body mirrors /patient/upload-base64's whole response body —
// EnvelopeNone (see internal/feegow/registry.go's patient.upload_base64
// Notes) because fileId lives OUTSIDE the usual "content" field, where
// EnvelopeStandard's parseSuccess would silently drop it.
type uploadBase64Body struct {
	Success bool   `json:"success"`
	FileID  int    `json:"fileId"`
	Content string `json:"content"`
}

// AnexarAoProntuario attaches a file to a paciente's prontuário via
// /patient/upload-base64.
func AnexarAoProntuario(ctx context.Context, client *feegow.Client, args AnexarAoProntuarioArgs) (*AnexarAoProntuarioResult, error) {
	result, err := anexarAoProntuario(ctx, client, args)
	if err != nil {
		return nil, SanitizeFeegowError(err)
	}
	return result, nil
}

func anexarAoProntuario(ctx context.Context, client *feegow.Client, args AnexarAoProntuarioArgs) (*AnexarAoProntuarioResult, error) {
	if err := requireConfirmacaoOperador(args.Confirmacao, "anexar_ao_prontuario"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Base64File) == "" {
		return nil, &ArgumentError{Msg: "base64_file é obrigatório"}
	}
	cpf := strings.TrimSpace(args.CPF)
	nascimento := strings.TrimSpace(args.Nascimento)
	if args.PacienteID <= 0 && (cpf == "" || nascimento == "") {
		return nil, &ArgumentError{Msg: "informe paciente_id OU (cpf + nascimento)"}
	}
	if nascimento != "" {
		if _, err := time.Parse(feegow.ISO8601, nascimento); err != nil {
			return nil, &ArgumentError{Msg: "nascimento deve estar em ISO-8601 (YYYY-MM-DD)"}
		}
	}

	wire := map[string]any{"base64_file": args.Base64File}
	if args.PacienteID > 0 {
		wire["paciente_id"] = args.PacienteID
	}
	if cpfDigits := onlyDigits(cpf); cpfDigits != "" {
		wire["cpf"] = cpfDigits
	}
	if nascimento != "" {
		wire["nascimento"] = nascimento
	}
	if args.ArquivoDescricao != "" {
		wire["arquivo_descricao"] = args.ArquivoDescricao
	}
	if args.ArquivoID != nil {
		wire["arquivo_id"] = *args.ArquivoID
	}

	resp, err := client.Call(ctx, "patient.upload_base64", feegow.Request{Params: wire})
	if err != nil {
		return nil, err
	}

	var body uploadBase64Body
	if err := json.Unmarshal(resp.Content, &body); err != nil {
		return nil, fmt.Errorf("tools: decoding patient/upload-base64 response: %w", err)
	}
	if err := checkEnvelopeNoneSuccess(body.Success); err != nil {
		return nil, err
	}

	auditAdminWrite("anexar_ao_prontuario", args.PacienteID, 0)
	return &AnexarAoProntuarioResult{Anexado: true, FileID: body.FileID}, nil
}
