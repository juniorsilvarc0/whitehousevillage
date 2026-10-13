-- Fase 5: ordens de manutenção (docs/db.md §11, spec §12 "Ordens de manutenção").
--
-- §11 projetava a tabela desde 20/08/2026 numa linha só:
--
--     maintenance_orders(id, unit_id, title, description, priority, status,
--                        stay_block_id?, opened_at, closed_at, cost_cents)
--
-- e faltavam nela as três coisas que o desenho de 09/10/2026 pede: o CÔMODO e o
-- BEM (o ar-condicionado é da suíte 1 do AP-03, não "do AP-03"), a AVARIA que
-- originou a ordem (é ela que a conclusão encerra com `consertado`) e quem
-- abriu, começou e fechou. Contrato em `/maintenance-orders` (tag Manutenção),
-- regras puras em `internal/domain/maintenance`.
--
-- ───────────────────────────────────────────────────────────────────────────
-- O QUE O BANCO GARANTE, E O QUE FICA COM A API
-- ───────────────────────────────────────────────────────────────────────────
--
-- No banco, porque escrita concorrente ou `psql` não passam pela API:
--
--   * o cômodo é DA UNIDADE da ordem — FK composta `(room_id, unit_id) →
--     unit_rooms(id, unit_id)`;
--   * a avaria traz cômodo e bem DELA, e portanto é da unidade da ordem — CHECK
--     + FK composta `(issue_id, room_id, item_id) → inventory_issues(id,
--     room_id, item_id)`. Somada à anterior, não existe ordem do AP-03 citando
--     avaria da GV-01;
--   * no máximo UMA ordem não encerrada por avaria — índice único parcial. É a
--     defesa contra o segundo toque no celular, e SELECT antes de INSERT seria
--     a corrida que ela fecha;
--   * um bloqueio de calendário pertence a no máximo uma ordem;
--   * os estados impossíveis de `status`, `started_at` e `closed_at`.
--
-- Na API, e não aqui (a decisão está escrita em §11):
--
--   * o `stay_block` apontado é `source = 'maintenance'` e é da unidade da
--     ordem. A API cria a linha na mesma transação da ordem. Fechar isso no
--     banco exigiria FK composta `(stay_block_id, unit_id) → stay_blocks(id,
--     unit_id)`, isto é, um índice único NOVO na tabela mais quente do sistema
--     — pago em toda reserva, para defender um escritor só, sem exploit medido;
--   * "encerrada não reabre" (`internal/domain/maintenance.Next`): CHECK não
--     enxerga o valor anterior, e gatilho para um escritor só é a decisão que
--     §11 já recusou para `expected_qty` e `unit_rooms.code`;
--   * "liberar o bloqueio" é `stay_blocks.status = 'cancelled'`, NUNCA DELETE.
--     A ordem continua apontando para a linha — e a FK é RESTRICT justamente
--     para que um DELETE esquecido em algum caminho falhe alto em vez de soltar
--     a ordem do próprio histórico.
--
-- Sem gatilho de tempo real nesta tabela: o mapa já é avisado pelo gatilho de
-- `stay_blocks` (20260827120000) quando o bloqueio da ordem nasce, muda ou é
-- liberado, e a ordem sem bloqueio não muda o mapa.

-- ═══════════════ 1. Os alvos das duas FKs compostas ═════════════════════════
--
-- FK composta só aponta para colunas cobertas por UNIQUE ou PK. `id` já é único
-- nas duas tabelas, então as duas UNIQUE abaixo não restringem nada que já não
-- estivesse restrito: existem para ser alvo, como `brokers_id_user_id_key`
-- (20261002180000). Nome no padrão do Postgres, o mesmo daquele precedente.
--
-- Custo: um índice btree a mais em cada tabela. As duas são de escrita rara
-- (cômodo nasce na implantação; avaria, algumas por semana), ao contrário de
-- `stay_blocks`, que é o motivo de a FK do bloqueio NÃO ser composta (acima).
ALTER TABLE unit_rooms
    ADD CONSTRAINT unit_rooms_id_unit_id_key UNIQUE (id, unit_id);

ALTER TABLE inventory_issues
    ADD CONSTRAINT inventory_issues_id_room_id_item_id_key UNIQUE (id, room_id, item_id);

