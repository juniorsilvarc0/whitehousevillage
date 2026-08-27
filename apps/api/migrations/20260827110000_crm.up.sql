-- CRM — funis, leads, oportunidades, atividades e histórico (spec §7, docs/db.md §7).
--
-- O antiexemplo declarado do projeto é a `crm_opportunities` de 118 colunas do
-- portal_amimoveis: uma tabela que era ao mesmo tempo cadastro de cliente,
-- orçamento, ficha de evento e agenda. Aqui a oportunidade fica em 24 colunas e
-- o detalhe de evento vive em satélite. O teto de ~25 colunas do CLAUDE.md é a
-- régua, e ela é conferida ao final desta migration por um bloco que ABORTA se
-- alguém a estourar no futuro.
--
-- O que foi herdado do que funcionava lá: `sla_days` com tarefa automática
-- idempotente ao entrar na etapa, `stage_history` insert-only e a página que
-- carrega tudo numa chamada.

-- ─────────────────────────── Vocabulários ───────────────────────────
--
-- Enum é `text` + `CHECK` (docs/db.md, Convenções): mudar o vocabulário é uma
-- migration de uma linha, não um `ALTER TYPE` que trava a tabela.
--
-- O tipo de atividade aparece em DOIS lugares — `crm_stages.auto_task_type` (o
-- que a etapa manda criar) e `crm_activities.type` (o que foi criado). Os dois
-- CHECKs carregam a mesma lista de propósito: se divergirem, a etapa passa a
-- pedir uma tarefa que a tabela de atividades recusa, e a automação quebra na
-- hora em que o card é movido — nunca no cadastro.

CREATE TABLE crm_pipelines (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid NOT NULL REFERENCES properties(id),
    name        text NOT NULL,
    is_default  boolean NOT NULL DEFAULT false,
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (property_id, name)          -- chave natural do seed
);

-- Um funil padrão, e só um. Sem esta parcial, dois `is_default = true` fariam a
-- tela abrir no funil que o `ORDER BY` sorteasse — e "às vezes abre no outro
-- funil" é o tipo de defeito que ninguém consegue reproduzir.
CREATE UNIQUE INDEX crm_pipelines_default_idx ON crm_pipelines(property_id) WHERE is_default;

CREATE TABLE crm_stages (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id        uuid NOT NULL REFERENCES crm_pipelines(id) ON DELETE CASCADE,
    name               text NOT NULL,
    position           int  NOT NULL CHECK (position >= 0),
    probability        numeric(5,2) NOT NULL DEFAULT 0 CHECK (probability BETWEEN 0 AND 100),
    color              text NOT NULL DEFAULT '#94A3B8' CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
    type               text NOT NULL CHECK (type IN ('aberto','ganho','perdido')),
    sla_days           int CHECK (sla_days IS NULL OR sla_days > 0),
    auto_task_subject  text,
    auto_task_type     text CHECK (auto_task_type IS NULL OR
                                   auto_task_type IN ('tarefa','ligacao','reuniao','email','whatsapp','nota')),
    auto_task_due_days int CHECK (auto_task_due_days IS NULL OR auto_task_due_days >= 0),
    auto_notify        boolean NOT NULL DEFAULT false,
    active             boolean NOT NULL DEFAULT true,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    UNIQUE (pipeline_id, name),         -- chave natural do seed

    -- Tarefa automática pela metade não existe: assunto sem tipo não sabe que
    -- atividade criar, e assunto sem prazo cria tarefa que nunca vence — que é
    -- o mesmo que não criar, só que ocupando a lista do corretor.
    CONSTRAINT crm_stages_auto_task_completa CHECK (
        (auto_task_subject IS NULL AND auto_task_type IS NULL AND auto_task_due_days IS NULL)
        OR
        (auto_task_subject IS NOT NULL AND auto_task_type IS NOT NULL AND auto_task_due_days IS NOT NULL)
    )
);

