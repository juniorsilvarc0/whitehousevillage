# Modelo de dados

> PostgreSQL 16. Extensões: `btree_gist` (constraint de sobreposição), `pg_trgm` (busca), `pgcrypto` (`gen_random_uuid`) — mais `plpgsql`, são as quatro que `\dx` devolve.

**O que já existe e o que ainda é projeto.** Esta página descreve o modelo inteiro,
inclusive módulos sem uma linha de SQL escrita. Seção cuja tabela **existe** foi
conferida coluna a coluna contra o banco; seção cuja tabela **ainda não existe** abre
com o aviso *Ainda não existe no banco*. A distinção não é cosmética: quem lê §10 e
escreve `INSERT INTO receivables` toma `42P01`, e quem "conserta" o schema por
migration para casar com a página cria coluna que ninguém escreve e ninguém lê — foi
exatamente o que aconteceu com `crm_opportunities.quote_id` (§7).

Conferido em **31/08/2026** contra o Postgres do compose (`schema_migrations =
20260827150000`) e, para `20260831100000` — que já está na árvore e ainda **não** foi
aplicada lá —, contra um Postgres descartável com todas as migrations do repositório
aplicadas. Em **02/10/2026**, §2, §5, §10, §14, §15 e §16 foram conferidos de novo
contra um Postgres descartável com todas as migrations até `20261002180000`
(`brokers` e as FKs de `broker_id`) e o seed aplicado. Em **03/10/2026**, §3, §4, §14,
§15 e §16 foram conferidos contra um Postgres descartável em `20261003100000`
(`public_name`, `unit_type_min_nights`, `rate_packages`) com os dois catálogos do seed
aplicados e alternados. Ainda em **03/10/2026**, §2, §13b, §14, §15 e §16 foram
conferidos em `20261003120000` (`site_content`, `site_media` e o recurso RBAC `site`),
com `up` → `down 1` → `up`, `down -all` → `up` e os dois catálogos semeados duas vezes
cada. Em **07/10/2026**, §3, §11, §14 e §15 foram conferidos em `20261007170000` (as oito
tabelas do inventário de bens por ambiente): as colunas, as constraints e os índices
foram lidos de `pg_constraint`/`pg_indexes`, cada `CHECK` e cada chave parcial foi
provada com `INSERT` que deve falhar, e o ciclo rodou `up` → `down 1` → `up` no banco de
desenvolvimento e `up` → `down -all` → `up` num Postgres descartável — o `down` não deixa
tabela, índice, constraint, função nem sequência para trás. Ainda em **07/10/2026**, §11,
§14 e §15 foram conferidos em `20261007200000` (`inventory_count_lines.replacement_cost_cents`,
o custo congelado no fechamento): coluna, `CHECK` e comentário foram lidos de
`information_schema`/`pg_constraint`; o `CHECK` foi provado com `INSERT` e `UPDATE` de
custo zero e negativo, que falham com `23514`; o backfill foi provado com conferências
fechada, cancelada e aberta numa transação desfeita; e o ciclo rodou `up` → `down 1` →
`up` no banco de desenvolvimento (PG 16) e `up` → `down 1` → `up` → `down -all` → `up`
num Postgres descartável (PG 17, no host, porque a VM do Docker estava sem disco). O
`pg_dump --schema-only` depois de `up` + `down 1` é idêntico ao de um banco que nunca viu
a migration. Ainda em **07/10/2026**, §3, §11, §14 e §15 foram conferidos em
`20261007213000` (`unit_rooms.code`, a identidade estável do cômodo). A coluna, as três
constraints e o comentário foram lidos de `information_schema`/`pg_constraint`. O backfill
foi provado com 24 cômodos em três unidades (acento composto e decomposto, colisão, nome
que vira vazio, corte em 60, ordem por `created_at`), primeiro numa transação desfeita e
depois comitado e migrado pelo `cmd/migrate`, no PG 16 do desenvolvimento (`en_US.utf8`) e
num PG 17 descartável em `locale C`, com resultado idêntico. Formato, tamanho e `UNIQUE`
foram provados com `INSERT` que deve falhar. O ciclo rodou `up` → `down 1` → `up` no
desenvolvimento e `up` → `down 1` → `up` → `down -all` → `up` no descartável, e o
`pg_dump --schema-only` depois de cada `down 1` é idêntico ao de antes da migration. Em
**09/10/2026**, §1, §11, §14, §15 e §16 foram conferidos em `20261009100000`
(`maintenance_orders`, as duas `UNIQUE` que servem de alvo às FKs compostas e o recurso RBAC
`maintenance`), num PG 17 descartável no host (o Docker estava parado). As 18 colunas, as
constraints e os índices foram lidos de `information_schema`/`pg_constraint`/`pg_indexes`. Cada
`CHECK`, cada FK (as compostas inclusive) e cada chave parcial foi provada com `INSERT`,
`UPDATE` ou `DELETE` que deve falhar, com o `SQLSTATE` e o nome da constraint conferidos, num
cenário em que só a constraint provada segura cada caso: 37 recusas e 11 controles que devem
passar, 48 casos sem nenhum resultado inesperado. A régua de 25 colunas, uma das 37, foi provada
acrescentando 8 colunas e rodando o bloco `DO`. O ciclo rodou `up` → `down 1`
→ `up` e `down -all` → `up`; o `pg_dump --schema-only` depois de `down 1` é idêntico ao de antes
da migration, e o de cada `up` é idêntico ao do primeiro. Os dois catálogos foram semeados duas
vezes cada, num banco recém-migrado e alternando no mesmo banco. Para
repetir a conferência de qualquer afirmação daqui:

```bash
# quais tabelas existem
psql -Atc "select table_name from information_schema.tables where table_schema='public' order by 1"
# quantas colunas tem uma tabela, e quais
psql -Atc "select ordinal_position, column_name, data_type, is_nullable
             from information_schema.columns where table_name='crm_opportunities'
            order by ordinal_position"
# constraints e índices
psql -Atc "select conname, pg_get_constraintdef(oid) from pg_constraint
            where conrelid='quotes'::regclass"
psql -Atc "select indexdef from pg_indexes where tablename='contacts'"
```

## Convenções

| Regra | Motivo |
|---|---|
| PK `uuid` com `DEFAULT gen_random_uuid()` | Não enumerável como `serial`. É **v4**, não v7: esta linha dizia v7 e nenhuma migration jamais gerou v7 — `pgcrypto` não tem gerador de v7 e o Postgres 16 não traz `uuidv7()`. Ordenação no tempo sai de `created_at`, que toda tabela tem |
| `created_at`/`updated_at` `timestamptz` + `created_by`/`updated_by` | Auditoria mínima em toda tabela |
| `property_id` em toda tabela de negócio | Não impede uma segunda propriedade depois; custa quase nada agora |
| Dinheiro `bigint` em centavos, sufixo `_cents` | Float em dinheiro é bug de auditoria |
| Estadia em `date` e `daterange`; instante em `timestamptz` | "Dia" de hospedagem não tem fuso; evento tem |
| Enum = `text` + `CHECK` | Migração muito mais simples que `ENUM` nativo |
| Máximo ~25 colunas por tabela | O `crm_opportunities` de 118 colunas do portal_amimoveis é o antiexemplo |
| Soft delete só onde há histórico (`deleted_at` + índice parcial) | Deletar reserva quebra o razão |
| Toda FK indexada | Evita seq scan em cascade e em join |
| Migrations nomeadas por timestamp | `20260820143000_nome.up.sql` — sem `T`, que é o formato que `golang-migrate` lê e o que está no disco; dois agentes em paralelo não colidem |

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
3. **A exclusividade da White House Completa cai quase de graça**: a Completa consome as 8 unidades, então vendê-la insere 8 linhas — qualquer unidade ocupada faz a inserção estourar `23P01`, que a API traduz para `409 DATE_CONFLICT`. Não há `SELECT` antes de `INSERT`, logo não há corrida. **O que a `EXCLUDE` não cobre** é a oitava linha nunca ter sido inserida: ausência não colide com nada. Essa metade é a invariante adiada de §5, e sem ela a frase "cai de graça" foi, por três revisões, meia verdade.

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

**O bloqueio de uma ordem de manutenção é uma linha daqui** (desde `20261009100000`, §11):
`source = 'maintenance'`, `confirmed`, apontada por `maintenance_orders.stay_block_id`. É a
mesma tabela de propósito — a manutenção impede a venda pela mesma `EXCLUDE` que impede duas
vendas. Liberar é `status = 'cancelled'` (ou o período cortado), **nunca `DELETE`**: a FK da
ordem é `ON DELETE RESTRICT`, e um `DELETE` numa linha citada falha com `23503` em
`maintenance_orders_stay_block_id_fkey`. Esta tabela não ganhou coluna, índice nem constraint
por causa disso (§11 diz por que a unidade e a fonte da linha apontada são garantia da API).

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
refresh_tokens(id, user_id, family_id, token_hash, user_agent, ip, expires_at, revoked_at,
               replaced_by, created_at)
password_resets(id, user_id, token_hash, expires_at, used_at)
```

O eixo **`scope`** é o que o portal_amimoveis não tem e é exatamente o que resolve "corretor vê só o dele": o repositório aplica `AND owner_id = $user` quando o escopo é `own`. Sem `if role == "corretor"` em lugar nenhum.

**Escopo `own` exige coluna de dono — e permissão simétrica.** Onde não há dono identificável, `resources.supports_own` é `false` e a grade nem oferece "só os meus"; oferecer um escopo que o SQL não sabe aplicar degrada silenciosamente para `all`. E conceder `criar` sem `excluir` no mesmo recurso é armadilha, não restrição: a revisão mediu o corretor bloqueando as 8 unidades por 364 dias em `calendar` (`POST /blocks` → 201) sem conseguir desfazer (`DELETE /blocks/{id}` → 403). O seed passou a conceder as quatro ações de `calendar` em `own`, o que só é seguro porque `stay_blocks.owner_id` existe e o repositório filtra por ele.

`users.broker_id` é o vínculo da conta de perfil `corretor` com o cadastro comercial dela (`brokers`, §10). Nasceu sem foreign key em `20260820140000`, porque `brokers` ainda não existia; desde `20261002180000` a FK existe, e é **composta**:

```sql
CONSTRAINT users_broker_id_fkey FOREIGN KEY (broker_id, id)
  REFERENCES brokers(id, user_id) ON UPDATE RESTRICT ON DELETE RESTRICT
```

O par, e não `(broker_id) → brokers(id)`, porque é este valor que o escopo `own` das reservas usa como "o corretor da própria conta" (F2-13, `commission.ResolveBroker`). Com a FK simples, a conta da gestão poderia apontar para o cadastro de um corretor que existe e passar a "ser" ele; com o par, `users.broker_id = B` só é aceito se `B.user_id` for a própria conta. Somado ao `UNIQUE (user_id)` de `brokers`, `users.broker_id` vale `NULL` ou exatamente o cadastro que aponta de volta para a conta. A única assimetria possível — `brokers.user_id` preenchido e `users.broker_id` nulo — falha **fechado**: o ator `own` sem corretor só grava venda direta.

Consequência para quem escreve: ligar a conta é `brokers.user_id` primeiro e `users.broker_id` depois; desligar é o inverso (`ON UPDATE RESTRICT` recusa a outra ordem — e `CASCADE` ali reescreveria `users.id`). Violação sai como `23503` com `constraint = users_broker_id_fkey`. A API não grava este campo nesta fase (o contrato o declara só leitura, e o CRUD de `/brokers` é do F2-17/Fase 3); hoje o vínculo nasce pelo seed (§16). O índice parcial `users(broker_id) WHERE broker_id IS NOT NULL` continua pagando o join do painel do corretor.

O recurso RBAC `brokers` (`supports_own = true`) tem dono na linha desde que a tabela existe: escopo `own` em `brokers` é `AND brokers.user_id = $usuario`.

`resources.sort_order` é a ordem das linhas na grade de perfis — o agrupamento visual da tela é dado, não uma ordenação alfabética que embaralharia "Reservas" com "Recebíveis".

`resources.actions` e `resources.supports_own` também são **dado**, não lista em Go: nem todo recurso tem as quatro ações (o razão financeiro é append-only e mensagem enviada não se apaga, então `finance.*` e `chat` não oferecem `excluir`; painel, relatórios e auditoria só oferecem `ver`), e escopo `own` só é oferecido onde existe dono identificável na linha. Onde não existe, a tela desabilita "só os meus" em vez de prometer um filtro que o SQL não sabe aplicar.

---

## 3. Inventário

```
properties(id, name, slug, timezone, address, city, state, active)
unit_types(id, property_id, code, name, capacity, consumes, cleaning_fee_cents,
           description?, sort_order, active, public_name?)      -- UNIQUE(property_id, code)
        -- consumes: 'one_member' (produto simples) | 'all_members' (a Completa, as Pool Suítes)
        -- public_name: nome de vitrine (site); NULL = usa name; CHECK unit_types_public_name_nao_vazio
units(id, property_id, code, name, floor, notes, sort_order, active)
        -- UNIQUE(property_id, code)
unit_type_members(unit_type_id, unit_id)   -- PK composta — o vínculo é AQUI, e só aqui
```

`amenities`, `unit_amenities` e `unit_photos` **ainda não existem no banco** — entram
com a ficha da unidade. **`unit_rooms` existe** desde `20261007170000`: é a unidade
dividida em cômodos, e vive em §11 porque quem a usa é o inventário. Desde
`20261007213000` o cômodo tem `code`, que é para ele o que `units.code` é para a unidade:
identidade que não se edita, única **na unidade** (§11). A unicidade de `units` e `unit_types` é
`(property_id, code)` nas duas tabelas, e não
`code` sozinho como esta página dizia: `property_id` existe em toda tabela justamente
para caber uma segunda propriedade, e o `AP-01` dela colidiria com o `AP-01` desta.

O inventário semeado depende do catálogo (§16). O **real** tem doze unidades (`AP-01..06`, `SP-01..04`, `GV-01`, `CV-01`) e catorze produtos: um por duplex, um por suíte, as *Pool Suítes* (`all_members` sobre `SP-01..04`), a Grand Villa, a Classic Villa e a Completa (`all_members` sobre as doze). O de **teste**, que a suíte de integração usa, é o de demonstração: oito unidades (`AP-01..03`, `SP-01..04`, `COB-01`) e quatro produtos, a Completa apontando para as oito.

**`unit_types.public_name`** (desde `20261003100000`) é o nome que o site mostra, separado do `name` interno que o painel usa — "Duplex Aurora" na vitrine, "AP 01 — Duplex Aurora" na operação. `NULL` quer dizer "use `name`"; string em branco é recusada pelo `CHECK (btrim(public_name) <> '')`, porque ela não cairia no fallback e o produto apareceria sem nome.

**`units` não tem `unit_type_id`** — esta página listava a coluna e a migration `20260820130000` nunca a criou, porque ela seria *errada*. A relação produto × unidade é **muitos-para-muitos de propósito**: `AP-01` é vendável como *Apartamento 2 Suítes* **e** como parte da *White House Completa*, e uma FK escalar em `units` só saberia escrever um dos dois. `unit_type_members` é a única verdade sobre essa composição, e é dela que a Completa tira as 8 linhas de `stay_blocks` que dão a exclusividade bidirecional. A partir de `20260827100000` esta tabela também é **lado de constraint**: acrescentar ou remover um membro de um produto `all_members` com venda viva é recusado no commit (§5), porque crescer a composição depois da venda abre exatamente o mesmo buraco que vender N−1.

**`unit_types.consumes` é imutável enquanto o produto tiver reserva viva** (`hold`, `confirmed`, `checked_in`), desde `20260827140000`. A porta que isso fecha foi medida com o stack no ar: com uma reserva confirmada da Completa ocupando as oito unidades, `UPDATE unit_types SET consumes='one_member' WHERE code='completa'` respondia `UPDATE 1`, sem recusa — e a venda seguinte da "Completa" passava a alocar **uma** unidade cobrando o preço da casa inteira.

O detalhe que torna esta guarda necessária, e não redundante com a invariante do §5: aquela é `WHEN (NEW.consumes = 'all_members')`, isto é, defende a **entrada** no regime da casa inteira. A troca perigosa é a **saída** — e ela é auto-imunizante, porque no instante em que o produto deixa de ser `all_members` a conferência de composição não encontra mais linha para ele e volta sem conferir nada. Trocar o tipo **tira o produto do alcance da invariante que o protegia**. A direção contrária (`one_member → all_members`) também é recusada, e por um caso que a contagem não pega: um produto de composição unitária atravessava a troca sem violar contagem nenhuma e mudava de significado em silêncio — medido, desligando só o gatilho novo, `UPDATE 1`.

A guarda trava a linha do produto em `FOR UPDATE` antes de contar. Não é zelo: criar reserva trava `unit_types` em `FOR KEY SHARE` (pela FK `unit_type_id`) e `UPDATE ... SET consumes` toma `FOR NO KEY UPDATE` — **os dois modos não conflitam**, e é por isso que venda e troca corriam lado a lado. `FOR UPDATE` é o único modo que conflita com `FOR KEY SHARE`. Medido nas duas versões: com a trava, a troca espera a venda em voo comitar e então recusa nomeando a reserva; sem a trava, a mesma troca passa em 100 ms e o banco fica com a Completa marcada `one_member` e uma pré-reserva viva de 8 unidades fora da invariante.

---

## 4. Calendário comercial e tarifário

```
holidays(id, property_id, date, name, active)            -- UNIQUE(property_id, date)
special_periods(id, property_id, name, kind, starts_on, ends_on, active)
        -- kind: reveillon | carnaval | alta | evento — intervalos PODEM se sobrepor