-- ═══════════════════════ 2. A ordem (`maintenance_orders`) ══════════════════
--
-- Dezoito colunas. `opened_at` É o `created_at` desta tabela, pelo mesmo motivo
-- de `inventory_counts.opened_at`: duas colunas para o mesmo instante seriam
-- duas fontes da mesma verdade.
CREATE TABLE maintenance_orders (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id   uuid NOT NULL REFERENCES properties(id),

    -- RESTRICT: unidade com ordem no histórico não se apaga — sai de linha
    -- desativada (`units.active`), como com reserva e conferência.
    unit_id       uuid NOT NULL REFERENCES units(id) ON DELETE RESTRICT,

    -- Sem REFERENCES simples: a FK é a composta `maintenance_orders_room_id_fkey`
    -- (no fim da tabela), que confere existência E unidade de uma vez. Como
    -- `unit_id` é NOT NULL, MATCH SIMPLE confere a FK inteira sempre que há
    -- cômodo; cômodo nulo não é conferido, e está certo.
    room_id       uuid,

    -- O bem é do catálogo da propriedade e NÃO precisa estar colocado no
    -- cômodo (o defeito pode ser do móvel que ninguém conta). RESTRICT, como em
    -- `inventory_issues`: o bem com histórico sai de linha desativado.
    item_id       uuid REFERENCES inventory_items(id) ON DELETE RESTRICT,

    -- Sem REFERENCES simples: a FK é a composta `maintenance_orders_issue_id_fkey`.
    issue_id      uuid,

    title         text NOT NULL
        CONSTRAINT maintenance_orders_title_nao_vazio CHECK (btrim(title) <> '')
        CONSTRAINT maintenance_orders_title_tamanho   CHECK (char_length(title) <= 200),
    -- `char_length`, e não `length` em bytes: o `maxLength` do contrato conta
    -- caracteres, e "manutenção" tem 10, não 12.
    description   text
        CONSTRAINT maintenance_orders_description_tamanho CHECK (char_length(description) <= 4000),

    -- Os dois vocabulários são, palavra por palavra, as constantes de
    -- `internal/domain/maintenance` (`Priority`, `Status`).
    priority      text NOT NULL DEFAULT 'normal'
        CONSTRAINT maintenance_orders_priority_valida
            CHECK (priority IN ('baixa','normal','alta','urgente')),
    status        text NOT NULL DEFAULT 'aberta'
        CONSTRAINT maintenance_orders_status_valido
            CHECK (status IN ('aberta','em_andamento','concluida','cancelada')),

    -- O bloqueio de calendário, quando há. RESTRICT: liberar é `cancelled`,
    -- nunca DELETE (cabeçalho). Ao remarcar um bloqueio que já terminou ou foi
    -- liberado, a API cria linha NOVA e passa a apontar para ela; a antiga fica
    -- no calendário como história (`maintenance.Replan`).
    stay_block_id uuid REFERENCES stay_blocks(id) ON DELETE RESTRICT,

    -- Custo do serviço. NULL = ainda não lançado (a nota chega depois). `> 0` e
    -- não `>= 0`, como em `inventory_items`: zero não é um segundo jeito de
    -- escrever "não sei".
    cost_cents    bigint
        CONSTRAINT maintenance_orders_custo_positivo CHECK (cost_cents IS NULL OR cost_cents > 0),

    opened_at     timestamptz NOT NULL DEFAULT now(),
    opened_by     uuid REFERENCES users(id),
    started_at    timestamptz,
    closed_at     timestamptz,
    closed_by     uuid REFERENCES users(id),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    -- Encerrada ⇔ tem instante de encerramento. Mesma forma de
    -- `inventory_counts_fechamento`. `cancelada` também tem `closed_at`: é
    -- quando se desistiu.
    CONSTRAINT maintenance_orders_fechamento
        CHECK ((status IN ('concluida','cancelada')) = (closed_at IS NOT NULL)),

    -- Quem fechou só existe se fechou. `closed_by` continua anulável
    -- (encerramento por rotina não tem autor); o contrário — autor de um
    -- encerramento que não aconteceu — é estado impossível.
    CONSTRAINT maintenance_orders_fechador
        CHECK (closed_by IS NULL OR closed_at IS NOT NULL),

    -- `em_andamento` é exatamente "começou e não encerrou": tem `started_at`, e
    -- `aberta` não tem (não há "desfazer o início" na máquina). Encerrada fica
    -- livre, porque concluir ou cancelar direto de `aberta` é permitido e deixa
    -- `started_at` nulo — o que é verdade, não lacuna.
    CONSTRAINT maintenance_orders_inicio
        CHECK (status IN ('concluida','cancelada')
               OR (started_at IS NOT NULL) = (status = 'em_andamento')),

    -- Separada de `_inicio` para o nome dizer à API qual regra caiu (o mesmo
    -- motivo de `unit_rooms_code_formato` × `_tamanho`).
    CONSTRAINT maintenance_orders_cronologia
        CHECK (started_at IS NULL OR closed_at IS NULL OR started_at <= closed_at),

    -- Avaria ⇒ cômodo e bem preenchidos. Sem isto, MATCH SIMPLE deixaria passar
    -- `(issue_id, NULL, NULL)` sem conferir nada: basta uma coluna nula da FK
    -- composta para a FK inteira não ser verificada.
    CONSTRAINT maintenance_orders_avaria_exige_comodo_e_bem
        CHECK (issue_id IS NULL OR (room_id IS NOT NULL AND item_id IS NOT NULL)),

    -- O cômodo é da unidade da ordem.
    CONSTRAINT maintenance_orders_room_id_fkey
        FOREIGN KEY (room_id, unit_id) REFERENCES unit_rooms(id, unit_id) ON DELETE RESTRICT,

    -- O cômodo e o bem são os da avaria. RESTRICT deliberado: a ordem diz o que
    -- foi consertado, e `DELETE /inventory/issues/{id}` de avaria citada vira
    -- `409 RESOURCE_IN_USE` na API (`details.maintenance_order_id`).
    CONSTRAINT maintenance_orders_issue_id_fkey
        FOREIGN KEY (issue_id, room_id, item_id)
        REFERENCES inventory_issues(id, room_id, item_id) ON DELETE RESTRICT
);

