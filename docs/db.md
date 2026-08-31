# Modelo de dados

> PostgreSQL 16. Extensões: `btree_gist` (constraint de sobreposição), `pg_trgm` (busca), `pgcrypto` (`gen_random_uuid`).

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

**`units` não tem `unit_type_id`** — esta página listava a coluna e a migration `20260820130000` nunca a criou, porque ela seria *errada*. A relação produto × unidade é **muitos-para-muitos de propósito**: `AP-01` é vendável como *Apartamento 2 Suítes* **e** como parte da *White House Completa*, e uma FK escalar em `units` só saberia escrever um dos dois. `unit_type_members` é a única verdade sobre essa composição, e é dela que a Completa tira as 8 linhas de `stay_blocks` que dão a exclusividade bidirecional. A partir de `20260827100000` esta tabela também é **lado de constraint**: acrescentar ou remover um membro de um produto `all_members` com venda viva é recusado no commit (§5), porque crescer a composição depois da venda abre exatamente o mesmo buraco que vender N−1.

**`unit_types.consumes` é imutável enquanto o produto tiver reserva viva** (`hold`, `confirmed`, `checked_in`), desde `20260827140000`. A porta que isso fecha foi medida com o stack no ar: com uma reserva confirmada da Completa ocupando as oito unidades, `UPDATE unit_types SET consumes='one_member' WHERE code='completa'` respondia `UPDATE 1`, sem recusa — e a venda seguinte da "Completa" passava a alocar **uma** unidade cobrando o preço da casa inteira.

O detalhe que torna esta guarda necessária, e não redundante com a invariante do §5: aquela é `WHEN (NEW.consumes = 'all_members')`, isto é, defende a **entrada** no regime da casa inteira. A troca perigosa é a **saída** — e ela é auto-imunizante, porque no instante em que o produto deixa de ser `all_members` a conferência de composição não encontra mais linha para ele e volta sem conferir nada. Trocar o tipo **tira o produto do alcance da invariante que o protegia**. A direção contrária (`one_member → all_members`) também é recusada, e por um caso que a contagem não pega: um produto de composição unitária atravessava a troca sem violar contagem nenhuma e mudava de significado em silêncio — medido, desligando só o gatilho novo, `UPDATE 1`.

A guarda trava a linha do produto em `FOR UPDATE` antes de contar. Não é zelo: criar reserva trava `unit_types` em `FOR KEY SHARE` (pela FK `unit_type_id`) e `UPDATE ... SET consumes` toma `FOR NO KEY UPDATE` — **os dois modos não conflitam**, e é por isso que venda e troca corriam lado a lado. `FOR UPDATE` é o único modo que conflita com `FOR KEY SHARE`. Medido nas duas versões: com a trava, a troca espera a venda em voo comitar e então recusa nomeando a reserva; sem a trava, a mesma troca passa em 100 ms e o banco fica com a Completa marcada `one_member` e uma pré-reserva viva de 8 unidades fora da invariante.

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

**Nenhuma linha aqui toca `stay_blocks`, e isso é a regra, não esquecimento.** Conferido: gravado um orçamento de 15–18/06, `stay_blocks` no período continua com zero linhas e a mesma unidade ainda pode ser vendida para as mesmas datas. Consequência aceita e correta: um orçamento pode virar `409 DATE_CONFLICT` na hora de virar reserva.

**O snapshot fecha, e quem garante é o banco.** `subtotal_cents` sem `quote_nights` é um número sem prova — passa em todos os `CHECK` de coluna e é indistinguível, na leitura, de um orçamento correto. Um `CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED` sobre as duas tabelas confere no commit: uma noite para cada dia de `[check_in, check_out)`, todas do produto orçado, somando exatamente o subtotal. Adiado pelo mesmo motivo do §5 — o cabeçalho nasce antes das noites (a FK exige) e portanto nasce com zero noites.

`CHECK (total_cents = subtotal_cents − discount_cents + cleaning_cents + event_deposit_cents)` escreve no banco a identidade do motor (`booking.Build`), que é exata em centavos porque o arredondamento acontece no total e nunca noite a noite. `balance_cents` (saldo) e `avg_nightly_cents` (diária média) **não são colunas**: são `total − sinal` e `total / noites`, e guardar o que se deriva é criar a segunda verdade que um dia diverge.

`valid_until` é o que separa "proposta em pé" de "preço que já venceu" — é por ela que um orçamento expira em vez de precisar ser apagado. É `timestamptz` porque vencimento é instante, e instante tem fuso. **De onde sai o número**: hoje, de quem grava. O dia em que a validade precisar ser configurável ela vira coluna de `commercial_policies`, ao lado de `hold_extension_hours` — não foi feita agora porque `PublicarPoliticaComercial` copia as colunas nome a nome, e uma coluna nova sem esse acerto voltaria ao `DEFAULT` a cada versão publicada, em silêncio.

