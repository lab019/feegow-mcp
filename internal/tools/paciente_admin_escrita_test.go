package tools

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/lab019/feegow-mcp/internal/feegow"
)

// --- atualizar_paciente ---------------------------------------------------

func TestAtualizarPaciente_RequiresConfirmacao_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for unconfirmed atualizar_paciente: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := AtualizarPaciente(ctxWithToken("tok"), client, AtualizarPacienteArgs{
		PacienteID: 1, NomeCompleto: "X", Confirmacao: false,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("AtualizarPaciente error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestAtualizarPaciente_RequiresPacienteID_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a missing paciente_id: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := AtualizarPaciente(ctxWithToken("tok"), client, AtualizarPacienteArgs{Confirmacao: true})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("AtualizarPaciente error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestAtualizarPaciente_RejectsInvalidGenero_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for invalid genero: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := AtualizarPaciente(ctxWithToken("tok"), client, AtualizarPacienteArgs{
		PacienteID: 1, Genero: "X", Confirmacao: true,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("AtualizarPaciente error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestAtualizarPaciente_RejectsMalformedDataNascimento_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for malformed data_nascimento: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := AtualizarPaciente(ctxWithToken("tok"), client, AtualizarPacienteArgs{
		PacienteID: 1, DataNascimento: "10-03-2000", Confirmacao: true,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("AtualizarPaciente error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestAtualizarPaciente_Success_SendsNormalizedWirePayload proves a
// well-formed call sends paciente_id plus every set field, with cpf/
// telefone/celular normalized to digits-only.
func TestAtualizarPaciente_Success_SendsNormalizedWirePayload(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/edit", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Paciente atualizado"}`)
	})
	client := newTestClient(t, mux)

	result, err := AtualizarPaciente(ctxWithToken("tok"), client, AtualizarPacienteArgs{
		PacienteID: 6655, NomeCompleto: "Jose Renato", CPF: "222.222.222-22",
		Telefone: "(21) 5555-4321", Confirmacao: true,
	})
	if err != nil {
		t.Fatalf("AtualizarPaciente: %v", err)
	}
	if !result.Atualizado {
		t.Fatalf("result = %+v, want Atualizado=true", result)
	}
	if gotBody["paciente_id"] != float64(6655) || gotBody["nome_completo"] != "Jose Renato" {
		t.Fatalf("wire body = %+v, missing expected fields", gotBody)
	}
	if gotBody["cpf"] != "22222222222" {
		t.Fatalf("wire cpf = %v, want digits-only", gotBody["cpf"])
	}
	if gotBody["telefone"] != "2155554321" {
		t.Fatalf("wire telefone = %v, want digits-only", gotBody["telefone"])
	}
}

// TestAtualizarPaciente_ServerRejection_IsSanitized proves a
// {"success":false,...} response (doc.txt's own documented failure shape)
// surfaces as the sanitized conflict error, not a raw Feegow body.
func TestAtualizarPaciente_ServerRejection_IsSanitized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/edit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":false,"content":"Paciente não atualizado"}`)
	})
	client := newTestClient(t, mux)

	_, err := AtualizarPaciente(ctxWithToken("tok"), client, AtualizarPacienteArgs{
		PacienteID: 1, Confirmacao: true,
	})
	if !errors.Is(err, ErrConflitoFeegow) {
		t.Fatalf("AtualizarPaciente error = %v, want ErrConflitoFeegow", err)
	}
}

// TestAtualizarPaciente_ServerRejection_NeverLeaksPII is item (a)'s
// "cubra também atualizar_paciente" regression test. /patient/edit is
// EnvelopeStandard, so its success:false already becomes a
// feegow.ConflictError automatically (parseSuccess) before this tool ever
// sees it, and SanitizeFeegowError already sanitizes every ConflictError —
// this proves that full pipeline holds end to end for this specific tool
// (not just at errors_test.go's unit level) when the planted PII is a
// realistic Feegow free-text body ("Paciente Fulano (CPF ...) possui
// pendência financeira").
func TestAtualizarPaciente_ServerRejection_NeverLeaksPII(t *testing.T) {
	const plantedName = "Fulano de Tal da Silva"
	const plantedCPF = "111.111.111-11"

	mux := http.NewServeMux()
	mux.HandleFunc("/patient/edit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":false,"content":"Paciente `+plantedName+` (CPF `+plantedCPF+`) possui pendência financeira"}`)
	})
	client := newTestClient(t, mux)

	_, err := AtualizarPaciente(ctxWithToken("tok"), client, AtualizarPacienteArgs{
		PacienteID: 1, Confirmacao: true,
	})
	if !errors.Is(err, ErrConflitoFeegow) {
		t.Fatalf("AtualizarPaciente error = %v, want ErrConflitoFeegow", err)
	}
	if s := err.Error(); strings.Contains(s, plantedName) || strings.Contains(s, plantedCPF) {
		t.Fatalf("AtualizarPaciente error leaked the raw Feegow body: %q", s)
	}
}

// TestAtualizarPaciente_RejectsCPFWithNoDigits_BeforeAnyFeegowCall is item
// (d)'s regression test for argumentDigitsField (normalize.go): before this
// fix, onlyDigits silently reduced a garbage cpf like "não sei" to "", and
// atualizarPaciente happily omitted "cpf" from the wire body — Feegow was
// never even asked to change it, yet the caller got back
// {"atualizado":true}, indistinguishable from a real update. The field must
// now be rejected, by name, before any Feegow call.
func TestAtualizarPaciente_RejectsCPFWithNoDigits_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a cpf with no digits: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := AtualizarPaciente(ctxWithToken("tok"), client, AtualizarPacienteArgs{
		PacienteID: 1, CPF: "não sei", Confirmacao: true,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("AtualizarPaciente error = %v (%T), want *ArgumentError", err, err)
	}
	if !strings.Contains(argErr.Msg, "cpf") {
		t.Fatalf("ArgumentError.Msg = %q, want it to name the field %q", argErr.Msg, "cpf")
	}
}

// TestAtualizarPaciente_RejectsPhoneFieldsWithNoDigits_BeforeAnyFeegowCall
// extends the cpf case above to every other digits-only field
// atualizar_paciente accepts, proving argumentDigitsField's fix is not
// cpf-specific.
func TestAtualizarPaciente_RejectsPhoneFieldsWithNoDigits_BeforeAnyFeegowCall(t *testing.T) {
	cases := []struct {
		field string
		args  AtualizarPacienteArgs
	}{
		{"telefone", AtualizarPacienteArgs{PacienteID: 1, Telefone: "ramal principal", Confirmacao: true}},
		{"celular", AtualizarPacienteArgs{PacienteID: 1, Celular: "sem número", Confirmacao: true}},
	}
	for _, c := range cases {
		t.Run(c.field, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				t.Fatalf("unexpected Feegow call for %s with no digits: %s %s", c.field, r.Method, r.URL)
			})
			client := newTestClient(t, mux)

			_, err := AtualizarPaciente(ctxWithToken("tok"), client, c.args)
			var argErr *ArgumentError
			if !errors.As(err, &argErr) {
				t.Fatalf("AtualizarPaciente(%s) error = %v (%T), want *ArgumentError", c.field, err, err)
			}
			if !strings.Contains(argErr.Msg, c.field) {
				t.Fatalf("ArgumentError.Msg = %q, want it to name the field %q", argErr.Msg, c.field)
			}
		})
	}
}

// TestAtualizarPaciente_NoPIIInLogs is item (e)'s coverage for
// atualizar_paciente: a write tool whose ARGUMENTS carry the PII (nome,
// cpf) rather than the response — auditAdminWrite must only ever log
// paciente_id, never any of the fields being written.
func TestAtualizarPaciente_NoPIIInLogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/edit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"content":"Paciente atualizado"}`)
	})

	assertNoPIIInLog(t, mux, []string{"Segredo Pessoal", "11111111111"}, func(client *feegow.Client) error {
		_, err := AtualizarPaciente(ctxWithToken("tok"), client, AtualizarPacienteArgs{
			PacienteID: 1, NomeCompleto: "Segredo Pessoal", CPF: "111.111.111-11", Confirmacao: true,
		})
		return err
	})
}

