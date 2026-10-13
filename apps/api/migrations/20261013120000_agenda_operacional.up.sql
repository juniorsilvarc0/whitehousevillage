-- F2-19: a agenda operacional (docs/db.md §9, spec §9 "Desenho").
--
-- §9 projetava, desde 20/08/2026, três tabelas numa linha cada:
--
--     agenda_events(id, property_id, type, title, starts_at, ends_at, all_day,
--                   unit_id?, reservation_id?, contact_id?, assignee_id?, status, notes)
--                   -- type: checkin|checkout|limpeza|manutencao|visita|evento|bloqueio
--     agenda_settings(property_id PK, business_hours jsonb, slot_minutes, buffer_minutes)
--     agenda_blocks(id, property_id, starts_at, ends_at, all_day, reason)
--
-- e faltavam nelas as quatro coisas que o desenho de 13/10/2026 pede: a ORIGEM
-- do evento (o automático segue a reserva; o manual, quem o marcou), os
-- HORÁRIOS da casa como dado (check-in, check-out e a janela de limpeza, que
-- até aqui não existiam em lugar nenhum), quem concluiu e quando, e o que o
-- banco recusa. Contrato na tag Agenda da OpenAPI, regras puras em
-- `internal/domain/agenda`.
--
-- ───────────────────────────────────────────────────────────────────────────
-- O QUE NÃO ESTÁ AQUI, DE PROPÓSITO
-- ───────────────────────────────────────────────────────────────────────────
--
--   * `manutencao` e `bloqueio` (de unidade) NÃO são gravados em
--     `agenda_events`: aparecem na agenda lidos de `stay_blocks` na hora. Uma
--     cópia seria uma segunda fonte, e as duas divergiriam no primeiro
--     bloqueio estendido por um caminho que não soubesse da agenda. O CHECK de
--     `type` é, palavra por palavra, `agenda.Type.Stored()`.
--   * O evento automático não guarda unidade nem titular: lê os dois da
--     reserva (`reservation_units`, `reservations.contact_id`) a cada leitura.
--     Copiados, realocar a unidade ou trocar o titular deixaria a agenda
--     apontando para o apartamento e a pessoa de antes.
--   * A agenda guarda `contact_id`, nunca o nome: a anonimização (LGPD) não
--     pode deixar cópia numa tabela que meio time lê.
--
-- ───────────────────────────────────────────────────────────────────────────
-- O QUE O BANCO GARANTE, E O QUE FICA COM A API
-- ───────────────────────────────────────────────────────────────────────────
--
-- No banco, porque escrita concorrente, seed e `psql` não passam pela API:
--
--   * a reserva tem no máximo UM check-in, UM check-out e UMA limpeza
--     automáticos — índice único parcial. É o que torna o `/confirm` repetido
--     inofensivo, e SELECT antes de INSERT seria a corrida que ele fecha;
--   * dois eventos MANUAIS agendados não ocupam a mesma unidade no mesmo
--     instante — `EXCLUDE`. É a disputa "duas visitas simultâneas na mesma
--     unidade e hora: exatamente uma vence" da spec §9, e o `23P01` vira o
--     mesmo `409 UNIT_NOT_AVAILABLE` da conferência da API;
--   * a forma de cada origem (automático: tipo da reserva, sem unidade, sem
--     titular, sem título; manual: tipo manual, com título) e os estados
--     impossíveis de `status`/`done_at`/`cancelled_at`/`done_by`;
--   * a coerência dos horários da casa: o dia de troca cabe sem a agenda
--     colidir consigo mesma.
--
-- Na API (`agenda.CheckUnit`), e não aqui: a colisão do evento com a ESTADIA,
-- com os eventos automáticos, com a manutenção e com o bloqueio de unidade, e
-- a do manual SEM unidade que ocupa as unidades da reserva. Nenhuma delas tem
-- a unidade na própria linha — a unidade está em `reservation_units` ou em
-- `stay_blocks` —, e `EXCLUDE` só compara colunas da mesma tabela. Isso é
-- aceito de propósito: a venda nunca é recusada por causa da agenda (o
-- conflito que ela cria aparece na leitura, `has_unit_conflict`), e a corrida
-- que importa — duas marcações à mão na mesma unidade — é a que o banco decide.
--
-- Sem gatilho de tempo real e sem gatilho de `updated_at` (nenhuma tabela deste
-- banco tem um): quem escreve o grava.

