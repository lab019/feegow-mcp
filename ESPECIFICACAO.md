# feegow-mcp — Especificação

MCP server que expõe o ERP **Feegow Clinic** como tools para o `agent-runtime`
da plataforma LAB019.

Este documento é o plano de implementação: arquitetura, contrato de auth,
recorte de tools, inventário de endpoints e fases. Nada aqui está implementado
ainda — o repositório começa vazio e este é o commit inicial.

---

## 1. O que este serviço é

Um **tradutor sem estado** entre o protocolo MCP e a API REST da Feegow.

Ele não guarda nada, não tem banco, não faz OAuth e não conhece o `org_id` da
plataforma. Recebe o token da clínica pronto no header, chama a Feegow, e
devolve o resultado normalizado.

O trabalho de engenharia real **não** é o proxy — é a **normalização**. A API da
Feegow usa três convenções de nome de parâmetro, três esquemas de paginação e
dois formatos de data, variando por endpoint dentro de um mesmo grupo (§5). Um
LLM não acerta isso de forma confiável, e cada erro vira um `422` no meio de uma
conversa com um paciente. As tools expõem uma superfície coerente; o client
traduz por endpoint.

Serviço irmão do `agent-mcp-google` — mesmo modelo stateless, mesma stack (Go +
`modelcontextprotocol/go-sdk`), mesmo formato de deploy (binário estático em
distroless).

| | |
| --- | --- |
| Stack | Go + `modelcontextprotocol/go-sdk` |
| Porta | `8087` |
| Transporte | Streamable-HTTP |
| Rotas | `POST /mcp`, `POST /mcp/admin`, `GET /healthz` |
| Rota pública | nenhuma — o runtime alcança por `agentnet` |
| Estado | nenhum (sem DB, sem cache, sem sessão) |

---

## 2. Auth — o token nunca é nosso

A Feegow autentica por um header `x-access-token`. O token é um **JWT liberado
pelo usuário master da licença** pela interface do Feegow Clinic. Ele é do
usuário/tenant: carrega a identidade e as permissões daquele usuário dentro do
ERP, e a Feegow — não este serviço — é a autoridade sobre o que ele pode fazer.

```
agent-runtime
  ├─ registro "feegow"        auth: byok:feegow        ─┐
  └─ registro "feegow-admin"  auth: byok:feegow_admin  ─┤ agent-secrets
                                                        │ GET /mcp/<provider>?org_id=<org>
                                                        ▼
                          Authorization: Bearer <token Feegow do tenant>
                                          │
                                   feegow-mcp :8087
                                          │  x-access-token: <o mesmo token>
                                          ▼
                                   api.feegow.com
```

O `agent-runtime` resolve o segredo por `(provider, org_id)` no `agent-secrets`
e encaminha como bearer. Este serviço extrai o bearer e o repassa no header da
Feegow. Requisição sem bearer válido é rejeitada com `401` antes de qualquer
dispatch MCP (fail-closed). O token **nunca** é logado.

### Onde o isolamento multi-tenant vive

Fora deste serviço — na resolução `byok` do runtime. Vale dizer isso
explicitamente em vez de fingir que protegemos algo que não enxergamos:

- **Mais forte num aspecto:** sem estado, não há cache por org nem store para
  vazar entre tenants. Duas requisições consecutivas com tokens diferentes não
  compartilham absolutamente nada.
- **Mais fraco em outro:** este serviço não consegue detectar um roteamento
  errado do runtime. Ele confia no chamador.

Por isso o teste de segurança aqui **não** é "org A não alcança segredo de org
B" (não temos org). É um **teste de statelessness**: nada de uma requisição
sobrevive para a seguinte. É a garantia que o serviço de fato oferece, e é
testável.

### O que o serviço faz com o token sem guardá-lo

O token é um JWT. O serviço decodifica **apenas o payload** — nunca loga o token
— para:

1. checar `exp` antes de sair o request, devolvendo *"credencial da clínica
   expirada, recadastre"* em vez de um `401` opaco vindo da Feegow;
2. registrar identidade de usuário/licença no log de auditoria, já que não há
   `org_id` da plataforma disponível.

---

## 3. Os dois perfis

