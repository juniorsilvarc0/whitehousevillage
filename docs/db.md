# Modelo de dados

> PostgreSQL 16. Extensões: `btree_gist` (constraint de sobreposição), `pg_trgm` (busca), `pgcrypto` (`gen_random_uuid`).

## Convenções

| Regra | Motivo |
|---|---|
| PK `uuid` v7 | Ordenável no tempo (bom para índice) e não enumerável como `serial` |
| `created_at`/`updated_at` `timestamptz` + `created_by`/`updated_by` | Auditoria mínima em toda tabela |
| `property_id` em toda tabela de negócio | Não impede uma segunda propriedade depois; custa quase nada agora |
| Dinheiro `bigint` em centavos, sufixo `_cents` | Float em dinheiro é bug de auditoria |
| Estadia em `date` e `daterange`; instante em `timestamptz` | "Dia" de hospedagem não tem fuso; evento tem |
| Enum = `text` + `CHECK` | Migração muito mais simples que `ENUM` nativo |
| Máximo ~25 colunas por tabela | O `crm_opportunities` de 118 colunas do portal_amimoveis é o antiexemplo |
| Soft delete só onde há histórico (`deleted_at` + índice parcial) | Deletar reserva quebra o razão |
| Toda FK indexada | Evita seq scan em cascade e em join |
| Migrations nomeadas por timestamp | `20260820T143000_nome.up.sql` — dois agentes em paralelo não colidem |

---

## 1. O coração: `stay_blocks`

Toda ocupação do calendário — reserva, bloqueio de manutenção, uso do proprietário ou importação de OTA — é uma linha aqui. **Não existe calendário paralelo.**

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE stay_blocks (
  id              uuid PRIMARY KEY,
  property_id     uuid NOT NULL REFERENCES properties(id),
  unit_id         uuid NOT NULL REFERENCES units(id) ON DELETE RESTRICT,
  reservation_id  uuid REFERENCES reservations(id) ON DELETE CASCADE,
  source          text NOT NULL CHECK (source IN ('reservation','maintenance','owner_hold','ota')),
  status          text NOT NULL CHECK (status IN ('hold','confirmed','completed','cancelled','expired')),
  period          daterange NOT NULL,        -- SEMPRE '[check_in, check_out)'
  expires_at      timestamptz,               -- obrigatório em hold
  external_ref    text,                      -- uid do evento iCal, quando source='ota'
  note            text,
  owner_id        uuid REFERENCES users(id),  -- dono comercial (scope='own')
  created_by      uuid REFERENCES users(id),  -- quem digitou (auditoria)
  created_at      timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT stay_period_valid CHECK (lower(period) < upper(period)),
  CONSTRAINT stay_hold_expires CHECK (status <> 'hold' OR expires_at IS NOT NULL),
  CONSTRAINT stay_no_overlap EXCLUDE USING gist (
      unit_id WITH =,
      period  WITH &&
  ) WHERE (status IN ('hold','confirmed'))
);

CREATE INDEX stay_blocks_period_idx ON stay_blocks USING gist (period);
CREATE INDEX stay_blocks_reservation_idx ON stay_blocks (reservation_id);
CREATE INDEX stay_blocks_hold_idx ON stay_blocks (expires_at) WHERE status = 'hold';
CREATE INDEX stay_blocks_ocupacao_idx ON stay_blocks USING gist (period)
  WHERE status IN ('hold','confirmed','completed');
CREATE INDEX stay_blocks_owner_idx ON stay_blocks (owner_id) WHERE owner_id IS NOT NULL;
CREATE UNIQUE INDEX stay_blocks_ota_uid ON stay_blocks (unit_id, external_ref)
  WHERE source = 'ota' AND external_ref IS NOT NULL;