-- ═══════════════════ 1. Os horários da casa (`agenda_settings`) ═════════════
--
-- Uma linha por propriedade: a PK é a FK. Dez colunas.
--
-- OS VALORES DOS DEFAULTs SÃO PROVISÓRIOS — a confirmar pelo dono — decisão 12
-- do roadmap; o dono edita em Configurações → Agenda (`PUT /agenda/settings`).
-- Check-in 14:00, check-out 11:00, limpeza das 11:00 às 14:00, funcionamento
-- das 08:00 às 20:00 nos sete dias, passo de 30 e folga de 15. Enquanto
-- ninguém gravou, `updated_by` é nulo, e é isso que a API devolve como
-- `provisional: true`. Por isso o seed insere só `property_id` e nunca faz
-- `DO UPDATE`: reescrever a linha apagaria o que o dono confirmou.
CREATE TABLE agenda_settings (
    property_id    uuid PRIMARY KEY REFERENCES properties(id) ON DELETE RESTRICT,

    -- Hora de parede da casa, sem fuso: o fuso é `properties.timezone`, e a
    -- conversão para instante é do Go (`calendar.Zone`), num lugar só.
    checkin_time   time NOT NULL DEFAULT '14:00',
    checkout_time  time NOT NULL DEFAULT '11:00',
    cleaning_start time NOT NULL DEFAULT '11:00',
    cleaning_end   time NOT NULL DEFAULT '14:00',

    -- Funcionamento por dia da semana ISO (1 = segunda … 7 = domingo); dia sem
    -- intervalo é dia fechado. A forma de cada intervalo (dia 1–7, `start <
    -- end`, até 4 por dia, sem sobreposição) é de `agenda.Settings.Validate`:
    -- o banco só confere que é um array, porque CHECK sobre o conteúdo de um
    -- jsonb seria uma segunda cópia da regra, escrita em outra língua.
    business_hours jsonb NOT NULL DEFAULT '[
        {"weekday":1,"start":"08:00","end":"20:00"},
        {"weekday":2,"start":"08:00","end":"20:00"},
        {"weekday":3,"start":"08:00","end":"20:00"},
        {"weekday":4,"start":"08:00","end":"20:00"},
        {"weekday":5,"start":"08:00","end":"20:00"},
        {"weekday":6,"start":"08:00","end":"20:00"},
        {"weekday":7,"start":"08:00","end":"20:00"}
    ]'::jsonb,

    -- O passo da grade das visões de dia e semana, e a duração do check-in e
    -- do check-out.
    slot_minutes   integer NOT NULL DEFAULT 30,
    -- A folga mínima entre dois compromissos da MESMA pessoa (só aviso).
    buffer_minutes integer NOT NULL DEFAULT 15,

    -- Nulo = ninguém gravou ainda = `provisional: true`.
    updated_by     uuid REFERENCES users(id) ON DELETE RESTRICT,
    updated_at     timestamptz NOT NULL DEFAULT now(),

    -- O dia de troca (um hóspede sai, a unidade é limpa, o seguinte entra)
    -- cabe sem a agenda colidir consigo mesma. É a mesma desigualdade de
    -- `agenda.Settings.Validate`, repetida aqui porque a linha tem dois
    -- escritores (a API e o seed) e `psql` não passa por nenhum dos dois.
    CONSTRAINT agenda_settings_dia_de_troca
        CHECK (checkout_time <= cleaning_start
               AND cleaning_start < cleaning_end
               AND cleaning_end <= checkin_time),

    -- De 5 a 240 minutos, e o dia se divide em passos inteiros. CASE, e não
    -- `BETWEEN … AND 1440 % slot_minutes = 0`: o Postgres não garante a ordem
    -- de avaliação de um AND (manual, "Expression Evaluation Rules"), e com
    -- `slot_minutes = 0` o módulo poderia ser avaliado primeiro e devolver
    -- `22012` (divisão por zero) em vez do `23514` nesta constraint, que é o
    -- que a API sabe traduzir.
    CONSTRAINT agenda_settings_slot
        CHECK (CASE WHEN slot_minutes BETWEEN 5 AND 240
                    THEN 1440 % slot_minutes = 0
                    ELSE false
               END),

    CONSTRAINT agenda_settings_buffer
        CHECK (buffer_minutes BETWEEN 0 AND 240),

    -- O check-out é o passo que TERMINA na hora de saída; o check-in, o que
    -- COMEÇA na de entrada. Os dois têm de caber no dia. Separadas de
    -- `_dia_de_troca` para o nome da constraint dizer à API qual campo caiu.
    CONSTRAINT agenda_settings_checkout_cabe
        CHECK (extract(epoch FROM checkout_time) / 60 >= slot_minutes),
    CONSTRAINT agenda_settings_checkin_cabe
        CHECK (extract(epoch FROM checkin_time) / 60 + slot_minutes <= 1440),

    CONSTRAINT agenda_settings_business_hours_array
        CHECK (jsonb_typeof(business_hours) = 'array')
);

