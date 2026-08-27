-- Extrai o bloco financeiro de `reservations` para o satélite 1:1
-- `reservation_pricing` (roadmap 1a / docs/db.md §5, dívida D2).
--
-- POR QUE AGORA: `reservations` nasceu com 32 colunas, acima do teto de ~25 da
-- regra 9 do CLAUDE.md, e nenhum código ainda lê essas colunas — o módulo de
-- reservas só nasce depois desta migration. Cada coluna nova (canal, remarcação)
-- encarece a extração; esta é a janela mais barata que vai existir.
--
-- POR QUE SATÉLITE E NÃO OUTRA COLUNA QUALQUER: o critério não é "financeiro",
-- é CICLO DE VIDA. `reservations` guarda identidade, estado e datas — muda a
-- cada transição (hold → confirmed → checked_in → …). O bloco abaixo é
-- SNAPSHOT CONGELADO: nasce no cálculo do orçamento e não muda mais. Misturar
-- os dois numa linha só faz cada `UPDATE` de estado reescrever, sem
-- necessidade, o preço acordado com o hóspede.
--
-- IMUTABILIDADE (contrato do time, não constraint):
--   Depois que a reserva é CONFIRMADA, a linha correspondente aqui é
--   somente-leitura. Reprecificar uma reserva confirmada — remarcação, upgrade,
--   correção de desconto — NÃO é `UPDATE` nesta tabela: é reserva nova
--   referenciando a anterior por `reservations.rebooked_from_id`, com a
--   original indo para `cancelled` (spec §5). Quem editar aqui apaga a prova do
--   que foi combinado, e o razão financeiro da Fase 2 passa a divergir do
--   contrato assinado.
--   Antes da confirmação (`quote`/`hold`) a linha pode ser recalculada à
--   vontade: ainda não há dinheiro nem promessa.
--   A regra fica em comentário e no domínio, não em trigger, porque a decisão
--   de "o que conta como confirmada" é comercial e versionada — congelá-la em
--   PL/pgSQL a tiraria de `internal/domain` (regra 1 do CLAUDE.md).

CREATE TABLE reservation_pricing (
    -- PK = FK: é isto que torna a relação 1:1 de verdade. Sem reserva não
    -- existe preço, e duas linhas de preço para a mesma reserva são impossíveis.
    reservation_id         uuid PRIMARY KEY REFERENCES reservations(id) ON DELETE CASCADE,

    subtotal_cents         bigint NOT NULL DEFAULT 0 CHECK (subtotal_cents >= 0),
    discount_pct           numeric(5,2) NOT NULL DEFAULT 0 CHECK (discount_pct BETWEEN 0 AND 100),
    discount_cents         bigint NOT NULL DEFAULT 0 CHECK (discount_cents >= 0),
    cleaning_cents         bigint NOT NULL DEFAULT 0 CHECK (cleaning_cents >= 0),
    event_deposit_cents    bigint NOT NULL DEFAULT 0 CHECK (event_deposit_cents >= 0),
    total_cents            bigint NOT NULL DEFAULT 0 CHECK (total_cents >= 0),
    deposit_cents          bigint NOT NULL DEFAULT 0 CHECK (deposit_cents >= 0),

    -- As três referências que a reserva congela (regra 7): qual tabela de
    -- tarifas precificou, qual versão da política comercial valia e qual
    -- política de cancelamento será aplicada se o hóspede desistir. Anuláveis
    -- porque um `quote` recém-aberto ainda não escolheu tarifário.
    rate_table_id          uuid REFERENCES rate_tables(id),
    policy_version         int CHECK (policy_version IS NULL OR policy_version > 0),
    cancellation_policy_id uuid REFERENCES cancellation_policies(id),

    created_at             timestamptz NOT NULL DEFAULT now(),

    -- O teto do banco é sanidade aritmética, não alçada comercial: o desconto
    -- não pode ser maior que as diárias que ele desconta. A alçada (≤5% / 6–10%
    -- / >10%, spec §3) continua sendo política versionada avaliada no domínio,
    -- porque como CHECK fixo ela viraria número de schema e as reservas antigas
    -- passariam a violar a regra nova.
    CONSTRAINT reservation_pricing_desconto_cabe CHECK (discount_cents <= subtotal_cents)
);

COMMENT ON TABLE reservation_pricing IS
    'Snapshot financeiro 1:1 da reserva. Imutável depois da confirmação: reprecificar reserva confirmada é reserva nova via reservations.rebooked_from_id, nunca UPDATE aqui.';

-- Índice na FK do tarifário: "quais reservas usaram a Tabela V1" é a consulta
-- que responde o impacto de uma mudança de preço, e é a única leitura desta
-- tabela que não entra pela PK.
CREATE INDEX reservation_pricing_rate_table_idx ON reservation_pricing(rate_table_id)
    WHERE rate_table_id IS NOT NULL;
CREATE INDEX reservation_pricing_cancellation_policy_idx ON reservation_pricing(cancellation_policy_id)
    WHERE cancellation_policy_id IS NOT NULL;

-- Migra o que existir. Na prática a tabela está vazia (o módulo de reservas
-- ainda não foi escrito), mas uma `up` que só funciona em banco vazio é uma
-- `up` que quebra no primeiro ambiente que tiver dado.
INSERT INTO reservation_pricing (
        reservation_id, subtotal_cents, discount_pct, discount_cents, cleaning_cents,
        event_deposit_cents, total_cents, deposit_cents,
        rate_table_id, policy_version, cancellation_policy_id, created_at)
SELECT  id, subtotal_cents, discount_pct, discount_cents, cleaning_cents,
        event_deposit_cents, total_cents, deposit_cents,
        rate_table_id, policy_version, cancellation_policy_id, created_at
  FROM  reservations;

-- Os CHECKs de coluna caem junto com as colunas; o CHECK de tabela
-- `discount_pct BETWEEN 0 AND 100` também, porque referencia só `discount_pct`.
ALTER TABLE reservations
    DROP COLUMN subtotal_cents,
    DROP COLUMN discount_pct,
    DROP COLUMN discount_cents,
    DROP COLUMN cleaning_cents,
    DROP COLUMN event_deposit_cents,
    DROP COLUMN total_cents,
    DROP COLUMN deposit_cents,
    DROP COLUMN rate_table_id,
    DROP COLUMN policy_version,
    DROP COLUMN cancellation_policy_id;