```

Três decisões que valem o projeto inteiro:

1. **`daterange` half-open `[in, out)`** — espelha exatamente a contagem de noites e permite back-to-back (check-out dia 24 + check-in dia 24 na mesma unidade não conflitam). Com `[]` isso quebraria.
2. **A constraint parcial** (`WHERE status IN ('hold','confirmed')`) faz a pré-reserva *realmente* segurar a data, e o cancelamento liberar sem apagar histórico.
3. **A exclusividade da White House Completa cai de graça**: a Completa consome as 8 unidades, então vendê-la insere 8 linhas — qualquer unidade ocupada faz a inserção estourar `23P01`, que a API traduz para `409 DATE_CONFLICT`. Não há `SELECT` antes de `INSERT`, logo não há corrida.

> **Regra de implementação**: inserir as 8 linhas sempre em ordem determinística (`ORDER BY units.code`), senão duas transações inserindo subconjuntos em ordens opostas causam deadlock. Retry automático apenas em `40001`/`40P01`; `23P01` nunca faz retry.

### Os cinco estados, e por que `completed` fica FORA da `EXCLUDE`

| Estado | Ocupa inventário | Aparece no mapa | Significado |
|---|---|---|---|
| `hold` | sim | sim | pré-reserva segurando a data até `expires_at` |
| `confirmed` | sim | sim | venda fechada |
| `completed` | **não** | **sim** | estadia consumada — o hóspede veio, ficou e saiu |
| `cancelled` | não | não | a venda não aconteceu |
| `expired` | não | não | a pré-reserva venceu sem confirmação |

`completed` nasceu em `20260826120000` porque o `/check-out` mandava a estadia para `cancelled` — o único terminal que existia. Efeito medido: reserva de cobertura 10–13/09/2030, confirmada, check-in, check-out → o mapa devolvia os três dias como `livre` e a ocupação de setembro/2030 contava **0 noites**. Pior que o mapa vazio era a ambiguidade: estadia consumada ficava byte a byte igual a venda perdida, e nenhuma consulta separava as duas.

**A constraint continua valendo só para `('hold','confirmed')`.** `completed` descreve consumo que já ocorreu — é lançamento de razão, não promessa de data. Três consequências práticas de tê-lo dentro do predicado, todas ruins:

- **Check-out antecipado travaria a unidade.** O bloco carrega o período do *contrato*, não o que o hóspede de fato ficou. Quem sai no dia 11 de uma reserva 10–13 deixa um bloco 10–13 para trás; com `completed` na `EXCLUDE`, as noites 11 e 12 ficariam vendidas para ninguém — overbooking ao contrário.
- **Correção de histórico estouraria `23P01`.** Importar estadia passada de OTA ou consertar a unidade errada num registro antigo viraria `409 DATE_CONFLICT`, que no contrato significa "data ocupada" — mentira para o operador.
- **O índice cresceria para sempre.** Fora do predicado, ele carrega só o inventário vivo e encolhe a cada check-out; dentro, acumularia toda a história no índice mais quente do sistema.

O custo aceito, dito por inteiro: o banco deixa de impedir duas estadias concluídas sobrepostas na mesma unidade. Como a transição é sempre `confirmed → completed` sobre linha que já esteve protegida, isso só nasceria de um `INSERT` retroativo — o mesmo caso de importação que queremos deixar passar.

> **Consequência para quem escreve consulta**: "ocupa o inventário" e "aparece no mapa" deixaram de ser o mesmo predicado. Bloqueio de venda é `('hold','confirmed')`; desenho do mapa, ocupação e ADR/RevPAR são `('hold','confirmed','completed')`.

### `owner_id` × `created_by` em `stay_blocks`

São duas perguntas diferentes e por isso duas colunas. `created_by` é **quem digitou** — fato de auditoria, imutável. `owner_id` é **de quem é a linha** — resposta comercial, que se transfere quando a carteira muda de mãos, e é o eixo de `scope='own'` (mesmo nome que em `reservations`, para o repositório traduzir o escopo com uma regra só).

Usar `created_by` como dono não funciona, e o teste mostra por quê: o corretor abre a reserva (`reservations.owner_id` = corretor), o admin confirma (`stay_blocks.created_by` = admin), e o calendário do corretor com `AND created_by = $usuario` devolve **zero** — a própria venda dele some porque quem apertou "confirmar" foi outra pessoa. Bloco de reserva espelha `reservations.owner_id`; bloqueio operacional herda quem criou.

---

## 2. Identidade e acesso

```
users(id, property_id, name, email UNIQUE, password_hash, role_id, broker_id?, phone?, active, last_login_at, deleted_at?)
roles(id, code UNIQUE, name, is_system)
resources(code PK, label, group_label, actions text[], supports_own bool, sort_order)
        -- catálogo: reservations, crm.opportunities, finance… — é a fonte de GET /roles/resources
