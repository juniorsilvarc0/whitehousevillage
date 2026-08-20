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
  status          text NOT NULL CHECK (status IN ('hold','confirmed','cancelled','expired')),
  period          daterange NOT NULL,        -- SEMPRE '[check_in, check_out)'
  expires_at      timestamptz,               -- obrigatório em hold
  external_ref    text,                      -- uid do evento iCal, quando source='ota'
  note            text,
  created_by      uuid REFERENCES users(id),
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
CREATE UNIQUE INDEX stay_blocks_ota_uid ON stay_blocks (unit_id, external_ref)
  WHERE source = 'ota' AND external_ref IS NOT NULL;
```

Três decisões que valem o projeto inteiro:

1. **`daterange` half-open `[in, out)`** — espelha exatamente a contagem de noites e permite back-to-back (check-out dia 24 + check-in dia 24 na mesma unidade não conflitam). Com `[]` isso quebraria.
2. **A constraint parcial** (`WHERE status IN ('hold','confirmed')`) faz a pré-reserva *realmente* segurar a data, e o cancelamento liberar sem apagar histórico.
3. **A exclusividade da White House Completa cai de graça**: a Completa consome as 8 unidades, então vendê-la insere 8 linhas — qualquer unidade ocupada faz a inserção estourar `23P01`, que a API traduz para `409 DATE_CONFLICT`. Não há `SELECT` antes de `INSERT`, logo não há corrida.

> **Regra de implementação**: inserir as 8 linhas sempre em ordem determinística (`ORDER BY units.code`), senão duas transações inserindo subconjuntos em ordens opostas causam deadlock. Retry automático apenas em `40001`/`40P01`; `23P01` nunca faz retry.

---

## 2. Identidade e acesso

```
users(id, property_id, name, email UNIQUE, password_hash, role_id, broker_id?, active, last_login_at)
roles(id, code UNIQUE, name, is_system)
resources(code PK, label, group)                      -- catálogo: reservations, crm.opportunities, finance...
role_permissions(role_id, resource_code, action, scope) -- action: ver|criar|editar|excluir · scope: all|own
refresh_tokens(id, user_id, token_hash, family_id, expires_at, revoked_at, replaced_by)
password_resets(id, user_id, token_hash, expires_at, used_at)
```

O eixo **`scope`** é o que o portal_amimoveis não tem e é exatamente o que resolve "corretor vê só o dele": o repositório aplica `AND owner_id = $user` quando o escopo é `own`. Sem `if role == "corretor"` em lugar nenhum.

---

## 3. Inventário

```
properties(id, name, slug, timezone, address_*, active)
unit_types(id, property_id, code, name, capacity, consumes, cleaning_fee_cents, sort_order, active)
        -- consumes: 'one_member' (produto simples) | 'all_members' (a Completa)
units(id, property_id, unit_type_id?, code UNIQUE, name, floor, notes, active)
unit_type_members(unit_type_id, unit_id)   -- PK composta
amenities(id, code, label, icon) · unit_amenities(unit_id, amenity_id)
unit_photos(id, unit_id, url, sort_order, caption)
```

Oito unidades (`AP-01..03`, `SP-01..04`, `COB-01`) e quatro produtos. A Completa é `consumes='all_members'` e aponta para as oito.

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
                    discount_auto_pct, discount_approval_pct, event_deposit_cents, valid_from)
cancellation_policies(id, property_id, version, name, valid_from)
cancellation_tiers(id, policy_id, days_before_min, days_before_max, refund_pct, retain_deposit_pct)
```

Precedência é **dado**, não `if/else` — mudar a ordem de resolução é `UPDATE date_type_rules`.

`special_periods` **não** leva constraint de exclusão: a sobreposição é intencional (Réveillon dentro da alta temporada) e a precedência resolve.

---

## 5. Reservas

```
reservations(id, property_id, code UNIQUE,            -- WH-2026-0001
             unit_type_id, contact_id, broker_id?, channel_id?, source,
             check_in date, check_out date, guests_count,
             status, is_event, event_type?,
             subtotal_cents, discount_pct, discount_cents, cleaning_cents,
             deposit_cents, total_cents,
             rate_table_id, policy_version, cancellation_policy_id,   -- snapshots
             hold_expires_at, confirmed_at, cancelled_at, cancel_reason,
             rebooked_from_id?, notes, created_by, created_at, updated_at)

reservation_nights(reservation_id, night date, date_type, unit_type_id, price_cents)  -- PK(reservation_id, night)
reservation_units(reservation_id, unit_id, stay_block_id, locked bool)                -- PK(reservation_id, unit_id)
reservation_guests(reservation_id, contact_id, is_lead_guest)
reservation_events(id, reservation_id, type, payload jsonb, actor_id, at)             -- append-only
```

**`reservation_nights` é o que faz auditoria, financeiro e BI funcionarem.** Guarda a tarifa efetivamente aplicada em cada noite; mudar o tarifário amanhã não reescreve o passado, e ADR/RevPAR saem de um `GROUP BY`.

Constraints: `CHECK (check_out > check_in)`, `CHECK (discount_pct BETWEEN 0 AND 10)`, `CHECK (guests_count > 0)`.

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
idempotency_keys(key, endpoint, request_hash, status, response_body, created_at)  -- UNIQUE(key, endpoint)
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
| `stay_blocks` | `EXCLUDE USING gist (unit_id WITH =, period WITH &&) WHERE status IN ('hold','confirmed')` |
| `stay_blocks` | `CHECK (lower(period) < upper(period))` · `CHECK (status<>'hold' OR expires_at IS NOT NULL)` · gist em `period` · parcial em `expires_at` |
| `reservations` | `UNIQUE(code)` · `CHECK (check_out > check_in)` · `CHECK (discount_pct BETWEEN 0 AND 10)` |
| `reservation_nights` | `PRIMARY KEY (reservation_id, night)` |
| `rates` | `UNIQUE(rate_table_id, unit_type_id, date_type)` |
| `holidays` | `UNIQUE(property_id, date)` |
| `special_periods` | `CHECK (ends_on >= starts_on)` + gist em `daterange(starts_on, ends_on, '[]')` |
| `contacts` | `UNIQUE(phone_e164) WHERE phone_e164 IS NOT NULL` · trigram em `name` |
| `chat_messages` | `UNIQUE(conversation_id, external_id)` |
| `chat_conversations` | `UNIQUE(integration_id, external_id)` |
| `channel_events` | `UNIQUE(channel_id, external_uid)` |
| `idempotency_keys` | `UNIQUE(key, endpoint)` |
| `role_permissions` | `PRIMARY KEY (role_id, resource_code, action)` |
| todas | índice em toda FK |

---

## 15. Migrations

- Ferramenta: **golang-migrate**. Arquivos em `apps/api/migrations/`, nomeados `AAAAMMDDTHHMMSS_descricao.{up,down}.sql`.
- Aplicadas por `cmd/migrate` como **passo separado** (`make migrate`), nunca no boot da API. A API consulta a versão em `/readyz` e **se recusa a servir** se a migration esperada não estiver aplicada.
- Toda `up` tem `down` correspondente; o CI roda `up` e depois `down` até zero num Postgres efêmero.
- **Só o agente `db-migrations` cria migration.** Nome por timestamp evita a colisão clássica de dois agentes criando `000007_*`.

## 16. Seeds

`cmd/seed` é idempotente e popula: propriedade, 8 unidades, 4 produtos e composição, feriados e períodos de 2026–2027, Tabela Comercial V1, política comercial e de cancelamento, funil padrão com SLA, catálogo de recursos e os 3 perfis, e um usuário de cada perfil para desenvolvimento.
