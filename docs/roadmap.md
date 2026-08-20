# Roadmap

> Documento vivo. O `tech-lead` atualiza ao fim de cada fatia. Registra também o que foi **decidido não fazer agora**.

## Estado atual

| | |
|---|---|
| Fase corrente | **0 — Fundação** |
| Última atualização | 20/08/2026 |
| Repositório | `github.com/juniorsilvarc0/whitehousevillage` |

## Fase 0 — Fundação

- [x] Repositório, monorepo, `.gitignore`, `Makefile`, `.env.example`
- [x] Documentação de produto e arquitetura (`docs/`)
- [x] Definições do time de agentes (`.claude/agents/`)
- [ ] `docker-compose` dev (postgres + api + admin + worker) e Dockerfiles
- [ ] `cmd/migrate` + migration inicial (extensões, `properties`, `users`, `roles`, `audit_log`)
- [ ] Esqueleto chi com `/healthz`, `/readyz`, `/metrics` e envelope de erro
- [ ] Auth (login, refresh rotativo, `/auth/me`) e RBAC por dados
- [ ] `cmd/seed` idempotente com os 3 perfis
- [ ] Casca do painel Next com o design system portado + login
- [ ] CI verde (lint, testes, ciclo de migrations)

**Pronto quando**: `make up && make migrate && make seed` sobe tudo numa máquina limpa, os três perfis logam e o CI está verde.

## Fase 1 — Núcleo ponta a ponta

- [ ] 1a Inventário: 8 unidades, 4 produtos, composição da Completa
- [ ] 1b Tarifário e políticas versionadas (Tabela V1)
- [ ] 1c Motor puro de disponibilidade e orçamento + suíte de testes
- [ ] 1d Reservas: `stay_blocks` com `EXCLUDE`, alocação, hold com expiração real, confirmação, cancelamento por política
- [ ] 1e Mapa de ocupação em tempo real (SSE)
- [ ] 1f CRM: funil, oportunidade, atividades, SLA, alertas, ganho → reserva
- [ ] 1g Chat WhatsApp (uazapi) com takeover

**Pronto quando**: a jornada roda ao vivo — mensagem no WhatsApp → lead → oportunidade → orçamento com desconto pedindo aprovação → pré-reserva travando as 8 unidades → sinal → reserva confirmada na agenda.

## Fase 2 — Dinheiro e rotina
Financeiro (recebíveis, pagáveis, pagamentos, conciliação, caução), comissões, agenda operacional, hóspedes e LGPD.
**Pronto quando**: confirmar reserva gera recebíveis e comissão sozinho, e o fechamento do mês bate com o razão.

## Fase 3 — Escala comercial
Portal do corretor, contratos em PDF, BI e KPIs (ocupação, ADR, RevPAR, conversão, motivos de perda).
**Pronto quando**: corretor opera sozinho no próprio escopo e o KPI de ocupação bate com a contagem manual no mapa.

## Fase 4 — Canais / OTA
`ChannelProvider`, iCal bidirecional, janela de risco, painel de conflitos, stubs de Airbnb e Booking.
**Pronto quando**: bloqueio criado no Airbnb aparece no mapa em ≤ 15 min e conflito vira alerta, nunca overbooking silencioso.

## Fase 5 — Operação e plataforma
Inventário operacional, ordens de manutenção, tokens com escopo, webhooks com outbox, agente de IA e MCP.

## Fase 6 — Hardening e go-live
Carga (mapa < 300 ms p95), `EXPLAIN ANALYZE` das 10 queries mais quentes, revisão de segurança, restore testado com RTO/RPO medidos, runbook e treinamento.

---

## Decisões registradas

| Data | Decisão | Motivo |
|---|---|---|
| 20/08/2026 | OTAs começam por **iCal**, não por API | Airbnb (Software Partner) e Booking (Connectivity) são fechados; a *Demand API* do briefing é da ponta compradora |
| 20/08/2026 | **Unidades físicas nominais**, produto ≠ unidade | Sem `unit_id` concreto não há constraint capaz de impedir overbooking; e a operação precisa saber qual apartamento limpar |
| 20/08/2026 | **pgx nativo**, não sqlc nem ORM | `daterange`, `EXCLUDE`, `jsonb` e `LISTEN/NOTIFY` não sobrevivem bem ao codegen; filtros dinâmicos também não |
| 20/08/2026 | **SSE**, não WebSocket | Push é unidirecional; atravessa Traefik sem upgrade e reconecta sozinho |
| 20/08/2026 | **River** para jobs, sobre o próprio Postgres | Retry, unique job e agendamento sem Redis na VPS |
| 20/08/2026 | RBAC com eixo de **escopo `all|own`** | É o que resolve "corretor vê só o dele" sem `if role ==` espalhado |
| 20/08/2026 | Perfis iniciais: **admin, usuario, corretor** | Definido pelo usuário; novos perfis entram por configuração, não por código |

## Fora de escopo por enquanto

Motor de reserva público com pagamento online · aplicativo nativo · multi-tenant comercial · emissão fiscal · rodar modelo de IA internamente · substituir o site de marketing.