role_permissions(role_id, resource_code, action, scope) -- action: ver|criar|editar|excluir · scope: all|own
refresh_tokens(id, user_id, token_hash, family_id, expires_at, revoked_at, replaced_by)
password_resets(id, user_id, token_hash, expires_at, used_at)
```

O eixo **`scope`** é o que o portal_amimoveis não tem e é exatamente o que resolve "corretor vê só o dele": o repositório aplica `AND owner_id = $user` quando o escopo é `own`. Sem `if role == "corretor"` em lugar nenhum.

**Escopo `own` exige coluna de dono — e permissão simétrica.** Onde não há dono identificável, `resources.supports_own` é `false` e a grade nem oferece "só os meus"; oferecer um escopo que o SQL não sabe aplicar degrada silenciosamente para `all`. E conceder `criar` sem `excluir` no mesmo recurso é armadilha, não restrição: a revisão mediu o corretor bloqueando as 8 unidades por 364 dias em `calendar` (`POST /blocks` → 201) sem conseguir desfazer (`DELETE /blocks/{id}` → 403). O seed passou a conceder as quatro ações de `calendar` em `own`, o que só é seguro porque `stay_blocks.owner_id` existe e o repositório filtra por ele.

`users.broker_id` é o vínculo do usuário de perfil `corretor` com o cadastro comercial dele (`brokers`, §10). Nasce **sem foreign key**, e de propósito: `brokers` só existe a partir da migration do financeiro, e uma FK não pode apontar para tabela que ainda não foi criada. A FK entra junto com a tabela referenciada; até lá o índice parcial `users(broker_id) WHERE broker_id IS NOT NULL` já paga o join do painel do corretor.

`resources.sort_order` é a ordem das linhas na grade de perfis — o agrupamento visual da tela é dado, não uma ordenação alfabética que embaralharia "Reservas" com "Recebíveis".

`resources.actions` e `resources.supports_own` também são **dado**, não lista em Go: nem todo recurso tem as quatro ações (o razão financeiro é append-only e mensagem enviada não se apaga, então `finance.*` e `chat` não oferecem `excluir`; painel, relatórios e auditoria só oferecem `ver`), e escopo `own` só é oferecido onde existe dono identificável na linha. Onde não existe, a tela desabilita "só os meus" em vez de prometer um filtro que o SQL não sabe aplicar.

---

## 3. Inventário

```
properties(id, name, slug, timezone, address_*, active)
unit_types(id, property_id, code, name, capacity, consumes, cleaning_fee_cents, sort_order, active)
        -- consumes: 'one_member' (produto simples) | 'all_members' (a Completa)
units(id, property_id, code UNIQUE, name, floor, notes, sort_order, active)
unit_type_members(unit_type_id, unit_id)   -- PK composta — o vínculo é AQUI, e só aqui
amenities(id, code, label, icon) · unit_amenities(unit_id, amenity_id)
unit_photos(id, unit_id, url, sort_order, caption)
```

Oito unidades (`AP-01..03`, `SP-01..04`, `COB-01`) e quatro produtos. A Completa é `consumes='all_members'` e aponta para as oito.

**`units` não tem `unit_type_id`** — esta página listava a coluna e a migration `20260820130000` nunca a criou, porque ela seria *errada*. A relação produto × unidade é **muitos-para-muitos de propósito**: `AP-01` é vendável como *Apartamento 2 Suítes* **e** como parte da *White House Completa*, e uma FK escalar em `units` só saberia escrever um dos dois. `unit_type_members` é a única verdade sobre essa composição, e é dela que a Completa tira as 8 linhas de `stay_blocks` que dão a exclusividade bidirecional de graça.

---

## 4. Calendário comercial e tarifário

```
holidays(id, property_id, date UNIQUE, name, active)
special_periods(id, property_id, name, kind, starts_on, ends_on, active)
        -- kind: reveillon | carnaval | alta | evento — intervalos PODEM se sobrepor
date_type_rules(kind PK, precedence int, weekday_mask int)
        -- reveillon/carnaval 100 · feriado 80 · alta 60 · fds 40 (sex,sáb) · normal 0
rate_tables(id, property_id, name, valid_from, valid_to, active)
rates(id, rate_table_id, unit_type_id, date_type, amount_cents)   -- UNIQUE(rate_table_id, unit_type_id, date_type)
min_nights_rules(id, rate_table_id, date_type, nights)
commercial_policies(id, property_id, version, deposit_pct, balance_due_days, hold_hours,
                    discount_auto_pct, discount_approval_pct, event_deposit_cents, valid_from,
                    hold_extension_hours, hold_max_extensions)
        -- o limite de `extend-hold` (spec §5) é política versionada e congela com policy_version;
        -- quantas extensões já houve sai de reservation_events, não de contador denormalizado