date_type_rules(kind PK, precedence int, weekday_mask int)
        -- reveillon/carnaval 100 · feriado 80 · alta 60 · fds 40 (sex,sáb) · normal 0
rate_tables(id, property_id, name, valid_from, valid_to, active)
rates(id, rate_table_id, unit_type_id, date_type, amount_cents)   -- UNIQUE(rate_table_id, unit_type_id, date_type)
min_nights_rules(id, rate_table_id, date_type, nights)
unit_type_min_nights(rate_table_id, unit_type_id, date_type, nights)
        -- PK (rate_table_id, unit_type_id, date_type); FKs rate_tables/unit_types ON DELETE CASCADE,
        --   date_type → date_type_rules(kind); CHECK (nights > 0); índice em unit_type_id
rate_packages(id, rate_table_id, unit_type_id, nights, date_types text[], total_cents)
        -- UNIQUE rate_packages_unicos (rate_table_id, unit_type_id, nights, date_types);
        --   CHECK (nights >= 2), CHECK (total_cents > 0), CHECK rate_packages_date_types_validos
        --   (cardinality > 0 e date_types <@ o vocabulário de date_type_rules); índice em unit_type_id
commercial_policies(id, property_id, version, deposit_pct, balance_due_days, hold_hours,
                    discount_auto_pct, discount_approval_pct, event_deposit_cents, valid_from,
                    hold_extension_hours, hold_max_extensions, quote_validity_days)
        -- 14 colunas; quote_validity_days entrou em 20260831100000 (D7), NOT NULL DEFAULT 7
        --   com CHECK (> 0) — validade zero é orçamento que nasce vencido
        -- o limite de `extend-hold` (spec §5) é política versionada e congela com policy_version;
        -- quantas extensões já houve sai de reservation_events, não de contador denormalizado
cancellation_policies(id, property_id, version, name, valid_from)
cancellation_tiers(id, policy_id, days_before_min, days_before_max, refund_pct, label, sort_order)
        -- min/max NULL = sem piso / sem teto (é o `-1` de booking.Tier traduzido para SQL)
```

Precedência é **dado**, não `if/else` — mudar a ordem de resolução é `UPDATE date_type_rules`.

**Tipo de data sem linha em `rates` é "sob consulta".** O catálogo real não tem diária de feriado, réveillon e carnaval para a Grand Villa, e nenhuma para a Completa: a ausência da linha é o dado, e o motor responde que o produto não tem preço de tabela naquela noite. `amount_cents` tem `CHECK (> 0)` justamente para zero não virar um jeito alternativo (e errado) de escrever isso.

**Estadia mínima por produto — `unit_type_min_nights`** (desde `20261003100000`). A regra geral (`min_nights_rules`) é por tipo de data; quando existe linha para `(tarifário, produto, tipo)` nesta tabela, ela **sobrepõe** a geral para aquele produto — as suítes pedem 2 noites num dia normal enquanto a regra geral pede 1. Sem linha, vale a geral. A regra de agregação continua a mesma (vale o maior mínimo entre as noites da estadia) e mora no domínio (`booking`); a tabela é só o dado. A PK é a chave natural, por isso não há `id`.

**Pacotes por duração — `rate_packages`** (desde `20261003100000`). Um pacote diz: *N noites CONSECUTIVAS cujos tipos de data estão todos em `date_types` podem ser cobradas por `total_cents`* em vez da soma das diárias (a Grand Villa: 2 noites normal/fds por R$ 6.500, 4 por R$ 12.000, 2 de alta por R$ 10.500, 4 por R$ 20.000). Decidir onde o pacote se aplica dentro de uma estadia — e se aplicá-lo é melhor para o hóspede — é do domínio (`booking`); a tabela não sabe aplicar nada. `date_types` é array porque o pacote cobre um **conjunto** de tipos; o Postgres não tem FK de elemento de array, e o vocabulário é garantido pelo `CHECK date_types <@ ARRAY['normal','fds','feriado','alta','reveillon','carnaval']` (elemento `NULL` também reprova, porque `<@` com `NULL` é falso). A `UNIQUE` compara o array **como está escrito**: `{normal,fds}` e `{fds,normal}` são linhas diferentes, e quem grava (o seed, a futura tela) precisa usar a ordem de `ordemDosTipos`. Nenhuma das duas tabelas tem `property_id`: penduram em `rate_tables`, como `rates` e `min_nights_rules`, e repetir a coluna seria uma segunda fonte da mesma verdade.

`special_periods` **não** leva constraint de exclusão: a sobreposição é intencional (Réveillon dentro da alta temporada) e a precedência resolve.

Três tabelas ganharam **chave natural** para o seed poder reencontrar a própria linha na segunda execução: `special_periods(property_id, name)`, `rate_tables(property_id, name)` e `cancellation_tiers(policy_id, sort_order)`. Por isso o nome do período do catálogo de teste carrega o ano (`Réveillon 2026/2027`): o Réveillon do ano seguinte é linha nova, não edição desta. O catálogo real usa os nomes que o dono deu, **sem ano** (`Natal`, `Réveillon`, `Férias de Janeiro`…): o calendário de 2027/2028 vai precisar de nomes distintos, ou de uma chave natural que inclua o intervalo.

---


## 4a. Orçamento persistido (`quotes`)

```
quotes(id, property_id, contact_id?, opportunity_id?,               -- 25 colunas
       unit_type_id, check_in date, check_out date, guests_count, is_event,
       subtotal_cents, discount_pct, discount_cents, cleaning_cents,
       event_deposit_cents, total_cents, deposit_cents,             -- congelados
       rate_table_id, policy_version, cancellation_policy_id?,      -- snapshots (regra 7)
       owner_id?, valid_until, reservation_id?,
       created_by, created_at, updated_at)

quote_nights(quote_id, night date, date_type, unit_type_id, price_cents)   -- PK(quote_id, night)
```

Até `20260827130000` o orçamento era **calculado e jogado fora**: `POST /quotes` rodava o motor e devolvia o número sem gravar nada. Três consequências, todas verificáveis na árvore: `/crm/opportunities/{id}/win` pedia um "orçamento vigente" que nenhum caminho de aplicação sabia criar (a suíte do CRM fabrica a linha por SQL direto, e o comentário dela diz isso com todas as letras); reabrir o orçamento de ontem **recalculava**, então uma alteração de tarifa mudava o preço que a gestão já tinha falado ao telefone; e não havia como responder quanto se orçou contra quanto virou venda.

**Por que tabela própria e não `reservations` em `quote`.** Toda reserva ocupa unidade física — é o que `reservation_units` e `stay_blocks` significam —, e orçamento **não bloqueia data** (spec §4): seria uma reserva permanentemente sem unidade, a exceção que enfraquece a leitura do módulo inteiro. Além disso o funil emite vários orçamentos para a mesma negociação, e cada tentativa viraria linha em `reservations` que nunca foi venda, gastando inclusive número de `WH-2026-…` em proposta recusada.

**O estado `quote` ainda é aceito por `reservations`, e isso é resíduo, não regra.**
Medido: `reservations_status_check` continua listando `'quote'` no vocabulário e
`internal/modules/reservas/estados.go` continua com `EstadoQuote` na máquina de
estados, como origem de `hold` e `cancelled`. Nenhum caminho de aplicação escreve esse
estado — `select status, count(*) from reservations group by 1` devolve hoje
`cancelled|8`, `confirmed|1`, `hold|2`, e zero em `quote`. Quem for implementar não
deve criar reserva em `quote`: o orçamento é `quotes`. Estreitar o `CHECK` e podar a
máquina é migration com dono, não edição desta página.

**Nenhuma linha aqui toca `stay_blocks`, e isso é a regra, não esquecimento.** Conferido: gravado um orçamento de 15–18/06, `stay_blocks` no período continua com zero linhas e a mesma unidade ainda pode ser vendida para as mesmas datas. Consequência aceita e correta: um orçamento pode virar `409 DATE_CONFLICT` na hora de virar reserva.

**O snapshot fecha, e quem garante é o banco.** `subtotal_cents` sem `quote_nights` é um número sem prova — passa em todos os `CHECK` de coluna e é indistinguível, na leitura, de um orçamento correto. Um `CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED` sobre as duas tabelas confere no commit: uma noite para cada dia de `[check_in, check_out)`, todas do produto orçado, somando exatamente o subtotal. Adiado pelo mesmo motivo do §5 — o cabeçalho nasce antes das noites (a FK exige) e portanto nasce com zero noites.

`CHECK (total_cents = subtotal_cents − discount_cents + cleaning_cents + event_deposit_cents)` escreve no banco a identidade do motor (`booking.Build`), que é exata em centavos porque o arredondamento acontece no total e nunca noite a noite. `balance_cents` (saldo) e `avg_nightly_cents` (diária média) **não são colunas**: são `total − sinal` e `total / noites`, e guardar o que se deriva é criar a segunda verdade que um dia diverge.

`valid_until` é o que separa "proposta em pé" de "preço que já venceu" — é por ela que um orçamento expira em vez de precisar ser apagado. É `timestamptz` porque vencimento é instante, e instante tem fuso. **De onde sai o número**: de quem grava quando informa; do padrão da política quando não informa. Desde `20260831100000` o **schema** guarda esse padrão em `commercial_policies.quote_validity_days` (§4, dívida **D7**). A **aplicação** ainda não o lê: conferido em 02/10/2026 nesta árvore, a emissão usa a constante `validadePadraoEmDias = 7` (`disponibilidade/dto_orcamento_salvo.go:22`, aplicada em `service_orcamentos.go:154`). Esta página dizia o contrário. A coluna passa a valer quando o F2-05 ligar a leitura — até lá, mudar de 7 para 15 dias continua exigindo recompilar a API. O que **não** muda: `quotes.valid_until` é `NOT NULL` e congelada na emissão (regra 7 do CLAUDE.md), então alterar a política nunca move a validade de orçamento já emitido.

A armadilha que essa coluna traz está nomeada na própria migration: `PublicarPoliticaComercial` monta o `INSERT` listando colunas nome a nome, e enquanto `quote_validity_days` não estiver nessa lista toda publicação de versão nova devolve o campo ao `DEFAULT 7` **em silêncio** — sem erro e sem linha de auditoria. É o item F2-05.

`reservation_id` tem **`UNIQUE` parcial**: uma reserva nasce de um orçamento só. Dois orçamentos reivindicando a mesma venda fariam a conversão contar duas vezes e a auditoria não saber qual preço foi o combinado.


## 5. Reservas

```
reservations(id, property_id, code UNIQUE,            -- WH-2026-0001, DEFAULT proximo_codigo_reserva()
             unit_type_id, contact_id, broker_id?, owner_id?, source,
             check_in date, check_out date, guests_count,
             status, is_event, event_type?,
             hold_expires_at, confirmed_at, cancelled_at, cancel_reason,
             rebooked_from_id?, notes, created_by, created_at, updated_at)   -- 23 colunas
             -- broker_id → brokers(id) ON DELETE RESTRICT desde 20261002180000 (abaixo)
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

Constraints **de `reservations`**: `UNIQUE(code)`, `CHECK (check_out > check_in)` e
`CHECK (guests_count > 0)`. O teto do desconto — `CHECK (discount_pct BETWEEN 0 AND 100)`
— mora em **`reservation_pricing`**, não aqui: `discount_pct` mudou de tabela em
`20260826100000` junto com o resto do snapshot, e esta página seguiu atribuindo o
`CHECK` a uma tabela que nem tem a coluna.

O teto do banco é sanidade, não alçada. A alçada comercial — ≤ 5% a gestão fecha, 6–10% pede o proprietário, > 10% não autorizado (spec §3) — é **política versionada**, avaliada no domínio e congelada na reserva. Como `CHECK` fixo em 10 ela viraria número de schema: mudar a alçada exigiria migration, e as reservas antigas passariam a violar a regra nova.

### `reservation_pricing` — o satélite financeiro

A tabela nasceu com **32 colunas**, acima do teto de ~25 desta página. A dívida foi paga em `20260826100000_reservation_pricing` (roadmap **1a**): `reservations` ficou com **23**.

O critério da separação não é "financeiro", é **ciclo de vida**. `reservations` guarda identidade, estado e datas — muda a cada transição (`hold` → `confirmed` → `checked_in` → …). `reservation_pricing` é *snapshot congelado*: nasce no cálculo do orçamento e não muda mais. Junto numa linha só, todo `UPDATE` de estado reescreveria sem necessidade o preço acordado com o hóspede.

**Imutável depois da confirmação** — contrato do time, não constraint. Reprecificar reserva confirmada (remarcação, upgrade, correção de desconto) é **reserva nova** referenciando a anterior por `rebooked_from_id`, com a original indo para `cancelled` (spec §5); nunca `UPDATE` aqui. Antes da confirmação (`quote`/`hold`) a linha é recalculável à vontade — ainda não há dinheiro nem promessa. Fica em comentário e no domínio, e não em trigger, porque "o que conta como confirmada" é decisão comercial versionada: em PL/pgSQL ela sairia de `internal/domain` (regra 1 do CLAUDE.md).

### Código legível sem corrida

`code` tem `DEFAULT proximo_codigo_reserva()`. A função faz um `INSERT ... ON CONFLICT DO UPDATE` em `reservation_code_counters`, que é **atômico**: a segunda transação bloqueia na linha do ano e lê o valor já incrementado — não existe janela entre ler e gravar porque é a mesma instrução. Medido: 30 transações concorrentes → 30 códigos distintos, `WH-2026-0001` a `WH-2026-0030`, zero erro.

Não é `SEQUENCE` porque sequence não é transacional, e a Fase 1 tem um caminho de rollback muito frequente — o `23P01` de data ocupada. Cada recusa queimaria um número, e a numeração de contrato teria buracos inexplicáveis.

Como é `DEFAULT`, nenhum caminho de criação pode esquecer de gerar o código, e a **ordem de travamento** fica garantida pelo banco: a linha de `reservations` nasce antes de qualquer `stay_blocks` (a FK exige), então o lock do contador vem sempre antes dos locks das unidades. Ordem única de aquisição é o que impede deadlock entre as duas travas. O repositório **omite** `code` no `INSERT` e o lê no `RETURNING`.

### `broker_id` — o corretor da venda

`broker_id` aponta para **`brokers(id)`**, não para `users(id)`: é o cadastro comercial que recebe a comissão (§10), e a partir do F2-13 é esta coluna que decide para quem vai o dinheiro. Até `20261002180000` ela não tinha FK nenhuma, e a medida do backlog F2-09 mostrou o efeito: um UUID inventado na hora respondeu `201` (`WH-2026-0008`). Desde então:

```sql
CONSTRAINT reservations_broker_id_fkey FOREIGN KEY (broker_id)
  REFERENCES brokers(id) ON DELETE RESTRICT
```

Anulável — `NULL` é venda direta, a maioria. `RESTRICT` porque corretor com venda não some: ele se desativa (`brokers.active = false`) e a venda continua dizendo de quem foi. Corretor inexistente sai como `23503` com `constraint = reservations_broker_id_fkey`, que a API traduz para `422 VALIDATION_ERROR` em `details.broker_id` — **sem** `SELECT` antes do `INSERT`, que seria TOCTOU. O nome segue o padrão do Postgres (`<tabela>_<coluna>_fkey`) para `db.campoDaConstraint` tirar dele o campo `broker_id`.

**O que a FK não fecha**: o corretor atribuir a venda a **outro** corretor que existe. Isso é autorização, não integridade, e é do F2-13 (escopo `own` só grava o `users.broker_id` do ator, §2).

**Os órfãos que existiam.** A migration anulou todo `broker_id` sem cadastro (com `brokers` recém-criada, isso é todo valor não nulo) em `reservations` e em `users`, e registrou cada um em `audit_log` com o valor antigo em `before`, ator nulo e `request_id = 'migration:20261002180000'`, além de um `RAISE WARNING` com a contagem e os códigos das reservas — conferido plantando os dois casos da medida (UUID inventado e id de outro usuário) num banco em `20260831100000` antes de aplicá-la. Não fere a regra 7 do CLAUDE.md: não há fato financeiro a congelar — nenhuma comissão existia, `broker_id` não está no snapshot de `reservation_pricing`, e o valor não identificava ninguém a quem pagar. O `down` não devolve esses valores (seriam a porta reaberta à mão); eles ficam na trilha.

### `owner_id` — o escopo `own`

`scope = 'own'` vira `AND owner_id = $usuario` no SQL do repositório (regra 8). É `users(id)` e não `brokers(id)`: quem o RBAC filtra é o **usuário autenticado**, e o vínculo com a carteira já vive em `users.broker_id`. Anulável — importação de OTA e bloqueio da operação nascem sem dono comercial, e reserva sem dono simplesmente não aparece para quem tem escopo `own`, que é o comportamento correto.

### A invariante da casa inteira — o que a `EXCLUDE` não vê

A `stay_no_overlap` impede duas ocupações da mesma unidade. Ela **não** impede a Completa ser vendida com sete das oito, porque o defeito é a **ausência de uma linha**, e constraint nenhuma vê o que não foi escrito. Sete linhas não colidem com nada; a oitava unidade fica livre, é vendida a outra pessoa, e um estranho dorme dentro da casa que alguém alugou inteira.