O recorte pedido são duas visões: um perfil de **atendimento ao cliente** e um
perfil de **administração do ERP** via chat privado.

**Um binário, dois toolsets, dois registros.** O toolset precisa ser diferente
já no `tools/list` — um agente de atendimento que enxerga `remover_pagamento`
na lista pode chamá-la, e a única defesa robusta é a tool não existir naquela
superfície.

| Registro | Rota | Auth | Segredo | `allow_anonymous` | Toolset |
| --- | --- | --- | --- | --- | --- |
| `feegow` | `POST /mcp` | `byok:feegow` | `mcp/feegow` | `true` | atendimento (35 endpoints) |
| `feegow-admin` | `POST /mcp/admin` | `byok:feegow_admin` | `mcp/feegow_admin` | `false` | superset (85 endpoints) |

### O gate do admin é dado do tenant, nunca configuração

Não há env var de política, nem allowlist de `org_id`. Um MCP global serve todos
os tenants a partir do mesmo processo — qualquer knob por cliente em env exigiria
redeploy do operador e não escalaria.

O gate é a **existência do segredo**: tenant que não guardou `feegow_admin` faz o
**runtime** falhar fechado (`McpAuthUnavailableError`, 502) antes de este serviço
ser chamado. A capacidade existe se, e somente se, a clínica emitiu um token
Feegow com esse poder — decisão do cliente, no ERP dele.

Complementos, todos também fora do env:

- `allow_anonymous` **difere** entre os dois registros, e é o único lugar onde
  os perfis divergem em política: `true` no atendimento, `false` no admin.

  No atendimento tem que ser `true`. O canal é PÚBLICO por decisão de produto —
  quem fala é o paciente, por voz ou widget, sem login — e é justamente por isso
  que esse perfil trabalha com identidade **declarada** (os dois fatos do §6),
  erro uniforme e retorno mínimo. Com `false` esse desenho inteiro vira peso
  morto: o dispatcher do runtime barra a tool em `role == "anonymous" and not
  allow_anonymous` **antes** de resolver o `byok`, e o canal público morre na
  primeira chamada.

  Sessão anônima **tem** `org_id` — vem de `app_metadata.org_id`, setado
  autoritativamente pelo Supabase — então o `byok:feegow` resolve o token
  daquela clínica normalmente. Uma versão anterior desta seção afirmava o
  contrário (que anônimo não teria org, logo não teria segredo a resolver) e
  concluía `false` nos dois; era falso, e foi o que produziu o valor errado
  aqui. O compose da `agent-operation` sempre registrou `true` no atendimento.

  No admin continua `false`, e aí sim por segurança: são as tools de
  financeiro, estoque, laudos e a remoção irreversível de fatura e pagamento. A
  ausência do segredo `feegow_admin` já é barreira real, mas não é a única que
  se quer ali.
- Qual specialist carrega qual server é `tools=["feegow"]` vs
  `tools=["feegow-admin"]` no AgentSpec, que o próprio tenant gerencia.

**Limitação conhecida:** `byok:` não oferece a propriedade "somente humano
autenticado". Um turno disparado pelo scheduler consegue resolver o segredo e
alcançar o perfil admin. A trava disso é o AgentSpec — o tenant não concede
`feegow-admin` a specialist de background. Ficou onde deve ficar: com o cliente.

### Configuração por clínica

Não existe. Unidade padrão e especialidade padrão não são config deste serviço:
tools de descoberta resolvem nome→id, e o default é escrito no **prompt do
specialist do tenant**, que já é a superfície de configuração dele. A identidade
da clínica sai do próprio token. A versão de cada endpoint é propriedade do
contrato (§5), não do cliente.

Sobra em env — tudo idêntico para todo tenant, descrevendo a instalação e nunca
o cliente:

| Variável | Default | Para quê |
| --- | --- | --- |
| `PORT` | `8087` | porta HTTP |
| `LOG_LEVEL` | `INFO` | verbosidade |
| `FEEGOW_HOST_OVERRIDE` | _(vazio)_ | redireciona todos os hosts para um stub — só teste/CI |

---

## 4. Layout

