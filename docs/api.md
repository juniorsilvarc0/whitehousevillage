# API — convenções e superfície

Base: `/api/v1`. JSON em tudo. O contrato formal é `apps/api/openapi/openapi.yaml`, validado no CI.

## 1. Envelope

```jsonc
// recurso único
{ "data": { "id": "...", "...": "..." } }

// lista
{ "data": [ ... ], "meta": { "page": 1, "per_page": 25, "total": 137, "total_pages": 6 } }

// erro
{ "error": { "code": "MIN_STAY_NOT_MET", "message": "Estadia mínima de 4 noites no Réveillon.",
             "details": { "required": 4, "requested": 2 } } }
```

Códigos de erro são **estáveis** e fazem parte do contrato — o front reage ao `code`, nunca à mensagem.

| Código | HTTP | Quando |
|---|---:|---|
| `VALIDATION_ERROR` | 422 | DTO inválido (`details` traz campo → erro) |
| `UNAUTHORIZED` / `FORBIDDEN` | 401 / 403 | Sem token / sem permissão ou fora do escopo |
| `NOT_FOUND` | 404 | Recurso inexistente ou fora do escopo do usuário |
| `DATE_CONFLICT` | 409 | Sobreposição de datas (`23P01` do Postgres) |
| `MIN_STAY_NOT_MET` | 422 | Noites abaixo do mínimo do período |
| `CAPACITY_EXCEEDED` | 422 | Hóspedes acima da capacidade |
| `DISCOUNT_ABOVE_LIMIT` | 422 | Desconto acima da alçada |
| `HOLD_EXPIRED` | 409 | Pré-reserva já expirou |
| `IDEMPOTENCY_MISMATCH` | 409 | Mesma chave com corpo diferente |
| `RATE_LIMITED` | 429 | Limite de requisições |

## 2. Verbos e CRUD completo

Todo recurso expõe os seis:

```
GET    /recurso              lista paginada e filtrada
POST   /recurso              cria (201 + Location)
GET    /recurso/{id}         detalhe
PUT    /recurso/{id}         substitui
PATCH  /recurso/{id}         parcial — só o que veio muda
DELETE /recurso/{id}         remove (soft delete onde há histórico)
```

Isso é garantido **estruturalmente**, não por disciplina:

1. `crud.Mount[T, Create, Update]()` registra os seis verbos de uma vez, já com paginação, filtros whitelisted, RBAC e auditoria.
2. A tabela declarativa em `internal/router/routes.go` é a fonte da OpenAPI.
3. Um **teste de contrato** no CI varre a tabela e falha se algum recurso não expõe os seis verbos, se algum verbo não checa permissão, ou se existe rota fora da OpenAPI.

`PATCH` distingue "campo ausente" de "campo nulo" com `Opt[T]`:

```go
type Opt[T any] struct{ Set, Valid bool; Value T }  // Set && !Valid  ⇒  limpar o campo
```

## 3. Listagem

`?page=1&per_page=25&sort=-created_at&q=texto&<filtros>`

- `per_page` máximo 100. `sort` aceita só colunas na whitelist do recurso.
- Filtros são declarados por recurso (`paginate.Spec`), e o `WHERE` sai parametrizado — nunca concatenação de string.
- Chat e auditoria usam **keyset** (`?before=<cursor>&limit=50`), porque `OFFSET` em tabela grande é caro e instável.

## 4. Cabeçalhos

| Header | Uso |
|---|---|
| `Authorization: Bearer <jwt>` | Autenticação (ou cookie `httpOnly` pelo BFF do painel) |
| `Idempotency-Key` | **Obrigatório** em POST que cria reserva ou movimento financeiro |
| `If-Match` / `ETag` | Concorrência otimista em reserva e oportunidade |
| `X-Request-Id` | Propagado para o log; devolvido na resposta |

## 5. Tipos

- Datas de estadia: `"2026-11-20"` (é `date`, não instante).
- Instantes: RFC 3339 com offset — `"2026-11-20T14:00:00-03:00"`.
- Dinheiro: inteiro em centavos, sufixo `_cents`. `total_cents: 705000` é R$ 7.050,00.
- Percentual: número decimal (`discount_pct: 5`).

## 6. Grupos de rotas