-- `position` é única DENTRO do funil, e a constraint é ADIÁVEL — aqui, ao
-- contrário da invariante da casa inteira (20260827100000), a forma adiável
-- pura serve: é uma `UNIQUE`, e `UNIQUE` aceita `DEFERRABLE`.
--
-- Adiar é o que permite reordenar o kanban. Arrastar a etapa 5 para a posição 2
-- é um `UPDATE` que reescreve quatro linhas; com a unicidade imediata, a
-- PRIMEIRA linha reescrita já colidiria com a posição que a segunda ainda não
-- liberou, e a única saída seria o truque feio de mandar todo mundo para
-- posições negativas antes de renumerar.
CREATE UNIQUE INDEX crm_stages_posicao_idx ON crm_stages(pipeline_id, position);
ALTER TABLE crm_stages ADD CONSTRAINT crm_stages_posicao_unica
    UNIQUE USING INDEX crm_stages_posicao_idx DEFERRABLE INITIALLY DEFERRED;

COMMENT ON COLUMN crm_stages.sla_days IS
    'Prazo em dias para a oportunidade sair desta etapa. Alimenta o alerta de SLA estourado, calculado no servidor sobre entered_stage_at. NULL nas etapas terminais: não há prazo para sair de Ganho.';
COMMENT ON COLUMN crm_stages.auto_task_due_days IS
    'Prazo da tarefa automática, em dias a partir da entrada na etapa. Zero significa "para hoje".';

-- ─────────────────────────── Leads ──────────────────────────────────
--
-- O lead é a ORIGEM (de onde a pessoa veio, o que ela procurava); a
-- oportunidade é a NEGOCIAÇÃO. Separados porque um mesmo lead pode virar duas
-- negociações — o Réveillon deste ano e o casamento do ano que vem — e juntá-los
-- obrigaria a escolher qual das duas apaga a outra.
CREATE TABLE crm_leads (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id           uuid NOT NULL REFERENCES properties(id),
    contact_id            uuid NOT NULL REFERENCES contacts(id),
    source                text NOT NULL DEFAULT 'manual',
    -- Sem FK de propósito, pelo mesmo motivo de `users.broker_id` (docs/db.md
    -- §2): `crm_campaigns` só nasce com o módulo de campanhas, e FK não aponta
    -- para tabela que ainda não existe. A FK entra junto com a tabela.
    campaign_id           uuid,
    status                text NOT NULL DEFAULT 'novo'
                              CHECK (status IN ('novo','em_atendimento','qualificado','convertido','descartado')),
    score                 int NOT NULL DEFAULT 0 CHECK (score BETWEEN 0 AND 100),
    interest_unit_type_id uuid REFERENCES unit_types(id),
    desired_check_in      date,
    desired_check_out     date,
    guests_count          int CHECK (guests_count IS NULL OR guests_count > 0),
    owner_id              uuid REFERENCES users(id),
    notes                 text,
    converted_at          timestamptz,
    created_by            uuid REFERENCES users(id),
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),

    -- Mesma meia-aberta das reservas: a data pretendida é a mesma grandeza que
    -- vai virar `[check_in, check_out)` no orçamento.
    CONSTRAINT crm_leads_datas CHECK (
        desired_check_in IS NULL OR desired_check_out IS NULL OR desired_check_out > desired_check_in),
    CONSTRAINT crm_leads_convertido CHECK ((status = 'convertido') = (converted_at IS NOT NULL))
);
CREATE INDEX crm_leads_contact_idx  ON crm_leads(contact_id);
CREATE INDEX crm_leads_owner_idx    ON crm_leads(owner_id)    WHERE owner_id IS NOT NULL;
CREATE INDEX crm_leads_campaign_idx ON crm_leads(campaign_id) WHERE campaign_id IS NOT NULL;
CREATE INDEX crm_leads_produto_idx  ON crm_leads(interest_unit_type_id) WHERE interest_unit_type_id IS NOT NULL;
CREATE INDEX crm_leads_criador_idx  ON crm_leads(created_by)  WHERE created_by IS NOT NULL;
-- A fila de trabalho: leads abertos, do mais novo para o mais velho.
CREATE INDEX crm_leads_fila_idx ON crm_leads(property_id, status, created_at DESC);

-- ─────────────────────────── Motivos de perda ───────────────────────
CREATE TABLE crm_lost_reasons (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid NOT NULL REFERENCES properties(id),
    label       text NOT NULL,
    sort_order  int  NOT NULL DEFAULT 0,
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (property_id, label)         -- chave natural do seed
);

