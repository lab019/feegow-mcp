FROM golang:1.24 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# VERSION é o que o binário passa a reportar no log de startup, em --version
# e como "serverInfo.version" no handshake MCP (ver internal/buildinfo).
# Precisa vir de fora porque nada aqui dentro sabe a resposta: o .git fica
# fora do contexto (.dockerignore), e a tag SEMVER desta imagem ainda NÃO
# existe no momento do build — o workflow de release a calcula depois, a
# partir dos Conventional Commits, e então retagueia esta mesma imagem sem
# rebuildar. O build-push passa o commit, escrito igualzinho à tag de imagem
# que o carrega (sha-<commit>), então o que o binário diz é literalmente uma
# tag que se pode dar `docker pull`. Vazio (build manual) → "dev".
ARG VERSION=""

# CGO_ENABLED=0 é o que torna o binário estático e portanto compatível com a
# imagem `static` abaixo (que não tem libc). -trimpath tira caminhos de build
# do binário; -s -w tiram a tabela de símbolos e o DWARF.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags="-s -w -X github.com/lab019/feegow-mcp/internal/buildinfo.version=${VERSION}" \
      -o /out/feegow-mcp .

# distroless/static:nonroot — sem shell, sem gerenciador de pacotes, sem libc.
# Consequência deliberada: NÃO existe HEALTHCHECK neste Dockerfile. Um
# `HEALTHCHECK CMD curl ...` precisaria de um shell e de um curl que esta
# imagem não tem, e adicioná-los reabriria exatamente a superfície que o
# distroless fecha — num serviço cujo tráfego é token de Feegow de clínica.
# A saúde é observada de fora, pelo /healthz que o main.go expõe (é assim que
# o compose do agent-operation e o Prometheus a checam).
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/feegow-mcp /feegow-mcp
USER nonroot:nonroot
EXPOSE 8087
ENTRYPOINT ["/feegow-mcp"]
