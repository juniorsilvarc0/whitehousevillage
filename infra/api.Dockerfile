# ─────────── build ───────────
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY apps/api/go.mod apps/api/go.su[m] ./apps/api/
WORKDIR /src/apps/api
RUN go mod download
COPY apps/api/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api    ./cmd/api  && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker 2>/dev/null || true

# ─────────── runtime ───────────
# distroless: sem shell, sem gerenciador de pacotes, usuário não-root.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/ /app/
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/api"]
