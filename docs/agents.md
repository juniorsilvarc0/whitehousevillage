# Time de agentes

Este projeto é construído por agentes especializados. As definições estão em `.claude/agents/*.md` e o contrato entre eles é este documento.

## 1. Princípio

> **Fronteira é pasta.** Dois agentes nunca recebem tarefa que escreva no mesmo diretório. Se a tarefa exige, ela é *quebrada em duas* com handoff — não paralelizada.

O que desacopla o time é o **contrato**: `apps/api/openapi/openapi.yaml` + `docs/db.md`. Com ele mergeado, backend e frontend avançam ao mesmo tempo sem se esperar.

## 2. Quem é quem

| Agente | Escrita exclusiva | Responsabilidade | Nunca toca |
|---|---|---|---|
| **`tech-lead`** | `docs/**`, `apps/api/openapi/**`, `apps/api/internal/domain/**`, `internal/router/routes.go` | Orquestra. Decide ordem, quebra tarefas, escreve o contrato e o domínio puro, revisa entregas, mantém `roadmap.md`. É o único que aciona os outros | Implementação de módulo, UI, infra |
| **`db-migrations`** | `apps/api/migrations/**`, `apps/api/cmd/seed/**`, `docs/db.md` | Migrations up/down, constraints, índices, seed idempotente | Go de aplicação, front, infra |
| **`backend-go`** | `apps/api/internal/{modules,platform,auth,jobs,realtime}/**`, `cmd/{api,worker}` | Handlers, services, repositórios, jobs, SSE, testes de unidade | Migrations, `internal/domain`, front |
| **`integracoes`** | `apps/api/internal/modules/{chat,channels,integrations}/**`, `docs/integracao.md` | uazapi, iCal, webhooks, tokens, MCP, agente de IA | Reservas, financeiro, front |
| **`next-frontend`** | `apps/admin/**` | Telas, design system, componentes, testes de componente | Qualquer coisa em `apps/api` |
| **`devops`** | `infra/**`, `Dockerfile*`, `.github/workflows/**`, `Makefile` | Compose, Traefik, CI, backup, observabilidade, deploy | Código de aplicação |
| **`qa-testes`** | `**/*_test.go`, `apps/admin/**/*.test.{ts,tsx}`, `tests/e2e/**`, `docs/testing.md` | Testes de integração, e2e, teste de concorrência do overbooking | Código de produção — **reporta, não conserta** |

### Por que `internal/domain` é do tech-lead

Tarifa, disponibilidade, orçamento e política são o contrato de negócio. Se cada agente puder alterá-los, a regra comercial se fragmenta em cinco lugares — que é exatamente o que aconteceu no sistema de referência. O domínio é escrito uma vez, com teste, e os módulos o consomem.

## 3. Protocolo de handoff

Toda tarefa carrega este bloco:

```
CONTRATO
  entrada:   <migration N aplicada | endpoint X na OpenAPI | tipo Y publicado>
  saída:     <arquivos que vou criar/alterar — todos dentro das MINHAS pastas>
  prova:     <comando que roda e passa>
  bloqueia:  <quem está esperando por mim>
```

Ordem canônica de uma fatia de funcionalidade:

```
tech-lead        contrato: rotas na OpenAPI + tipos do domínio + ADR
   ↓
db-migrations    migration + seed
   ↓
backend-go  ‖  integracoes        (paralelos — pastas disjuntas)
   ↓
next-frontend    telas contra a OpenAPI  (começa assim que o contrato existe, não espera o backend)
   ↓
qa-testes        integração + e2e contra o CONTRATO, não contra a implementação
   ↓
devops           deploy, métricas, alerta
```

## 4. Como o orquestrador evita conflito de escrita

Quatro mecanismos, em ordem de importância:

1. **Ownership de pasta** — regra primária, resolve a maior parte.
2. **Migrations nomeadas por timestamp** (`20260820T143000_add_stay_blocks.up.sql`), não sequenciais. Elimina a colisão de dois agentes criando `000007_*`. Só o `db-migrations` cria migration.
3. **Worktree para tarefa longa e paralela** — `git worktree add ../wt-<agente>-<tarefa> -b feat/<agente>/<tarefa>`. O merge é serializado pelo `tech-lead` na ordem `db → backend/integrações → frontend → qa`. Tarefa curta e isolada roda direto na branch, sem worktree (worktree tem custo).
4. **Arquivos-ímã de conflito têm dono único**: `internal/router/routes.go`, `apps/admin/src/config/navigation.ts` e `openapi.yaml` pertencem ao `tech-lead`. Os agentes **propõem a linha no relatório**; o tech-lead aplica. Isso mata os conflitos que sobram.

## 5. Regras comuns a todos

- Rodar `make check` (lint + typecheck + testes do próprio escopo) **antes** de reportar.
- Reportar em texto, no retorno da tarefa. **Não criar arquivo `.md` de resumo** — o repositório guarda código e decisão (ADR), não diário.
- Ler `CLAUDE.md` e as regras inegociáveis antes de escrever a primeira linha.
- Se a tarefa exigir escrever fora das próprias pastas: **parar e devolver ao `tech-lead`**, não invadir.
- Se o contrato estiver errado, **não contornar em silêncio**: apontar para o `tech-lead` corrigir a OpenAPI.
- Escopo é da fase corrente. Pedido fora da fase vira linha no `roadmap.md`, não código.

## 6. Definition of done

Uma entrega só está pronta quando:

- [ ] `make check` passa
- [ ] A rota está na OpenAPI e o teste de contrato aceita
- [ ] Há teste automatizado cobrindo o caminho feliz e ao menos um erro de regra de negócio
- [ ] Migrations têm `down` e o CI validou o ciclo completo
- [ ] Permissão e escopo (`all|own`) foram verificados para os três perfis
- [ ] Nada foi escrito fora das pastas do agente
