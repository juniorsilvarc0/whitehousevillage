# White House Village Manager — instruções do repositório

ERP + CRM de gestão de temporada e eventos, **mais o site de vendas por onde o cliente reserva**. Backend Go (`apps/api`), painel de gestão em Next.js (`apps/admin`), front de cliente em HTML/CSS/JS servido por nginx (`apps/site`), Postgres. Leia `docs/prd.md` e `docs/spec.md` antes de qualquer implementação; o contrato é `apps/api/openapi/openapi.yaml` e o modelo é `docs/db.md`.

## Regras inegociáveis

1. **`internal/domain` é puro.** Tarifa, disponibilidade, orçamento e política vivem lá, sem SQL, sem HTTP, sem `pgx`. Regra de negócio em handler ou repositório é falha de revisão.
2. **Overbooking é impedido pelo banco**, não por `SELECT` seguido de `INSERT`. A constraint `EXCLUDE USING gist` em `stay_blocks` é a única defesa válida. Erro `23P01` vira `409 DATE_CONFLICT` — nunca 500, nunca retry.
3. **Migrations só pelo agente `db-migrations`**, nomeadas por timestamp (`20260820143000_nome.up.sql`). Aplicadas por `cmd/migrate` num passo separado. Nunca no boot da aplicação.
4. **Dinheiro é `bigint` em centavos** (`*_cents`). Float em dinheiro é bug.
5. **Datas de estadia são `date` e `daterange` half-open `[in, out)`** — permite back-to-back e espelha a contagem de noites. Instantes são `timestamptz`. Fuso `America/Fortaleza` num helper único.
6. **DTO tipado sempre.** `map[string]any` em handler é proibido; `PATCH` usa `Opt[T]` para distinguir "campo ausente" de "campo nulo".
7. **Toda entidade financeira congela o que usou** — `reservation_nights` guarda a tarifa aplicada por noite, a reserva guarda a versão da política. Mudar o tarifário nunca reescreve o passado.
8. **RBAC é dado, não código.** Papel × recurso × ação × escopo (`all|own`) vive em tabela. Nada de `if role == "corretor"`.
9. **Máximo ~25 colunas por tabela.** Acima disso, tabela satélite.
10. **Ownership de pasta**: cada agente escreve só nas próprias pastas (`docs/agents.md`). `router/routes.go`, `config/navigation.ts` e `openapi.yaml` são do `tech-lead`.

## Comandos

```bash
make up · make migrate · make seed · make check · make psql
```

`make check` (lint + typecheck + testes) precisa passar antes de reportar qualquer entrega.

## Convenções de API

`/api/v1`; lista devolve `{"data":[...],"meta":{page,per_page,total,total_pages}}`; erro devolve `{"error":{code,message,details}}` com `code` estável. `Idempotency-Key` é obrigatório em POST que cria reserva ou dinheiro.

## O site de vendas vive aqui desde 02/10/2026

`apps/site` é o front de cliente — era um MVP separado, em `/Users/junior/DEV/spincode/whitehouse`, e foi importado para cá. Desde 03/10/2026 (passo **A2** de `docs/unificacao-site-crm.md`) ele **não calcula mais nada**: catálogo, calendário e orçamento vêm de `/api/v1/public/*`, o mesmo motor do painel. Duas coisas valem sempre:

- **O site pergunta, não calcula.** Nenhuma regra de tarifa, disponibilidade ou política nasce em `apps/site`. Ele chama a API.
- **Rota pública é a decisão mais cara da tabela.** `AcessoPublico` exige `Motivo` escrito, toda `/public/*` passa pelo limitador por IP, e nenhuma resposta pública carrega dado de terceiro, desconto ou vocabulário interno de negociação.

## O que este repositório não é

Não é multi-tenant, não emite documento fiscal e não roda modelo de IA próprio.