CREATE INDEX agenda_settings_editor_idx
    ON agenda_settings(updated_by) WHERE updated_by IS NOT NULL;

COMMENT ON TABLE agenda_settings IS
    'Horários da operação, uma linha por propriedade (spec §9). Os DEFAULTs são PROVISÓRIOS, a confirmar pelo dono — decisão 12 do roadmap; o dono edita em Configurações → Agenda (PUT /agenda/settings, settings:editar). updated_by nulo = ninguém gravou = provisional: true. O seed insere só property_id e nunca reescreve a linha.';
COMMENT ON COLUMN agenda_settings.business_hours IS
    'Array de {weekday (ISO 1–7), start, end ("HH:MM"; end pode ser "24:00")}. Dia sem intervalo é dia fechado. A forma de cada intervalo é validada por agenda.Settings.Validate; o banco só confere que é array.';

-- ═══════════════════════ 2. O evento (`agenda_events`) ══════════════════════
--
-- Vinte colunas. Duas origens:
--
--   * `reservation` — nasceu da reserva confirmada (`agenda.Plan`) e a segue:
--     check-in, check-out ou limpeza, com `reservation_id` e SEM unidade, sem
--     titular e sem título (os três são lidos ou compostos na leitura);
--   * `manual` — marcado em `POST /agenda/events`: visita, evento ou limpeza
--     avulsa, com título; unidade, reserva e cliente opcionais.
--
-- Nada é apagado: desmarcar é `status = 'cancelado'` com `cancelled_at`, e por
-- isso as FKs que saem daqui são RESTRICT — a agenda é histórico de operação.
CREATE TABLE agenda_events (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id    uuid NOT NULL REFERENCES properties(id),

    -- Os vocabulários são, palavra por palavra, as constantes de
    -- `internal/domain/agenda` (`Type.Stored`, `Origin.Stored`, `Status`).
    type           text NOT NULL,
    origin         text NOT NULL,

    -- RESTRICT em todas as FKs deste evento (estas quatro, `done_by` e
    -- `created_by`): reserva, unidade, contato e usuário citados pela
    -- agenda não se apagam — saem de cena pelo estado (`cancelled`,
    -- `active = false`, anonimização, `deleted_at`), e a agenda continua
    -- dizendo o que aconteceu, onde e com quem.
    reservation_id uuid REFERENCES reservations(id) ON DELETE RESTRICT,
    unit_id        uuid REFERENCES units(id) ON DELETE RESTRICT,
    contact_id     uuid REFERENCES contacts(id) ON DELETE RESTRICT,
    -- O responsável — e o eixo do escopo `own` da agenda: `own` é
    -- `assignee_id = <ator>` no SQL.
    assignee_id    uuid REFERENCES users(id) ON DELETE RESTRICT,

    -- Nulo no automático: o título é composto na leitura (`agenda.AutoTitle`),
    -- e a regra de composição muda sem migration.
    title          text,

    -- Janela half-open [starts_at, ends_at), em instantes — como a estadia é
    -- [entrada, saída) em datas. Half-open é o que deixa a limpeza das 11:00
    -- às 14:00 e o check-in das 14:00 encostarem sem colidir.
    starts_at      timestamptz NOT NULL,
    ends_at        timestamptz NOT NULL,
    -- Dia inteiro começa e termina à meia-noite DA CASA (`agenda.CheckWindow`).
    all_day        boolean NOT NULL DEFAULT false,

    status         text NOT NULL DEFAULT 'agendado',
    notes          text,

    done_at        timestamptz,
    done_by        uuid REFERENCES users(id) ON DELETE RESTRICT,
    cancelled_at   timestamptz,

    created_by     uuid REFERENCES users(id) ON DELETE RESTRICT,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT agenda_events_type_check
        CHECK (type IN ('checkin','checkout','limpeza','visita','evento')),
    CONSTRAINT agenda_events_origin_check
        CHECK (origin IN ('reservation','manual')),
    CONSTRAINT agenda_events_status_check
        CHECK (status IN ('agendado','concluido','cancelado')),

    -- Duração positiva. É conferida antes da `EXCLUDE`: CHECK roda antes da
    -- inserção no índice, então a janela vazia ou invertida sai como `23514`
    -- aqui, e nunca como o erro de `tstzrange` com limites trocados.
    CONSTRAINT agenda_events_periodo
        CHECK (ends_at > starts_at),

    -- A forma do automático: tipo da reserva, reserva presente, e nada do que
    -- é lido dela na hora. Sem isto, uma unidade gravada no automático viraria
    -- a segunda fonte que a realocação esqueceria de corrigir.
    CONSTRAINT agenda_events_automatico
        CHECK (origin <> 'reservation'
               OR (type IN ('checkin','checkout','limpeza')
                   AND reservation_id IS NOT NULL
                   AND unit_id IS NULL
                   AND contact_id IS NULL
                   AND title IS NULL
                   AND NOT all_day)),

    -- A forma do manual: check-in e check-out só existem como consequência de
    -- uma reserva (`agenda.ManualTypes`), e o manual tem título.
    CONSTRAINT agenda_events_manual
        CHECK (origin <> 'manual'
               OR (type IN ('visita','evento','limpeza') AND title IS NOT NULL)),

    -- `char_length`, e não `length` em bytes: o `maxLength` do contrato conta
    -- caracteres. Separadas para o nome dizer à API qual regra caiu.
    CONSTRAINT agenda_events_title_nao_vazio
        CHECK (title IS NULL OR btrim(title) <> ''),
    CONSTRAINT agenda_events_title_tamanho
        CHECK (title IS NULL OR char_length(title) <= 200),
    CONSTRAINT agenda_events_notes_tamanho
        CHECK (notes IS NULL OR char_length(notes) <= 2000),

    -- Concluído ⇔ tem instante de conclusão; cancelado ⇔ tem instante de
    -- cancelamento. Mesma forma de `inventory_counts_fechamento`.
    CONSTRAINT agenda_events_conclusao
        CHECK ((status = 'concluido') = (done_at IS NOT NULL)),
    CONSTRAINT agenda_events_cancelamento
        CHECK ((status = 'cancelado') = (cancelled_at IS NOT NULL)),
    -- `done_by` continua anulável (o check-in automático conclui pela reserva,
    -- e a conclusão pode não ter autor); autor de uma conclusão que não
    -- aconteceu é estado impossível. Forma de `maintenance_orders_fechador`.
    CONSTRAINT agenda_events_concluidor
        CHECK (done_by IS NULL OR done_at IS NOT NULL),

    -- Dois eventos MANUAIS agendados não ocupam a mesma unidade ao mesmo
    -- tempo. É a defesa contra as duas visitas simultâneas da spec §9: o
    -- perdedor sai com `23P01`, que a API traduz para o mesmo `409
    -- UNIT_NOT_AVAILABLE` da conferência dela — nunca 500, nunca retry.
    --
    -- O predicado tem as três condições, e cada uma tem motivo: `manual`
    -- porque o automático não tem unidade gravada (o CHECK acima garante;
    -- repetir aqui deixa o índice dizer sozinho o que cobre); `unit_id IS NOT
    -- NULL` porque a reunião sem apartamento não ocupa unidade; `agendado`
    -- porque o concluído e o cancelado não ocupam mais nada, e a visita
    -- desmarcada não pode impedir a seguinte. `[)`: a visita das 10:00 encosta
    -- na das 09:00 às 10:00 sem colidir.
    CONSTRAINT agenda_events_unidade_sem_sobreposicao EXCLUDE USING gist (
        unit_id WITH =,
        tstzrange(starts_at, ends_at, '[)') WITH &&
    ) WHERE (origin = 'manual' AND unit_id IS NOT NULL AND status = 'agendado')
);