A regra que fecha isso, desde `20260827100000`:

> Para toda reserva **viva** (`hold`, `confirmed`, `checked_in`) de produto `consumes = 'all_members'`, o número de linhas em `reservation_units` é **igual** ao tamanho de `unit_type_members` do produto.

Ela é do banco, não da aplicação — quatro `CREATE CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED` chamando `conferir_composicao_completa(uuid)`, um por lado do buraco:

| Gatilho em | Fecha o vetor |
|---|---|
| `reservation_units` (INSERT/DELETE) | a venda que aloca N−1; a unidade desvinculada de uma venda viva |
| `reservations` (INSERT/UPDATE de `status`, `unit_type_id`) | a reserva que nasce, ou revive, sem nenhuma unidade |
| `unit_type_members` (INSERT/DELETE) | a composição que cresce (ou encolhe) **depois** da venda |
| `unit_types` (UPDATE de `consumes`) | o produto que vira `all_members` com venda viva |

Desde `20260827140000` existe um **quinto** gatilho, e ele é de outra natureza: `unit_types_consumes_com_venda_viva` recusa a **transição** de `consumes` enquanto o produto tem reserva viva, nas duas direções (§3). Os quatro de cima conferem a invariante; ele impede que o produto saia do alcance dela. Os dois ficam: no dia em que alguém afrouxar a transição, é a invariante que continua impedindo a casa de ser vendida pela metade.

**Adiar não é otimização, é requisito.** Um `CHECK` — mesmo materializado numa coluna — não pode ser adiado no Postgres, e reprovaria a ordem de escrita **correta** da aplicação: a linha de `reservations` nasce antes das unidades (a FK exige), e `Realocar` fica legitimamente com N−1 entre o `DELETE` da unidade antiga e o `INSERT` da nova. Estado intermediário inconsistente é normal; o que não pode existir é estado inconsistente **comitado**. Só o `CONSTRAINT TRIGGER` junta as duas coisas de que a regra precisa: código que enxerga ausência, avaliado no `COMMIT`.

Violação sai como `23514` com `constraint = reservation_units_composicao_completa`, e a mensagem diz o código da reserva e quantas de quantas unidades ela ocupa.

**Custo medido** (`pgbench`, inserção pura da Completa — reserva + 8 blocos + 8 unidades): 6,24 ms → 6,82 ms por venda, **+0,58 ms (+9,3%)**, ~72 µs por conferência. Sob os 50 pedidos simultâneos do critério de aceite da spec §5 a diferença some na medição (~22 tps nos dois casos): a venda já serializa na linha do ano de `reservation_code_counters` e na GiST da `EXCLUDE`, e 72 µs não aparecem ao lado disso.

---

## 6. Contatos

```
contacts(id, property_id, name, email, phone_e164, doc_type, doc_number, birth_date,
         city, state, notes, lgpd_basis, marketing_opt_in, consent_at,
         anonymized_at, created_at, updated_at)
```
Uma pessoa, um registro: lead, hóspede, corretor e proprietário apontam para cá. Duas
unicidades sustentam isso, e são eixos diferentes:

- **Telefone** — `contacts_phone_idx`, `UNIQUE(phone_e164) WHERE phone_e164 IS NOT NULL`.
- **Documento** — `contacts_doc_unico_idx`, `UNIQUE(property_id, coalesce(doc_type,''), doc_number) WHERE doc_number IS NOT NULL AND doc_number <> ''`, desde `20260827150000`.

O índice do documento é novo e o motivo dele é o de sempre nesta base. Antes dele o
banco aceitava dois CPFs iguais — medido, dois `INSERT` com `11144477735` responderam
`INSERT 0 1` cada um. Quem segurava era `Repository.TravarDocumento`, um
`pg_advisory_xact_lock` por `(propriedade, tipo, número)` que funciona (seis criações
simultâneas pela API deram 1× 201 e 5× 409) e cujo alcance é o problema: seed,
importação, `psql` ou um segundo serviço criam a duplicata sem encostar na trava, e
duas fichas com o mesmo CPF são o hóspede com dois históricos e a exportação LGPD
devolvendo metade da vida da pessoa. `coalesce(doc_type,'')` porque em índice único
NULO é distinto de NULO, e dois CPFs sem tipo passariam pelo índice feito para
impedi-los. Parcial porque anonimizar zera `doc_number`, e ficha anonimizada não pode
colidir com outra.

Há também `contacts_doc_idx` (não único) em `doc_number` para a busca e
`contacts_name_trgm_idx` em `name`.

**A anonimização ainda não tem função no banco.** Esta página prometia
`anonymize(contact_id)`; nenhuma migration a criou e `\df` não devolve nada com esse
nome — hoje `SELECT anonymize(…)` responde `42883`. O direito de eliminação é F2-24, e
é lá que ela nasce, junto da decisão de o que exatamente sobrevive no razão.

---

## 7. CRM

Entregue em `20260827110000`. Nove tabelas; nenhuma passa de 25 colunas, e a migration **falha** se uma passar — a régua da regra 9 do CLAUDE.md deixou de ser boa intenção e virou um bloco `DO` no fim do arquivo.

```
crm_pipelines(id, property_id, name, is_default, active, created_at, updated_at)
        -- UNIQUE(property_id, name) chave natural do seed
        -- UNIQUE(property_id) WHERE is_default — um funil padrão, e só um
crm_stages(id, pipeline_id, name, position, probability, color, type,      -- aberto|ganho|perdido
           sla_days?, auto_task_subject?, auto_task_type?, auto_task_due_days?, auto_notify,
           active, created_at, updated_at)
        -- UNIQUE(pipeline_id, name) · UNIQUE(pipeline_id, position) DEFERRABLE
crm_leads(id, property_id, contact_id, source, campaign_id?, status, score,
          interest_unit_type_id?, desired_check_in?, desired_check_out?, guests_count?,
          owner_id?, notes?, converted_at?, created_by?, created_at, updated_at)
crm_lost_reasons(id, property_id, label, sort_order, active, created_at, updated_at)
        -- UNIQUE(property_id, label) chave natural do seed
crm_opportunities(id, property_id, contact_id NOT NULL, lead_id?, pipeline_id, stage_id, title,
                  unit_type_id?, check_in?, check_out?, guests_count?, reservation_id?,
                  amount_cents, probability, expected_close?, owner_id?, status,
                  lost_reason_id?, entered_stage_at, closed_at?, created_by?, created_at, updated_at)
                  -- 23 colunas. Sem quote_id: ver a subseção abaixo
                  -- select count(*) from information_schema.columns
                  --  where table_name='crm_opportunities'   → 23
crm_opportunity_event_details(opportunity_id PK, event_type, guests_expected, needs_catering,
                              setup_starts_at, teardown_ends_at, notes, created_at, updated_at)
crm_stage_history(id, opportunity_id, from_stage_id?, to_stage_id, user_id?, reason?, at)  -- insert-only
crm_activities(id, property_id, type, subject, description?, due_at?, done_at?, status, priority,
               lead_id?, opportunity_id?, contact_id?, stage_id?, owner_id?, auto,
               created_by?, created_at, updated_at)
crm_notes(id, property_id, body, pinned, lead_id?, opportunity_id?, contact_id?,
          created_by?, created_at, updated_at)
```

`crm_documents` e `crm_campaigns` ainda não existem — entram com o módulo de documentos e o de campanhas. Por isso `crm_leads.campaign_id` nasce **sem foreign key**, pelo mesmo motivo de `users.broker_id` (§2): FK não aponta para tabela que ainda não foi criada. Até lá o índice parcial já paga o filtro por campanha.

Herdado do portal_amimoveis (o que funciona): `sla_days` + tarefa automática idempotente ao entrar na etapa, `stage_history` insert-only, endpoint agregador `/full`.
Descartado (o que não funciona): a tabela larga de 118 colunas — detalhe de evento vive em satélite.

### Quatro regras de negócio que viraram constraint

| Regra (spec §7) | Como o banco garante |
|---|---|
| "`perdido` exige motivo" | `CHECK (status <> 'perdido' OR lost_reason_id IS NOT NULL)` — e o simétrico, `CHECK (status = 'perdido' OR lost_reason_id IS NULL)`, para motivo não sobreviver a uma reabertura e mentir no relatório |
| Oportunidade fechada tem data de fechamento | `CHECK ((status = 'aberto') = (closed_at IS NULL))` — sem isso "ganhamos em que mês?" não tem resposta |
| "Entrar na etapa cria **uma** tarefa automática, idempotente" | `UNIQUE (opportunity_id, stage_id) WHERE auto AND status = 'pendente'` |
| Tarefa automática pela metade não existe | `CHECK` que amarra `auto_task_subject`, `auto_task_type` e `auto_task_due_days`: os três ou nenhum |

A idempotência da tarefa automática é o caso que mais importa. Feita como `SELECT ... IF NOT EXISTS THEN INSERT`, ela é o mesmo TOCTOU da regra 2 do CLAUDE.md um nível acima: dois cliques no card, duas abas, ou o `POST` que o navegador repetiu, e o corretor abre a manhã com a mesma ligação duas vezes na lista. Com a parcial única, a segunda inserção estoura `23505` e o repositório usa `ON CONFLICT DO NOTHING` — a garantia deixa de depender de quem escreve a automação. O predicado tem `status = 'pendente'` porque a etapa **pode** gerar tarefa nova depois que a anterior foi concluída (o card voltou para "Negociação"); o que não pode é haver duas pendentes ao mesmo tempo.

### `position` é a `UNIQUE` adiável — e aqui a forma pura serve

`UNIQUE (pipeline_id, position) DEFERRABLE INITIALLY DEFERRED`. Ao contrário da invariante da casa inteira (§5), que precisou de `CONSTRAINT TRIGGER` porque `CHECK` não é adiável, aqui a constraint nativa dá conta — e adiar é o que permite **reordenar o kanban**: arrastar a etapa 5 para a posição 2 é um `UPDATE` que reescreve quatro linhas, e com unicidade imediata a primeira reescrita já colidiria com a posição que a segunda ainda não liberou. A alternativa seria o truque feio de mandar todo mundo para posições negativas antes de renumerar.

### O orçamento vigente sai de `quotes.opportunity_id` — não há coluna aqui

**`crm_opportunities` não tem `quote_id`.** Ela existiu de `20260827110000` a
`20260827150000` apontando para `reservations(id)`, porque naquele momento orçamento
era uma reserva em status `quote`. `20260827130000` mudou isso: o orçamento virou
tabela própria (§4a) e a FK não foi repontada. O que sobrou foi pior que uma FK errada
— coluna que ninguém escrevia e ninguém lia. Medido antes de derrubá-la: a
oportunidade ganha pelo `/win` ficava com `quote_id = NULL`, e não havia um único
`SET quote_id` em todo o `apps/api`.

**Quem responde "qual é o orçamento vigente" é o lado do orçamento**, e vigente é o
último emitido:

```sql
SELECT q.id FROM quotes q
 WHERE q.opportunity_id = o.id
 ORDER BY q.created_at DESC, q.id DESC LIMIT 1
```

É o que `internal/modules/crm/repository_oportunidades.go` executa, coberto por
`quotes_oportunidade_idx`. A direção da FK é a decisão, não um detalhe: uma negociação
emite vários orçamentos, e uma coluna escalar no card só guardaria um — emitir o
segundo teria de reescrever a oportunidade, e "quanto se orçou antes de fechar" ficaria
sem resposta. Do lado de `quotes`, cada emissão é linha nova e "vigente" é uma
ordenação, não um `UPDATE`.

O campo `quote_id` do JSON de `GET /crm/opportunities/{id}` **continua existindo** e é
o resultado dessa subconsulta: nome de campo do contrato, não coluna do banco. Duas
armadilhas que esta página já armou uma vez:

- `UPDATE crm_opportunities SET quote_id = …` responde `42703` (coluna inexistente).
- Repor a coluna por migration "para consertar" reintroduz exatamente a coluna morta
  que `20260827150000` removeu.

Se um dia o produto quiser **o orçamento escolhido** — diferente do último emitido —,
a coluna volta apontando para `quotes(id)`, e volta junto com o caminho de aplicação
que a escreve. Coluna sem dono não entra.

### O kanban tem um índice, e ele é parcial

```sql
CREATE INDEX crm_opportunities_kanban_idx
    ON crm_opportunities(pipeline_id, stage_id, owner_id, entered_stage_at)
    WHERE status = 'aberto';
```

A ordem das colunas é a ordem dos filtros: funil sempre, etapa quase sempre, dono quando o escopo é `own` (`AND owner_id = $usuario`), e `entered_stage_at` fecha porque é por ele que o cartão mais parado sobe no topo da coluna. O predicado `status = 'aberto'` existe pela mesma razão do `stay_blocks_ocupacao_idx`: ganho e perdido acumulam para sempre e o quadro nunca os desenha.

As atividades vencidas têm **dois** índices parciais, e de propósito: `(owner_id, due_at) WHERE status = 'pendente'` é a caixa de entrada do corretor (igualdade no dono, faixa no prazo); `(property_id, due_at) WHERE status = 'pendente' AND due_at IS NOT NULL` é a varredura de alertas do servidor, que roda sem dono — com `owner_id` na frente ela teria de varrer o índice inteiro.

---

## 8. Chat

> **Ainda não existe no banco.** Nenhuma das tabelas abaixo foi criada; o modelo é o
> desenho do módulo. O recurso RBAC `chat` já está no catálogo de `resources`, o que
> é normal — a grade de permissões nasce antes das tabelas que ela vai proteger.

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

> **Ainda não existe no banco.** As três tabelas nascem em F2-19; o recurso RBAC
> `agenda` já está no catálogo.

```
agenda_events(id, property_id, type, title, starts_at, ends_at, all_day,
              unit_id?, reservation_id?, contact_id?, assignee_id?, status, notes)
              -- type: checkin|checkout|limpeza|manutencao|visita|evento|bloqueio
agenda_settings(property_id PK, business_hours jsonb, slot_minutes, buffer_minutes)
agenda_blocks(id, property_id, starts_at, ends_at, all_day, reason)
```

---

## 10. Financeiro

> **`brokers` existe desde `20261002180000` (F2-09). As outras nove tabelas desta
> seção ainda não existem no banco** — conferido em `information_schema.tables` em
> 02/10/2026. O desenho delas é ponto de partida da Fase 2, não descrição do que está
> lá: nascem em F2-10, e é o contrato de F2-08 que decide a forma final. Os recursos
> RBAC `finance.receivables`, `finance.payables` e `finance.commissions` já existem em
> `resources` — catálogo de permissão, não tabela de negócio —, e qualquer `SELECT`
> nas tabelas ainda não criadas responde `42P01`.

### `brokers` — o cadastro comercial do corretor (existe)

```
brokers(id, property_id, contact_id, user_id?, goal_cents, active,
        created_by?, created_at, updated_at)                           -- 9 colunas
```

| Coluna | Regra |
|---|---|
| `contact_id` | `NOT NULL` → `contacts(id) ON DELETE RESTRICT` · `UNIQUE` (`brokers_contact_id_key`) |
| `user_id` | anulável → `users(id) ON DELETE RESTRICT` · `UNIQUE` (`brokers_user_id_key`) — único **quando presente**: `NULL` não colide com `NULL` |
| `goal_cents` | `bigint NOT NULL DEFAULT 0` · `CHECK (goal_cents >= 0)` (`brokers_goal_cents_check`) — meta **mensal** (spec §11); `0` é "sem meta definida" |
| `active` | `NOT NULL DEFAULT true` — o soft delete desta tabela |
| `(id, user_id)` | `UNIQUE` (`brokers_id_user_id_key`) — não restringe nada (`id` já é PK); existe porque é o alvo da FK composta `users_broker_id_fkey` (§2) |

Índices: `brokers_property_idx (property_id)`, `brokers_criador_idx (created_by) WHERE created_by IS NOT NULL`; `contact_id` e `user_id` são servidos pelos índices das `UNIQUE`. Quem aponta para cá: `reservations.broker_id` (§5) e `users.broker_id` (§2), as duas com `RESTRICT`.

As decisões, com o porquê:

- **A pessoa vive em `contacts`** ("uma pessoa, um registro", spec §6). Nome, telefone e documento ficam lá — e por isso a anonimização da LGPD alcança o corretor sem tocar a comissão. `UNIQUE (contact_id)`: dois cadastros para a mesma pessoa dividiriam vendas, metas e comissões entre dois ids, e o "desempenho por corretor" (spec §15) contaria uma pessoa como duas.
- **A conta de login é opcional.** O corretor parceiro pode existir sem acesso ao painel; quando tem conta, ela é no máximo uma, e o vínculo é conferido dos dois lados (§2).
- **Corretor com venda não se apaga.** `RESTRICT` nas FKs que apontam para cá; `active = false` é como ele sai de cena. Não há `deleted_at` ao lado — diria a mesma coisa de outro jeito.
- **Autor e instantes no padrão do CRM e de `quotes`**: `created_by`, `created_at`, `updated_at`. Quem mudou a meta fica em `audit_log`, com antes e depois — `updated_by` guardaria só o último.
- **`commission_rule_id` não existe ainda.** `commission_rules` só nasce no F2-10, e a convenção do projeto é a FK nascer junto com a tabela que ela referencia — a mesma que deixou `users.broker_id` e `crm_leads.campaign_id` sem FK até o alvo existir. Criar a coluna antes seria criar outro `uuid` que aceita qualquer coisa, exatamente o que o F2-09 fechou.
- **Sem gatilho**: nenhuma tabela vizinha mantém `updated_at` por trigger (quem escreve o atualiza), e o barramento de tempo real (§13a) não desenha corretor.