// --- anexar_ao_prontuario -------------------------------------------------

func TestAnexarAoProntuario_RequiresConfirmacao_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for unconfirmed anexar_ao_prontuario: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := AnexarAoProntuario(ctxWithToken("tok"), client, AnexarAoProntuarioArgs{
		PacienteID: 1, Base64File: "data:application/pdf;base64,AAAA", Confirmacao: false,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("AnexarAoProntuario error = %v (%T), want *ArgumentError", err, err)
	}
}

func TestAnexarAoProntuario_RequiresBase64File_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for a missing base64_file: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := AnexarAoProntuario(ctxWithToken("tok"), client, AnexarAoProntuarioArgs{
		PacienteID: 1, Confirmacao: true,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("AnexarAoProntuario error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestAnexarAoProntuario_RequiresPacienteIDOrCPFPlusNascimento proves
// neither paciente_id nor a complete cpf+nascimento pair alone is
// sufficient without the other identification path.
func TestAnexarAoProntuario_RequiresPacienteIDOrCPFPlusNascimento(t *testing.T) {
	cases := []struct {
		name string
		args AnexarAoProntuarioArgs
	}{
		{"nothing at all", AnexarAoProntuarioArgs{Base64File: "data:application/pdf;base64,AAAA", Confirmacao: true}},
		{"cpf without nascimento", AnexarAoProntuarioArgs{CPF: "11111111111", Base64File: "data:application/pdf;base64,AAAA", Confirmacao: true}},
		{"nascimento without cpf", AnexarAoProntuarioArgs{Nascimento: "2000-01-01", Base64File: "data:application/pdf;base64,AAAA", Confirmacao: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				t.Fatalf("unexpected Feegow call for %s: %s %s", c.name, r.Method, r.URL)
			})
			client := newTestClient(t, mux)

			_, err := AnexarAoProntuario(ctxWithToken("tok"), client, c.args)
			var argErr *ArgumentError
			if !errors.As(err, &argErr) {
				t.Fatalf("AnexarAoProntuario(%s) error = %v (%T), want *ArgumentError", c.name, err, err)
			}
		})
	}
}

// TestAnexarAoProntuario_Success_ByPacienteID proves the paciente_id path
// and decodes fileId from the non-standard {success,fileId,content} body.
func TestAnexarAoProntuario_Success_ByPacienteID(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/upload-base64", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"fileId":6255,"content":"Arquivo enviado com sucesso."}`)
	})
	client := newTestClient(t, mux)

	result, err := AnexarAoProntuario(ctxWithToken("tok"), client, AnexarAoProntuarioArgs{
		PacienteID: 6563, Base64File: "data:application/pdf;base64,AAAA", ArquivoDescricao: "Exame", Confirmacao: true,
	})
	if err != nil {
		t.Fatalf("AnexarAoProntuario: %v", err)
	}
	if !result.Anexado || result.FileID != 6255 {
		t.Fatalf("result = %+v, want {Anexado:true FileID:6255}", result)
	}
	if gotBody["paciente_id"] != float64(6563) || gotBody["base64_file"] != "data:application/pdf;base64,AAAA" {
		t.Fatalf("wire body = %+v, missing expected fields", gotBody)
	}
}