-- ═══════════════════════ 3. A chave da agenda que se enche sozinha ══════════
--
-- A reserva tem no máximo um check-in, um check-out e uma limpeza AUTOMÁTICOS.
-- Em qualquer estado, e de propósito: não existe segundo check-in legítimo
-- para a mesma reserva — reserva cancelada não volta a confirmar (remarcar é
-- reserva NOVA, spec §5) —, então o automático cancelado continua ocupando a
-- vaga, e o `/confirm` repetido e o preenchimento das reservas anteriores à
-- agenda ficam idempotentes sem olhar o estado da linha que já está lá. A
-- recusa sai como `23505` NESTE nome; para um INSERT absorvê-la, o alvo do
-- ON CONFLICT repete o predicado:
--   ON CONFLICT (reservation_id, type) WHERE origin = 'reservation' DO NOTHING
-- O manual ligado à reserva (a faxina do meio da estadia) não entra.
CREATE UNIQUE INDEX agenda_events_da_reserva_idx
    ON agenda_events(reservation_id, type)
    WHERE origin = 'reservation';

-- ═══════════════════════ 4. As leituras e as FKs ════════════════════════════
--
-- A janela: "os eventos que CRUZAM [from, to)" é sobreposição de intervalos, e
-- btree não responde isso sem varrer desde o começo da história. A consulta
-- precisa escrever a MESMA expressão do índice para o planejador usá-lo:
--   WHERE property_id = $1
--     AND tstzrange(starts_at, ends_at, '[)') && tstzrange($2, $3, '[)')
-- `property_id` na frente pelo `btree_gist`, como em `stay_no_overlap`. É
-- também o índice da FK `property_id`.
CREATE INDEX agenda_events_janela_idx
    ON agenda_events USING gist (property_id, tstzrange(starts_at, ends_at, '[)'));