```
main.go                    # HTTP, /healthz, os dois mounts, graceful shutdown
internal/auth/             # extração do bearer, middleware fail-closed (401),
                           # decode do payload JWT (exp + auditoria)
internal/feegow/           # client multi-host, NORMALIZAÇÃO (§5), envelope,
                           # os dois formatos de erro, paginação
internal/tools/            # lógica pura por domínio — sem dependência do SDK MCP
internal/mcpserver/        # único lugar que fala com go-sdk/mcp: registro + perfis
docs/feegow-api.md         # tabela de tradução (§5) — o contrato
```

A separação `tools` (puro) ↔ `mcpserver` (SDK) é a mesma do `agent-mcp-google`:
mantém os testes sem rede e sem SDK.

---

## 5. Normalização — o núcleo do serviço

A API da Feegow não tem uma convenção. Tem várias, e elas variam **por endpoint
dentro do mesmo grupo**.

### Formatos de data

| Endpoint | Parâmetro | Formato |
| --- | --- | --- |
| `/appoints/search`, `/appoints/available-schedule` | `data_start` / `data_end` | `DD-MM-YYYY` |
| `/appoints/new-appoint` | `data` | `DD-MM-YYYY` |
| `/lock/list` | `date_start` / `date_end` | `YYYY-MM-DD` |
| `/financial/list-invoice` | `data_start` | `DD-MM-YYYY` |
| `/financial/list-sales` | `date_start` | `YYYY-MM-DD` |
| `/financial/dmed` | `dataInicio` / `dataFim` | `YYYY-MM-DD` |
| `cartao-beneficios/*/datagrid` | `initialDate` | camelCase inglês |

Três convenções de nome (`data_start`, `date_start`, `dataInicio`) e dois
formatos de data.

### Paginação

| Endpoint | Esquema | Armadilha |
| --- | --- | --- |
| `/appoints/search` | `start` + `offset` | `offset=50` é o **tamanho da página**, não deslocamento. E a paginação **só liga** quando `list_procedures` é usado |
| `/financial/dmed` | `limit` + `offset` | aqui `offset` é deslocamento de verdade |
| `cartao-beneficios/*/datagrid` | `page` + `perPage` | doc diz "perPage padrão é 1", o exemplo usa 10 |

O mesmo parâmetro `offset` significa coisas opostas em dois endpoints.

### Regra

As tools expõem **sempre**: datas em ISO-8601 (`YYYY-MM-DD`), paginação como
`limit`/`offset` com semântica de deslocamento, nomes em uma única convenção.
O `internal/feegow` traduz por endpoint, e o `docs/feegow-api.md` é a tabela de
tradução — com **um teste por endpoint** provando a conversão. Não é anexo: é o
coração do repositório.

### Envelope e erros

Sucesso é `{"success": bool, "content": ...}`. Erro tem **duas formas**:

```jsonc
// 409 — envelope normal
{"success": false, "content": "String do erro"}

// 422 — SEM envelope, mapa de campo → erros de validação
{"paciente_id": ["validation.required"]}
```

| Código | Significado documentado |
| --- | --- |
| `401` | Chave da API não está definida no header |
| `403` | **Chave da API inativa** — não é permissão negada |
| `409` | Conflito interno, específico de cada método |
| `422` | Input inválido |
| `5xx` | Erro interno |

Não há `404` nem `429` no contrato documentado. `403` pertence à família de
"credencial morta" (junto com `exp` vencido), e a mensagem ao agente deve dizer
*recadastre a credencial*, não *você não tem permissão*.

### Hosts

Quatro, e eles pertencem ao contrato (constantes por grupo no código), não ao env:

| Host | Grupos |
| --- | --- |
| `api.feegow.com/v1/api` | a maioria |
| `cartao-beneficios.feegow.com` | Cartão de Benefício |
| `core.feegow.com.br` | Estoque (`/financial2/external/...`) |
| `core.feegow.com` | Financeiro (`/financial2/external/...`) |

O mesmo caminho `financial2/external` aparece sob dois TLDs diferentes. Ou a doc
erra em um dos dois, ou são serviços distintos — item de smoke test (§8).

---

## 6. Tools — por tarefa, não por endpoint

O perfil de atendimento cobre 35 endpoints; o admin, 85. Mapear 1:1 repete o
problema que a plataforma já enfrentou e resolveu na ADR 0002 do
`agent-base-tools`: catálogo grande demais para o modelo navegar, e o operador
acaba concedendo o server inteiro por não conseguir selecionar tools.