cancellation_policies(id, property_id, version, name, valid_from)
cancellation_tiers(id, policy_id, days_before_min, days_before_max, refund_pct, label, sort_order)
        -- min/max NULL = sem piso / sem teto (é o `-1` de booking.Tier traduzido para SQL)
```

Precedência é **dado**, não `if/else` — mudar a ordem de resolução é `UPDATE date_type_rules`.

`special_periods` **não** leva constraint de exclusão: a sobreposição é intencional (Réveillon dentro da alta temporada) e a precedência resolve.

Três tabelas ganharam **chave natural** para o seed poder reencontrar a própria linha na segunda execução: `special_periods(property_id, name)`, `rate_tables(property_id, name)` e `cancellation_tiers(policy_id, sort_order)`. Por isso o nome do período carrega o ano (`Réveillon 2026/2027`): o Réveillon do ano seguinte é linha nova, não edição desta.

---

## 5. Reservas

```
reservations(id, property_id, code UNIQUE,            -- WH-2026-0001, DEFAULT proximo_codigo_reserva()
             unit_type_id, contact_id, broker_id?, owner_id?, source,
             check_in date, check_out date, guests_count,
             status, is_event, event_type?,
             hold_expires_at, confirmed_at, cancelled_at, cancel_reason,
             rebooked_from_id?, notes, created_by, created_at, updated_at)   -- 23 colunas
             -- channel_id entra com o módulo de canais (Fase 4); ainda não existe na tabela

reservation_pricing(reservation_id PK/FK,            -- 1:1, satélite do bloco financeiro
             subtotal_cents, discount_pct, discount_cents, cleaning_cents,
             event_deposit_cents, total_cents, deposit_cents,
             rate_table_id, policy_version, cancellation_policy_id,   -- snapshots (regra 7)
             created_at)

reservation_code_counters(year PK, last_number, updated_at)   -- numeração sem corrida

reservation_nights(reservation_id, night date, date_type, unit_type_id, price_cents)  -- PK(reservation_id, night)
reservation_units(reservation_id, unit_id, stay_block_id, locked bool)                -- PK(reservation_id, unit_id)
reservation_guests(reservation_id, contact_id, is_lead_guest)
reservation_events(id, reservation_id, type, payload jsonb, actor_id, at)             -- append-only
```

**`reservation_nights` é o que faz auditoria, financeiro e BI funcionarem.** Guarda a tarifa efetivamente aplicada em cada noite; mudar o tarifário amanhã não reescreve o passado, e ADR/RevPAR saem de um `GROUP BY`.

Constraints: `CHECK (check_out > check_in)`, `CHECK (guests_count > 0)` e `CHECK (discount_pct BETWEEN 0 AND 100)`.

O teto do banco é sanidade, não alçada. A alçada comercial — ≤ 5% a gestão fecha, 6–10% pede o proprietário, > 10% não autorizado (spec §3) — é **política versionada**, avaliada no domínio e congelada na reserva. Como `CHECK` fixo em 10 ela viraria número de schema: mudar a alçada exigiria migration, e as reservas antigas passariam a violar a regra nova.

### `reservation_pricing` — o satélite financeiro

A tabela nasceu com **32 colunas**, acima do teto de ~25 desta página. A dívida foi paga em `20260826100000_reservation_pricing` (roadmap **1a**): `reservations` ficou com **23**.

O critério da separação não é "financeiro", é **ciclo de vida**. `reservations` guarda identidade, estado e datas — muda a cada transição (`hold` → `confirmed` → `checked_in` → …). `reservation_pricing` é *snapshot congelado*: nasce no cálculo do orçamento e não muda mais. Junto numa linha só, todo `UPDATE` de estado reescreveria sem necessidade o preço acordado com o hóspede.

**Imutável depois da confirmação** — contrato do time, não constraint. Reprecificar reserva confirmada (remarcação, upgrade, correção de desconto) é **reserva nova** referenciando a anterior por `rebooked_from_id`, com a original indo para `cancelled` (spec §5); nunca `UPDATE` aqui. Antes da confirmação (`quote`/`hold`) a linha é recalculável à vontade — ainda não há dinheiro nem promessa. Fica em comentário e no domínio, e não em trigger, porque "o que conta como confirmada" é decisão comercial versionada: em PL/pgSQL ela sairia de `internal/domain` (regra 1 do CLAUDE.md).

### Código legível sem corrida

`code` tem `DEFAULT proximo_codigo_reserva()`. A função faz um `INSERT ... ON CONFLICT DO UPDATE` em `reservation_code_counters`, que é **atômico**: a segunda transação bloqueia na linha do ano e lê o valor já incrementado — não existe janela entre ler e gravar porque é a mesma instrução. Medido: 30 transações concorrentes → 30 códigos distintos, `WH-2026-0001` a `WH-2026-0030`, zero erro.

Não é `SEQUENCE` porque sequence não é transacional, e a Fase 1 tem um caminho de rollback muito frequente — o `23P01` de data ocupada. Cada recusa queimaria um número, e a numeração de contrato teria buracos inexplicáveis.

Como é `DEFAULT`, nenhum caminho de criação pode esquecer de gerar o código, e a **ordem de travamento** fica garantida pelo banco: a linha de `reservations` nasce antes de qualquer `stay_blocks` (a FK exige), então o lock do contador vem sempre antes dos locks das unidades. Ordem única de aquisição é o que impede deadlock entre as duas travas. O repositório **omite** `code` no `INSERT` e o lê no `RETURNING`.

### `owner_id` — o escopo `own`

`scope = 'own'` vira `AND owner_id = $usuario` no SQL do repositório (regra 8). É `users(id)` e não `brokers(id)`: quem o RBAC filtra é o **usuário autenticado**, e o vínculo com a carteira já vive em `users.broker_id`. Anulável — importação de OTA e bloqueio da operação nascem sem dono comercial, e reserva sem dono simplesmente não aparece para quem tem escopo `own`, que é o comportamento correto.

---

## 6. Contatos

```
contacts(id, property_id, name, email, phone_e164, doc_type, doc_number, birth_date,
         city, state, notes, lgpd_basis, marketing_opt_in, consent_at,
         anonymized_at, created_at, updated_at)