// TestAnexarAoProntuario_Success_ByCPFAndNascimento proves the alternate
// identification path (no paciente_id) also works and normalizes cpf.
func TestAnexarAoProntuario_Success_ByCPFAndNascimento(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/upload-base64", func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		writeJSON(t, w, http.StatusOK, `{"success":true,"fileId":1,"content":"Arquivo enviado com sucesso."}`)
	})
	client := newTestClient(t, mux)

	_, err := AnexarAoProntuario(ctxWithToken("tok"), client, AnexarAoProntuarioArgs{
		CPF: "348.880.158-64", Nascimento: "2000-01-31", Base64File: "data:application/pdf;base64,AAAA", Confirmacao: true,
	})
	if err != nil {
		t.Fatalf("AnexarAoProntuario: %v", err)
	}
	if gotBody["cpf"] != "34888015864" || gotBody["nascimento"] != "2000-01-31" {
		t.Fatalf("wire body = %+v, want normalized cpf + nascimento", gotBody)
	}
	if _, has := gotBody["paciente_id"]; has {
		t.Fatalf("wire body leaked paciente_id when identifying by cpf+nascimento: %+v", gotBody)
	}
}

// TestAnexarAoProntuario_RejectsMalformedNascimento_BeforeAnyFeegowCall
// proves nascimento is validated as ISO-8601 before any Feegow call.
func TestAnexarAoProntuario_RejectsMalformedNascimento_BeforeAnyFeegowCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feegow call for malformed nascimento: %s %s", r.Method, r.URL)
	})
	client := newTestClient(t, mux)

	_, err := AnexarAoProntuario(ctxWithToken("tok"), client, AnexarAoProntuarioArgs{
		CPF: "11111111111", Nascimento: "31-01-2000", Base64File: "data:application/pdf;base64,AAAA", Confirmacao: true,
	})
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("AnexarAoProntuario error = %v (%T), want *ArgumentError", err, err)
	}
}