**Pendência fora do schema**: `DELETE /contacts/{id}` conta os vínculos do contato antes de apagar (`contatos.Vinculos`) e ainda não conta `brokers`. Apagar a ficha de um corretor estoura `23503` em `brokers_contact_id_fkey`, que sai como `422` genérico em vez do `409 RESOURCE_IN_USE` com a contagem — handoff do F2-09 para o `backend-go`.

### O que ainda não existe (F2-10)

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
expense_categories
-- e, em brokers (acima), a coluna commission_rule_id? → commission_rules(id), que nasce com ela
```

Comissão incide sobre diárias, nunca sobre limpeza ou caução. Caução é `receivables.kind='security_deposit'` com `refundable=true`; o check-out gera `payables.kind='deposit_refund'`, integral ou parcial com laudo anexado.

---

## 11. Operação — inventário de bens por ambiente e ordens de manutenção

Entregue em `20261007170000`; o custo congelado na linha da conferência veio em
`20261007200000`, a identidade estável do cômodo (`unit_rooms.code`) em
`20261007213000`, e as ordens de manutenção (`maintenance_orders`, subseção própria abaixo)
em `20261009100000`. **Nove tabelas existem**; duas continuam projeto (no fim da seção).

Esta seção projetava, desde 20/08/2026, duas tabelas que nunca foram criadas —
`inventory_items(… min_stock, cost_cents)` e `unit_inventory(unit_id, item_id,
standard_qty)` — e faltavam nelas as duas dimensões de que o módulo precisava: o
**ambiente** (o enxoval não está "no AP-01", está na cozinha do AP-01: quem confere
caminha cômodo a cômodo) e a **foto** (quem recebe a lista reconhece a taça, não lê
"taça de vinho tinto nº 2"). Por isso `unit_inventory` virou `room_inventory`, pendurada
no cômodo, e o cômodo é `unit_rooms` — conceito que não existia em lugar nenhum deste
banco.

```
unit_rooms(id, property_id, unit_id → units, name, kind, sort_order, active,
           created_at, updated_at, code)                                  -- 10 colunas
        -- kind: quarto | banheiro | cozinha | sala | area_externa | lavanderia | varanda | outro
        -- UNIQUE (unit_id, name); UNIQUE (unit_id, code); unit_id ON DELETE RESTRICT
        -- UNIQUE (id, unit_id): só alvo da FK composta de maintenance_orders (20261009100000)
        -- code: ^[a-z0-9]+(-[a-z0-9]+)*$, até 60, não editável pela API
inventory_items(id, property_id, name, description?, category, unit_measure,
                replacement_cost_cents?, active, source_ref?, created_at, updated_at)
                                                                          -- 11 colunas
        -- category: louca | talher | copo | cama | banho | mobilia | eletro | utensilio
        --           | decoracao | outro
        -- unit_measure: un (padrão) | par | jogo | kg | l | m
room_inventory(room_id → unit_rooms, item_id → inventory_items, expected_qty, note?,
               created_at, updated_at)                      -- PK composta, 6 colunas
inventory_media(id, property_id, mime, bytes, width?, height?, original_name,
                storage_key UNIQUE, thumb_key?, created_at, created_by?)  -- 11 colunas
inventory_item_media(item_id, media_id, sort_order, created_at)
                                                             -- PK composta, 4 colunas
inventory_counts(id, property_id, unit_id, status, note?, opened_by?, opened_at,
                 closed_by?, closed_at?, updated_at)                      -- 10 colunas
        -- status: aberta | fechada | cancelada
inventory_count_lines(id, count_id, room_id, item_id, expected_qty, counted_qty?,
                      note?, counted_by?, counted_at?, created_at, updated_at,
                      replacement_cost_cents?)                            -- 12 colunas
        -- UNIQUE (count_id, room_id, item_id)
inventory_issues(id, property_id, room_id, item_id, kind, qty, note?, reservation_id?,
                 count_id?, resolution?, reported_by?, reported_at, resolved_by?,
                 resolved_at?, updated_at)                                -- 15 colunas
        -- kind: quebrado | faltando | avariado | outro
        -- resolution: reposto | consertado | cobrado | perda_aceita | descartado
        -- UNIQUE (id, room_id, item_id): só alvo da FK composta de maintenance_orders
maintenance_orders(id, property_id, unit_id, room_id?, item_id?, issue_id?, title,
                   description?, priority, status, stay_block_id?, cost_cents?,
                   opened_at, opened_by?, started_at?, closed_at?, closed_by?,
                   updated_at)                                            -- 18 colunas
        -- subseção "Ordens de manutenção", abaixo
```

**O item é CATÁLOGO da propriedade; a quantidade é COLOCAÇÃO por ambiente.** A
alternativa óbvia — uma linha por `(cômodo, bem)`, com nome, foto e custo na própria
linha — foi medida contra a casa real e custa isto: "Prato raso branco" está em 6 das 12
unidades, e nessa forma são 6 linhas independentes do mesmo objeto. Corrigir nome ou
custo é um `UPDATE` × 6, e a primeira esquecida transforma o prato em dois bens
diferentes que nenhum relatório soma — "quantos pratos a casa tem" deixa de ter resposta
conferível. A foto é pior: ela pertence ao **objeto**, não à prateleira, e linha por
cômodo pede o mesmo arquivo enviado 6 vezes. E a avaria ("3 quebrados") precisa apontar
para o objeto para ser cobrável do hóspede; apontando para a linha-do-cômodo, o prejuízo
da casa é uma soma sobre linhas que ninguém audita. É a mesma separação de `unit_types` ×
`unit_type_members` (§3): o cadastro num lugar, a composição noutro.

- **`min_stock` não entrou** — estoque mínimo é reposição, e reposição é
  `stock_movements`, que continua projeto. E `cost_cents` virou
  **`replacement_cost_cents`**: o número que a operação usa é o de **repor** o prato
  quebrado hoje, não o de comprá-lo em 2019. `NULL` = não cotado; o `CHECK` é `> 0` e
  não `>= 0`, para zero não virar um segundo jeito (errado) de escrever "não sei".
- **`source_ref`** guarda a origem da importação no formato `fonte:conversa:mensagem`
  (`chatwoot:2184:367988`) e é **único quando preenchido, por propriedade**
  (`inventory_items_source_ref_idx`, índice único **parcial**). É o que torna a
  importação repetível: a segunda passada reencontra a própria linha com
  `ON CONFLICT (property_id, source_ref) WHERE source_ref IS NOT NULL` em vez de criar
  um segundo "Prato raso branco". Escopado por propriedade pelo mesmo motivo de
  `units(property_id, code)` — a chave global impediria uma segunda propriedade de
  importar a mesma mensagem de origem.
- **Mídia própria, e não `site_media`.** `site_media` (§13b) parece servir e não serve,
  por dois motivos de schema: ela **não tem `property_id`** (é configuração da vitrine,
  exceção consciente à regra) e o **`down` de `20261003120000` a derruba** — pendurar a
  foto do inventário lá deixaria aquele `down` quebrado ou, pior, apagando bens em
  cascata. `inventory_media` é tabela de negócio: tem `property_id` e cai só com
  `20261007170000`. Imutável como a outra (trocar a foto é enviar arquivo novo), sem
  `CHECK` de lista em `mime` nem teto de tamanho — limite é regra da API, não migration.
  `thumb_key` é a miniatura que o upload gera: anulável (a geração pode falhar sem
  invalidar o original), **única quando presente** e com `CHECK` de que difere de
  `storage_key`, porque as duas moram no mesmo namespace do volume e a colisão
  sobrescreveria a foto.
- **Item × foto é muitos-para-muitos** (`inventory_item_media`), não uma coluna
  `media_id` no item: a foto da bancada mostra a travessa, a leiteira e o açucareiro de
  uma vez, e o mesmo item quer a foto de catálogo **e** a do defeito. As duas pontas são
  `ON DELETE CASCADE` porque a ligação não tem vida própria. A **capa** do item é o
  menor `sort_order`, desempatado por `media_id`.
- **No máximo UMA conferência aberta por unidade**, e é índice único **parcial**
  (`inventory_counts_aberta_idx ON (unit_id) WHERE status = 'aberta'`). Sem ela, dois
  funcionários abrem a contagem do AP-01 no mesmo plantão, cada um conta metade e o
  fechamento de um sobrescreve o do outro — conferir com `SELECT` antes de `INSERT` é
  TOCTOU e não vale sob concorrência. Parcial porque a unidade acumula conferências
  **fechadas** para sempre, e é por isso que uma `UNIQUE` comum não serviria.
  `opened_at` é o `created_at` desta tabela; duas colunas para o mesmo instante seriam
  duas fontes da mesma verdade. `inventory_counts_fechamento` é o
  `CHECK ((status = 'aberta') = (closed_at IS NULL))`, mesma forma de
  `crm_opportunities`.
- **A linha da conferência congela as duas metades da conta** (regra 7 do CLAUDE.md):
  `expected_qty` na **abertura**, copiada de `room_inventory.expected_qty`, e
  `replacement_cost_cents` no **fechamento** (`20261007200000`), copiada de
  `inventory_items.replacement_cost_cents` no mesmo `UPDATE` que fecha a conferência.
  Mesmo motivo de `reservation_nights` guardar a tarifa aplicada: lida por `JOIN`, mudar
  o padrão da casa hoje reescreveria o que a contagem de março esperava, e a divergência
  que foi apurada e cobrada deixaria de existir no relatório. Medido: com a linha
  congelada em 12 e `room_inventory` alterado para 8 depois, a linha continua dizendo 12.
  O custo segue a mesma lógica. A perda de cada divergência é `falta × replacement_cost_cents`
  **da linha**, e o contrato exige que `GET /inventory/counts/{id}` de uma conferência
  `fechada` a devolva lida de volta, não recalculada: recotar o prato em junho não muda a
  perda apurada em março. Medido: custo congelado em 2590, item recotado para 3990, e a
  perda de uma falta de 3 continua 7770 (recalculada seria 11970). O custo fica `NULL` em
  conferência aberta, em cancelada e quando o bem não tinha custo cotado no instante do
  fechamento. Zero é recusado por `inventory_count_lines_custo_positivo`, como em
  `inventory_items`, porque entraria na conta como perda de R$ 0,00. Duas garantias são
  da API, e não do banco: "`NULL` em aberta e em cancelada" e "não muda depois de
  fechada". `CHECK` não enxerga o status do pai, e o mesmo vale para `expected_qty`. O
  backfill da migration deu às conferências já fechadas o custo **atual** do item, uma
  aproximação aceitável só porque `20261007170000` nunca chegou a produção.
  **`counted_qty` `NULL` é "não contado"; zero é "contei e não achei nenhum"** — são
  coisas diferentes, e é a diferença que a tela de pendências pergunta
  (`inventory_count_lines_pendentes_idx`, parcial em `counted_qty IS NULL`).
  `inventory_count_lines_contagem` amarra `counted_qty` e `counted_at`: um sem o outro é
  estado impossível.
- **Pendência de avaria é `resolution IS NULL`.** Não há coluna `status` ao lado de
  propósito — ela seria uma segunda fonte da mesma verdade, e o par
  "`status='aberta'` com `resolution` preenchida" é um estado impossível que alguém
  acabaria gravando. `inventory_issues_desfecho` amarra `resolution` e `resolved_at`.
  `reservation_id` é anulável (desgaste sem hóspede, achado em conferência de rotina) e
  **`ON DELETE RESTRICT`**: é este vínculo que permite cobrar o hóspede, e perdê-lo em
  silêncio apagaria o lastro da cobrança.
- **Soft delete é `active`** em `unit_rooms` e `inventory_items`, não `deleted_at`: as
  FKs de conferência e avaria são `RESTRICT`, então cômodo ou item com histórico não
  some — sai de linha desativado. É a decisão de `brokers.active` (§10).
- **`room_inventory`, `inventory_item_media` e `inventory_count_lines` não têm
  `property_id`**: penduram nos pais, como `rates` pendura em `rate_tables` (§4), e
  repetir a coluna seria uma segunda fonte da mesma verdade. A consequência, dita para
  ninguém achar que a FK a fechou: numa segunda propriedade, nada no banco impede colocar
  um item da propriedade A num cômodo da propriedade B — a conferência é da API, que
  filtra tudo por `property_id`. Fechar exigiria `UNIQUE (property_id, id)` nos pais e FK
  composta (o padrão de `users.broker_id`, §2); lá havia exploit medido, aqui não há, e
  não se compra constraint no escuro.
- A migration termina com o mesmo bloco `DO` de `20260827110000`: **aborta** se qualquer
  tabela do módulo passar de 25 colunas. A maior tem 15. `20261007200000` repete o bloco
  para `inventory_count_lines`, que foi de 11 para 12, e `20261007213000` para
  `unit_rooms`, que foi de 9 para 10.

**`unit_rooms.code` é a identidade do cômodo; `name` é o rótulo** (`20261007213000`). É o
`units.code` do cômodo: `NOT NULL`, minúsculo, sem acento, separado por hífen
(`unit_rooms_code_formato`, o mesmo `pattern` de `Ambiente.code` no contrato), até 60
caracteres (`unit_rooms_code_tamanho`) e único **na unidade** (`unit_rooms_code_unico`). Na
unidade, e não na propriedade, de propósito: `cozinha` existe em cada um dos 6 duplex, e é
por esse código que a cópia de inventário casa a cozinha do AP-01 com a do AP-02, mesmo
depois de alguém renomear uma das duas. `name` continua editável e único na unidade
(`unit_rooms_nome_unico`). Duas garantias são da API, e não do banco. A primeira: o `code`
**não muda depois de criado**. `PUT` e `PATCH` não aceitam o campo, mas nada impede um
`UPDATE` pelo psql, e fechar isso exigiria gatilho para um escritor só (a decisão de
`expected_qty`). A segunda: a **derivação**, quando o `POST` não traz o código. Não há
`DEFAULT` nem gatilho que derive, porque `DEFAULT` não lê outra coluna e regra mantida em
dois lugares acaba divergindo. A migration aplicou a regra uma vez, às linhas que já
existiam, e a API a repete. As duas implementações têm de dar o mesmo resultado, caractere
a caractere:

1. Remover os code points U+0300–U+036F (acento combinante, de nome que chega decomposto
   em NFD, como o macOS às vezes entrega). Sem isso, "Área" viraria `a-rea`.
2. Trocar caractere por caractere pela tabela, e nada além dela: `ÀÁÂÃÄàáâãä` → `a`,
   `ÈÉÊËèéêë` → `e`, `ÌÍÎÏìíîï` → `i`, `ÒÓÔÕÖòóôõö` → `o`, `ÙÚÛÜùúûü` → `u`, `Çç` → `c`,
   `Ññ` → `n`, `A–Z` → `a–z`. Sem `lower()` nem minúscula Unicode: o do Postgres depende do
   locale, o `strings.ToLower` do Go segue a tabela Unicode, e os dois discordam fora do
   ASCII. Letra fora da tabela (`ø`, `ß`, `º`) segue para o passo 3 como qualquer símbolo.
3. Cada sequência **máxima** de caracteres fora de `[a-z0-9]` vira **um** hífen.
4. Tirar os hífens das duas pontas.
5. Passando de 60 caracteres, ficar com os 60 primeiros e tirar o hífen do fim, se
   sobrar. A string já é ASCII aqui, então caractere = byte.
6. Vazio (nome só de emoji ou de pontuação) vira `comodo`.
7. O código é o primeiro **livre na unidade**, contra todos os cômodos dela, ativos ou
   não, na sequência `base`, `base-2`, `base-3`… No candidato com sufixo `-n`, a base é
   antes cortada em `60 − len("-n")` caracteres e perde o hífen do fim, se sobrar.

"Área da churrasqueira" → `area-da-churrasqueira`; "Suíte 1 (térreo)" → `suite-1-terreo`;
"Sala" e depois "SALA!" na mesma unidade → `sala` e `sala-2`; "Quarto", "Quarto!" e depois
"Quarto 2" → `quarto`, `quarto-2` e `quarto-2-2`; "🛏️" → `comodo`. O backfill processou
cada unidade em ordem de `(created_at, id)`, cada cômodo enxergando os códigos dados aos
anteriores. Assim o mais antigo ficou com o código sem sufixo, como se a API o tivesse
derivado no primeiro `POST`. A derivação concorrente é da API: dois `POST` simultâneos
que derivam o mesmo código esbarram em `unit_rooms_code_unico` (`23505`), e só o código
**informado** pelo cliente é `409 CODE_IN_USE`. O derivado tenta o próximo sufixo. O
`down` é **lossy**: os códigos somem, e o `up` seguinte os deriva do `name` **atual**.
Cômodo renomeado ganha código novo (medido: "Quarto" renomeado para "Suíte Master" volta
como `suite-master`, não `quarto`). Hoje isso é inofensivo, mas deixa de ser no dia em que
uma importação real tiver rodado.

**Permissão: dois recursos, e por quê.** O recurso RBAC `inventory` **já estava** no
catálogo do seed desde `20260820140000`, e a migration não o toca porque `resources` é
semeado, não migrado (§16). Só que ele **já estava ocupado**:
`internal/modules/inventario` (`dto.go`, `const Recurso = "inventory"`) o usa para o CRUD
de **propriedade, produtos, unidades e composição** — todas as linhas de
`internal/router/rotas_inventario.go`. Pendurar as oito tabelas deste módulo no mesmo
recurso faria `inventory:editar` significar duas coisas: a permissão que deixa quem limpa
salvar "contei 9 taças" no celular deixaria apagar um produto que a casa vende, e o
inverso — um toque na tela de distância. Decidido em 07/10/2026, no `catalogoSeed` de
`cmd/seed/acesso.go` (nenhuma mudança de schema: `resources.code` é texto livre):

- **`inventory`** — rótulo corrigido para **"Cadastro de unidades e produtos"**. Dizia
  "Inventário e enxoval" e mentia: não há uma peça de enxoval nas rotas que ele protege.
  Grupo "Operação", as quatro ações, `supports_own = false`, `sort_order` 13.
- **`inventory.goods`** ("Bens e enxoval por ambiente", grupo "Operação", as quatro ações,
  `supports_own = false` — cômodo não tem dono no sentido do RBAC, `sort_order` 14, e
  `channels` foi para 15; desde 09/10/2026 está em 16, porque `maintenance` entrou em 15)
  é o recurso das oito tabelas deste módulo. Namespace com ponto
  como `crm.*` e `finance.*`. Concedido em `all` ao `admin` (catálogo inteiro) e ao perfil
  de operação `usuario`; **não** ao `corretor` nem à conta de serviço `vitrine` — nenhum
  dos dois tem o que fazer com bens.

O espelho do painel (`apps/admin/src/lib/auth/recursos.ts`) carrega a mesma linha, e isso
não é opcional: `permissions.test.ts` lê o `catalogoSeed` do Go e reprova enquanto as duas
listas divergirem — recurso que o painel não sabe citar é tela que some para todo mundo,
inclusive o admin, sem nenhum 403 para denunciar. Recurso novo entra no seed primeiro.

**Seed de dados de inventário não existe, e o bloqueio do ambiente acabou em
`20261007213000`.** Nenhum ambiente, item ou foto é semeado. Até aquela migration, no caso
do ambiente, isso era decisão e não pendência. A única chave natural de `unit_rooms` era
`(unit_id, name)`, e `name` é justamente o campo que o gestor edita na tela. Seed ou
importação chaveados num rótulo editável não são idempotentes contra instalação viva:
renomear "Quarto grande" para "Suíte Master" no painel e reimportar recriaria o "Quarto
grande" como cômodo a mais, fantasma na lista de conferência e na contagem. O
pré-requisito que este parágrafo pedia agora existe, e é **`unit_rooms.code`**, que não se
edita, como `units.code` ("AP-01"). A importação do levantamento fotográfico (GV-01 e
CV-01, 36 ambientes e 759 itens) e qualquer seed de ambiente que vier reencontram o cômodo
por **`(unit_id, code)`**, com `ON CONFLICT (unit_id, code)` sobre `unit_rooms_code_unico`.
O item continua chaveado por `(property_id, source_ref)`. A cópia de inventário entre
unidades casa origem e destino pelo mesmo `code`.

O `code` resolve o rename. Não resolve tudo, e duas cautelas continuam valendo:

- **A importação declara o `code`; não o deriva do nome.** O ambiente tem dois escritores
  por desenho, a tela e a importação, e duas fontes descrevendo o mesmo cômodo escrevem
  strings diferentes ("Rooftop" na tela, "Área da churrasqueira (rooftop)" no
  levantamento). Derivados, viram `rooftop` e `area-da-churrasqueira-rooftop`, e a `UNIQUE`
  pega duplicata exata, não quase-duplicata: a casa ficaria com o mesmo cômodo duas vezes
  e `room_inventory` partido entre as duas metades. A fonte da importação carrega o próprio
  código, estável entre passadas, e cada cômodo entra por uma fonte só.
- **A reimportação não reescreve o que a tela edita.** Depois que o cômodo existe,
  `name`, `kind`, `sort_order` e `active` são do gestor. Um `DO UPDATE SET name =
  EXCLUDED.name` desfaria o rename a cada passada, que é o defeito que o `code` veio
  fechar, só que pelo outro lado. É a regra 3 do seed (§16): corrige divergência de chave,
  não apaga decisão da gestão. E como `unit_rooms_nome_unico` continua de pé, cômodo
  importado com `code` novo e `name` já usado na unidade falha com `23505` nessa
  constraint. Esse é um conflito real, a mostrar, e não a resolver em silêncio.

### Ordens de manutenção (`maintenance_orders`)

Entregue em `20261009100000`. Contrato em `/maintenance-orders` (tag Manutenção), regras
puras em `internal/domain/maintenance`, resumo em spec §12. Esta seção projetava, desde
20/08/2026, `maintenance_orders(id, unit_id, title, description, priority, status,
stay_block_id?, opened_at, closed_at, cost_cents)`. Faltavam nela o **cômodo** e o **bem** (o
ar-condicionado é da suíte 1 do AP-03, não "do AP-03"), a **avaria** que originou a ordem (é
ela que a conclusão encerra com `consertado`), `property_id` e quem abriu, começou e fechou.

```
maintenance_orders(
  id            uuid PK DEFAULT gen_random_uuid(),
  property_id   uuid NOT NULL → properties,
  unit_id       uuid NOT NULL → units ON DELETE RESTRICT,
  room_id       uuid?          -- FK composta (room_id, unit_id) → unit_rooms(id, unit_id)
  item_id       uuid? → inventory_items ON DELETE RESTRICT,
  issue_id      uuid?          -- FK composta (issue_id, room_id, item_id) → inventory_issues
  title         text NOT NULL  -- btrim <> '' · char_length <= 200
  description   text?          -- char_length <= 4000
  priority      text NOT NULL DEFAULT 'normal'  -- baixa | normal | alta | urgente
  status        text NOT NULL DEFAULT 'aberta'  -- aberta | em_andamento | concluida | cancelada
  stay_block_id uuid? → stay_blocks ON DELETE RESTRICT,
  cost_cents    bigint?        -- > 0
  opened_at     timestamptz NOT NULL DEFAULT now(),   -- é o created_at desta tabela
  opened_by     uuid? → users,
  started_at    timestamptz?,
  closed_at     timestamptz?,
  closed_by     uuid? → users,
  updated_at    timestamptz NOT NULL DEFAULT now())   -- 18 colunas