```
`UNIQUE(phone_e164) WHERE phone_e164 IS NOT NULL` e índice em `doc_number`. Uma pessoa, um registro: lead, hóspede, corretor e proprietário apontam para cá.

`anonymize(contact_id)` substitui PII por hash e mantém os registros financeiros — atende ao direito de eliminação sem quebrar o razão fiscal.

---

## 7. CRM

```
crm_pipelines(id, property_id, name, is_default, active)
crm_stages(id, pipeline_id, name, position, probability, color, type,      -- aberto|ganho|perdido
           sla_days, auto_task_subject, auto_task_type, auto_task_due_days, auto_notify)
crm_leads(id, property_id, contact_id, source, campaign_id?, status, score,
          interest_unit_type_id?, desired_check_in?, desired_check_out?, owner_id, converted_at)
crm_opportunities(id, property_id, contact_id NOT NULL, lead_id?, pipeline_id, stage_id,
                  unit_type_id?, check_in?, check_out?, quote_id?, reservation_id?,
                  amount_cents, probability, expected_close, owner_id, status,
                  lost_reason_id?, entered_stage_at, created_by, created_at, updated_at)
crm_opportunity_event_details(opportunity_id PK, event_type, guests_expected, needs_catering, notes)
crm_stage_history(id, opportunity_id, from_stage_id, to_stage_id, user_id, reason, at)  -- insert-only
crm_activities(id, property_id, type, subject, description, due_at, done_at, status, priority,
               lead_id?, opportunity_id?, contact_id?, owner_id, stage_id?, auto bool)
crm_lost_reasons(id, label, active) · crm_notes · crm_documents · crm_campaigns
```

Herdado do portal_amimoveis (o que funciona): `sla_days` + tarefa automática idempotente ao entrar na etapa, `stage_history` insert-only, endpoint agregador `/full`.
Descartado (o que não funciona): a tabela larga de 118 colunas — detalhe de evento vive em tabela satélite.

---

## 8. Chat

```
chat_integrations(id, property_id, provider, phone_number, config jsonb, status, is_active)
chat_conversations(id, integration_id, external_id, contact_id?, status,   -- bot|human|resolved
                   assigned_to?, last_message_at, unread_count, archived_at)
                   -- UNIQUE(integration_id, external_id)   ← external_id é o chatid
chat_messages(id, conversation_id, external_id, direction, type, content, media_url,
              media_mime, quoted_message_id?, delivery_status, sent_by_user_id?,
              is_deleted, metadata jsonb, created_at)
              -- UNIQUE(conversation_id, external_id)   ← mata o eco de fromMe