**Auth e acesso**
`POST /auth/login` · `/auth/refresh` · `/auth/logout` · `GET /auth/me` · `POST /auth/password/forgot|reset` · `/users` CRUD · `/roles` CRUD · `GET /roles/resources` · `PUT /roles/{id}/permissions`

**Inventário**
`/properties` · `/unit-types` CRUD + `/{id}/members` · `/units` CRUD · `/amenities`

**Tarifário e política**
`/rate-tables` · `/rates` CRUD + `POST /rates/bulk` · `/holidays` · `/special-periods` · `/min-nights` · `/policies/commercial` · `/policies/cancellation` + `/tiers`

**Disponibilidade e reservas**
```
GET  /availability?from&to&unit_type_id        disponibilidade por produto
GET  /availability/units?from&to               matriz unidade × dia (fonte do mapa)
POST /quotes                                   orçamento — não bloqueia data
GET  /quotes/{id}
POST /reservations            [Idempotency-Key]  cria como hold e aloca unidade(s)
GET  /reservations?status&from&to&q&page
GET|PATCH|DELETE /reservations/{id}
GET  /reservations/{id}/full                   monta a tela inteira numa chamada
POST /reservations/{id}/confirm                registra sinal → confirmed
POST /reservations/{id}/cancel[?dry_run=1]     aplica a política congelada
POST /reservations/{id}/reschedule
POST /reservations/{id}/check-in | /check-out
POST /reservations/{id}/reassign-unit
POST /reservations/{id}/extend-hold
POST /blocks  ·  DELETE /blocks/{id}           bloqueio operacional
```

**CRM**
`/crm/pipelines` (+`/duplicate`) · `/crm/stages` (+`/reorder`) · `/crm/leads` (+`/{id}/convert`) · `/crm/opportunities` (+`/kanban`, `/{id}/full`, `/{id}/stage`, `/{id}/quote`, `/{id}/win`, `/{id}/lose`) · `/crm/activities` (+`/{id}/complete`) · `/crm/lost-reasons` · `/crm/campaigns`

**Chat**
`/chat/conversations` (+`/{id}/messages`, `/{id}/send`, `/send-audio`, `/send-file`, `/{id}/takeover`, `/{id}/resolve`) · `/chat/messages/{id}` · `/chat/quick-replies` · `/chat/connection/{qr,state,disconnect}` · `POST /chat/webhook/uazapi` *(público, autenticado por segredo)*

**Agenda** `/agenda/events` CRUD · `GET /agenda/board?view=day|week|month|list` · `/agenda/settings` · `/agenda/blocks`

**Financeiro** `/finance/receivables` (+`/{id}/settle`) · `/finance/payables` · `/finance/payments` · `/finance/reconciliation` · `/finance/commissions` · `/finance/owner-payouts` (+`/generate`) · `/finance/summary`

**Corretores** `/brokers` CRUD · `/brokers/{id}/commissions` · `/brokers/me/{leads,opportunities,commissions}`

**Operação** `/inventory/items` · `/inventory/stock` · `/housekeeping/tasks` · `/maintenance/orders`

**Canais** `/channels` · `/channels/{id}/listings` · `POST /channels/{id}/sync` · `/channels/conflicts` · `GET /ical/{export_token}.ics` *(público, token no path)*

**Integrações** `/integrations/api-tokens` · `/integrations/webhooks` (+`/{id}/deliveries`, `/{id}/test`) · `/integrations/logs` · `/integracao/*` *(ferramentas do agente de IA)* · `/mcp`

**Config, auditoria e BI** `/settings` · `/audit` · `/bi/kpis` · `/bi/occupancy` · `/bi/funnel` · `/bi/revenue`

**Realtime** `GET /stream?topics=calendar,chat:{id},crm` *(SSE, com `Last-Event-ID`)*

## 7. Realtime

O evento é **magro** de propósito:

```
event: calendar
data: {"entity":"stay_block","id":"...","unit_id":"...","v":7}
```

O cliente recebe "algo mudou" e refaz o fetch autenticado. Assim nenhum dado sensível trafega pelo barramento e o RBAC continua sendo aplicado num lugar só. Publicação por `pg_notify` **dentro da transação** — se a transação falhar, o evento não sai.

## 8. Versionamento

`/api/v1` estável. Mudança quebrando contrato cria `/api/v2` coexistindo, com `Deprecation` e `Sunset` no v1. Campo novo em resposta não é breaking; remover ou renomear é.