### Perfil atendimento

| Tool | Endpoints por trás |
| --- | --- |
| `listar_catalogo(tipo)` | unidades, locais, especialidades, convênios, procedimentos (+tipos, grupos, pacotes), profissionais, canais, motivos, status — ~14 endpoints numa tool |
| `buscar_horarios_livres` | `/appoints/available-schedule` + `/lock/list` |
| `consultar_agenda` | `/appoints/search` |
| `agendar` | `/appoints/new-appoint` |
| `remarcar` | `/appoints/reschedule` |
| `cancelar` | `/appoints/cancel-appoint` |
| `atualizar_status` | `/appoints/statusUpdate` |
| `buscar_paciente` | `/patient/search`, `/patient/list` |
| `criar_paciente` / `atualizar_paciente` | `/patient/create`, `/patient/edit` |
| `consultar_beneficio` | `/external/contract/datagrid`, `/external/plan/datagrid` |

O corte entre perfis é **por endpoint, não por grupo**: o grupo "Cartão de
Benefício" tem listagem (atendimento) e `create`/`update` de contrato e plano
(admin) lado a lado. `/patient/upload-base64` grava no prontuário e fica no
admin.

### Perfil admin

Superset, somando Financeiro (23), Estoque (7), Propostas (5), Laudos (4),
Faturamento (3), Relatórios (2), Funcionários (1) e as escritas de Cartão de
Benefício (4).

---

## 7. Regras de negócio que viram guarda e teste

A documentação entrega regras que, ignoradas, viram `409` na frente do paciente:

1. **Sem agendamento retroativo** — `data >= hoje`, validado antes do request.
2. **`valor` é em centavos**, e se `plano=1` (convênio) o `valor` *deve* ser `0`.
   Regra condicional entre dois campos.
3. **`unidade_id=0` ≠ omitido** — `0` significa "unidade principal", ausente
   significa "todas as unidades". Um default errado muda o resultado em silêncio.
4. **`agendar` é uma corrida** — `available-schedule` diz livre e `new-appoint`
   responde `409` "Horário ocupado". A tool devolve isso como estado recuperável
   ("esse horário acabou de ser ocupado, os próximos são…"), não como falha.
5. **`hora` no `new-appoint`, `horario` no `reschedule`** — mesmo conceito, dois
   nomes.

---

## 8. Bugs conhecidos na documentação

Nenhum endpoint vira tool sem passar por smoke test contra uma licença real. A
doc já se mostrou imprecisa seis vezes:

| Onde | Problema |
| --- | --- |
| `GET /medical-reports/create` | método `GET` descrito enviando atributos no body |
| `/appoints/cancel-appoint` | `agendamento_id` documentado como "Identificação do paciente" |
| `/appoints/new-appoint` | `canal_id` documentado como "Identificação do profissional" |
| `/appoints/available-schedule` | `tipo` tipado como `numeric`, valores reais são `E`/`P` |
| `core.feegow.com` vs `core.feegow.com.br` | mesmo path `financial2/external` sob dois TLDs |
| `cartao-beneficios` | `perPage` "padrão é 1" contra exemplo usando 10 |
| SDK oficial (`@feegow/publicapi`) | `getAvailableSchedule()` chama `patient/list` |

---

## 9. Testes

Tudo sem rede, com `httptest` no lugar dos quatro hosts da Feegow:

- **Statelessness** — nada de uma requisição sobrevive para a seguinte (§2).
- **Normalização** — um teste por endpoint provando data, paginação e nome de
  parâmetro contra a tabela de tradução (§5).
- **Perfis** — o `tools/list` de `/mcp` não contém nenhuma tool admin, sob
  qualquer env; e nenhuma decisão de perfil lê variável de ambiente.
- **Fail-closed** — sem bearer, bearer malformado, JWT expirado: o request para
  a Feegow **não sai**.
- **Sem vazamento** — token e payload de paciente nunca aparecem em log ou em
  mensagem de erro (teste que varre a saída).
- **Erros** — as duas formas (`409` com envelope, `422` sem) viram erro legível
  de tool; nunca panic.
