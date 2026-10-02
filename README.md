# White House Village Manager

ERP + CRM de gestão de aluguel por temporada e eventos da **White House Village** — Praia do Coqueiro, Luís Correia (PI).

O objetivo é substituir o caderno e o WhatsApp como fonte da verdade: disponibilidade em tempo real, reservas com ciclo de vida e dinheiro, CRM com funil e SLA, atendimento por WhatsApp, agenda, financeiro, inventário, área de corretores, BI e integrações. **O que já existe hoje** é o núcleo das Fases 0 e 1 — autenticação e RBAC por dados, inventário, tarifário e política versionados, disponibilidade e orçamento, reservas com o ciclo inteiro, contatos com trilha de LGPD, CRM com funil ao vivo e o mapa de ocupação por SSE — mais o cadastro de corretores da Fase 2. Financeiro, comissões, agenda, chat, canais e BI ainda não existem; o estado item a item está em [`docs/roadmap.md`](docs/roadmap.md).

Desde 02/10/2026 o **site de vendas** também vive aqui (`apps/site`). A intenção é que o cliente consulte datas e reserve no site e a gestão opere no painel, os dois falando com a mesma API. **Hoje o site ainda não fala com a API**: calcula preço em JavaScript com dados fictícios, e nenhum número que ele mostra vale como preço. A dívida e o plano para quitá-la estão em [`docs/unificacao-site-crm.md`](docs/unificacao-site-crm.md).

## Stack

| Camada | Tecnologia |
|---|---|
| API | **Go 1.25** · chi v5 · pgx/v5 nativo · golang-migrate · JWT |
| Jobs | `cmd/worker`: um loop próprio, sem fila externa, com um job (`holds.expire`, a expiração da pré-reserva) |
| Banco | **PostgreSQL 16** (`btree_gist`, `daterange`, `LISTEN/NOTIFY`) |
| Painel | **Next.js 16** (App Router, RSC) · React 19 · TypeScript · Tailwind v4 CSS-first · shadcn/ui (base-nova sobre Base UI) |
| Site | HTML/CSS/JS sem build, servido por **nginx 1.27** |
| Realtime | SSE com fan-out por `LISTEN/NOTIFY` |
| Infra | Docker Compose de desenvolvimento e GitHub Actions. **Ainda não há** compose de produção, proxy com TLS, deploy nem backup automático — ver `docs/infra.md`, que descreve o alvo, não o que existe |

## Estrutura

```
apps/api/        backend Go — cmd/{api,worker,migrate,seed} + internal/{platform,domain,modules,router,auth}
apps/admin/      painel Next.js — src/{app,components,config,lib}
apps/site/       front de cliente — nginx, estático, sem build; Dockerfile, nginx.conf e e2e/fumaca-site.mjs
docs/            PRD, spec funcional, modelo de dados, infra, integrações, API, UI, agentes, roadmap, backlog
infra/           docker-compose.yml (desenvolvimento), api.Dockerfile, admin.Dockerfile
.claude/agents/  time de agentes especializados
```

## Subir o ambiente de desenvolvimento

```bash
cp .env.example .env
make up          # sobe postgres + api + worker + admin + site
make migrate     # aplica migrations (passo explícito, nunca no boot)
make seed        # produtos, unidades, tarifas, perfis e usuários de teste
make check       # lint (gofmt, vet, golangci-lint, eslint, tsc) + testes de unidade e de componente
make smoke       # fumaça contra o que está no ar: painel no navegador e site público
```

- API: http://localhost:8080 · sondas `/healthz` (vida) e `/readyz` (banco e versão do schema), na raiz, fora de `/api/v1` (`API_HOST_PORT`)
- Painel: http://localhost:3100 (`ADMIN_PORT`)
- Site: http://localhost:3200 (`SITE_PORT`)

As três portas são do **host** e saem do `.env` — troque-as quando outro projeto já estiver usando. Dentro do Compose elas são fixas (8080, 3000 e 80), porque é nisso que o painel, o healthcheck e o `API_INTERNAL_URL` se apoiam.

Outros alvos que importam: `make smoke-stack` (reconstrói as imagens, migra, semeia e roda a fumaça — é o que o CI roda), `make test-integration` (Postgres efêmero, suíte serializada com `-p 1` e os testes de concorrência repetidos), `make psql` e `make backup` (dump manual em `infra/backups/`, que reprova se o dump vier incompleto). **`make restore` não existe** e reprova de propósito: restaure à mão, numa base nova.

## A regra que sustenta o produto

A White House Completa é composta pelas 8 unidades físicas. Vendê-la insere 8 linhas em `stay_blocks`; qualquer unidade individual ocupada faz a inserção estourar a constraint. **A exclusividade bidirecional é garantida pelo banco, não por código de aplicação** — overbooking é impossível mesmo sob concorrência.

```sql
CONSTRAINT stay_no_overlap EXCLUDE USING gist (unit_id WITH =, period WITH &&)
  WHERE (status IN ('hold','confirmed'))
```

## Documentação

Comece por [`docs/prd.md`](docs/prd.md) e [`docs/spec.md`](docs/spec.md). O contrato da API é [`apps/api/openapi/openapi.yaml`](apps/api/openapi/openapi.yaml); o modelo de dados é [`docs/db.md`](docs/db.md); o que está pronto, o que está aberto e as decisões que esperam o dono do negócio estão em [`docs/roadmap.md`](docs/roadmap.md).