-- A ordem da lista (`starts_at`, desempate por `id`) e a paginação estável.
CREATE INDEX agenda_events_inicio_idx
    ON agenda_events(property_id, starts_at, id);

-- O escopo `own` (`assignee_id = <ator>`) e o filtro `assignee_id`, já na
-- ordem da lista; é também a FK. Parcial porque o automático nasce sem
-- responsável e a limpeza que ninguém pegou fica assim.
CREATE INDEX agenda_events_responsavel_idx
    ON agenda_events(assignee_id, starts_at) WHERE assignee_id IS NOT NULL;

-- As FKs majoritariamente nulas, parciais em `IS NOT NULL`: a igualdade da FK
-- implica o predicado, e o planejador usa o índice parcial na conferência.
-- `agenda_events_da_reserva_idx` não serviria à FK da reserva: só enxerga os
-- automáticos, e a conferência precisa achar também o manual ligado a ela. O
-- índice da `EXCLUDE` não serve à FK da unidade pelo mesmo motivo (só os
-- manuais agendados).
CREATE INDEX agenda_events_reserva_idx
    ON agenda_events(reservation_id) WHERE reservation_id IS NOT NULL;
CREATE INDEX agenda_events_unidade_idx
    ON agenda_events(unit_id) WHERE unit_id IS NOT NULL;
CREATE INDEX agenda_events_contato_idx
    ON agenda_events(contact_id) WHERE contact_id IS NOT NULL;
CREATE INDEX agenda_events_concluidor_idx
    ON agenda_events(done_by) WHERE done_by IS NOT NULL;
CREATE INDEX agenda_events_autor_idx
    ON agenda_events(created_by) WHERE created_by IS NOT NULL;

COMMENT ON TABLE agenda_events IS
    'Evento da agenda operacional (spec §9). origin = reservation: check-in, check-out ou limpeza da reserva confirmada, sem unidade, titular nem título — lidos da reserva na hora; no máximo um de cada tipo por reserva (agenda_events_da_reserva_idx). origin = manual: visita, evento ou limpeza avulsa; dois manuais agendados não ocupam a mesma unidade ao mesmo tempo (agenda_events_unidade_sem_sobreposicao, 23P01 → 409 UNIT_NOT_AVAILABLE). manutencao e bloqueio de unidade NÃO são gravados aqui: são lidos de stay_blocks. Nada é apagado: desmarcar é status = cancelado.';
