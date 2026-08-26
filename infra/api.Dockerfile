# Imagem única para os quatro binários Go (api, worker, migrate, seed). Uma
# imagem só garante que a migration que roda em produção é a mesma que passou
# no CI — e que o serviço `migrate` do compose não depende de imagem de terceiro.

# ─────────── build ───────────
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./apps/api/
WORKDIR /src/apps/api
RUN go mod download
COPY apps/api/ ./
# Sem `|| true`: build quebrado tem que derrubar a imagem aqui. Mascarar a
# falha só a empurra para o runtime, onde ela vira "exec format error" às 2h.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ \
      ./cmd/api ./cmd/worker ./cmd/migrate ./cmd/seed

# ─────────── runtime ───────────
# distroless: sem shell, sem gerenciador de pacotes, usuário não-root.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/ /app/
# Os .sql viajam dentro da imagem: na VPS o serviço `migrate` roda esta mesma
# imagem, sem bind mount do repositório (que lá nem sempre existe).
COPY apps/api/migrations/ /migrations/
ENV MIGRATIONS_PATH=/migrations
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/api"]