```

Os vocabulários de `priority` e `status` são, palavra por palavra, as constantes de
`internal/domain/maintenance` (`Priority`, `Status`). `opened_at` é o `created_at` da tabela,
como em `inventory_counts`. Não há gatilho de `updated_at` (nenhuma tabela deste banco tem um):
quem escreve o grava.

**O que o banco garante**, porque escrita concorrente e `psql` não passam pela API:

- **O cômodo é da unidade da ordem.** `maintenance_orders_room_id_fkey` é a FK composta
  `(room_id, unit_id) → unit_rooms(id, unit_id)`. O alvo é uma `UNIQUE (id, unit_id)` nova em
  `unit_rooms` (`unit_rooms_id_unit_id_key`), que não restringe nada além do que a PK já
  restringia: existe para ser alvo, como `brokers_id_user_id_key` (§2). Com `MATCH SIMPLE`,
  cômodo nulo não é conferido, e está certo. Como `unit_id` é `NOT NULL`, cômodo preenchido é
  sempre conferido. A mesma FK recusa, com `23503`, mover a ordem para outra unidade deixando o
  cômodo para trás, mudar a unidade de um cômodo citado e apagar um cômodo citado.
- **A avaria traz o cômodo e o bem dela.** São duas peças. A primeira é
  `maintenance_orders_avaria_exige_comodo_e_bem`, o `CHECK (issue_id IS NULL OR (room_id IS
  NOT NULL AND item_id IS NOT NULL))`. A segunda é `maintenance_orders_issue_id_fkey`, a FK
  composta `(issue_id, room_id, item_id) → inventory_issues(id, room_id, item_id) ON DELETE
  RESTRICT`, cujo alvo é a `UNIQUE` nova `inventory_issues_id_room_id_item_id_key`. O `CHECK`
  não é redundante. Em `MATCH SIMPLE`, basta **uma** coluna nula para a FK composta inteira não
  ser conferida, e sem ele `(issue_id, NULL, NULL)` passaria citando avaria sem conferir nada.
  Somada à FK do cômodo, a avaria fica obrigatoriamente na unidade da ordem: não existe ordem
  do AP-03 citando avaria da GV-01. `RESTRICT` é deliberado. A ordem diz o que foi consertado,
  e por isso `DELETE /inventory/issues/{id}` de avaria citada é `409 RESOURCE_IN_USE` (o `23503`
  sai com `constraint = maintenance_orders_issue_id_fkey`). A mesma FK recusa trocar o cômodo
  ou o bem de uma avaria citada, que a API também não aceita (`AvariaSubstituir`).
- **No máximo uma ordem não encerrada por avaria.** É o índice único parcial
  `maintenance_orders_avaria_aberta_idx ON (issue_id) WHERE issue_id IS NOT NULL AND status IN
  ('aberta','em_andamento')`, e a API traduz o `23505` **neste nome** para `409
  MAINTENANCE_ORDER_ALREADY_OPEN`. É a defesa contra o segundo toque no celular. `SELECT` antes
  do `INSERT` seria TOCTOU, como em `inventory_counts_aberta_idx`. É parcial porque a avaria
  acumula ordens encerradas: a primeira cancelada e a segunda que consertou, ou o retrabalho
  depois de uma concluída. Uma `UNIQUE` comum recusaria o retrabalho. Precedência medida: o
  índice único é conferido na inserção, e as FKs só por gatilho, no fim da instrução. Com
  ordem aberta na avaria, até um par cômodo/bem divergente sai como `23505` neste índice, e não
  como `23503`. A API valida o par antes (`422`), então o caso só aparece na corrida, que é
  justamente a que o índice existe para decidir.
- **Um bloqueio pertence a no máximo uma ordem.** `maintenance_orders_bloqueio_idx`, `UNIQUE
  (stay_block_id) WHERE stay_block_id IS NOT NULL`. Sem ele, duas ordens apontando para a mesma
  linha fariam o encerramento de uma liberar o calendário no meio do serviço da outra.
- **Os estados impossíveis.** São quatro `CHECK`, separados para o nome da constraint dizer à
  API qual regra caiu:
  - `maintenance_orders_fechamento` — `(status IN ('concluida','cancelada')) = (closed_at IS
    NOT NULL)`, a forma de `inventory_counts_fechamento`. `cancelada` também tem `closed_at`: é
    quando se desistiu.
  - `maintenance_orders_inicio` — `status IN ('concluida','cancelada') OR (started_at IS NOT
    NULL) = (status = 'em_andamento')`. `em_andamento` tem `started_at`, e `aberta` não tem,
    porque a máquina não tem "desfazer o início". Encerrada fica livre, porque concluir ou
    cancelar direto de `aberta` é permitido e deixa `started_at` nulo, o que é verdade e não
    lacuna.
  - `maintenance_orders_cronologia` — `started_at IS NULL OR closed_at IS NULL OR started_at <=
    closed_at`. Os instantes são do servidor, e a regra compara dois deles. Por isso os três
    (`opened_at`, `started_at`, `closed_at`) devem sair do mesmo relógio, o `now()` do banco
    que já é o `DEFAULT` de `opened_at`. Um `time.Now()` do Go num deles abriria espaço para
    deriva entre relógios.
  - `maintenance_orders_fechador` — `closed_by IS NULL OR closed_at IS NOT NULL`. `closed_by`
    continua anulável (encerramento por rotina não tem autor), mas autor de um encerramento
    que não aconteceu é estado impossível.
- **Custo e texto.** `maintenance_orders_custo_positivo` (`cost_cents IS NULL OR > 0`): `NULL`
  é "não lançado", e zero não é um segundo jeito de escrever "não sei", como em
  `inventory_items`. `maintenance_orders_title_nao_vazio`, `_title_tamanho` (200) e
  `_description_tamanho` (4000) usam `char_length` e não `length` em bytes, porque o
  `maxLength` do contrato conta caracteres.

**O que fica com a API, e por quê:**

- **A fonte e a unidade do `stay_block` apontado.** A API cria a linha de `stay_blocks`
  (`source = 'maintenance'`, `confirmed`, na unidade da ordem) na mesma transação da ordem.
  Fechar isso no banco exigiria FK composta `(stay_block_id, unit_id) → stay_blocks(id,
  unit_id)` e, portanto, um índice único novo em `stay_blocks`. Seria pago em toda reserva, na
  tabela mais quente do sistema, para defender um escritor só e sem exploit medido.
- **"Encerrada não reabre"** (`maintenance.Next`). `CHECK` não enxerga o valor anterior, e
  gatilho para um escritor só é a decisão que esta seção já recusou para `expected_qty` e
  `unit_rooms.code`. O mesmo vale para "concluída só aceita o custo" (`maintenance.CheckEdit`).
- **"Liberar o bloqueio" é `stay_blocks.status = 'cancelled'`, ou o período cortado em D,
  nunca `DELETE`** (`maintenance.ReleaseOn`). A ordem continua apontando para a linha, que é a
  história de "esteve bloqueada de tal a tal". A FK é `RESTRICT` justamente para que um
  `DELETE` esquecido em algum caminho falhe alto, com `23503` em
  `maintenance_orders_stay_block_id_fkey`, em vez de soltar a ordem do próprio histórico.
  Remarcar um bloqueio que já terminou ou foi liberado cria linha **nova** e a ordem passa a
  apontar para ela. A antiga fica no calendário, sem ordem que a cite.
- **A propriedade.** Como nas outras tabelas desta seção, nada no banco impede uma ordem da
  propriedade A citar unidade da propriedade B. A API filtra tudo por `property_id`.

**Índices: a lista de trabalho não tem índice que a ordene, e não poderia ter.** A ordem
padrão é "abertas primeiro; entre elas, da mais urgente para a menos e, na mesma prioridade, a
mais antiga". A escada de prioridade chega como **parâmetro**
(`array_position($1::text[], priority)`, de `maintenance.ByUrgency()`), e nenhum índice casa
com expressão sobre parâmetro. Uma expressão fixa no índice seria a segunda cópia da escada que
o domínio existe para não ter. O que o índice faz é entregar barato o conjunto que ela ordena:

- `maintenance_orders_abertas_idx ON (property_id, opened_at, id) WHERE status IN
  ('aberta','em_andamento')` — as não encerradas, que são poucas e são o que a tela do celular
  abre (`open=true`, `status=aberta|em_andamento`). Elas saem por antiguidade, e a prioridade é
  ordenada em memória sobre dezenas de linhas (plano com `enable_seqscan = off`, já que a
  tabela da prova estava vazia: `Index Scan using maintenance_orders_abertas_idx` + `Sort`
  pela chave da escada). É parcial pelo argumento do
  kanban (§7) e de `inventory_issues_abertas_idx`: as encerradas acumulam para sempre e nunca
  entram na lista de trabalho. A lista de histórico (sem filtro, ou só encerradas) lê a maior
  parte da tabela, e `meta.total` conta cada linha filtrada de qualquer jeito. Ali nenhum
  índice ganha de uma varredura, na escala de uma casa de doze unidades. Filtro só por
  `priority` (quatro valores) também não é seletivo o bastante para pagar índice.
- `maintenance_orders_unidade_idx (unit_id, opened_at DESC, id)` — histórico da unidade, o
  filtro `unit_id` e a FK.
- `maintenance_orders_comodo_idx (room_id, unit_id) WHERE room_id IS NOT NULL` e
  `maintenance_orders_avaria_idx (issue_id, room_id, item_id) WHERE issue_id IS NOT NULL` — as
  FKs compostas, com as colunas na ordem da FK, e os filtros `room_id` e `issue_id`. São
  parciais porque a maioria das ordens não tem cômodo nem avaria. A igualdade da FK implica
  `IS NOT NULL`, então o Postgres usa o índice parcial na conferência (medido com plano
  genérico: `Index Cond: ((room_id = $1) AND (unit_id = $2))`).
  `maintenance_orders_avaria_aberta_idx` não serviria à FK da avaria: só enxerga as não
  encerradas, e a FK precisa achar a ordem **concluída** que cita a avaria.
- `maintenance_orders_bem_idx (item_id, opened_at DESC, id) WHERE item_id IS NOT NULL` — "o
  bem que dá defeito toda temporada" e a FK. `maintenance_orders_bloqueio_idx` cobre a FK do
  bloqueio, e `_autor_idx`/`_fechador_idx` (parciais) cobrem `opened_by` e `closed_by`.
- `property_id` fica coberto só pelo índice **parcial** das abertas, como
  `inventory_issues.property_id`. A consulta de §14 o conta como coberto, porque não lê o
  predicado. Uma conferência de FK ao apagar uma propriedade não usaria esse índice, e
  propriedade não se apaga.

**Sem gatilho de tempo real nesta tabela.** O mapa é avisado pelo gatilho de `stay_blocks`
(§13a) quando o bloqueio da ordem nasce, muda ou é liberado, e uma ordem sem bloqueio não muda
o mapa. A migration termina com o bloco `DO` da régua de 25 colunas (a tabela tem 18).

**Permissão: o recurso `maintenance`** ("Ordens de manutenção", grupo "Operação", as quatro
ações, `supports_own = false`, `sort_order` 15; `channels` foi para 16). É recurso próprio, e
não `inventory.goods` nem `calendar`, porque a ordem **bloqueia a unidade dela** no calendário
sem pedir `calendar:*` (contrato, tag Manutenção). Pendurada nos bens, quem conta taças
bloquearia a casa para venda. Pendurada no calendário, quem bloqueia data lançaria custo de
serviço. Sem dono porque a ordem é da casa: `opened_by` é auditoria, como
`stay_blocks.created_by` (§1), e escopo `own` sobre ele faria a ordem sumir da tela de quem vai
consertá-la. Concedido em `all` ao `admin` (catálogo inteiro) e ao `usuario`, que também tem
`inventory.goods:ver`, de que o formulário precisa para escolher cômodo e bem. **Não** é
concedido ao `corretor`, porque seria um bloqueio de calendário em escopo `all` pela porta dos
fundos do `calendar` em `own`. Também não é concedido à conta de serviço `vitrine`, cuja matriz
o seed mantém com exatamente duas células (§16). `TestManutencaoEhDaOperacao`
(`cmd/seed/acesso_test.go`) reprova qualquer outra concessão.

### O que ainda não existe (operação)

> **Ainda não existe no banco.** Nenhuma das duas foi criada.

```
stock_movements(id, item_id, qty, kind, reservation_id?, unit_id?, at, created_by)
housekeeping_tasks(id, unit_id, reservation_id?, scheduled_for, status, checklist jsonb, assignee_id)
```

---

## 12. Canais / OTA

> **Ainda não existe no banco.** Nenhuma das quatro tabelas foi criada — é por isso
> que `reservations` ainda não tem `channel_id` (§5) e que `stay_blocks.source='ota'`
> e `external_ref` existem sem ninguém que os escreva. O recurso RBAC `channels` já
> está no catálogo.

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
webhooks(id, name, url, events text[], secret, active)                  -- ↑ ainda não existem
webhook_deliveries(id, webhook_id, event, payload jsonb, attempt, status_code,
                   response_body, error, next_retry_at, delivered_at, dead_at)
integration_logs(id, provider, direction, action, status, payload jsonb, error, created_at)
-- ─────────── daqui para baixo, existe no banco ───────────
idempotency_keys(key, endpoint, actor_id, property_id, request_hash, status, response_body, created_at)
        -- PK(key, endpoint, actor_id, property_id) — a chave SEM ator vazava resposta entre usuários
app_settings(namespace, key, value jsonb, is_secret, updated_by, updated_at)      -- PK(namespace, key)
audit_log(id, property_id, actor_id, action, entity, entity_id, before jsonb, after jsonb,
          ip, user_agent, request_id, at)
pii_access_log(id, actor_id, contact_id, reason, at)
```