chat_quick_replies · chat_labels · chat_conversation_labels
```

`delivery_status` só avança (`pending < sent < delivered < read`) — a guarda é do repositório, para webhook fora de ordem não regredir o tick.

---

## 9. Agenda

```
agenda_events(id, property_id, type, title, starts_at, ends_at, all_day,
              unit_id?, reservation_id?, contact_id?, assignee_id?, status, notes)
              -- type: checkin|checkout|limpeza|manutencao|visita|evento|bloqueio
agenda_settings(property_id PK, business_hours jsonb, slot_minutes, buffer_minutes)
agenda_blocks(id, property_id, starts_at, ends_at, all_day, reason)
```

---

## 10. Financeiro

```
accounts(id, property_id, code, name, kind)                 -- plano de contas
receivables(id, property_id, reservation_id?, contact_id, kind, description,
            amount_cents, paid_cents, due_date, status, refundable, created_at)
            -- kind: deposit | balance | security_deposit | extra
payables(id, property_id, kind, party_type, party_id, description,
         amount_cents, paid_cents, due_date, status)
         -- kind: commission | owner_payout | supplier | expense | deposit_refund
payments(id, property_id, receivable_id?, payable_id?, method, amount_cents,
         paid_at, external_ref, reconciled_at, created_by)
commission_rules(id, broker_id?, unit_type_id?, pct, min_cents, valid_from)
commissions(id, broker_id, reservation_id, base_cents, pct, amount_cents, status, payable_id?)
owner_payouts(id, property_id, period_start, period_end, gross_cents, fees_cents, net_cents, status)
ledger_entries(id, property_id, entry_date, account_code, debit_cents, credit_cents,
               ref_type, ref_id, created_at)                -- append-only, sem UPDATE
expense_categories · brokers(id, contact_id, user_id?, commission_rule_id?, goal_cents, active)
```

Comissão incide sobre diárias, nunca sobre limpeza ou caução. Caução é `receivables.kind='security_deposit'` com `refundable=true`; o check-out gera `payables.kind='deposit_refund'`, integral ou parcial com laudo anexado.

---

## 11. Operação

```
inventory_items(id, property_id, name, category, unit_measure, min_stock, cost_cents)
unit_inventory(unit_id, item_id, standard_qty)               -- PK composta
stock_movements(id, item_id, qty, kind, reservation_id?, unit_id?, at, created_by)
housekeeping_tasks(id, unit_id, reservation_id?, scheduled_for, status, checklist jsonb, assignee_id)
maintenance_orders(id, unit_id, title, description, priority, status,
                   stay_block_id?, opened_at, closed_at, cost_cents)
```

Ordem de manutenção cria `stay_block` de origem `maintenance` — bloqueia o calendário como qualquer outra ocupação.

---

## 12. Canais / OTA

```
channels(id, code, name, kind, active)                       -- kind: ical | api
channel_listings(id, channel_id, unit_id, external_listing_id, import_url,
                 export_token UNIQUE, risk_window_days, last_sync_at, last_error, active)
channel_events(id, channel_id, listing_id, external_uid, unit_id, period, raw jsonb, imported_at)
                 -- UNIQUE(channel_id, external_uid)
channel_conflicts(id, listing_id, unit_id, period, our_reservation_id?, external_uid,
                  detected_at, resolved_at, resolution)
```

---

## 13. Integrações e auditoria

```
api_tokens(id, name, prefix, token_hash UNIQUE, scopes text[], expires_at, last_used_at, revoked_at, created_by)
webhooks(id, name, url, events text[], secret, active)
webhook_deliveries(id, webhook_id, event, payload jsonb, attempt, status_code,
                   response_body, error, next_retry_at, delivered_at, dead_at)
integration_logs(id, provider, direction, action, status, payload jsonb, error, created_at)
idempotency_keys(key, endpoint, actor_id, property_id, request_hash, status, response_body, created_at)
        -- PK(key, endpoint, actor_id, property_id) — a chave SEM ator vazava resposta entre usuários
app_settings(namespace, key, value jsonb, is_secret, updated_by, updated_at)      -- PK(namespace, key)
audit_log(id, property_id, actor_id, action, entity, entity_id, before jsonb, after jsonb,
          ip, user_agent, request_id, at)