-- ─────────────────────────── Oportunidades ──────────────────────────
--
-- 24 colunas. O que ficou de fora e por quê:
--   · detalhe de evento (convidados, buffet, montagem) → satélite
--     `crm_opportunity_event_details`, porque só ~1 em cada 10 oportunidades é
--     evento e a coluna vazia nas outras nove é o começo das 118;
--   · histórico de etapa → `crm_stage_history`, porque é fato datado e
--     insert-only, não estado;
--   · notas e atividades → tabelas próprias, pela mesma razão.
CREATE TABLE crm_opportunities (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id    uuid NOT NULL REFERENCES properties(id),
    -- Contato OBRIGATÓRIO (spec §7). Negociação sem pessoa do outro lado é
    -- card no quadro sem ninguém para ligar.
    contact_id     uuid NOT NULL REFERENCES contacts(id),
    lead_id        uuid REFERENCES crm_leads(id) ON DELETE SET NULL,
    pipeline_id    uuid NOT NULL REFERENCES crm_pipelines(id),
    stage_id       uuid NOT NULL REFERENCES crm_stages(id),
    title          text NOT NULL,
    unit_type_id   uuid REFERENCES unit_types(id),
    check_in       date,
    check_out      date,
    guests_count   int CHECK (guests_count IS NULL OR guests_count > 0),
    -- O orçamento vigente É uma reserva em `quote` (spec §5): é ela que carrega
    -- as noites, a tarifa congelada e a política. Apontar para ela em vez de
    -- copiar o valor é o que faz "ganhar cria a reserva com o orçamento
    -- vigente, sem redigitar nada" ser um `UPDATE` de status, e não um
    -- recálculo que pode dar outro número.
    quote_id       uuid REFERENCES reservations(id) ON DELETE SET NULL,
    reservation_id uuid REFERENCES reservations(id) ON DELETE SET NULL,
    amount_cents   bigint NOT NULL DEFAULT 0 CHECK (amount_cents >= 0),
    probability    numeric(5,2) NOT NULL DEFAULT 0 CHECK (probability BETWEEN 0 AND 100),
    expected_close date,
    owner_id       uuid REFERENCES users(id),
    status         text NOT NULL DEFAULT 'aberto' CHECK (status IN ('aberto','ganho','perdido')),
    lost_reason_id uuid REFERENCES crm_lost_reasons(id),
    entered_stage_at timestamptz NOT NULL DEFAULT now(),
    closed_at      timestamptz,
    created_by     uuid REFERENCES users(id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT crm_opportunities_datas CHECK (
        check_in IS NULL OR check_out IS NULL OR check_out > check_in),

    -- "`perdido` exige motivo" (spec §7) é regra de negócio explícita, e o
    -- lugar dela é aqui: perda sem motivo torna o relatório de perdas — que é
    -- o único jeito de descobrir se o problema é preço ou disponibilidade —
    -- uma coluna de "outros".
    CONSTRAINT crm_opportunities_perda_motivada CHECK (
        status <> 'perdido' OR lost_reason_id IS NOT NULL),

    -- Motivo de perda em oportunidade que não foi perdida é lixo que sobrevive
    -- a uma reabertura e mente no relatório.
    CONSTRAINT crm_opportunities_motivo_so_na_perda CHECK (
        status = 'perdido' OR lost_reason_id IS NULL),

    -- Fechada tem data de fechamento; aberta não tem. Sem isto, "ganhamos em
    -- que mês?" não tem resposta, e o funil de conversão não fecha.
    CONSTRAINT crm_opportunities_fechamento CHECK (
        (status = 'aberto') = (closed_at IS NULL))
);

COMMENT ON TABLE crm_opportunities IS
    'A negociação. 24 colunas por escolha: o detalhe de evento vive em crm_opportunity_event_details e o histórico em crm_stage_history. A tabela larga de 118 colunas do sistema de referência é o antiexemplo declarado do projeto.';
COMMENT ON COLUMN crm_opportunities.quote_id IS
    'Orçamento vigente: uma reserva em status quote. Ganhar a oportunidade promove ESTA reserva em vez de recalcular — recalcular pode dar outro número que o hóspede nunca ouviu.';
COMMENT ON COLUMN crm_opportunities.entered_stage_at IS
    'Instante da entrada na etapa atual. É o relógio do SLA (crm_stages.sla_days) e a base do alerta "parado há N dias". Reescrito a cada mudança de etapa, junto com a linha de crm_stage_history.';

-- O kanban: cartões ABERTOS de um funil, coluna por coluna, e o corretor vendo
-- só os próprios (scope='own' vira `AND owner_id = $usuario`). A ordem das
-- colunas do índice é a ordem dos filtros — funil sempre, etapa quase sempre,
-- dono quando o escopo é `own` — e `entered_stage_at` fecha porque é por ele
-- que o cartão mais parado sobe no topo da coluna.
--
-- Parcial em `status = 'aberto'`: o quadro nunca desenha ganho nem perdido, e
-- esses dois acumulam para sempre. Sem o predicado, o índice do caminho mais
-- quente do CRM cresceria monotonicamente com dado que ele nunca lê.
CREATE INDEX crm_opportunities_kanban_idx
    ON crm_opportunities(pipeline_id, stage_id, owner_id, entered_stage_at)
    WHERE status = 'aberto';

-- O relatório de fechamento (ganhos e perdas por mês) e a previsão.
CREATE INDEX crm_opportunities_fechadas_idx
    ON crm_opportunities(property_id, status, closed_at DESC) WHERE status <> 'aberto';
CREATE INDEX crm_opportunities_previsao_idx
    ON crm_opportunities(property_id, expected_close)
    WHERE status = 'aberto' AND expected_close IS NOT NULL;

-- Toda FK indexada (docs/db.md, Convenções). Parciais nas majoritariamente
-- nulas, pela mesma razão de `reservations`.
CREATE INDEX crm_opportunities_contact_idx  ON crm_opportunities(contact_id);
CREATE INDEX crm_opportunities_stage_idx    ON crm_opportunities(stage_id);
CREATE INDEX crm_opportunities_lead_idx     ON crm_opportunities(lead_id)        WHERE lead_id IS NOT NULL;
CREATE INDEX crm_opportunities_quote_idx    ON crm_opportunities(quote_id)       WHERE quote_id IS NOT NULL;
CREATE INDEX crm_opportunities_reserva_idx  ON crm_opportunities(reservation_id) WHERE reservation_id IS NOT NULL;
CREATE INDEX crm_opportunities_produto_idx  ON crm_opportunities(unit_type_id)   WHERE unit_type_id IS NOT NULL;
CREATE INDEX crm_opportunities_motivo_idx   ON crm_opportunities(lost_reason_id) WHERE lost_reason_id IS NOT NULL;
CREATE INDEX crm_opportunities_criador_idx  ON crm_opportunities(created_by)     WHERE created_by IS NOT NULL;
CREATE INDEX crm_opportunities_owner_idx    ON crm_opportunities(owner_id)       WHERE owner_id IS NOT NULL;

-- ─────────────────────────── Satélite de evento ─────────────────────
CREATE TABLE crm_opportunity_event_details (
    opportunity_id   uuid PRIMARY KEY REFERENCES crm_opportunities(id) ON DELETE CASCADE,
    event_type       text,
    guests_expected  int CHECK (guests_expected IS NULL OR guests_expected > 0),
    needs_catering   boolean NOT NULL DEFAULT false,
    setup_starts_at  timestamptz,
    teardown_ends_at timestamptz,
    notes            text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT crm_event_details_janela CHECK (
        setup_starts_at IS NULL OR teardown_ends_at IS NULL OR teardown_ends_at > setup_starts_at)
);
COMMENT ON TABLE crm_opportunity_event_details IS
    'Satélite 1:1 da oportunidade, só para evento. A PK É a FK: é isso que faz o 1:1. Existe para que oito colunas de festa não fiquem nulas nas nove oportunidades de hospedagem simples a cada dez.';

-- ─────────────────────────── Histórico de etapa ─────────────────────
--
-- Insert-only por contrato do time, como `reservation_events` e
-- `ledger_entries`: nunca `UPDATE`, nunca `DELETE`. Não vira trigger de
-- proibição porque o `ON DELETE CASCADE` da oportunidade precisa poder limpar a
-- linha no expurgo de LGPD — um trigger que barrasse `DELETE` transformaria o
-- direito de eliminação num erro de integridade.
CREATE TABLE crm_stage_history (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    opportunity_id uuid NOT NULL REFERENCES crm_opportunities(id) ON DELETE CASCADE,
    from_stage_id  uuid REFERENCES crm_stages(id),   -- NULL na criação do card
    to_stage_id    uuid NOT NULL REFERENCES crm_stages(id),
    user_id        uuid REFERENCES users(id),
    reason         text,
    at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT crm_stage_history_movimento CHECK (from_stage_id IS DISTINCT FROM to_stage_id)
);
-- A timeline da página `/full`: do mais recente para o mais antigo.
CREATE INDEX crm_stage_history_idx      ON crm_stage_history(opportunity_id, at DESC);
CREATE INDEX crm_stage_history_para_idx ON crm_stage_history(to_stage_id);
CREATE INDEX crm_stage_history_de_idx   ON crm_stage_history(from_stage_id) WHERE from_stage_id IS NOT NULL;
CREATE INDEX crm_stage_history_user_idx ON crm_stage_history(user_id) WHERE user_id IS NOT NULL;

-- ─────────────────────────── Atividades ─────────────────────────────
CREATE TABLE crm_activities (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id    uuid NOT NULL REFERENCES properties(id),
    type           text NOT NULL CHECK (type IN ('tarefa','ligacao','reuniao','email','whatsapp','nota')),
    subject        text NOT NULL,
    description    text,
    due_at         timestamptz,
    done_at        timestamptz,
    status         text NOT NULL DEFAULT 'pendente'
                       CHECK (status IN ('pendente','concluida','cancelada')),
    priority       text NOT NULL DEFAULT 'normal' CHECK (priority IN ('baixa','normal','alta')),
    lead_id        uuid REFERENCES crm_leads(id)          ON DELETE CASCADE,
    opportunity_id uuid REFERENCES crm_opportunities(id)  ON DELETE CASCADE,
    contact_id     uuid REFERENCES contacts(id),
    stage_id       uuid REFERENCES crm_stages(id),
    owner_id       uuid REFERENCES users(id),
    auto           boolean NOT NULL DEFAULT false,
    created_by     uuid REFERENCES users(id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    -- Atividade solta não existe: tarefa que não aponta para lead, oportunidade
    -- ou contato não aparece em tela nenhuma e nunca mais é vista.
    CONSTRAINT crm_activities_tem_vinculo CHECK (
        num_nonnulls(lead_id, opportunity_id, contact_id) >= 1),

    -- Concluída tem quando; não-concluída não tem. É o par de `done_at` com o
    -- status, e é o que impede "concluída sem data" de contaminar o tempo médio
    -- de atendimento.
    CONSTRAINT crm_activities_conclusao CHECK ((status = 'concluida') = (done_at IS NOT NULL)),

    -- Tarefa automática nasce de uma etapa e com prazo — é isso que ela é.
    CONSTRAINT crm_activities_auto_tem_etapa CHECK (
        NOT auto OR (stage_id IS NOT NULL AND opportunity_id IS NOT NULL AND due_at IS NOT NULL))
);

-- ═══ A idempotência da tarefa automática vira REGRA DO BANCO ═══
--
-- A spec §7 pede: "entrar numa etapa cria UMA tarefa automática (idempotente:
-- não duplica se já existe pendente da mesma etapa)". Implementar isso como
-- `SELECT ... IF NOT EXISTS THEN INSERT` é o mesmo TOCTOU da regra 2 do
-- CLAUDE.md: dois cliques no mesmo card, duas abas abertas, ou o `POST` que o
-- navegador repetiu, e o corretor abre a manhã com a mesma ligação duas vezes
-- na lista.
--
-- Com a parcial única, a segunda inserção estoura `23505` e o repositório usa
-- `ON CONFLICT DO NOTHING` — a idempotência deixa de depender de quem escreve o
-- código da automação. O predicado tem `status = 'pendente'` porque a etapa
-- PODE gerar tarefa nova depois que a anterior foi concluída (o card voltou
-- para "Negociação" e o follow-up recomeça); o que não pode é haver duas
-- pendentes ao mesmo tempo.
CREATE UNIQUE INDEX crm_activities_auto_idx
    ON crm_activities(opportunity_id, stage_id)
    WHERE auto AND status = 'pendente';

-- A caixa de entrada do corretor: o que é meu e está vencendo. Ordem
-- (dono, prazo) porque o filtro por dono é igualdade e o de prazo é faixa.
CREATE INDEX crm_activities_minhas_idx ON crm_activities(owner_id, due_at)
    WHERE status = 'pendente';

-- A varredura de alertas do servidor (SLA estourado, tarefa vencida): roda por
-- propriedade e por prazo, sem dono. É índice separado, e não o mesmo de cima,
-- porque `owner_id` na frente forçaria varrer o índice inteiro para achar o que
-- venceu — e o corretor sem tarefa nenhuma não pode custar uma leitura a mais
-- no alerta de todo mundo.
CREATE INDEX crm_activities_vencidas_idx ON crm_activities(property_id, due_at)
    WHERE status = 'pendente' AND due_at IS NOT NULL;

CREATE INDEX crm_activities_opp_idx     ON crm_activities(opportunity_id) WHERE opportunity_id IS NOT NULL;
CREATE INDEX crm_activities_lead_idx    ON crm_activities(lead_id)        WHERE lead_id IS NOT NULL;
CREATE INDEX crm_activities_contact_idx ON crm_activities(contact_id)     WHERE contact_id IS NOT NULL;
CREATE INDEX crm_activities_stage_idx   ON crm_activities(stage_id)       WHERE stage_id IS NOT NULL;
CREATE INDEX crm_activities_criador_idx ON crm_activities(created_by)     WHERE created_by IS NOT NULL;

-- ─────────────────────────── Notas ──────────────────────────────────
CREATE TABLE crm_notes (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id    uuid NOT NULL REFERENCES properties(id),
    body           text NOT NULL CHECK (length(btrim(body)) > 0),
    pinned         boolean NOT NULL DEFAULT false,
    lead_id        uuid REFERENCES crm_leads(id)         ON DELETE CASCADE,
    opportunity_id uuid REFERENCES crm_opportunities(id) ON DELETE CASCADE,
    contact_id     uuid REFERENCES contacts(id),
    created_by     uuid REFERENCES users(id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT crm_notes_tem_vinculo CHECK (
        num_nonnulls(lead_id, opportunity_id, contact_id) >= 1)
);
CREATE INDEX crm_notes_opp_idx     ON crm_notes(opportunity_id, created_at DESC) WHERE opportunity_id IS NOT NULL;
CREATE INDEX crm_notes_lead_idx    ON crm_notes(lead_id, created_at DESC)        WHERE lead_id IS NOT NULL;
CREATE INDEX crm_notes_contact_idx ON crm_notes(contact_id, created_at DESC)     WHERE contact_id IS NOT NULL;
CREATE INDEX crm_notes_criador_idx ON crm_notes(created_by) WHERE created_by IS NOT NULL;

-- ═══════════ A régua de ~25 colunas deixa de ser boa intenção ═══════════
--
-- A regra 9 do CLAUDE.md ("máximo ~25 colunas por tabela") já foi violada uma
-- vez nesta árvore: `reservation_pricing` nasceu com 32 e custou a migration
-- 20260826100000 para ser desmontada. Regra que só existe em texto é regra que
-- volta. Este bloco falha a migration se qualquer tabela do CRM passar do teto
-- — inclusive numa migration futura que só some uma coluna "rapidinho".
DO $$
DECLARE
    v_excesso text;
BEGIN
    SELECT string_agg(format('%s (%s colunas)', tabela, colunas), ', ' ORDER BY tabela)
      INTO v_excesso
      FROM (
        SELECT c.relname AS tabela, count(*) AS colunas
          FROM pg_attribute a
          JOIN pg_class c ON c.oid = a.attrelid
          JOIN pg_namespace n ON n.oid = c.relnamespace
         WHERE n.nspname = current_schema()
           AND c.relkind = 'r'
           AND c.relname LIKE 'crm\_%'
           AND a.attnum > 0
           AND NOT a.attisdropped
         GROUP BY c.relname
        HAVING count(*) > 25
      ) t;

    IF v_excesso IS NOT NULL THEN
        RAISE EXCEPTION 'tabela do CRM acima do teto de 25 colunas: %', v_excesso
            USING HINT = 'extraia uma tabela satélite, como crm_opportunity_event_details — a de 118 colunas do portal_amimoveis é o antiexemplo do projeto (CLAUDE.md, regra 9)';
    END IF;
END;
$$;