`reservation_id` tem **`UNIQUE` parcial**: uma reserva nasce de um orçamento só. Dois orçamentos reivindicando a mesma venda fariam a conversão contar duas vezes e a auditoria não saber qual preço foi o combinado.


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
`UNIQUE(phone_e164) WHERE phone_e164 IS NOT NULL` e índice em `doc_number`. Uma pessoa, um registro: lead, hóspede, corretor e proprietário apontam para cá.

`anonymize(contact_id)` substitui PII por hash e mantém os registros financeiros — atende ao direito de eliminação sem quebrar o razão fiscal.

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
                  unit_type_id?, check_in?, check_out?, guests_count?, quote_id?, reservation_id?,
                  amount_cents, probability, expected_close?, owner_id?, status,
                  lost_reason_id?, entered_stage_at, closed_at?, created_by?, created_at, updated_at)
                  -- 24 colunas
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

### `quote_id` — o orçamento vigente é uma reserva

O orçamento **é** uma linha de `reservations` em status `quote` (spec §5): é ela que carrega `reservation_nights`, a tarifa congelada e a política. `crm_opportunities.quote_id` aponta para ela em vez de copiar o valor, e é isso que faz "ganhar cria a reserva com o orçamento vigente, sem redigitar nada" ser uma promoção de status, e não um recálculo que pode dar outro número — o número que o hóspede ouviu ao telefone.

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
| `20260826100000_reservation_pricing` | extrai o satélite financeiro 1:1 e leva `reservations` de 32 para 22 colunas (dívida **1a** / **D2**, **paga**). `owner_id` entra na migration seguinte e a tabela fica com as **23** de hoje |
| `20260826110000_reservas_fase1` | `reservations.owner_id` (escopo `own`); `reservation_code_counters` + `proximo_codigo_reserva()` como `DEFAULT` de `code`; `hold_extension_hours`/`hold_max_extensions` na política comercial; os índices das consultas quentes da Fase 1 |
| `20260826120000_historico_ocupacao_dono_do_bloco_e_idempotencia_por_ator` | `stay_blocks.status = 'completed'` (o check-out deixa de apagar a estadia do mapa) + gist parcial da ocupação; `idempotency_keys` chaveada por `(key, endpoint, actor_id, property_id)` (o replay deixa de vazar resposta entre usuários); `stay_blocks.owner_id` + índice parcial (o escopo `own` de `calendar` vira SQL) |
| `20260827100000_invariante_da_casa_inteira` | a exclusividade da Completa vira regra **do banco**: quatro `CONSTRAINT TRIGGER` adiados sobre `reservation_units`, `reservations`, `unit_type_members` e `unit_types.consumes` (§5). Repara o estado legado reparável e **aborta**, nomeando reserva e unidade, o que exige decisão comercial |
| `20260827110000_crm` | as nove tabelas do CRM (§7), a `UNIQUE` adiável de `position`, a parcial única que torna a tarefa automática idempotente, e o bloco que faz a migration falhar se uma tabela do CRM passar de 25 colunas |
| `20260827120000_notificacoes_tempo_real` | gatilhos de `pg_notify` em `stay_blocks`, `reservations` e `crm_opportunities` nos canais `whv_calendar` e `whv_crm`, com payload de identificadores e `v = pg_current_xact_id()` (§13a) |
| `20260827130000_orcamentos_persistidos` | `quotes` (25 colunas) e `quote_nights`, com os `CHECK` da aritmética do motor e o `CONSTRAINT TRIGGER` adiado que faz o snapshot fechar (§4a). O orçamento deixa de ser calculado e jogado fora — é o que destrava `/win` |
| `20260827140000_troca_de_consumes_com_venda_viva` | `unit_types.consumes` passa a ser imutável com reserva viva, nas duas direções, com trava de linha contra a venda concorrente (§3). Avisa (`RAISE WARNING`, no log do servidor) sobre estado legado já trocado, sem reparar nem abortar — reclassificação passada pode ter sido decisão comercial legítima |

## 16. Seeds

`cmd/seed` popula, numa **transação única**: propriedade, 8 unidades, 4 produtos e a composição (a Completa apontando para as oito), tipos de data com precedência, feriados e períodos de 2026–2027, Tabela Comercial V1 (24 tarifas + estadia mínima), política comercial e de cancelamento v1, **quatro contatos de demonstração**, o catálogo de 23 recursos, os 3 perfis com a matriz inteira e um usuário de cada perfil para desenvolvimento.

