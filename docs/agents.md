# Time de agentes

Este projeto é construído por agentes especializados. As definições estão em `.claude/agents/*.md` e o contrato entre eles é este documento.

## 1. Princípio

> **Fronteira é pasta.** Dois agentes nunca recebem tarefa que escreva no mesmo diretório. Se a tarefa exige, ela é *quebrada em duas* com handoff — não paralelizada.

O que desacopla o time é o **contrato**: `apps/api/openapi/openapi.yaml` + `docs/db.md`. Com ele mergeado, backend e frontend avançam ao mesmo tempo sem se esperar.

## 2. Quem é quem

| Agente | Escrita exclusiva | Responsabilidade | Nunca toca |
|---|---|---|---|
| **`squad-lead`** | `docs/roadmap.md`, `docs/backlog/**`, `.claude/agents/**` | **Chefe do squad.** Abre a FASE: monta o backlog de implantação (cada item com efeito, dono, entrada, prova e quem bloqueia), decide a ordem, revisa código antes de entrar, audita segurança, cobra teste que possa falhar e impede que dívida passe calada | Código de produção. Se está editando `.go` ou `.tsx`, delegou errado |
| **`tech-lead`** | `apps/api/openapi/**`, `apps/api/internal/domain/**`, `internal/router/routes.go`, `apps/admin/src/config/navigation.ts`, `docs/**` exceto o do squad lead | Orquestra **uma fatia**: escreve o contrato e o domínio puro, distribui, integra as linhas propostas em `routes.go` e `navigation.ts`, revisa entregas | Implementação de módulo, UI, infra |
| **`db-migrations`** | `apps/api/migrations/**`, `apps/api/cmd/seed/**`, `docs/db.md` | Migrations up/down, constraints, índices, seed idempotente | Go de aplicação, front, infra |
| **`backend-go`** | `apps/api/internal/{modules,platform,auth,jobs,realtime}/**`, `cmd/{api,worker}` | Handlers, services, repositórios, jobs, SSE, testes de unidade | Migrations, `internal/domain`, front |
| **`integracoes`** | `apps/api/internal/modules/{chat,channels,integrations}/**`, `docs/integracao.md` | uazapi, iCal, webhooks, tokens, MCP, agente de IA | Reservas, financeiro, front |
| **`next-frontend`** | `apps/admin/**` | Telas, design system, componentes, testes de componente | Qualquer coisa em `apps/api` |
| **`devops`** | `infra/**`, `Dockerfile*`, `.github/workflows/**`, `Makefile` | Compose, Traefik, CI, backup, observabilidade, deploy | Código de aplicação |
| **`qa-testes`** | `**/*_test.go`, `apps/admin/**/*.test.{ts,tsx}`, `tests/e2e/**`, `docs/testing.md` | Testes de integração, e2e, teste de concorrência do overbooking | Código de produção — **reporta, não conserta** |

### Onde termina o squad lead e começa o tech lead

O squad lead decide **quais fatias existem e em que ordem**; o tech lead decide **como uma fatia é desenhada**. A fronteira importa porque os dois revisam, e revisão em duas camadas só não vira burocracia se cada uma olhar coisa diferente: o tech lead cobra o desenho (o domínio está puro? o contrato veio antes?), o squad lead cobra o que entra (isso funciona servindo? a garantia está no banco? existe teste que possa falhar? a dívida tem efeito, motivo e fase escritos?).

Os dois compartilham `docs/`, e por isso a divisão é nominal: `roadmap.md` e `backlog/**` são do squad lead; `spec.md`, `db.md`, `api.md` e o resto são do tech lead.

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
2. **Migrations nomeadas por timestamp** (`20260820143000_add_stay_blocks.up.sql`), não sequenciais. Elimina a colisão de dois agentes criando `000007_*`. Só o `db-migrations` cria migration.
3. **Worktree para tarefa longa e paralela** — `git worktree add ../wt-<agente>-<tarefa> -b feat/<agente>/<tarefa>`. O merge é serializado pelo `tech-lead` na ordem `db → backend/integrações → frontend → qa`. Tarefa curta e isolada roda direto na branch, sem worktree (worktree tem custo).
4. **Arquivos-ímã de conflito têm dono único**: `internal/router/routes.go`, `apps/admin/src/config/navigation.ts` e `openapi.yaml` pertencem ao `tech-lead`. Os agentes **propõem a linha no relatório**; o tech-lead aplica. Isso mata os conflitos que sobram.