-- ═══════════════════════ 3. As duas chaves parciais ═════════════════════════
--
-- No máximo UMA ordem não encerrada por avaria. É o `409
-- MAINTENANCE_ORDER_ALREADY_OPEN` do contrato: a API traduz o 23505 NESTE nome.
-- Parcial porque a avaria pode acumular ordens encerradas (a primeira foi
-- cancelada, a segunda consertou) — uma UNIQUE comum recusaria o retrabalho.
CREATE UNIQUE INDEX maintenance_orders_avaria_aberta_idx
    ON maintenance_orders(issue_id)
    WHERE issue_id IS NOT NULL AND status IN ('aberta','em_andamento');

-- Um bloqueio pertence a no máximo uma ordem. Sem ela, duas ordens apontando
-- para a mesma linha de `stay_blocks` fariam o encerramento de uma liberar o
-- calendário no meio do serviço da outra. Cobre também a FK `stay_block_id`.
CREATE UNIQUE INDEX maintenance_orders_bloqueio_idx
    ON maintenance_orders(stay_block_id)
    WHERE stay_block_id IS NOT NULL;

-- ═══════════════════════ 4. A lista de trabalho e as FKs ════════════════════
--
-- A lista de trabalho ("abertas primeiro; entre elas, da mais urgente para a
-- menos e, na mesma prioridade, da mais antiga") NÃO tem índice que a ordene:
-- a escada de prioridade chega como PARÂMETRO (`array_position($1::text[],
-- priority)`, `maintenance.ByUrgency()`), e nenhum índice casa com uma
-- expressão sobre parâmetro. O que um índice pode fazer é entregar barato o
-- conjunto que ela ordena — as ordens não encerradas, que são poucas e são o
-- que a tela do celular abre (`open=true`, `status=aberta|em_andamento`). Elas
-- saem daqui já por antiguidade; a prioridade é ordenada em memória sobre
-- dezenas de linhas.
--
-- Parcial pelo argumento do kanban (§7) e de `inventory_issues_abertas_idx`: as
-- encerradas acumulam para sempre e nunca entram na lista de trabalho. A lista
-- de histórico (sem filtro, ou só encerradas) lê a maior parte da tabela, e
-- `meta.total` conta cada linha filtrada de qualquer jeito: ali nenhum índice
-- ganha de uma varredura, na escala de uma casa de doze unidades.
CREATE INDEX maintenance_orders_abertas_idx
    ON maintenance_orders(property_id, opened_at, id)
    WHERE status IN ('aberta','em_andamento');

-- Histórico de uma unidade, da mais recente para a mais antiga, com desempate;
-- é também o filtro `unit_id` da lista e a FK `unit_id`.
CREATE INDEX maintenance_orders_unidade_idx ON maintenance_orders(unit_id, opened_at DESC, id);

-- As FKs compostas, com as colunas na ordem da FK: é a busca que o Postgres
-- faz ao apagar um cômodo ou uma avaria (`WHERE room_id = $1 AND unit_id =
-- $2`), e servem os filtros `room_id` e `issue_id` da lista. Parciais porque a
-- maioria das ordens não tem cômodo, nem avaria; a igualdade da FK implica
-- `IS NOT NULL`, então o planejador usa o índice parcial na conferência.
-- `maintenance_orders_avaria_aberta_idx` não serve aqui: ela só enxerga as não
-- encerradas, e a FK precisa achar a ordem concluída que cita a avaria.
CREATE INDEX maintenance_orders_comodo_idx
    ON maintenance_orders(room_id, unit_id) WHERE room_id IS NOT NULL;
CREATE INDEX maintenance_orders_avaria_idx
    ON maintenance_orders(issue_id, room_id, item_id) WHERE issue_id IS NOT NULL;

-- "O bem que dá defeito toda temporada" — o filtro `item_id` — e a FK.
CREATE INDEX maintenance_orders_bem_idx
    ON maintenance_orders(item_id, opened_at DESC, id) WHERE item_id IS NOT NULL;

CREATE INDEX maintenance_orders_autor_idx
    ON maintenance_orders(opened_by) WHERE opened_by IS NOT NULL;
CREATE INDEX maintenance_orders_fechador_idx
    ON maintenance_orders(closed_by) WHERE closed_by IS NOT NULL;

COMMENT ON TABLE maintenance_orders IS
    'Ordem de manutenção de uma unidade (spec §12). Cômodo é da unidade (FK composta maintenance_orders_room_id_fkey); avaria traz cômodo e bem dela (maintenance_orders_issue_id_fkey); no máximo uma ordem não encerrada por avaria (maintenance_orders_avaria_aberta_idx). O bloqueio de calendário é stay_block_id, source = maintenance, criado pela API na mesma transação; liberar é stay_blocks.status = cancelled, nunca DELETE. opened_at é o created_at desta tabela.';
COMMENT ON COLUMN maintenance_orders.stay_block_id IS
    'Linha de stay_blocks (source = maintenance) da ordem. Garantido pela API, não pelo banco: a fonte e a unidade da linha apontada. Remarcar um bloqueio terminado ou liberado aponta para linha NOVA; a antiga fica no calendário como história.';
COMMENT ON COLUMN maintenance_orders.cost_cents IS
    'Custo do serviço em centavos. NULL = ainda não lançado; zero é recusado (maintenance_orders_custo_positivo). Concluída ainda aceita o custo pela API; cancelada não.';
COMMENT ON COLUMN maintenance_orders.started_at IS
    'Preenchido por /start. Ordem concluída ou cancelada direto de aberta fica sem ele, e isso é verdade, não lacuna.';

-- ═══════════ 5. A régua de ~25 colunas, conferida e não só prometida ═══════
--
-- Mesmo bloco de 20261007213000, sobre a tabela nova (18 colunas).
DO $$
DECLARE
    v_colunas int;
BEGIN
    SELECT count(*)
      INTO v_colunas
      FROM pg_attribute a
     WHERE a.attrelid = 'maintenance_orders'::regclass
       AND a.attnum > 0
       AND NOT a.attisdropped;

    IF v_colunas > 25 THEN
        RAISE EXCEPTION 'maintenance_orders acima do teto de 25 colunas: % colunas', v_colunas
            USING HINT = 'extraia uma tabela satélite 1:1 — a crm_opportunities de 118 colunas do portal_amimoveis é o antiexemplo do projeto (CLAUDE.md, regra 9)';
    END IF;
END;
$$;