- **Regras de negócio** — as cinco guardas do §7.

---

## 10. Riscos

1. **LGPD / dado de saúde.** É o repositório mais sensível da stack depois do
   `agent-secrets`. Desde o primeiro commit: nunca logar payload de paciente,
   tools de leitura retornam **campos selecionados** (o LLM não precisa do
   prontuário inteiro), paginação com teto rígido.
2. **Escrita em ERP de produção do cliente.** Um cancelamento errado é
   irreversível para a clínica. A Fase 3 só entra depois da Fase 2 validada, e
   cada escrita carrega confirmação explícita.
3. **Superfície do perfil admin.** É o ERP inteiro na mão de um LLM. O gate por
   ausência de segredo (§3) é o que impede isso de virar incidente, e é a coisa
   mais importante a testar.
4. **A doc é imprecisa** (§8). Todo endpoint precisa de smoke test real antes de
   virar tool.

---

## 11. Deploy

- `Dockerfile` multi-stage → `gcr.io/distroless/static` (sem healthcheck: a
  imagem não tem shell, mesma situação do `agent-mcp-google`)
- Workflow `release` com SEMVER por Conventional Commits + imagem no GHCR
- `agent-operation/docker-compose.prod.yml`: serviço `feegow-mcp` + **dois**
  sidecars one-shot (`feegow-register`, `feegow-admin-register`), PUT idempotente
  com retry, no mesmo formato do `code-workspace-register`
- Sem rota pública no Caddy; sem DB; sem metering (não consome compute próprio)

### Pendência fora deste repo

O resolver `byok` do runtime lê o namespace **`mcp`** do `agent-secrets`
(`MCP_AUTH_HTTP_URL` aponta para `/mcp`), mas o cofre self-service do tenant — a
tela de Secrets — grava no namespace **`secret`** (`PUT /tenant/secret/{name}`).

Hoje o token da clínica só entra pelo **admin path**
(`PUT /admin/mcp/feegow?org_id=`, com `SECRETS_ADMIN_TOKEN`): uma ação de
operador por clínica, não auto-serviço. Duas saídas:

- **MVP:** aceitar onboarding manual por clínica;
- **Completo:** estender o caminho self-service para gravar em `mcp/<provider>` —
  trabalho no `agent-secrets` + `agent-base-tools`, que vale para qualquer MCP
  `byok` futuro, não só a Feegow.

Deve virar issue separada.

### Onboarding do cliente

Orientar a clínica a emitir o token a partir de um **usuário Feegow dedicado à
integração**, não do usuário pessoal de alguém. A trilha de auditoria dentro do
ERP passa a distinguir o que o agente fez do que a recepcionista fez, e desativar
a integração não depende de desativar uma pessoa.

---

## 12. Fases

| Fase | Entrega | Depende de |
| --- | --- | --- |
| **0** | Smoke test dos 85 endpoints contra uma licença real → `docs/feegow-api.md` como tabela de tradução, com o que de fato responde | licença de teste |
| **1** | Esqueleto, auth, client multi-host, **camada de normalização**, `/healthz`, CI | — |
| **2** | Tools de atendimento, somente leitura | 0, 1 |
| **3** | Escritas de atendimento + as cinco guardas do §7 | 2 |
| **4** | Perfil admin + testes de perfil | 2 |
| **5** | Dockerfile, release, compose + registers, wiring do `agent-secrets` | 1 |

As fases 1 e 5 não dependem da licença de teste.

---

## 13. Inventário completo — 85 endpoints, 16 grupos

Extraído da documentação oficial (*Feegow REST API v1.0*, 146 páginas). A coluna
**Perfil** marca `atendimento` para os 35 endpoints do toolset de atendimento; o
perfil `admin` é o superset e inclui todos os 85.

#### Agendamentos

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/appoints/status` | Tipos de status | atendimento |
| `POST` | `api.feegow.com/v1/api` | `/appoints/statusUpdate` | Atualizar status | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/appoints/motives` | Lista motivos | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/appoints/list-channel` | Listar canais | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/appoints/search` | Listar agendamentos | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/appoints/available-schedule` | Disponibilidade de horários | atendimento |
| `POST` | `api.feegow.com/v1/api` | `/appoints/new-appoint` | Criar novo agendamento | atendimento |
| `POST` | `api.feegow.com/v1/api` | `/appoints/cancel-appoint` | Cancelar agendamento | atendimento |
| `POST` | `api.feegow.com/v1/api` | `/appoints/reschedule` | Remarcar agendamento | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/appoints/queue-position` | Gerar senha de atendimento | atendimento |