// TestAnexarAoProntuario_ServerSuccessFalse_IsAnError proves a 200 response
// whose body reports success:false (EnvelopeNone means this never becomes
// a feegow.ConflictError automatically — the tool must check it itself)
// surfaces as an error rather than a false "Anexado: true".
func TestAnexarAoProntuario_ServerSuccessFalse_IsAnError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/upload-base64", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":false,"content":"Arquivo inválido"}`)
	})
	client := newTestClient(t, mux)

	_, err := AnexarAoProntuario(ctxWithToken("tok"), client, AnexarAoProntuarioArgs{
		PacienteID: 1, Base64File: "data:application/pdf;base64,AAAA", Confirmacao: true,
	})
	if err == nil {
		t.Fatal("want an error when Feegow reports success:false")
	}
}

// TestAnexarAoProntuario_ServerRejection_NeverLeaksPII is item (a)'s core
// regression test — the exact bug the adversarial review caught:
// anexarAoProntuario used to interpolate Feegow's raw, free-text
// EnvelopeNone body straight into its own error
// (`fmt.Errorf("...: %s", body.Content)`), and that body can carry the
// patient's own nome/CPF right back into a public-facing agent transcript.
// checkEnvelopeNoneSuccess/ErrOperacaoNaoConfirmadaFeegow must keep that
// from happening no matter what free text Feegow sends back.
func TestAnexarAoProntuario_ServerRejection_NeverLeaksPII(t *testing.T) {
	const plantedName = "Fulano de Tal da Silva"
	const plantedCPF = "111.111.111-11"

	mux := http.NewServeMux()
	mux.HandleFunc("/patient/upload-base64", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":false,"content":"Paciente `+plantedName+` (CPF `+plantedCPF+`) já possui um arquivo com esse nome"}`)
	})
	client := newTestClient(t, mux)

	_, err := AnexarAoProntuario(ctxWithToken("tok"), client, AnexarAoProntuarioArgs{
		PacienteID: 1, Base64File: "data:application/pdf;base64,AAAA", Confirmacao: true,
	})
	if !errors.Is(err, ErrOperacaoNaoConfirmadaFeegow) {
		t.Fatalf("AnexarAoProntuario error = %v, want ErrOperacaoNaoConfirmadaFeegow", err)
	}
	if s := err.Error(); strings.Contains(s, plantedName) || strings.Contains(s, plantedCPF) {
		t.Fatalf("AnexarAoProntuario error leaked the raw Feegow body: %q", s)
	}
}

// TestAnexarAoProntuario_NoPIIInLogs is item (e)'s coverage for
// anexar_ao_prontuario: the caller's own cpf/nascimento arguments must
// never reach the audit log line, same discipline as
// TestAtualizarPaciente_NoPIIInLogs.
func TestAnexarAoProntuario_NoPIIInLogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/patient/upload-base64", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"success":true,"fileId":1,"content":"Arquivo enviado com sucesso."}`)
	})

	assertNoPIIInLog(t, mux, []string{"11111111111"}, func(client *feegow.Client) error {
		_, err := AnexarAoProntuario(ctxWithToken("tok"), client, AnexarAoProntuarioArgs{
			CPF: "111.111.111-11", Nascimento: "2000-01-01", Base64File: "data:application/pdf;base64,AAAA", Confirmacao: true,
		})
		return err
	})
}