**As quatro primeiras ainda não existem no banco** (`api_tokens`, `webhooks`,
`webhook_deliveries`, `integration_logs`) — entram com o módulo de integrações. As
quatro de baixo existem e foram conferidas coluna a coluna.

`audit_log` particionado por mês a partir do segundo ano — **ainda não está
particionado**; hoje é tabela simples, e a partição é decisão de operação, não de
schema desta fase.

---

## 13a. O barramento de tempo real (`LISTEN`/`NOTIFY`)

Entregue em `20260827120000`. O mapa de ocupação e o kanban se mexem sozinhos porque o **banco** avisa, em gatilhos `AFTER` sobre `stay_blocks`, `reservations` e `crm_opportunities`. `internal/modules/stream` é quem escuta.

| Canal | Emissores | Entidades |
|---|---|---|
| `whv_calendar` | `stay_blocks`, `reservations` | `stay_block`, `reservation` |
| `whv_crm` | `crm_opportunities` | `opportunity` |

Um canal por assunto porque `LISTEN` é por conexão: a aba que só olha o kanban não precisa acordar a cada check-out.

### O payload é magro, e isso é segurança — não economia

```json
{"topic":"calendar","entity":"stay_block","id":"…","unit_id":"…","property_id":"…","v":8412}
{"topic":"crm","entity":"opportunity","id":"…","pipeline_id":"…","property_id":"…","v":8413}
```

Só identificadores. Nome de hóspede, valor, telefone e motivo de cancelamento **não** trafegam, porque **`NOTIFY` não tem RBAC**: quem der `LISTEN` numa conexão recebe tudo, e o eixo `scope='own'` vive no `WHERE` do repositório. Mandar a linha inteira pelo canal seria construir, ao lado da API, um segundo caminho de leitura sem permissão nenhuma.

O contrato com o cliente é: o evento diz **o que** mudou; o cliente **refaz o fetch** pela API, com o token dele. Se ele não pode ver aquela reserva, o refetch devolve 404 e a tela não muda — que é o comportamento correto.

Efeito colateral bem-vindo: o limite de **8 kB** do `pg_notify` deixa de ser um risco. O payload tem tamanho fixo por construção — **207 bytes medidos**, 2,5% do teto —, então não existe reserva grande nem justificativa longa o bastante para estourá-lo. Um payload que carregasse a linha estouraria justamente no caso extremo, e `NOTIFY` que estoura aborta a **transação de negócio**: a venda falharia por causa do tempo real.

### `v` é o id da transação, não um contador

`v = pg_current_xact_id()`. Duas consequências úteis: todos os eventos de uma mesma mudança atômica compartilham `v` (vender a Completa emite 8 `stay_block` + 1 `reservation` com o **mesmo** `v` — é **uma** venda e **um** refetch, não nove), e `v` menor que o último aplicado é resposta velha, descartável sem refetch. O que `v` **não** é: uma sequência densa. Transação abortada queima um id — um `409 DATE_CONFLICT` queima um —, então **buraco em `v` é normal e não significa evento perdido**. Cliente que tratar buraco como perda vai refazer a tela inteira a cada conflito de data.

### Por que gatilho, e não `NOTIFY` no Go

Porque `NOTIFY` é transacional: só é entregue se a transação **comitar**. Emitindo do Go depois do commit haveria dois modos de falha reais — o processo morrer entre o commit e o notify (a venda aconteceu e o mapa não soube), e o notify sair de uma transação que depois faz rollback (o mapa mostra reserva que não existe). No gatilho, os dois são impossíveis. E o `UPDATE` em massa do job de expiração passa a notificar sem que o job saiba que o tempo real existe.

Cada gatilho de `UPDATE` tem `WHEN` com as colunas que a tela realmente desenha — um `UPDATE` que só acerta a `note` de um bloco **não** acorda ninguém. `updated_at` fica de fora de propósito: ele muda em toda escrita, e incluí-lo tornaria o `WHEN` sempre verdadeiro, ou seja, nenhum filtro.

**Custo medido**: 9 notificações por venda da Completa. Com o envelope numa função PL/pgSQL compartilhada eram **+1,98 ms por venda** (~220 µs por chamada aninhada); montando o mesmo JSON dentro da própria função de gatilho, **+0,3 ms**. Mesmo payload, byte a byte, seis vezes mais barato — e o preço é a forma do envelope aparecer em três funções em vez de uma, o que está escrito no cabeçalho da migration.

---

## 13b. Conteúdo do site (`site_content`, `site_media`)

Entregue em `20261003120000`. É o banco do menu **Site** do painel — contrato em
`docs/site-cms.md`. O site de vendas mantém o texto original no HTML; o banco guarda
só o que a gestão **editou**.

```
site_content(key PK, value jsonb, updated_at, updated_by? → users)        -- 4 colunas
site_media(id PK, kind, mime, bytes, original_name, storage_key UNIQUE,
           created_at, created_by? → users)                               -- 8 colunas
```

- **`site_content`**: uma linha por campo editado. Campo sem linha = texto original;
  "Restaurar original" é `DELETE` da linha. Salvar = publicar (sem rascunho); a trilha
  de quem mudou o quê fica em `audit_log`, não em histórico na tabela — por isso não há
  soft delete. A chave é a do **catálogo de campos em código**
  (`internal/modules/site/catalogo.go`); o banco só confere a forma
  (`site_content_key_formato`: `^[a-z0-9]+([.-][a-z0-9]+)*$`, ex. `inicio.titulo`,
  `categoria.grand-villa.foto`) e que o valor não é JSON `null`
  (`site_content_value_nao_nulo`) — restaurar é apagar, não gravar nulo. A forma do
  valor por tipo (string, `{media_id, alt}`, `{media_id}`, lista) é validada pela API
  contra o catálogo; `media_id` dentro do JSON **não** é FK (o Postgres não tem FK em
  jsonb), e mídia inexistente é `422` da API.
- **`site_media`**: arquivo enviado pelo painel, guardado no volume da API
  (`MEDIA_DIR=/data/midia`). **Imutável**: trocar a foto é enviar arquivo novo e apontar o
  campo para o novo id — é o que permite `Cache-Control: immutable` na URL pública.
  `kind` em `CHECK (imagem|video)`; `bytes > 0`; `mime` e `storage_key` não vazios;
  `storage_key` único (`site_media_storage_key_unica`) e nunca derivado do nome enviado.
  Os limites de tamanho (15 MB / 300 MB) e o tipo pelos bytes são regra da API, não
  `CHECK`: mudar limite não deve exigir migration.
- **Sem `property_id`**, exceção consciente à regra 7: o site é um só (não é
  multi-tenant) e as duas tabelas são configuração da vitrine, como `app_settings`, não
  registro de negócio.
- `updated_by`/`created_by` são `NULL`áveis (gravação por script ou seed) e têm índice
  próprio (`site_content_updated_by_idx`, `site_media_created_by_idx`).
- O `down` derruba as duas tabelas; os arquivos do volume não são tocados — o banco não
  é dono deles.

**Permissão**: recurso RBAC `site` ("Site (textos, fotos e vídeos)", grupo "Site"),
ações **`ver`** e **`editar`** só — não se cria nem se apaga campo, o catálogo é fixo —,
`supports_own = false` (não há dono: o site é um só). Registrado como os demais recursos,
pelo `catalogoSeed` de `cmd/seed/acesso.go` (§16). Concedido em `all` ao `admin` (catálogo
inteiro) e ao perfil de gestão `usuario`; **não** ao `corretor` nem à conta de serviço
`vitrine`.

---

## 14. Índices e constraints que não podem faltar