#### Bloqueios

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/lock/list` | Listar bloqueios | atendimento |

#### Cartão de Benefício

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `cartao-beneficios.feegow.com` | `/external/contract/datagrid` | Listagem de Contratos | atendimento |
| `POST` | `cartao-beneficios.feegow.com` | `/external/contract/create` | Criação da Contrato | admin |
| `GET` | `cartao-beneficios.feegow.com` | `/external/plan/datagrid` | Listagem de Planos | atendimento |
| `POST` | `cartao-beneficios.feegow.com` | `/external/contract/update` | Alteração da Contrato | admin |
| `POST` | `cartao-beneficios.feegow.com` | `/external/plan/update` | Alteração de Plano | admin |
| `POST` | `cartao-beneficios.feegow.com` | `/external/plan/create` | Criação de Plano | admin |

#### Convênios

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/insurance/list` | Listar convênios | atendimento |

#### Empresa

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/company/list-unity` | Listar unidades | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/company/list-local` | Listar locais | atendimento |

#### Especialidades

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/specialties/list` | Listar especialidades | atendimento |

#### Estoque

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `POST` | `api.feegow.com/v1/api` | `/core/financial/financial-stock/product/insert` | Inserir Produto | admin |
| `POST` | `core.feegow.com.br` | `/financial2/external/financial-stock/product/entry` | Entrada do Produtos | admin |
| `POST` | `core.feegow.com.br` | `/financial2/external/financial-stock/product/movement` | Movimentação de Produtos | admin |
| `POST` | `core.feegow.com.br` | `/financial2/external/financial-stock/product/exit` | Saída de Produto | admin |
| `POST` | `core.feegow.com.br` | `/financial2/external/financial-stock/location/list` | Localizações do Produto | admin |
| `POST` | `api.feegow.com/v1/api` | `/core/financial/base/product/position` | Obter Posição de Produtos | admin |
| `POST` | `api.feegow.com/v1/api` | `/core/financial/base/product/list` | Obter Lista de Produtos | admin |

#### Faturamento

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/billing/insurances-billing` | Buscar Guia | admin |
| `PUT` | `api.feegow.com/v1/api` | `/billing/insurances-billing` | Editar Guia | admin |
| `POST` | `api.feegow.com/v1/api` | `/billing/insurances-billing` | Inserir Guia | admin |

#### Financeiro

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/financial/list-suppliers` | Listar fornecedores | admin |
| `GET` | `api.feegow.com/v1/api` | `/financial/search-supplier` | Informações do fornecedor | admin |
| `POST` | `api.feegow.com/v1/api` | `/core/financial/account/association` | Associação de conta financeira | admin |
| `POST` | `api.feegow.com/v1/api` | `/core/financial/base/financial-category` | Categoria financeira (Plano de contas) | admin |
| `POST` | `api.feegow.com/v1/api` | `/core/financial/base/cost-center` | Centros de Custos | admin |
| `GET` | `api.feegow.com/v1/api` | `/financial/list-medical-transfer` | Listar repasses | admin |
| `GET` | `api.feegow.com/v1/api` | `/financial/list-invoice` | Listar contas | admin |
| `POST` | `api.feegow.com/v1/api` | `/core/financial/base/current-accounts` | Obter Contas Correntes | admin |
| `GET` | `api.feegow.com/v1/api` | `/financial/find-invoice-by-nfse-number` | Obter invoices por nota fiscal | admin |
| `POST` | `api.feegow.com/v1/api` | `/financial/update-invoice-nfse-number` | Atualizar Número da Nota Fiscal Eletrônica (NFS-e) | admin |
| `GET` | `api.feegow.com/v1/api` | `/financial/credit-card-flags` | Obter bandeiras de cartão de crédito | admin |
| `DELETE` | `api.feegow.com/v1/api` | `/core/financial/invoice/remove` | Remover Fatura | admin |
| `DELETE` | `api.feegow.com/v1/api` | `/core/financial/payment/remove` | Remover Pagamento | admin |
| `POST` | `api.feegow.com/v1/api` | `/financial/pay-movement` | Pagamento de Conta | admin |
| `POST` | `api.feegow.com/v1/api` | `/core/financial/invoice/create` | Criação da Conta | admin |
| `POST` | `api.feegow.com/v1/api` | `/financial/create-account` | Criar Conta por Agendamento | admin |
| `POST` | `api.feegow.com/v1/api` | `/core/financial/voucher/create` | Criação de Voucher | admin |
| `POST` | `api.feegow.com/v1/api` | `/core/financial/voucher/cancel` | Cancelamento de Voucher | admin |
| `GET` | `api.feegow.com/v1/api` | `/core/financial/voucher/list` | listagem de Voucher | admin |
| `GET` | `api.feegow.com/v1/api` | `/financial/list-sales` | Listagem de Vendas | admin |
| `GET` | `core.feegow.com` | `/financial2/external/private-table/list` | Listagem de Tabelas Privadas | admin |
| `GET` | `api.feegow.com/v1/api` | `/financial/dmed` | Obtém DMED do Profissional | admin |
| `POST` | `api.feegow.com/v1/api` | `/financial/pay-booking` | Pagar Agendamneto | admin |

#### Funcionários

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/employee/list` | Listar funcionários | admin |