pii_access_log(id, actor_id, contact_id, reason, at)
```

`audit_log` particionado por mês a partir do segundo ano.

---

## 14. Índices e constraints que não podem faltar

| Tabela | Constraint / índice |
|---|---|
| `stay_blocks` | `EXCLUDE USING gist (unit_id WITH =, period WITH &&) WHERE status IN ('hold','confirmed')` — `completed` fica de fora de propósito (§1) |
| `stay_blocks` | `CHECK (lower(period) < upper(period))` · `CHECK (status<>'hold' OR expires_at IS NOT NULL)` · gist em `period` · parcial em `expires_at` |
| `stay_blocks` | gist parcial em `period WHERE status IN ('hold','confirmed','completed')` — o mapa e a ocupação · parcial em `owner_id` — o escopo `own` de `calendar` |
| `reservations` | `UNIQUE(code)` · `CHECK (check_out > check_in)` · `CHECK (discount_pct BETWEEN 0 AND 100)` — alçada é política versionada, não constraint |
| `reservation_nights` | `PRIMARY KEY (reservation_id, night)` |
| `rates` | `UNIQUE(rate_table_id, unit_type_id, date_type)` |
| `holidays` | `UNIQUE(property_id, date)` |
| `special_periods` | `CHECK (ends_on >= starts_on)` + gist em `daterange(starts_on, ends_on, '[]')` |
| `contacts` | `UNIQUE(phone_e164) WHERE phone_e164 IS NOT NULL` · trigram em `name` |
| `chat_messages` | `UNIQUE(conversation_id, external_id)` |
| `chat_conversations` | `UNIQUE(integration_id, external_id)` |
| `channel_events` | `UNIQUE(channel_id, external_uid)` |
| `idempotency_keys` | `PRIMARY KEY (key, endpoint, actor_id, property_id)` — sem o ator na chave, o replay devolve a resposta de um usuário a outro |
| `reservations` | `code` com `DEFAULT proximo_codigo_reserva()` — numeração por ano, densa e sem corrida |
| `reservations` | `(property_id, status, check_in)` — a listagem · `(unit_type_id, check_in)` — ocupação por produto |
| `reservations` | parciais em `owner_id`, `broker_id`, `rebooked_from_id`, `created_by` — FKs majoritariamente nulas |
| `reservation_pricing` | `PRIMARY KEY (reservation_id)` **é** a FK — é isto que faz o 1:1 · `CHECK (discount_cents <= subtotal_cents)` |
| `reservation_units` | `(unit_id)` e parcial em `(stay_block_id)` — a PK começa por `reservation_id` e não serve a busca pela unidade |
| `reservation_guests` | `(contact_id)` — mesma razão |
| `reservation_nights` | `(unit_type_id, night)` — ADR/RevPAR saem daqui |
| `reservation_code_counters` | `PRIMARY KEY (year)` — é a linha em que a numeração serializa |
| `idempotency_keys` | `(created_at)` — a varredura que expira chaves |
| `role_permissions` | `PRIMARY KEY (role_id, resource_code, action)` |
| `resources` | `CHECK (cardinality(actions) > 0 AND actions <@ ARRAY['ver','criar','editar','excluir'])` |
| `users` | `UNIQUE(email)` · `INDEX(broker_id) WHERE broker_id IS NOT NULL` — parcial porque só corretor tem vínculo; a FK entra com `brokers` |
| `special_periods` | `UNIQUE(property_id, name)` — chave natural do seed |
| `rate_tables` | `UNIQUE(property_id, name)` — chave natural do seed |
| `cancellation_tiers` | `UNIQUE(policy_id, sort_order)` — chave natural do seed |
| todas | índice em toda FK |

**Disponibilidade por unidade e período não ganha índice próprio.** O `EXCLUDE USING gist (unit_id WITH =, period WITH &&) WHERE status IN ('hold','confirmed')` já cria exatamente esse índice, com exatamente o predicado do mapa de ocupação. Criar um igual ao lado dobraria o custo de escrita sem ganhar leitura nenhuma.

---

## 15. Migrations

- Ferramenta: **golang-migrate**. Arquivos em `apps/api/migrations/`, nomeados `AAAAMMDDHHMMSS_descricao.{up,down}.sql`.
- Aplicadas por `cmd/migrate` como **passo separado** (`make migrate`), nunca no boot da API. A API consulta a versão em `/readyz` e **se recusa a servir** se a migration esperada não estiver aplicada.
- Toda `up` tem `down` correspondente; o CI roda `up` e depois `down` até zero num Postgres efêmero.
- **Só o agente `db-migrations` cria migration.** Nome por timestamp evita a colisão clássica de dois agentes criando `000007_*`.

Entregues até aqui — a última é a versão que o binário exige em `/readyz` (`router.SchemaVersionEsperada`), e por isso ela sobe **no mesmo commit** da migration:

| Migration | O que trouxe |
|---|---|
| `20260820120000_core` | propriedade, identidade e RBAC (`users`, `roles`, `resources`, `role_permissions`, `refresh_tokens`, `password_resets`), auditoria, `app_settings` e `idempotency_keys` |
| `20260820130000_inventario_reservas` | inventário, calendário comercial e tarifário, políticas, contatos, `reservations`, `reservation_*` e `stay_blocks` com a `EXCLUDE` |
| `20260820140000_catalogo_e_chaves_naturais` | `users.broker_id` + índice parcial; catálogo completo em `resources` (`actions`, `supports_own`, `sort_order` e o `CHECK` do vocabulário); chaves naturais de `special_periods`, `rate_tables` e `cancellation_tiers` |
| `20260826100000_reservation_pricing` | extrai o satélite financeiro 1:1 e leva `reservations` de 32 para 22 colunas (dívida **1a** / **D2**) |
| `20260826110000_reservas_fase1` | `reservations.owner_id` (escopo `own`); `reservation_code_counters` + `proximo_codigo_reserva()` como `DEFAULT` de `code`; `hold_extension_hours`/`hold_max_extensions` na política comercial; os índices das consultas quentes da Fase 1 |
| `20260826120000_historico_ocupacao_dono_do_bloco_e_idempotencia_por_ator` | `stay_blocks.status = 'completed'` (o check-out deixa de apagar a estadia do mapa) + gist parcial da ocupação; `idempotency_keys` chaveada por `(key, endpoint, actor_id, property_id)` (o replay deixa de vazar resposta entre usuários); `stay_blocks.owner_id` + índice parcial (o escopo `own` de `calendar` vira SQL) |

## 16. Seeds

`cmd/seed` popula, numa **transação única**: propriedade, 8 unidades, 4 produtos e a composição (a Completa apontando para as oito), tipos de data com precedência, feriados e períodos de 2026–2027, Tabela Comercial V1 (24 tarifas + estadia mínima), política comercial e de cancelamento v1, um contato de demonstração, o catálogo de 23 recursos, os 3 perfis com a matriz inteira e um usuário de cada perfil para desenvolvimento.

O **contato de demonstração** existe porque `reservations.contact_id` é `NOT NULL`: sem ele não há como abrir orçamento, pré-reserva ou smoke test da jornada num banco recém-semeado. Sua chave natural é o telefone (`UNIQUE(phone_e164)`), o que torna a etapa idempotente. É gated como os usuários de desenvolvimento — em produção só entra com `SEED_DEMO_DATA=true`, porque contato fictício polui a base real de leads. E-mail em `.invalid` (RFC 2606, nunca resolve) e telefone em faixa não atribuível a celular no Brasil, para o WhatsApp jamais casar uma pessoa real com esta linha.

**Idempotente por contrato**: rodar dez vezes tem o mesmo efeito de rodar uma. Três decisões sustentam isso:

1. Toda escrita é `INSERT ... ON CONFLICT` sobre a **chave natural** da tabela (`slug`, `code`, `(property_id, code)`, `(rate_table_id, unit_type_id, date_type)`…), nunca sobre id gerado — id novo a cada execução é exatamente o que duplicaria tudo na segunda rodada.
2. O `DO UPDATE` leva `WHERE (colunas) IS DISTINCT FROM (EXCLUDED.colunas)`: linha já correta não é reescrita e não sobe `updated_at`. A segunda execução loga zero criadas e zero atualizadas — é essa a prova de idempotência.
3. O seed **corrige divergência, mas não apaga acréscimo**: nunca há `DELETE`. Permissão que a gestão concedeu a mais na tela sobrevive; escopo que divergiu da matriz volta ao valor do seed.

Ainda **não** semeia o funil padrão do CRM: `crm_pipelines`/`crm_stages` só existem depois da migration do módulo.

Senha de desenvolvimento com hash argon2id via `internal/auth`; **nunca em texto, nunca em log**. Em `APP_ENV=production` o bloco de usuários é pulado com aviso, a menos que `SEED_DEV_USERS=true` — conta com senha conhecida em produção é porta dos fundos, não conveniência.
