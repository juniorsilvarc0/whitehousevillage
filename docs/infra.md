# Infraestrutura

## 1. Ambientes

| Ambiente | Onde | Domínio | Dados |
|---|---|---|---|
| **dev** | Máquina do desenvolvedor, Docker Compose | `localhost:3000` / `:8080` | Seed |
| **staging** *(opcional)* | VPS, mesmo compose com outro `.env` | `staging.dominio` | Cópia anonimizada |
| **prod** | VPS com Traefik | `app.dominio` (admin) e `api.dominio` | Real, com backup |

Configuração **100% por variável de ambiente** (`.env.example` é a fonte da verdade). Nenhum segredo no repositório.

## 2. Serviços

```
                         ┌─────────── Traefik ───────────┐
   Internet ── 443 ──►   │ SSL Let's Encrypt (HTTP-01)   │
                         │ app.dominio → admin:3000      │
                         │ api.dominio → api:8080        │
                         └───────────────┬───────────────┘
                                         │
             ┌───────────────┬───────────┴────────────┬──────────────┐
             ▼               ▼                        ▼              ▼
        admin (Next)     api (Go)                worker (Go)     postgres:16
        standalone       chi + pgx               River jobs      volume nomeado
                              │                       │              ▲
                              └───── LISTEN/NOTIFY ───┴──────────────┘
                                                              backup (cron pg_dump)
```

| Serviço | Imagem | Notas |
|---|---|---|
| `traefik` | `traefik:v3` | Entrypoints 80/443, redirect para HTTPS, `acme.json` em volume com `chmod 600` |
| `api` | multi-stage Go → `distroless/static` | Binário estático, usuário não-root, `/healthz` como healthcheck |
| `worker` | mesma imagem, entrypoint `worker` | Jobs; escala independente da API |
| `admin` | `node:22-alpine` → `next start` (output standalone) | Só o BFF fala com a API |
| `postgres` | `postgres:16-alpine` | Volume nomeado, `TZ=America/Fortaleza`, healthcheck `pg_isready` |
| `migrate` | mesma imagem da API, entrypoint `migrate` | Roda sob demanda (`make migrate`), **nunca** no boot da API |
| `seed` | idem, entrypoint `seed` | Idempotente |
| `backup` | `postgres:16-alpine` + cron | `pg_dump` diário comprimido e cifrado |

`depends_on` com `condition: service_healthy` — a API não sobe antes do banco responder.

## 3. Deploy

```bash
# primeira vez na VPS
git clone git@github.com:juniorsilvarc0/whitehousevillage.git /opt/whv
cd /opt/whv && cp .env.example .env && vim .env      # domínio, e-mail ACME, segredos
mkdir -p infra/traefik && touch infra/traefik/acme.json && chmod 600 infra/traefik/acme.json
make up && make migrate && make seed

# atualização
git pull && make build && make migrate && docker compose up -d
```

Rollback: as imagens são versionadas por tag de commit; `docker compose up -d` com a tag anterior volta a aplicação. **Migration com `down` testada** é o que permite rollback de schema — por isso toda `up` tem `down`.

## 4. CI/CD (GitHub Actions)

| Job | O que roda |
|---|---|
| `lint` | `golangci-lint run ./...` · `eslint` · `tsc --noEmit` |
| `test-api` | `go test ./... -race` — inclui o **teste de concorrência do overbooking** |
| `test-admin` | `vitest --run` |
| `migrations` | Postgres efêmero: `migrate up` → `migrate down` até zero |
| `contract` | Valida que toda rota está na OpenAPI, expõe os 6 verbos e checa permissão |
| `build` | Build das imagens; em `main`, push para o registry com a tag do commit |
| `e2e` *(a partir da Fase 1)* | Playwright contra o compose completo |

Regras: `main` protegida, PR obrigatório, CI verde para merge, sem push direto.

## 5. Backup e restore

- `pg_dump` diário comprimido, cifrado (`age`/`gpg`), retenção 30 dias local + cópia remota opcional (`rclone`).
- **Restore é testado automaticamente**: job diário restaura o dump em base descartável e roda um `SELECT count(*)` sanity — backup que nunca foi restaurado não é backup.
- Alvos: **RPO < 24 h**, **RTO < 4 h**. Procedimento de restore documentado no runbook abaixo.

```bash
# restore
gunzip -c backup-20260820.sql.gz | docker compose exec -T postgres psql -U $POSTGRES_USER -d $POSTGRES_DB
```

## 6. Observabilidade

- **Logs** JSON estruturados (`slog`) com `request_id`, `user_id`, rota, status e latência. `docker compose logs` em dev; rotação por driver em prod.
- **Health**: `/healthz` (processo vivo) e `/readyz` (pool do banco + **versão da migration esperada**). A API se recusa a servir se o schema estiver defasado.
- **Métricas** Prometheus em `/metrics`: latência por rota, erro por código, fila de jobs, jobs falhos, conexões — e métricas de negócio (`whv_holds_expired_total`, `whv_ical_conflicts_total`, `whv_reservations_confirmed_total`).
- **Alertas** mínimos: API fora do ar, fila de jobs crescendo, job falhando repetidamente, conflito de canal detectado, backup do dia ausente.

## 7. Segurança

- TLS obrigatório; HSTS; cookies `httpOnly`, `Secure`, `SameSite=Lax`.
- Senha com argon2id. Refresh rotativo com detecção de reuso.
- Rate limit por IP no login e por token na API pública.
- Postgres **não** exposto para fora do compose. Segredos por env, nunca no git.
- Imagens sem shell (`distroless`) e usuário não-root.
- `acme.json` com permissão 600 — Traefik recusa subir de outro jeito.
- Backups cifrados (contêm dado pessoal de hóspede).

## 8. Runbook (o mínimo para operar)

| Situação | O que fazer |
|---|---|
| API não sobe | `docker compose logs api` → geralmente migration pendente: `make migrate` |
| `/readyz` vermelho | Checar Postgres (`make psql`) e a versão da migration |
| Fila de jobs crescendo | `docker compose logs worker`; job travado libera com o advisory lock ao reiniciar o worker |
| WhatsApp mudo | `/chat/connection/state`; se desconectado, reconectar por QR e confirmar o webhook registrado |
| Conflito de canal | Painel `/canais/conflitos` → realocar unidade ou cancelar com compensação |
| Overbooking reportado | **Não deveria existir por reserva direta** (constraint). Se vier de OTA, é conflito de janela de risco — tratar pelo painel |
| Restaurar backup | Ver §5; sempre em base nova, nunca por cima da produção |