#### Laudos

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/medical-reports/get-laudos-list` | Listar laudos | admin |
| `GET` | `api.feegow.com/v1/api` | `/medical-reports/get-labs-report-file` | Buscar arquivo de laudos | admin |
| `GET` | `api.feegow.com/v1/api` | `/medical-reports/create` | Registrar laudo no Feegow a partir do agendamento. No bod... | admin |
| `GET` | `api.feegow.com/v1/api` | `/medical-reports/search` | Visualizar laudo registrado no Feegow a partir do agendam... | admin |

#### Pacientes

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/patient/search` | Informações | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/patient/list` | Listar pacientes | atendimento |
| `POST` | `api.feegow.com/v1/api` | `/patient/create` | Criar paciente | atendimento |
| `POST` | `api.feegow.com/v1/api` | `/patient/edit` | Editar paciente | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/patient/list-sources` | Listar origens | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/patient/list-dependents` | Listar dependentes | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/patient/list-privates` | Listar tabelas particulares | atendimento |
| `POST` | `api.feegow.com/v1/api` | `/patient/upload-base64` | Upload de arquivo para o prontuário do paciente | admin |
| `GET` | `api.feegow.com/v1/api` | `/patient/health-programs` | Listar programas de saúde | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/patient/exam-requests` | Listar pedidos de exâmes | atendimento |

#### Procedimentos

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/procedures/list` | Listar procedimentos | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/procedures/types` | Tipos de procedimentos | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/procedures/insurance-procedures-list` | Listar Convênios do procedimento | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/procedures/imported-franchise-records` | Listar procedimentos importados franquia | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/procedures/bundles` | Listar pacotes | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/procedures/groups` | Grupos de procedimentos | atendimento |

#### Profissionais

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/professional/list` | Listar profissionais | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/professional/search` | Informações e Especialidades | atendimento |
| `GET` | `api.feegow.com/v1/api` | `/professional/insurance` | Convênios aceitos | atendimento |

#### Propostas

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/proposal/list` | Listar propostas | admin |
| `POST` | `api.feegow.com/v1/api` | `/proposal/create` | Criar proposta | admin |
| `POST` | `api.feegow.com/v1/api` | `/proposal/change-status` | Mudar status da proposta | admin |
| `GET` | `api.feegow.com/v1/api` | `/proposal/proposal-url` | Listar URL de proposta por id | admin |
| `GET` | `api.feegow.com/v1/api` | `/proposal/list-dates` | Listar propostas pela data | admin |

#### Relatórios

| Método | Host | Path | Descrição | Perfil |
| --- | --- | --- | --- | --- |
| `GET` | `api.feegow.com/v1/api` | `/reports/list` | Listar relatórios | admin |
| `POST` | `api.feegow.com/v1/api` | `/reports/generate` | Gerar Relatório | admin |