| Tabela | Constraint / índice |
|---|---|
| `stay_blocks` | `EXCLUDE USING gist (unit_id WITH =, period WITH &&) WHERE status IN ('hold','confirmed')` — `completed` fica de fora de propósito (§1) |
| `stay_blocks` | `CHECK (lower(period) < upper(period))` · `CHECK (status<>'hold' OR expires_at IS NOT NULL)` · gist em `period` · parcial em `expires_at` |
| `stay_blocks` | gist parcial em `period WHERE status IN ('hold','confirmed','completed')` — o mapa e a ocupação · parcial em `owner_id` — o escopo `own` de `calendar` |
| `reservations` | `UNIQUE(code)` · `CHECK (check_out > check_in)` · `CHECK (guests_count > 0)` — o teto do desconto **não** está aqui: `discount_pct` mora em `reservation_pricing` desde `20260826100000` |
| `reservation_nights` | `PRIMARY KEY (reservation_id, night)` |
| `rates` | `UNIQUE(rate_table_id, unit_type_id, date_type)` |
| `holidays` | `UNIQUE(property_id, date)` |
| `special_periods` | `CHECK (ends_on >= starts_on)` + gist em `daterange(starts_on, ends_on, '[]')` |
| `contacts` | `UNIQUE(phone_e164) WHERE phone_e164 IS NOT NULL` · `UNIQUE(property_id, coalesce(doc_type,''), doc_number) WHERE doc_number IS NOT NULL AND doc_number <> ''` — documento único por propriedade, e é dele que sai o `409 CONTACT_DUPLICATE` (§6) · trigram em `name` · `(doc_number) WHERE doc_number IS NOT NULL` para a busca |
| `chat_messages` | `UNIQUE(conversation_id, external_id)` — **tabela ainda não existe** (§8) |
| `chat_conversations` | `UNIQUE(integration_id, external_id)` — **tabela ainda não existe** (§8) |
| `channel_events` | `UNIQUE(channel_id, external_uid)` — **tabela ainda não existe** (§12) |
| `idempotency_keys` | `PRIMARY KEY (key, endpoint, actor_id, property_id)` — sem o ator na chave, o replay devolve a resposta de um usuário a outro |
| `reservations` | `code` com `DEFAULT proximo_codigo_reserva()` — numeração por ano, densa e sem corrida |
| `reservations` | `(property_id, status, check_in)` — a listagem · `(unit_type_id, check_in)` — ocupação por produto |
| `reservations` | parciais em `owner_id`, `broker_id`, `rebooked_from_id`, `created_by` — FKs majoritariamente nulas |
| `reservations` | `reservations_broker_id_fkey`: `broker_id → brokers(id) ON DELETE RESTRICT` — corretor inexistente é `23503`, que a API traduz para `422` em `broker_id` (§5) |
| `reservation_pricing` | `PRIMARY KEY (reservation_id)` **é** a FK — é isto que faz o 1:1 · `CHECK (discount_cents <= subtotal_cents)` · `CHECK (discount_pct BETWEEN 0 AND 100)` — teto de sanidade; a alçada comercial é política versionada, não constraint (§5) |
| `reservation_units` | `(unit_id)` e parcial em `(stay_block_id)` — a PK começa por `reservation_id` e não serve a busca pela unidade |
| `reservation_guests` | `(contact_id)` — mesma razão |
| `reservation_nights` | `(unit_type_id, night)` — ADR/RevPAR saem daqui |
| `reservation_code_counters` | `PRIMARY KEY (year)` — é a linha em que a numeração serializa |
| `idempotency_keys` | `(created_at)` — a varredura que expira chaves |
| `role_permissions` | `PRIMARY KEY (role_id, resource_code, action)` |
| `resources` | `CHECK (cardinality(actions) > 0 AND actions <@ ARRAY['ver','criar','editar','excluir'])` |
| `users` | `UNIQUE(email)` · `INDEX(broker_id) WHERE broker_id IS NOT NULL` — parcial porque só corretor tem vínculo |
| `users` | `users_broker_id_fkey`: `(broker_id, id) → brokers(id, user_id) ON UPDATE RESTRICT ON DELETE RESTRICT` — FK **composta**: a conta só aponta para o cadastro que aponta de volta para ela (§2) |
| `brokers` | `UNIQUE(contact_id)` — uma pessoa, um cadastro · `UNIQUE(user_id)` — uma conta, no máximo um cadastro (NULLs não colidem) · `UNIQUE(id, user_id)` — alvo da FK composta · `CHECK (goal_cents >= 0)` · `(property_id)` · `(created_by) WHERE created_by IS NOT NULL` (§10) |
| `special_periods` | `UNIQUE(property_id, name)` — chave natural do seed |
| `rate_tables` | `UNIQUE(property_id, name)` — chave natural do seed |
| `cancellation_tiers` | `UNIQUE(policy_id, sort_order)` — chave natural do seed |
| `reservation_units` | `CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED` — a invariante da casa inteira (§5). Gêmeos em `reservations`, `unit_type_members` e `unit_types.consumes`; `CHECK` não serviria porque `CHECK` não é adiável |
| `quotes` | `CHECK (total_cents = subtotal_cents - discount_cents + cleaning_cents + event_deposit_cents)` — a identidade do motor virou fato do banco · `CHECK (discount_cents <= subtotal_cents)` · `CHECK (is_event OR event_deposit_cents = 0)` · `CHECK (valid_until > created_at)` |
| `quotes` | `UNIQUE(reservation_id) WHERE reservation_id IS NOT NULL` — uma reserva nasce de um orçamento só · `(property_id, created_at DESC)` a listagem · `(property_id, valid_until)` o que está de pé · `(unit_type_id, check_in)` a conversão por produto |
| `quote_nights` | `PRIMARY KEY (quote_id, night)` · `CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED` — as noites fecham com o subtotal, uma por dia da estadia, todas do produto orçado (§4a) · `(unit_type_id, night)` — preço orçado × preço vendido |
| `unit_types` | `CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED` em `consumes` — troca recusada com reserva viva, nas duas direções, com `FOR UPDATE` na linha do produto para serializar contra a venda concorrente (§3) |
| `crm_pipelines` | `UNIQUE(property_id, name)` — chave natural do seed · `UNIQUE(property_id) WHERE is_default` — um funil padrão, e só um |
| `crm_stages` | `UNIQUE(pipeline_id, position) DEFERRABLE INITIALLY DEFERRED` — reordenar o kanban reescreve várias linhas numa instrução · `UNIQUE(pipeline_id, name)` — chave natural do seed |
| `crm_stages` | `CHECK` amarrando `auto_task_subject`/`auto_task_type`/`auto_task_due_days`: os três ou nenhum — tarefa automática sem prazo nunca vence |
| `crm_opportunities` | `CHECK (status <> 'perdido' OR lost_reason_id IS NOT NULL)` e o simétrico · `CHECK ((status = 'aberto') = (closed_at IS NULL))` |
| `crm_opportunities` | `(pipeline_id, stage_id, owner_id, entered_stage_at) WHERE status = 'aberto'` — o kanban, parcial porque ganho e perdido acumulam para sempre |
| `crm_activities` | `UNIQUE(opportunity_id, stage_id) WHERE auto AND status = 'pendente'` — a idempotência da tarefa automática vira regra do banco, não `SELECT`-antes-de-`INSERT` |
| `crm_activities` | `(owner_id, due_at) WHERE status='pendente'` — a caixa de entrada · `(property_id, due_at) WHERE status='pendente' AND due_at IS NOT NULL` — a varredura de alertas |
| `crm_activities` · `crm_notes` | `CHECK (num_nonnulls(lead_id, opportunity_id, contact_id) >= 1)` — atividade solta não aparece em tela nenhuma |
| `crm_lost_reasons` | `UNIQUE(property_id, label)` — chave natural do seed |
| `crm_opportunities` | **não tem `quote_id`** desde `20260827150000`; o orçamento vigente é `quotes.opportunity_id` (§7) |
| `site_content` | `PRIMARY KEY (key)` · `site_content_key_formato` `CHECK (key ~ '^[a-z0-9]+([.-][a-z0-9]+)*$')` · `site_content_value_nao_nulo` `CHECK (jsonb_typeof(value) <> 'null')` · `(updated_by)` (§13b) |
| `site_media` | `site_media_storage_key_unica` `UNIQUE (storage_key)` · `CHECK (kind IN ('imagem','video'))` · `CHECK (bytes > 0)` · `mime` e `storage_key` não vazios · `(created_by)` (§13b) |
| `inventory_counts` | `inventory_counts_aberta_idx` `UNIQUE (unit_id) WHERE status = 'aberta'` — no máximo **uma** conferência aberta por unidade; parcial porque a unidade acumula fechadas para sempre (§11) · `CHECK ((status = 'aberta') = (closed_at IS NULL))` · `(unit_id, opened_at DESC, id)` e `(property_id, opened_at DESC, id)` — histórico ordenado com desempate |
| `inventory_items` | `inventory_items_source_ref_idx` `UNIQUE (property_id, source_ref) WHERE source_ref IS NOT NULL` — é o que torna a importação idempotente · `CHECK (replacement_cost_cents IS NULL OR > 0)` — zero não é sinônimo de "não cotado" · `(property_id, category, name, id)` — a tela do catálogo, filtro e ordenação no mesmo índice |
| `inventory_count_lines` | `UNIQUE (count_id, room_id, item_id)` — linha duplicada mostraria a divergência em dobro · `CHECK ((counted_qty IS NULL) = (counted_at IS NULL))` · `(count_id, room_id, item_id) WHERE counted_qty IS NULL` — o que falta contar · `inventory_count_lines_custo_positivo` `CHECK (replacement_cost_cents IS NULL OR replacement_cost_cents > 0)` — zero entraria na conta como perda de R$ 0,00. `expected_qty` é **congelada** na abertura e `replacement_cost_cents` no fechamento (regra 7, §11) |
| `inventory_issues` | `CHECK ((resolution IS NULL) = (resolved_at IS NULL))` — pendência aberta é `resolution IS NULL`, e não há coluna `status` ao lado · `CHECK (qty > 0)` · `(property_id, reported_at DESC, id) WHERE resolution IS NULL` — a lista de pendências, parcial pelo mesmo motivo do índice do kanban · `(reservation_id) WHERE reservation_id IS NOT NULL` — o que cobrar da estadia · `inventory_issues_id_room_id_item_id_key` `UNIQUE (id, room_id, item_id)` — só alvo da FK composta de `maintenance_orders` (`20261009100000`) |
| `unit_rooms` | `UNIQUE (unit_id, name)` — dois "Suíte 1" fazem contar a mesma cama duas vezes · `unit_rooms_id_unit_id_key` `UNIQUE (id, unit_id)` — só alvo da FK composta de `maintenance_orders` (`20261009100000`): o cômodo da ordem é da unidade dela · `unit_rooms_code_unico` `UNIQUE (unit_id, code)` — a identidade estável que a importação e a cópia usam; único na unidade, e não na propriedade, porque o mesmo `cozinha` em dois duplex é o que casa origem e destino da cópia (§11) · `unit_rooms_code_formato` `CHECK (code ~ '^[a-z0-9]+(-[a-z0-9]+)*$')` e `unit_rooms_code_tamanho` `CHECK (length(code) <= 60)` — separadas para o nome da constraint dizer à API qual regra caiu · `CHECK` do vocabulário de `kind` · `(unit_id, sort_order, name, id)` — a lista de conferência sai ordenada do índice, com desempate determinístico |
| `inventory_media` | `inventory_media_storage_key_unica` `UNIQUE (storage_key)` · `UNIQUE (thumb_key) WHERE thumb_key IS NOT NULL` e `CHECK (thumb_key <> storage_key)` — as duas chaves moram no mesmo namespace do volume, e a colisão sobrescreveria a foto |
| `inventory_item_media` | `PRIMARY KEY (item_id, media_id)` — muitos-para-muitos · `(item_id, sort_order, media_id)` — a capa é o menor `sort_order`, desempatado por `media_id` |
| `room_inventory` | `PRIMARY KEY (room_id, item_id)` · `CHECK (expected_qty >= 0)` · `(item_id)` — "em que ambientes este item está", e é por ele que o `RESTRICT` passa ao tentar apagar um item |
| `maintenance_orders` | `maintenance_orders_avaria_aberta_idx` `UNIQUE (issue_id) WHERE issue_id IS NOT NULL AND status IN ('aberta','em_andamento')` — no máximo **uma** ordem não encerrada por avaria; o `23505` neste nome é o `409 MAINTENANCE_ORDER_ALREADY_OPEN` · `maintenance_orders_bloqueio_idx` `UNIQUE (stay_block_id) WHERE stay_block_id IS NOT NULL` — um bloqueio, no máximo uma ordem (§11) |
| `maintenance_orders` | `maintenance_orders_room_id_fkey` `(room_id, unit_id) → unit_rooms(id, unit_id)` — o cômodo é da unidade · `maintenance_orders_issue_id_fkey` `(issue_id, room_id, item_id) → inventory_issues(id, room_id, item_id) ON DELETE RESTRICT` + `maintenance_orders_avaria_exige_comodo_e_bem` — a avaria traz cômodo e bem dela, e por isso é da unidade · `stay_block_id`, `item_id` e `unit_id` em `ON DELETE RESTRICT` — liberar bloqueio é `cancelled`, nunca `DELETE` |
| `maintenance_orders` | `maintenance_orders_fechamento` `CHECK ((status IN ('concluida','cancelada')) = (closed_at IS NOT NULL))` · `maintenance_orders_inicio` (`em_andamento` ⇔ `started_at`, fora das encerradas) · `maintenance_orders_cronologia` (`started_at <= closed_at`) · `maintenance_orders_fechador` (`closed_by` só com `closed_at`) · `maintenance_orders_custo_positivo` (`cost_cents > 0`) · vocabulários de `priority` e `status` · `title` não vazio e até 200 caracteres, `description` até 4000 |
| `maintenance_orders` | `(property_id, opened_at, id) WHERE status IN ('aberta','em_andamento')` — a lista de trabalho; a prioridade chega como parâmetro e é ordenada em memória (§11) · `(unit_id, opened_at DESC, id)` · `(room_id, unit_id)`, `(issue_id, room_id, item_id)`, `(item_id, opened_at DESC, id)`, `(opened_by)`, `(closed_by)` parciais em `IS NOT NULL` — cada FK, as compostas na ordem da FK |
| quase todas | índice em toda FK — **não é "todas"**, e a diferença é medível |

A linha anterior dizia `todas`. A consulta abaixo devolve hoje **17** chaves
estrangeiras sem índice que comece por elas (reconferido em `20261009100000`: a mesma lista
de antes; as nove FKs de `maintenance_orders`, as duas compostas inclusive, têm índice que
começa pelas colunas delas, na ordem da FK):

```sql
SELECT c.conrelid::regclass, c.conname
  FROM pg_constraint c
 WHERE c.contype = 'f' AND c.connamespace = 'public'::regnamespace
   AND NOT EXISTS (SELECT 1 FROM pg_index i
                    WHERE i.indrelid = c.conrelid
                      AND (i.indkey::int2[])[0:array_length(c.conkey,1)-1] = c.conkey::int2[]);
```

Nem toda uma delas é dívida. Quatro grupos, e só o terceiro custa alguma coisa:

- **Coberta como coluna não-inicial de um índice composto** — `rates(unit_type_id)` e
  os `date_type` (`rates`, `reservation_nights`, `quote_nights`,
  `min_nights_rules`, `unit_type_min_nights`) vivem dentro de `UNIQUE`/PK que começa
  por outra coluna. O join
  pelo tarifário usa o índice; a varredura por tipo de data, não. Ninguém varre por
  tipo de data.
- **Tabela de log, escrita muito e lida por outro eixo** — `audit_log(property_id)`,
  `pii_access_log(actor_id)`, `reservation_events(actor_id)`,
  `idempotency_keys(actor_id, property_id)`. O índice pagaria escrita em todo pedido
  para servir consulta que ninguém faz.
- **Dívida de verdade, pequena** — `password_resets(user_id)` não tem índice nenhum, e
  `auth/repository.go:357` faz `UPDATE password_resets SET used_at = now() WHERE
  user_id = $1 AND used_at IS NULL` a cada troca de senha: hoje é *seq scan* numa
  tabela de dezenas de linhas, e continua sendo *seq scan* quando ela tiver milhares.
  `role_permissions(resource_code)` custa menos do que parece — toda leitura da matriz
  filtra por `role_id`, que é o prefixo da PK; o índice ausente só pesa na checagem de
  FK quando `resources` muda, e `resources` é catálogo estático.
- **Falso positivo da consulta: FK composta coberta por índice de uma coluna** —
  `users_broker_id_fkey` (`(broker_id, id)`, desde `20261002180000`). A consulta exige
  um índice que comece pelas DUAS colunas, mas a checagem que o Postgres faz ao
  apagar ou alterar um `brokers` é `WHERE broker_id = $1 AND id = $2`, e o índice
  parcial `users_broker_idx (broker_id)` a resolve sozinho — `users.broker_id` é, na
  prática, único (a FK composta somada a `UNIQUE (brokers.user_id)` impede duas contas
  no mesmo cadastro). Medido com plano genérico e `enable_seqscan = off`:
  `Index Scan using users_broker_idx … Index Cond: (broker_id = $1) Filter: (id = $2)`.
  Um índice `(broker_id, id)` ao lado seria um segundo índice para a mesma busca.

O erro oposto também existe, e a consulta não o vê: ela não lê o **predicado** do índice.
`inventory_issues.property_id` e `maintenance_orders.property_id` contam como cobertas, mas o
único índice que começa por elas é parcial (`resolution IS NULL`; `status IN
('aberta','em_andamento')`), e a conferência de FK ao apagar uma propriedade
(`WHERE property_id = $1`) não o usaria. Fica assim porque propriedade não se apaga. Já os
índices parciais em `IS NOT NULL` servem à FK, porque a igualdade da FK implica o predicado.

Fica escrito em vez de virar migration porque índice que ninguém mede é peso de
escrita comprado no escuro.

**Disponibilidade por unidade e período não ganha índice próprio.** O `EXCLUDE USING gist (unit_id WITH =, period WITH &&) WHERE status IN ('hold','confirmed')` já cria exatamente esse índice, com exatamente o predicado do mapa de ocupação. Criar um igual ao lado dobraria o custo de escrita sem ganhar leitura nenhuma.

---

## 15. Migrations

- Ferramenta: **golang-migrate**. Arquivos em `apps/api/migrations/`, nomeados `AAAAMMDDHHMMSS_descricao.{up,down}.sql`.
- Aplicadas por `cmd/migrate` como **passo separado** (`make migrate`), nunca no boot da API. A API consulta a versão em `/readyz` e **se recusa a servir** se a migration esperada não estiver aplicada.
- Toda `up` tem `down` correspondente; o CI roda `up` e depois `down` até zero num Postgres efêmero.
- **Só o agente `db-migrations` cria migration.** Nome por timestamp evita a colisão clássica de dois agentes criando `000007_*`.

Entregues até aqui — a última é a versão que o binário exige em `/readyz` (`router.SchemaVersionEsperada`, hoje `20261009100000`), e por isso ela sobe **no mesmo commit** da migration; `TestSchemaVersionEsperadaAcompanhaAUltimaMigration` reprova se divergirem:

| Migration | O que trouxe |
|---|---|
| `20260820120000_core` | propriedade, identidade e RBAC (`users`, `roles`, `resources`, `role_permissions`, `refresh_tokens`, `password_resets`), auditoria, `app_settings` e `idempotency_keys` |
| `20260820130000_inventario_reservas` | inventário, calendário comercial e tarifário, políticas, contatos, `reservations`, `reservation_*` e `stay_blocks` com a `EXCLUDE` |
| `20260820140000_catalogo_e_chaves_naturais` | `users.broker_id` + índice parcial; catálogo completo em `resources` (`actions`, `supports_own`, `sort_order` e o `CHECK` do vocabulário); chaves naturais de `special_periods`, `rate_tables` e `cancellation_tiers` |
| `20260826100000_reservation_pricing` | extrai o satélite financeiro 1:1 e leva `reservations` de 32 para 22 colunas (dívida **1a** / **D2**, **paga**). `owner_id` entra na migration seguinte e a tabela fica com as **23** de hoje |
| `20260826110000_reservas_fase1` | `reservations.owner_id` (escopo `own`); `reservation_code_counters` + `proximo_codigo_reserva()` como `DEFAULT` de `code`; `hold_extension_hours`/`hold_max_extensions` na política comercial; os índices das consultas quentes da Fase 1 |
| `20260826120000_historico_ocupacao_dono_do_bloco_e_idempotencia_por_ator` | `stay_blocks.status = 'completed'` (o check-out deixa de apagar a estadia do mapa) + gist parcial da ocupação; `idempotency_keys` chaveada por `(key, endpoint, actor_id, property_id)` (o replay deixa de vazar resposta entre usuários); `stay_blocks.owner_id` + índice parcial (o escopo `own` de `calendar` vira SQL) |
| `20260827100000_invariante_da_casa_inteira` | a exclusividade da Completa vira regra **do banco**: quatro `CONSTRAINT TRIGGER` adiados sobre `reservation_units`, `reservations`, `unit_type_members` e `unit_types.consumes` (§5). Repara o estado legado reparável e **aborta**, nomeando reserva e unidade, o que exige decisão comercial |
| `20260827110000_crm` | as nove tabelas do CRM (§7), a `UNIQUE` adiável de `position`, a parcial única que torna a tarefa automática idempotente, e o bloco que faz a migration falhar se uma tabela do CRM passar de 25 colunas |
| `20260827120000_notificacoes_tempo_real` | gatilhos de `pg_notify` em `stay_blocks`, `reservations` e `crm_opportunities` nos canais `whv_calendar` e `whv_crm`, com payload de identificadores e `v = pg_current_xact_id()` (§13a) |
| `20260827130000_orcamentos_persistidos` | `quotes` (25 colunas) e `quote_nights`, com os `CHECK` da aritmética do motor e o `CONSTRAINT TRIGGER` adiado que faz o snapshot fechar (§4a). O orçamento deixa de ser calculado e jogado fora — é o que destrava `/win` |
| `20260827140000_troca_de_consumes_com_venda_viva` | `unit_types.consumes` passa a ser imutável com reserva viva, nas duas direções, com trava de linha contra a venda concorrente (§3). Avisa (`RAISE WARNING`, no log do servidor) sobre estado legado já trocado, sem reparar nem abortar — reclassificação passada pode ter sido decisão comercial legítima |
| `20260827150000_documento_unico_e_coluna_morta_do_funil` | `contacts_doc_unico_idx` — o `409 CONTACT_DUPLICATE` que a API prometia passa a ter lastro no banco, e não só na trava de aplicação que não alcança seed nem `psql` (§6). E `crm_opportunities.quote_id`, coluna que ninguém escrevia nem lia desde que o orçamento virou tabela própria, é derrubada (§7) |
| `20260831100000_validade_do_orcamento_versionada` | `commercial_policies.quote_validity_days` (`NOT NULL DEFAULT 7`, `CHECK (> 0)`) — a validade do orçamento vira dado versionado no schema (dívida **D7**, metade de schema; a leitura pela aplicação é F2-05, §4a) |
| `20261002180000_brokers_e_fk_do_corretor` | `brokers` (9 colunas, §10) e as FKs `reservations_broker_id_fkey` e `users_broker_id_fkey` — esta **composta**, `(broker_id, id) → brokers(id, user_id)` (§2). Anula, com linha em `audit_log` e `RAISE WARNING`, todo `broker_id` órfão que encontrar; o `down` não os devolve (§5). `commission_rule_id` fica para o F2-10, junto com `commission_rules` |
| `20261003100000_catalogo_real_vitrine_minimo_e_pacotes` | o que o catálogo real precisa (§3, §4): `unit_types.public_name` (nome de vitrine, `NULL` = `name`, nunca em branco), `unit_type_min_nights` (estadia mínima por produto, sobrepõe `min_nights_rules`) e `rate_packages` (preço por duração, com o vocabulário de `date_types` em `CHECK`). O `down` derruba as duas tabelas e a coluna |
| `20261003120000_conteudo_e_midia_do_site` | o banco do menu **Site** (§13b, `docs/site-cms.md`): `site_content` (campo editado do site, chave em `CHECK` de formato, valor jsonb não nulo) e `site_media` (foto/vídeo imutável, `storage_key` única). O recurso RBAC `site` não é migration: entra pelo seed, como todo o catálogo de `resources` |
| `20261007170000_inventario_de_bens_por_ambiente` | o inventário de bens físicos por ambiente (§11), oito tabelas: `unit_rooms` (o cômodo, conceito novo no banco), `inventory_items` (catálogo da propriedade, com `source_ref` único parcial para importação idempotente), `room_inventory` (a colocação — é o `unit_inventory` projetado, com o cômodo no lugar da unidade), `inventory_media` + `inventory_item_media` (foto própria, muitos-para-muitos; **não** `site_media`, que não tem `property_id` e cai no `down` do CMS), `inventory_counts` + `inventory_count_lines` (conferência, com no máximo uma aberta por unidade e `expected_qty` congelada na abertura) e `inventory_issues` (quebrado/faltando/avariado, com `reservation_id` para cobrar o hóspede). `min_stock` fica para `stock_movements`; `cost_cents` virou `replacement_cost_cents`. O `down` derruba as oito e não deixa sobra |
| `20261007200000_congela_custo_na_conferencia` | `inventory_count_lines.replacement_cost_cents` (anulável, `CHECK > 0` em `inventory_count_lines_custo_positivo`): o custo de reposição **congelado no fechamento**, de onde sai a perda de cada divergência. Com ele, a conferência fechada devolve lida de volta a perda que apurou, e recotar o item não a reescreve (regra 7, §11). Backfill das conferências já fechadas com o custo **atual** do item: aproximação de desenvolvimento, porque `20261007170000` nunca chegou a produção. A tabela vai de 11 para 12 colunas, conferida pelo bloco `DO` da régua. O `down` derruba a coluna (e com ela o `CHECK` e o comentário) e é **lossy**: o custo congelado some, e o `up` seguinte o refaz com o custo do dia |
| `20261007213000_codigo_do_comodo` | `unit_rooms.code` (`text NOT NULL`): a identidade estável do cômodo, que a API não deixa editar e que a importação do levantamento fotográfico e a cópia de inventário entre unidades usam como chave (§11). `unit_rooms_code_formato` (`CHECK` do `pattern` do contrato), `unit_rooms_code_tamanho` (`CHECK (length(code) <= 60)`) e `unit_rooms_code_unico` (`UNIQUE (unit_id, code)`). Backfill das linhas existentes, antes do `SET NOT NULL`, pela regra de derivação que a API repete (escrita na migration e em §11): sem acento por tabela explícita (`translate`, sem extensão nova e sem `lower()`), hífen no lugar de cada sequência fora de `[a-z0-9]`, corte em 60, `comodo` para nome que vira vazio e `-2`, `-3`… em colisão na unidade, em ordem de `(created_at, id)`. A tabela vai de 9 para 10 colunas, conferida pelo bloco `DO` da régua. O `down` derruba as três constraints e a coluna sem deixar sobra e é **lossy**: o `up` seguinte re-deriva os códigos do `name` atual |
| `20261009100000_ordens_de_manutencao` | `maintenance_orders` (18 colunas, §11): a ordem de manutenção de uma unidade, com cômodo, bem e avaria opcionais, prioridade, estado, custo e o bloqueio de calendário em `stay_block_id` (`ON DELETE RESTRICT`: liberar é `cancelled`). O banco garante que o cômodo é da unidade (FK composta `maintenance_orders_room_id_fkey`), que a avaria traz cômodo e bem dela (`CHECK` + FK composta `maintenance_orders_issue_id_fkey`), no máximo uma ordem não encerrada por avaria (`maintenance_orders_avaria_aberta_idx`, o `409 MAINTENANCE_ORDER_ALREADY_OPEN`), um bloqueio por ordem (`maintenance_orders_bloqueio_idx`) e os estados impossíveis de `status`/`started_at`/`closed_at`/`closed_by`. Os alvos das FKs compostas são duas `UNIQUE` novas, `unit_rooms_id_unit_id_key` e `inventory_issues_id_room_id_item_id_key`. Bloco `DO` da régua. Sem gatilho de tempo real (o de `stay_blocks` já avisa o mapa). O recurso RBAC `maintenance` entra pelo seed, não pela migration. O `down` derruba a tabela e as duas `UNIQUE` sem deixar sobra e é **lossy**: as ordens somem, e as linhas de `stay_blocks` que elas criaram ficam como bloqueio operacional comum |

