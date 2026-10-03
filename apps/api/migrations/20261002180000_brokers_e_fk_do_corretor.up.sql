-- F2-09 — `brokers` existe, e `broker_id` deixa de aceitar qualquer coisa.
--
-- Medido antes desta migration, no banco com todas as anteriores aplicadas:
--
--     SELECT count(*) FROM information_schema.tables
--      WHERE table_schema = 'public' AND table_name = 'brokers';          → 0
--     SELECT table_name, column_name FROM information_schema.columns
--      WHERE table_schema = 'public' AND column_name ILIKE '%broker%';
--                                      → reservations.broker_id, users.broker_id
--     SELECT conname FROM pg_constraint WHERE conname ILIKE '%broker%';   → 0 linhas
--
-- E com a API no ar (backlog F2-09): `POST /reservations` com um UUID gerado na
-- hora respondeu 201 (`WH-2026-0008`), e o corretor `own` gravou a venda no id
-- de OUTRO usuário, também 201 (`WH-2026-0009`). O valor ia do corpo para o
-- INSERT sem nada no caminho. A partir do F2-13 é esta coluna que decide para
-- quem vai a comissão: campo de dinheiro que o beneficiário escreve à vontade.
--
-- A garantia é do BANCO, não do service: conferir com SELECT e gravar depois é
-- TOCTOU (o corretor pode ser excluído entre as duas instruções), e só a FK vale
-- sob concorrência. O `23503` é traduzido para `422 VALIDATION_ERROR` em
-- `details.broker_id` pelo `backend-go` — o nome da constraint está fixado
-- abaixo para isso.
--
-- O que a FK NÃO fecha, dito aqui para ninguém achar que fechou: o corretor
-- atribuir a venda a OUTRO corretor que existe. Essa metade é autorização, não
-- integridade, e é do F2-13 (`commission.ResolveBroker`, escopo `own` só grava o
-- `users.broker_id` do próprio ator).