Os **contatos de demonstração** existem por duas razões. A primeira: `reservations.contact_id` e `crm_opportunities.contact_id` são `NOT NULL`, então sem contato não há como abrir orçamento, pré-reserva, oportunidade nem smoke test da jornada num banco recém-semeado. A segunda: com **um** contato só, a tela de contatos e o funil nascem praticamente vazios — não dá para ver ordenação, busca por nome, recorte por base legal, nem a diferença entre quem aceitou receber oferta e quem não aceitou. Tela vazia não prova que a tela funciona.

São quatro, e as **bases legais variam de propósito**: `lgpd_basis` é o que sustenta guardar a ficha (LGPD art. 7), e o seed é o único lugar onde alguém aprende o vocabulário por exemplo — um seed em que todos são `legitimo_interesse` ensina que o campo é decorativo. O lead do formulário é `consentimento` e é o único com `marketing_opt_in = true`, com `consent_at` gravado; quem já se hospedou é `contrato`, que legitima guardar o cadastro e **não** legitima mandar oferta; a prospecção de evento é `legitimo_interesse`. O instante do consentimento é **fixo e com fuso** (`2026-08-01T12:00:00-03:00`): `now()` faria a linha voltar como "atualizada" em toda execução, e data sem fuso renderia meia-noite UTC, que em America/Fortaleza é o dia anterior às 21h.

A chave natural é o telefone (`UNIQUE(phone_e164)`), o que torna a etapa idempotente. É gated como os usuários de desenvolvimento — em produção só entra com `SEED_DEMO_DATA=true`, porque contato fictício polui a base real de leads. E-mail em `.invalid` (RFC 2606, nunca resolve) e telefone E.164 na faixa `9 0000 xxxx`, não atribuível a celular no Brasil, para o WhatsApp jamais casar uma pessoa real com estas linhas. `doc_type`/`doc_number` ficam nulos: CPF inventado ou passa na validação de dígito e vira dado plausível em que alguém acredita, ou não passa e quebra a primeira tela que validar.

**Idempotente por contrato**: rodar dez vezes tem o mesmo efeito de rodar uma. Três decisões sustentam isso:

1. Toda escrita é `INSERT ... ON CONFLICT` sobre a **chave natural** da tabela (`slug`, `code`, `(property_id, code)`, `(rate_table_id, unit_type_id, date_type)`…), nunca sobre id gerado — id novo a cada execução é exatamente o que duplicaria tudo na segunda rodada.
2. O `DO UPDATE` leva `WHERE (colunas) IS DISTINCT FROM (EXCLUDED.colunas)`: linha já correta não é reescrita e não sobe `updated_at`. A segunda execução loga zero criadas e zero atualizadas — é essa a prova de idempotência.
3. O seed **corrige divergência, mas não apaga acréscimo**: nunca há `DELETE`. Permissão que a gestão concedeu a mais na tela sobrevive; escopo que divergiu da matriz volta ao valor do seed.

Desde `20260827110000` o seed também popula o **funil padrão** (`Funil de Reservas`, `is_default`), as **oito etapas** da spec §7 — Novo lead → Em atendimento → Disponibilidade consultada → Orçamento enviado → Negociação → Pré-reserva → Ganho / Perdido — com `probability`, `color`, `sla_days` e tarefa automática, e os **oito motivos de perda** que a constraint `crm_opportunities_perda_motivada` obriga a preencher. Chaves naturais: `(property_id, name)` no funil, `(pipeline_id, name)` na etapa, `(property_id, label)` no motivo.

Um detalhe do funil só é possível por causa da constraint adiável: as oito etapas entram numa **instrução só**, com `position` de 1 a 8, e durante a instrução duas etapas podem ocupar a mesma posição. Com a unicidade imediata, ajustar a ordem no seed exigiria uma passada por posições negativas antes de renumerar.

Os motivos de perda existem para que o relatório responda uma pergunta de gestão — o que se perde por **preço** (mexe no tarifário), por **data** (mexe no inventário) e por **atendimento** (mexe no SLA). Por isso "Outro motivo" é o último da ordem e não o primeiro: lista que começa em "outros" devolve "outros" como resposta, e ninguém age em cima disso.

Senha de desenvolvimento com hash argon2id via `internal/auth`; **nunca em texto, nunca em log**. Em `APP_ENV=production` o bloco de usuários é pulado com aviso, a menos que `SEED_DEV_USERS=true` — conta com senha conhecida em produção é porta dos fundos, não conveniência.