COMMENT ON COLUMN agenda_events.title IS
    'Gravado só no manual. No automático é nulo e composto na leitura (agenda.AutoTitle: tipo + código da reserva).';
COMMENT ON COLUMN agenda_events.assignee_id IS
    'Responsável. É o eixo do escopo own da agenda: own é assignee_id = <ator> no SQL.';

-- ═══════════════ 5. O bloqueio de horário da operação (`agenda_blocks`) ═════
--
-- Nove colunas. "Equipe em treinamento das 14:00 às 16:00": o TEMPO da
-- operação, não uma unidade — tirar unidade de venda é `stay_blocks`
-- (`POST /blocks`). Não recusa nada (gera o aviso `bloqueio_de_horario`), e
-- por isso não tem EXCLUDE: dois bloqueios no mesmo horário são dois avisos.
-- Apaga-se de verdade (`DELETE /agenda/blocks/{id}` → 204): é configuração do
-- tempo, não histórico, e o antes fica em `audit_log`. Sem responsável, e o
-- escopo `own` não se aplica (a API responde 403 `own_scope_not_applicable`).
CREATE TABLE agenda_blocks (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid NOT NULL REFERENCES properties(id),
    starts_at   timestamptz NOT NULL,
    ends_at     timestamptz NOT NULL,
    all_day     boolean NOT NULL DEFAULT false,
    reason      text NOT NULL,
    created_by  uuid REFERENCES users(id) ON DELETE RESTRICT,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT agenda_blocks_periodo
        CHECK (ends_at > starts_at),
    CONSTRAINT agenda_blocks_reason_nao_vazio
        CHECK (btrim(reason) <> ''),
    CONSTRAINT agenda_blocks_reason_tamanho
        CHECK (char_length(reason) <= 200)
);

-- A janela, com a mesma expressão de `agenda_events_janela_idx`; é também o
-- índice da FK `property_id`.
CREATE INDEX agenda_blocks_janela_idx
    ON agenda_blocks USING gist (property_id, tstzrange(starts_at, ends_at, '[)'));
CREATE INDEX agenda_blocks_autor_idx
    ON agenda_blocks(created_by) WHERE created_by IS NOT NULL;

COMMENT ON TABLE agenda_blocks IS
    'Bloqueio de horário da operação (spec §9): o tempo da operação, não uma unidade. Não recusa nada — o evento marcado em cima recebe o aviso bloqueio_de_horario. Apaga-se de verdade (o antes fica em audit_log).';

-- ═══════════ 6. A linha de configuração de toda propriedade que já existe ═══
--
-- Só `property_id`: os valores são os DEFAULTs provisórios, e `updated_by`
-- nulo é o `provisional: true`. Propriedade criada depois desta migration
-- ganha a linha pelo seed (etapa `agenda_settings`, `ON CONFLICT DO NOTHING`).
INSERT INTO agenda_settings (property_id)
SELECT id FROM properties
ON CONFLICT DO NOTHING;

-- ═══════════ 7. A régua de ~25 colunas, conferida e não só prometida ═══════
--
-- Mesmo bloco de 20261007170000, sobre as três tabelas deste módulo (10, 20 e
-- 9 colunas).
DO $$
DECLARE
    v_excesso text;
BEGIN
    SELECT string_agg(format('%s (%s colunas)', tabela, colunas), ', ' ORDER BY tabela)
      INTO v_excesso
      FROM (
        SELECT c.relname AS tabela, count(*) AS colunas
          FROM pg_attribute a
          JOIN pg_class c     ON c.oid = a.attrelid
          JOIN pg_namespace n ON n.oid = c.relnamespace
         WHERE n.nspname = current_schema()
           AND c.relkind = 'r'
           AND c.relname IN ('agenda_settings','agenda_events','agenda_blocks')
           AND a.attnum > 0
           AND NOT a.attisdropped
         GROUP BY c.relname
        HAVING count(*) > 25
      ) t;

    IF v_excesso IS NOT NULL THEN
        RAISE EXCEPTION 'tabela da agenda acima do teto de 25 colunas: %', v_excesso
            USING HINT = 'extraia uma tabela satélite 1:1 — a crm_opportunities de 118 colunas do portal_amimoveis é o antiexemplo do projeto (CLAUDE.md, regra 9)';
    END IF;
END;
$$;
