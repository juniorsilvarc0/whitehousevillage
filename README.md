# White House Village Manager

ERP + CRM de gestão de aluguel por temporada e eventos da **White House Village** — Praia do Coqueiro, Luís Correia (PI).

Substitui o caderno e o WhatsApp como fonte da verdade: disponibilidade em tempo real, reservas com ciclo de vida e dinheiro, CRM com funil e SLA, atendimento por WhatsApp, agenda, financeiro, inventário, área de corretores, BI e integrações.

## Stack

| Camada | Tecnologia |
|---|---|
| API | **Go 1.25** · chi v5 · pgx/v5 nativo · River (jobs) · golang-migrate · JWT |
| Banco | **PostgreSQL 16** (`btree_gist`, `daterange`, `LISTEN/NOTIFY`) |
| Admin | **Next.js 16** (App Router, RSC) · React 19 · TypeScript · Tailwind v4 CSS-first · shadcn/ui (base-nova sobre Base UI) |
| Realtime | SSE com fan-out por `LISTEN/NOTIFY` |
| Infra | Docker Compose + Traefik (SSL Let's Encrypt) em VPS |

## Estrutura

```
apps/api/      backend Go — cmd/{api,worker,migrate,seed} + internal/{platform,domain,modules}
apps/admin/    painel Next.js — app/(app)/** + features/** + components/**
docs/          PRD, spec funcional, modelo de dados, infra, integrações, API, UI, agentes, roadmap
infra/         compose, Traefik, Dockerfiles, backup
.claude/agents/  time de agentes especializados
```

## Subir o ambiente de desenvolvimento

```bash
cp .env.example .env
make up          # sobe postgres + api + admin
make migrate     # aplica migrations (passo explícito, nunca no boot)
make seed        # produtos, unidades, tarifas, perfis e usuários de teste
make check       # lint + typecheck + testes
```

- API: http://localhost:8080 · `/healthz` `/readyz` `/metrics`
- Admin: http://localhost:3000

## A regra que sustenta o produto

A White House Completa é composta pelas 8 unidades físicas. Vendê-la insere 8 linhas em `stay_blocks`; qualquer unidade individual ocupada faz a inserção estourar a constraint. **A exclusividade bidirecional é garantida pelo banco, não por código de aplicação** — overbooking é impossível mesmo sob concorrência.

```sql
CONSTRAINT stay_no_overlap EXCLUDE USING gist (unit_id WITH =, period WITH &&)
  WHERE (status IN ('hold','confirmed'))
```

## Documentação

Comece por [`docs/prd.md`](docs/prd.md) e [`docs/spec.md`](docs/spec.md). O contrato da API é [`apps/api/openapi/openapi.yaml`](apps/api/openapi/openapi.yaml); o modelo de dados é [`docs/db.md`](docs/db.md).