## 16. Seeds

`cmd/seed` popula, numa **transação única**: propriedade, unidades, produtos e composição, tipos de data com precedência, feriados e períodos de 2026–2027, Tabela Comercial V1 (tarifas, estadia mínima geral e por produto, pacotes) — tudo isso do **catálogo** escolhido, abaixo —, política comercial e de cancelamento v1, **quatro contatos de demonstração**, o catálogo de 26 recursos, os 3 perfis com a matriz inteira, um usuário de cada perfil para desenvolvimento, desde `20261002180000` o **cadastro comercial do corretor de desenvolvimento** (contato + `brokers` + `users.broker_id`) e, desde 03/10/2026, a **conta de serviço da vitrine** (perfil `vitrine` + usuário `vitrine@site.whitehouse.invalid`, abaixo).

**O corretor de desenvolvimento** (etapa `corretores_de_desenvolvimento`, `cmd/seed/corretores.go`). Sem ele, `corretor@wh.local` entra no painel com `broker_id` nulo, e a partir do F2-13 isso tem efeito: em escopo `own` o corretor só atribui venda ao próprio `users.broker_id`, e a API não tem como criar o vínculo (o CRUD de `/brokers` é do F2-17/Fase 3). São três escritas por conta de perfil `corretor` em `usuariosSeed`, na ordem que as FKs exigem: a ficha em `contacts` (chave natural: telefone; `+5585900000010`, base `contrato`, sem opt-in), o cadastro em `brokers` (chave natural: `UNIQUE (user_id)`) e o vínculo em `users.broker_id`. O `DO UPDATE` do cadastro corrige só `contact_id`; `goal_cents` e `active` são decisão da gestão e o seed não os reescreve — a mesma regra das contas, que não têm a senha reescrita. A etapa segue a trava das **contas** de desenvolvimento (`SEED_DEV_USERS`), não a dos contatos de demonstração: o cadastro é da conta. E termina com uma pós-condição conferida no banco — conta de corretor sem cadastro que aponte de volta para ela é erro, e o seed inteiro volta atrás —, porque os `JOIN`s da etapa descartariam em silêncio um e-mail digitado errado e a contagem diria "inalterada".

**Dois catálogos, escolhidos por `SEED_CATALOGO`** (`cmd/seed/catalogo*.go`, desde 03/10/2026):

| | `real` (padrão) | `teste` |
|---|---|---|
| Quem usa | `make seed`, compose, VPS | `make it-seed` (suíte de integração, local e CI) |
| Unidades | 12: `AP-01..06`, `SP-01..04`, `GV-01`, `CV-01` | 8: `AP-01..03`, `SP-01..04`, `COB-01` |
| Produtos | 14 (6 duplex, 4 suítes, `pool-suites`, `grand-villa`, `classic-villa`, `completa`), com `public_name` | 4 (`apto-2s`, `suite-piscina`, `cobertura`, `completa`), `public_name` nulo |
| Tarifas | 75 — a Completa e três tipos da Grand Villa são **sob consulta** (sem linha) | 24 |
| Estadia mínima | geral (feriado 2) + 39 linhas por produto | geral (feriado 3), nenhuma por produto |
| Pacotes | 4 (Grand Villa) | nenhum |
| Feriados / períodos | 10 noites de feriado / 7 períodos | 7 / 4 |

O de teste é **congelado**: é byte a byte o que o seed semeava até 03/10/2026, porque ~33 arquivos de teste de integração dependem dos seus códigos — `TestCatalogoDeTesteCongelado` reprova se as contagens mudarem. Valor desconhecido em `SEED_CATALOGO` é erro, não fallback. Propriedade, tipos de data, políticas, perfis, funil e contas são os mesmos nos dois.

**Um banco nunca fica com os dois catálogos vendendo.** Semear um catálogo **desativa** (`active = false`; nunca apaga — unidade tem histórico em `stay_blocks`) as unidades, os produtos, os feriados e os períodos **do outro catálogo** que não existem no escolhido; o que a gestão cadastrou pela tela não é de catálogo nenhum e não é tocado. E reativa os próprios, que o outro tinha desativado. Para os produtos do catálogo escolhido, a composição, as tarifas, os mínimos por produto e os pacotes ficam **exatamente** iguais à lista: o que falta entra e o que sobra é **removido** — sem isso a Completa real herdaria a `COB-01` e as diárias da Completa de teste. Antes de mexer, o seed confere venda viva (`hold`, `confirmed`, `checked_in`): se a troca mudasse a composição ou o `consumes` de um produto com reserva de pé, ele aborta com a mensagem nomeando produto, unidades e reservas (`o catálogo não pode ser trocado com venda viva: a composição mudaria — completa perderia AP-04, … (reservas: WH-2026-0001)`), e a transação inteira volta. Os gatilhos adiados do §5 reprovariam o mesmo no `COMMIT`, mas falando de invariante, não de seed.

Contagens num banco recém-migrado, **medidas em 09/10/2026** em `20261009100000`, as duas num banco limpo. Catálogo `teste`: primeira execução `previstas 328, criadas 327, atualizadas 1` (o vínculo do corretor é `UPDATE` numa conta que a etapa anterior criou); segunda `criadas 0, atualizadas 0, inalteradas 328`. Catálogo `real`: primeira `previstas 454, criadas 453, atualizadas 1`; segunda `criadas 0, atualizadas 0, inalteradas 454`. Numa instalação já semeada antes de `maintenance`, a primeira execução nova dá `criadas 9, atualizadas 1`: o recurso, as quatro ações para `admin` e para `usuario`, e o `sort_order` de `channels`, que foi de 15 para 16. A segunda volta a `criadas 0, atualizadas 0`. (Eram 301 e 427 até a conta da vitrine, que soma 4: o perfil, as duas células e o usuário; 305 e 431 até o recurso `site`, que soma 5: o recurso e `ver`/`editar` para `admin` e `usuario`; 310 e 436 até `inventory.goods` (07/10/2026), que soma 9: o recurso e as quatro ações para `admin` e para `usuario`; e 319 e 445 até `maintenance` (09/10/2026), que soma os mesmos 9.) Alternar no mesmo banco (`real` → `teste` → `real`) passa sem erro; o log de cada etapa traz também `desativadas` e `removidas`, que voltam a zero na execução seguinte.

**A conta de serviço da vitrine** (etapas `perfil_da_vitrine` e `conta_da_vitrine`, `cmd/seed/vitrine.go`). `POST /public/holds` (B1 de `docs/unificacao-site-crm.md`) roda o **mesmo** serviço de reserva e de contato do painel, e esses serviços exigem um usuário autenticado no contexto — dele saem escopo, `owner_id`/`created_by` e auditoria. Em vez de abrir um atalho "sem usuário" no domínio, o handler público carrega esta conta com `CarregarSessao` e chama o serviço como qualquer ator; o que o site grava aparece como "Site (vitrine)" na trilha.

- **Perfil `vitrine`** ("Site — vitrine pública", `is_system = false`) com **exatamente duas células**: `contacts:criar:all` e `reservations:criar:all`. É a única exceção à regra 3 abaixo: o perfil serve a uma rota sem login, então célula concedida a mais na tela é concedida à internet inteira — a reexecução do seed **apaga** o que sobrar (conta em `removidas`, com aviso no log). `is_system = false` de propósito: `is_system` é o sinal de perfil raiz e passa por cima do teto de privilégio.
- **Usuário `vitrine@site.whitehouse.invalid`** ("Site (vitrine)", na propriedade padrão, ativo, sem `broker_id`). **Ninguém entra com ele**: o `password_hash` é um argon2id legítimo de 32 bytes aleatórios gerados na criação e descartados — nunca logados nem guardados. Hash válido, e não um marcador (`!`, vazio), porque o login trata hash fora de formato como erro interno (500), o que quebraria a resposta idêntica `INVALID_CREDENTIALS` e entregaria a conta. O hash só é gerado na criação; a reexecução corrige nome, perfil e `broker_id`, mas **não** reescreve a senha e **não reativa** a conta: desativá-la (ou excluí-la) no painel é o botão de emergência que fecha as escritas do site, e o seed roda a cada deploy. Diferente das contas de desenvolvimento, esta roda também em produção — não tem senha conhecida.

Os **contatos de demonstração** existem por duas razões. A primeira: `reservations.contact_id` e `crm_opportunities.contact_id` são `NOT NULL`, então sem contato não há como abrir orçamento, pré-reserva, oportunidade nem smoke test da jornada num banco recém-semeado. A segunda: com **um** contato só, a tela de contatos e o funil nascem praticamente vazios — não dá para ver ordenação, busca por nome, recorte por base legal, nem a diferença entre quem aceitou receber oferta e quem não aceitou. Tela vazia não prova que a tela funciona.

São quatro, e as **bases legais variam de propósito**: `lgpd_basis` é o que sustenta guardar a ficha (LGPD art. 7), e o seed é o único lugar onde alguém aprende o vocabulário por exemplo — um seed em que todos são `legitimo_interesse` ensina que o campo é decorativo. O lead do formulário é `consentimento` e é o único com `marketing_opt_in = true`, com `consent_at` gravado; quem já se hospedou é `contrato`, que legitima guardar o cadastro e **não** legitima mandar oferta; a prospecção de evento é `legitimo_interesse`. O instante do consentimento é **fixo e com fuso** (`2026-08-01T12:00:00-03:00`): `now()` faria a linha voltar como "atualizada" em toda execução, e data sem fuso renderia meia-noite UTC, que em America/Fortaleza é o dia anterior às 21h.

A chave natural é o telefone (`UNIQUE(phone_e164)`), o que torna a etapa idempotente. É gated como os usuários de desenvolvimento — em produção só entra com `SEED_DEMO_DATA=true`, porque contato fictício polui a base real de leads. E-mail em `.invalid` (RFC 2606, nunca resolve) e telefone E.164 na faixa `9 0000 xxxx`, não atribuível a celular no Brasil, para o WhatsApp jamais casar uma pessoa real com estas linhas. `doc_type`/`doc_number` ficam nulos: CPF inventado ou passa na validação de dígito e vira dado plausível em que alguém acredita, ou não passa e quebra a primeira tela que validar.

**Idempotente por contrato**: rodar dez vezes tem o mesmo efeito de rodar uma. Três decisões sustentam isso:

1. Toda escrita é `INSERT ... ON CONFLICT` sobre a **chave natural** da tabela (`slug`, `code`, `(property_id, code)`, `(rate_table_id, unit_type_id, date_type)`…), nunca sobre id gerado — id novo a cada execução é exatamente o que duplicaria tudo na segunda rodada.
2. O `DO UPDATE` leva `WHERE (colunas) IS DISTINCT FROM (EXCLUDED.colunas)`: linha já correta não é reescrita e não sobe `updated_at`. A segunda execução loga zero criadas e zero atualizadas — é essa a prova de idempotência.
3. O seed **corrige divergência, mas não apaga acréscimo** — com uma exceção delimitada. Permissão que a gestão concedeu a mais na tela sobrevive; escopo que divergiu da matriz volta ao valor do seed. As exceções são duas: os produtos **do catálogo** — composição, tarifas, mínimos por produto e pacotes deles são mantidos exatamente iguais à lista (acima) — e a matriz do perfil **`vitrine`**, que fica com exatamente as suas duas células (acima). Nada fora disso leva `DELETE`, e nenhuma linha de cadastro (unidade, produto, feriado, período) é apagada — só desativada.

Desde `20260827110000` o seed também popula o **funil padrão** (`Funil de Reservas`, `is_default`), as **oito etapas** da spec §7 — Novo lead → Em atendimento → Disponibilidade consultada → Orçamento enviado → Negociação → Pré-reserva → Ganho / Perdido — com `probability`, `color`, `sla_days` e tarefa automática, e os **oito motivos de perda** que a constraint `crm_opportunities_perda_motivada` obriga a preencher. Chaves naturais: `(property_id, name)` no funil, `(pipeline_id, name)` na etapa, `(property_id, label)` no motivo.

Um detalhe do funil só é possível por causa da constraint adiável: as oito etapas entram numa **instrução só**, com `position` de 1 a 8, e durante a instrução duas etapas podem ocupar a mesma posição. Com a unicidade imediata, ajustar a ordem no seed exigiria uma passada por posições negativas antes de renumerar.

Os motivos de perda existem para que o relatório responda uma pergunta de gestão — o que se perde por **preço** (mexe no tarifário), por **data** (mexe no inventário) e por **atendimento** (mexe no SLA). Por isso "Outro motivo" é o último da ordem e não o primeiro: lista que começa em "outros" devolve "outros" como resposta, e ninguém age em cima disso.

Senha de desenvolvimento com hash argon2id via `internal/auth`; **nunca em texto, nunca em log**. Em `APP_ENV=production` o bloco de usuários é pulado com aviso, a menos que `SEED_DEV_USERS=true` — conta com senha conhecida em produção é porta dos fundos, não conveniência.