-- ═══════════════════════════ 1. A tabela ═══════════════════════════════
--
-- O cadastro comercial do corretor (spec §11). Não é a conta de login: o
-- corretor parceiro pode existir sem acesso ao painel, e a conta pode trocar de
-- dono sem a carteira trocar. Por isso são duas tabelas e `user_id` é anulável.
--
-- Nove colunas — longe do teto de 25 (CLAUDE.md regra 9).
--
-- `commission_rule_id`, previsto em docs/db.md §10, NÃO nasce agora:
-- `commission_rules` só existe no F2-10, e a convenção do projeto é a FK nascer
-- junto com a tabela que ela referencia — a mesma que deixou `users.broker_id`
-- (20260820140000) e `crm_leads.campaign_id` (20260827110000) sem FK até o alvo
-- existir. Criar a coluna agora seria criar exatamente o que esta migration
-- existe para fechar: um uuid que aceita qualquer coisa.
CREATE TABLE brokers (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid NOT NULL REFERENCES properties(id),

    -- "Uma pessoa, um registro" (spec §6): o corretor é um contato, como o
    -- hóspede e o proprietário. Nome, telefone e documento vivem LÁ — e é por
    -- isso que a anonimização da LGPD alcança o corretor sem tocar a comissão.
    -- RESTRICT porque contato com cadastro comercial não pode sumir: o
    -- `DELETE /contacts/{id}` precisa contar este vínculo (handoff do F2-09).
    contact_id  uuid NOT NULL REFERENCES contacts(id) ON DELETE RESTRICT,

    -- A conta de login, quando o corretor tem uma. RESTRICT pelo mesmo motivo
    -- do contato; conta de usuário se desativa (`active`, `deleted_at`), não se
    -- apaga.
    user_id     uuid REFERENCES users(id) ON DELETE RESTRICT,

    -- Meta MENSAL de vendas (spec §11), em centavos. Zero é "sem meta
    -- definida": meta de zero reais não é meta, e o NOT NULL poupa toda leitura
    -- do painel de tratar NULL e zero como duas coisas.
    goal_cents  bigint NOT NULL DEFAULT 0,

    -- Corretor com venda NÃO se apaga (as FKs abaixo são RESTRICT): sai de cena
    -- desativado. `active` é o soft delete desta tabela, e cobre o caso inteiro
    -- — uma coluna `deleted_at` ao lado diria a mesma coisa de outro jeito.
    active      boolean NOT NULL DEFAULT true,

    -- Mesmo padrão de autor e instantes das tabelas do CRM e de `quotes`: quem
    -- MUDOU o quê fica em `audit_log`, com antes e depois — `updated_by` aqui
    -- guardaria só o último, e a pergunta que importa nesta tabela ("quem
    -- trocou a meta?") quer o histórico inteiro.
    created_by  uuid REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT brokers_goal_cents_check CHECK (goal_cents >= 0),

    -- Uma pessoa, um cadastro de corretor. Dois cadastros para o mesmo contato
    -- dividiriam vendas, metas e comissões da mesma pessoa entre dois ids, e o
    -- "desempenho por corretor" (spec §15) contaria uma pessoa como duas.
    CONSTRAINT brokers_contact_id_key UNIQUE (contact_id),

    -- Uma conta, no máximo um cadastro. NULL não colide com NULL (NULLS
    -- DISTINCT, o padrão do Postgres): corretor parceiro sem login são muitas
    -- linhas com `user_id` nulo, e é isso que "único quando presente" quer dizer.
    CONSTRAINT brokers_user_id_key UNIQUE (user_id),

    -- Alvo da FK composta de `users` (seção 3). `id` sozinho já é único, então
    -- esta constraint não restringe nada aqui — ela existe porque FK só aponta
    -- para colunas cobertas por UNIQUE, e é o par (id, user_id) que amarra a
    -- conta ao cadastro DELA.
    CONSTRAINT brokers_id_user_id_key UNIQUE (id, user_id)
);

-- Toda FK indexada. `contact_id` e `user_id` já têm índice pelas UNIQUE acima.
-- `property_id` é o filtro de toda listagem de corretores; `created_by` é
-- parcial, como nas tabelas do CRM, porque a linha criada por seed ou script
-- nasce sem autor.
CREATE INDEX brokers_property_idx ON brokers(property_id);
CREATE INDEX brokers_criador_idx  ON brokers(created_by) WHERE created_by IS NOT NULL;

COMMENT ON TABLE brokers IS
    'Cadastro comercial do corretor (spec §11). A pessoa vive em contacts; a conta de login, quando há, em users. É o alvo de reservations.broker_id — a coluna que decide para quem vai a comissão.';
COMMENT ON COLUMN brokers.user_id IS
    'Conta de login do corretor, quando ele tem acesso ao painel. Única quando presente. É o dono da linha para o escopo own do recurso brokers.';
COMMENT ON COLUMN brokers.goal_cents IS
    'Meta mensal de vendas em centavos. 0 = sem meta definida.';

-- ═══════════════ 2. Os valores que já estão lá — os órfãos ═══════════════
--
-- `ADD CONSTRAINT ... FOREIGN KEY` valida as linhas existentes, e nos bancos de
-- desenvolvimento há `broker_id` inventado (as duas reservas da medida do
-- backlog, e as contas de corretor que a suíte de integração liga a um UUID
-- qualquer). Como `brokers` acabou de nascer VAZIA, todo `broker_id` não nulo é
-- órfão por definição: não existe — nem existiu — cadastro para onde ele
-- aponte. Sem este bloco a migration abortaria em todo banco que já foi usado.
--
-- A DECISÃO: anular. E por que isso não fere a regra 7 do CLAUDE.md ("toda
-- entidade financeira congela o que usou"): congelar é proteger o FATO que
-- gerou dinheiro — a tarifa da noite, a versão da política. Aqui não há fato.
-- Nenhuma comissão existe (`commissions` só nasce no F2-10), `broker_id` não
-- entra no snapshot de `reservation_pricing`, e o valor não identifica pessoa
-- nenhuma a quem pagar. `NULL` é a única leitura verdadeira do que essa venda
-- sabe sobre o corretor: nada. É venda direta até alguém com escopo `all`
-- atribuí-la — e essa escrita, sim, passa a ser auditada e conferida pela FK.
--
-- Por que não abortar, como 20260827100000 aborta o impasse da casa inteira:
-- lá o dado pede decisão humana com telefone na mão (quem fica com a unidade?).
-- Aqui não há decisão a tomar — não existe corretor para escolher.
--
-- O QUE NÃO SE PERDE: cada anulação vira uma linha em `audit_log`, com o valor
-- antigo em `before`, ator nulo (a migration não é usuário) e `request_id`
-- carimbado com a versão. `RAISE WARNING` sozinho não bastaria: aparece no log
-- do `cmd/migrate` e no do servidor, e some com eles. A trilha fica consultável:
--
--     SELECT entity_id, before->>'code', before->>'broker_id', at
--       FROM audit_log
--      WHERE request_id = 'migration:20261002180000';
--
-- Há um caso em que o UUID anulado JÁ FOI um corretor de verdade: `down`
-- seguido de `up`. O `down` derruba `brokers` e deixa os ids para trás (ver o
-- `.down.sql`); esta `up` os encontra órfãos e os registra aqui. Se for preciso
-- refazer uma atribuição, ela sai desta consulta, com escopo `all` e auditoria
-- — não por migration. Em banco de desenvolvimento, `make seed` religa a conta
-- do corretor.
--
-- `updated_at` sobe: a linha mudou, e quem sincroniza por ele precisa saber.
-- O gatilho de tempo real de `reservations` não acorda — `broker_id` não está
-- no `WHEN` dele, e o mapa não desenha corretor.
DO $$
DECLARE
    v_reservas int;
    v_codigos  text;
    v_contas   int;
BEGIN
    WITH alvo AS (
        SELECT r.id, r.property_id, r.code, r.broker_id
          FROM reservations r
         WHERE r.broker_id IS NOT NULL
           AND NOT EXISTS (SELECT 1 FROM brokers b WHERE b.id = r.broker_id)
           FOR UPDATE
    ), anulada AS (
        UPDATE reservations r
           SET broker_id = NULL, updated_at = now()
          FROM alvo
         WHERE r.id = alvo.id
        RETURNING alvo.id, alvo.property_id, alvo.code, alvo.broker_id
    ), trilha AS (
        INSERT INTO audit_log (property_id, actor_id, action, entity, entity_id,
                               before, after, request_id)
        SELECT a.property_id, NULL, 'reservations.broker_orfao_anulado',
               'reservations', a.id,
               jsonb_build_object('code', a.code, 'broker_id', a.broker_id),
               jsonb_build_object('code', a.code, 'broker_id', NULL),
               'migration:20261002180000'
          FROM anulada a
        RETURNING 1
    )
    SELECT (SELECT count(*) FROM trilha),
           (SELECT string_agg(code, ', ' ORDER BY code) FROM anulada)
      INTO v_reservas, v_codigos;

    -- As contas: mesma regra. O e-mail NÃO vai para a mensagem nem para a
    -- trilha — é dado pessoal, e o id basta para achar a conta.
    WITH alvo AS (
        SELECT u.id, u.property_id, u.broker_id
          FROM users u
         WHERE u.broker_id IS NOT NULL
           AND NOT EXISTS (SELECT 1 FROM brokers b WHERE b.id = u.broker_id)
           FOR UPDATE
    ), anulada AS (
        UPDATE users u
           SET broker_id = NULL, updated_at = now()
          FROM alvo
         WHERE u.id = alvo.id
        RETURNING alvo.id, alvo.property_id, alvo.broker_id
    ), trilha AS (
        INSERT INTO audit_log (property_id, actor_id, action, entity, entity_id,
                               before, after, request_id)
        SELECT a.property_id, NULL, 'users.broker_orfao_anulado',
               'users', a.id,
               jsonb_build_object('broker_id', a.broker_id),
               jsonb_build_object('broker_id', NULL),
               'migration:20261002180000'
          FROM anulada a
        RETURNING 1
    )
    SELECT count(*) INTO v_contas FROM trilha;

    IF v_reservas > 0 THEN
        RAISE WARNING
            'F2-09: % reserva(s) tinham broker_id sem cadastro de corretor e passaram a venda direta (broker_id = NULL): %',
            v_reservas, v_codigos
            USING HINT = 'o valor antigo está em audit_log, request_id = ''migration:20261002180000''';
    END IF;
    IF v_contas > 0 THEN
        RAISE WARNING
            'F2-09: % conta(s) de usuário tinham broker_id sem cadastro de corretor e ficaram sem vínculo (broker_id = NULL)',
            v_contas
            USING HINT = 'o valor antigo está em audit_log, request_id = ''migration:20261002180000''; rode o seed para religar as contas de desenvolvimento';
    END IF;
END;
$$;

-- ═══════════════════ 3. As chaves estrangeiras ═══════════════════════════
--
-- Os nomes seguem o padrão do próprio Postgres (`<tabela>_<coluna>_fkey`) de
-- propósito: `db.campoDaConstraint` tira o nome do campo do nome da constraint,
-- e `reservations_broker_id_fkey` vira `broker_id` sem tradução manual.
--
-- RESTRICT nas duas: corretor com venda não some. Ele se desativa
-- (`brokers.active = false`) e a venda continua dizendo de quem foi.

-- A venda → o corretor. O índice parcial `reservations_broker_idx`
-- (20260826110000) já cobre o lado que referencia.
ALTER TABLE reservations
    ADD CONSTRAINT reservations_broker_id_fkey
        FOREIGN KEY (broker_id) REFERENCES brokers(id) ON DELETE RESTRICT;

-- A conta → o cadastro DELA. A FK é COMPOSTA, e é aqui que ela paga o que
-- custa: com `(broker_id) → brokers(id)` simples, `users.broker_id` poderia
-- apontar para o cadastro de OUTRO corretor que existe, e é justamente este
-- valor que o F2-13 usa como "o corretor da própria conta" no escopo `own`.
-- Com o par, `users.broker_id = B` só é aceito se `B.user_id` for esta conta.
--
-- Somado ao `UNIQUE (user_id)` de `brokers`, o efeito é que `users.broker_id`
-- vale NULL ou exatamente o cadastro que aponta de volta para a conta — nunca
-- outro. As duas colunas descrevem o mesmo vínculo dos dois lados, e o banco
-- impede que digam coisas diferentes. A única assimetria possível é a conta
-- ainda não ligada (`brokers.user_id` preenchido, `users.broker_id` nulo), e
-- ela falha FECHADO: o ator `own` sem corretor só grava venda direta.
--
-- `ON UPDATE RESTRICT` não é enfeite: com CASCADE, mudar `brokers.user_id`
-- reescreveria `users.id`. Desligar a conta do cadastro é limpar
-- `users.broker_id` primeiro e `brokers.user_id` depois — a ordem que o banco
-- passa a exigir.
--
-- MATCH SIMPLE (o padrão) basta: `users.id` nunca é nulo, então toda conta com
-- `broker_id` preenchido é conferida. O lado que referencia já tem índice nas
-- duas colunas: `users_broker_idx` (parcial, 20260820140000) e a PK em `id`.
ALTER TABLE users
    ADD CONSTRAINT users_broker_id_fkey
        FOREIGN KEY (broker_id, id) REFERENCES brokers(id, user_id)
        ON UPDATE RESTRICT ON DELETE RESTRICT;