## 4.1 O papel que faltava: `integrador`

Duas rodadas seguidas produziram **tarefa órfã** — trabalho que todo mundo enxergou, ninguém fez, e passou porque não era pasta de ninguém:

- `SchemaVersionEsperada` (`internal/router/saude.go`) ficou atrás da migration **duas vezes**, sinalizada por quatro agentes num relatório cada;
- `audit.Middleware` foi pedido pelo dono do pacote de auditoria, pelo inventário e pelo tarifário — e nunca foi ligado no `router.go`, então 43 linhas de trilha nasceram sem IP nem user-agent;
- o teto de tamanho do bloqueio operacional foi mandado para `internal/domain`, recusado por não ser pasta de quem recebeu, e ficou sem dono nos dois relatórios.

**Toda rodada tem um `integrador`**, dono dos arquivos que não pertencem a módulo nenhum: `internal/router/{router,routes,saude}.go`, o que sobrar de `internal/platform`, `openapi.yaml` quando o ajuste é consequência de implementação, `Makefile` e configuração de ferramenta. Ele entra **por último** na fase de correção e a fecha varrendo os relatórios dos outros atrás de "precisa que alguém", "fora da minha pasta" e "sinalizo para quem for".

Sem esse papel, posse por pasta vira desculpa: cada agente entrega o seu, e o buraco entre eles fica de pé até a revisão adversarial cobrar.

## 4.2 O que a rodada de 27/08 ensinou: órfão avisado continua órfão

O papel de `integrador` funcionou — as oito rotas de contatos foram ligadas, `SchemaVersionEsperada` subiu, as migrations foram aplicadas e o stack voltou a servir o que a árvore contém. Mas o mecanismo continua sendo **um humano (ou um agente) lendo relatórios**, e nesta rodada os relatórios traziam mais de trinta itens "PARA O INTEGRADOR". Aviso em prosa não escala e não vence uma sessão que termina antes da hora.

A conclusão prática: **todo órfão recorrente vira teste, e o teste mora na pasta do `integrador`**. Foi o que se fez aqui, e cada um deles nasceu de um defeito que já tinha atravessado ao menos uma rodada:

| Órfão que se repetia | Guarda que passou a pegá-lo sozinho |
|---|---|
| `SchemaVersionEsperada` atrás da migration (3 rodadas) | `TestSchemaVersionEsperadaAcompanhaAUltimaMigration`, que já existia — o que faltava era o CI olhar para ele |
| Código de erro no contrato sem espelho em Go, e vice-versa | `internal/router/contrato_de_erros_test.go` — varre o enum da OpenAPI e os literais do Go **nos dois sentidos** |
| Rota nova nascendo fora da varredura de campo desconhecido | a própria varredura já falhava pedindo o alvo; o alvo de `/contacts` entrou |
| Tela entregue e ausente da imagem servida | `make smoke-stack` (constrói, migra, semeia e percorre) e a fumaça reprovando em **404**, não só em 5xx |
| Item de menu apontando para tela que não existe | `navigation.test.ts` (painel) + a fumaça reprovando prefetch RSC 404 |
| Painel com lint e 334 testes que o CI nunca rodava | job `admin` do CI passou a rodar `pnpm lint` e `pnpm test --run` |

A regra que sai daí, para a próxima rodada: **quem escrever "PARA O INTEGRADOR" sobre algo que já apareceu num relatório anterior deve propor a guarda automática junto**, e não só o conserto. Item que só existe em prosa volta.

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
- [ ] `make smoke` passa com o stack no ar — e, quando a entrega muda imagem, schema ou seed, `make smoke-stack`, que sobe tudo antes. Suíte verde não prova aplicação servida: já houve rodada com tudo verde e o painel morrendo no boot
- [ ] A rota está na OpenAPI e o teste de contrato aceita
- [ ] Todo `code` de erro novo está nos DOIS lados (enum do `openapi.yaml` e literal em Go); `internal/router/contrato_de_erros_test.go` confere
- [ ] Há teste automatizado cobrindo o caminho feliz e ao menos um erro de regra de negócio
- [ ] Migrations têm `down` e o CI validou o ciclo completo
- [ ] Permissão e escopo (`all|own`) foram verificados para os três perfis
- [ ] Nada foi escrito fora das pastas do agente
