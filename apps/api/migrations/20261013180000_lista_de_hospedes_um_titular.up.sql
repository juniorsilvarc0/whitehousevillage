-- F2-22 — a rooming list tem no máximo um titular, e ele é o da reserva.
--
-- Medido pelo tech-lead em 13/10/2026, pela API: depois de `PATCH
-- /reservations/{id}` com outro `contact_id`, o `/full` devolvia o
-- `contact_name` novo e, em `guests`, só o titular ANTIGO com
-- `is_lead_guest: true`. O cartão "Cliente" do painel lê o titular da lista e
-- mostrava o nome e o telefone de quem já não era o cliente. A troca mexia em
-- `reservations.contact_id` e em mais nada, e nada no banco ligava as duas
-- colunas. `reservation_guests` (20260820130000) aceitava qualquer número de
-- linhas `is_lead_guest`, de qualquer pessoa.
--
-- O titular tem uma fonte só, `reservations.contact_id` (spec §6, "A rooming
-- list"). A linha `is_lead_guest` da lista é a projeção dela, e esta
-- migration põe a projeção sob guarda do banco:
--
--   * no máximo um titular por reserva: índice único parcial
--     `reservation_guests_titular_idx`, conferido na própria instrução;
--   * a linha de titular é a do `contact_id` da reserva: FK composta e ADIADA
--     `reservation_guests_titular_fkey`, de `(reservation_id,
--     lead_contact_id)` para `reservations (id, contact_id)`.
--     `lead_contact_id` é coluna GERADA: vale `contact_id` na linha de titular
--     e NULL na de acompanhante, que a FK (MATCH SIMPLE) não confere. Trocar
--     `reservations.contact_id` sem trocar a lista, ou a lista sem a reserva,
--     falha no COMMIT com 23503.
--
-- O que fica com a APLICAÇÃO, e por quê: "ao menos um titular". A reserva nasce
-- com a linha dele (`InserirTitular`), a troca a substitui e
-- `guests.CheckRemove` recusa retirá-la. No banco, isso pediria a FK inversa,
-- de `reservations` para a linha de titular, e toda reserva teria de nascer
-- com a lista na mesma transação. As fixtures que criam reserva por SQL, sem
-- lista, quebrariam todas. Reserva sem linha nenhuma continua aceita, e retirar
-- a linha do titular também: as duas recusas são de `internal/domain/guests`.
--
-- Contrato em `/reservations/{id}/guests` (tag Reservas da OpenAPI), regras
-- puras em `internal/domain/guests`, schema em docs/db.md §5. A sincronização
-- da troca de titular (`guests.PlanEdit`) é do `backend-go`, em
-- `internal/modules/reservas`. Sem ela, o `PATCH` que troca o titular passa a
-- falhar no COMMIT, e é para isso que a FK existe.

-- ═══════════════════════════════ 0. A trava ═══════════════════════════════════
--
-- O reparo (1 e 2) lê `reservations.contact_id` e escreve a lista, e a FK (4)
-- valida o resultado. Num redeploy a API antiga continua no ar durante o
-- `migrate` (`infra/deploy.sh`: migrate → seed → `up -d`). Sem trava, um
-- `PATCH` que troca o titular entre o reparo e a validação comita no schema
-- velho, e a validação reprova a migration inteira. É o TOCTOU de sempre, do
-- lado do schema: conferir e depois confiar no que se conferiu.
--
-- As duas tabelas iriam para ACCESS EXCLUSIVE de qualquer jeito: o `ADD COLUMN
-- ... STORED` reescreve a lista, e o `ADD CONSTRAINT ... UNIQUE` cria índice em
-- `reservations`. Travar agora só fecha a janela e tira do meio da migration a
-- escalada de trava (de RowExclusive para AccessExclusive), que é onde nasce o
-- deadlock com quem lê e depois escreve. A ordem é a da aplicação: a reserva,
-- depois a lista. Quem chega durante a migration espera alguns milissegundos.
LOCK TABLE reservations, reservation_guests IN ACCESS EXCLUSIVE MODE;

-- ═══════════════════ 1 e 2. O reparo, com trilha ══════════════════════════════
--
-- A FK do passo 4 valida as linhas existentes, e nos bancos que já viram uma
-- troca de titular a linha de titular aponta para a pessoa de antes. Sem o
-- reparo, a migration abortaria em todo banco usado.
--
-- 1. O titular antigo SAI da lista. Não vira acompanhante, pela regra da troca
--    (spec §6): troca de titular é quase sempre correção (contato errado,
--    homônimo, a agência no lugar do hóspede), e rebaixá-lo deixaria um
--    estranho associado à estadia, lido com documento e telefone por quem abre
--    a reserva. Se ele também vem, a recepção o acrescenta de volta.
-- 2. O titular da reserva ENTRA, ou é PROMOVIDO, se já era acompanhante. Vale
--    para toda reserva, em qualquer estado: a linha é a projeção de
--    `reservations.contact_id`, e o cartão "Cliente" a lê. `(xmax = 0)` separa
--    o inserido do promovido. No `ON CONFLICT DO UPDATE` a linha existente é
--    travada antes de reescrita, e a versão nova herda a trava, então o
--    promovido sai com `xmax` diferente de zero. É o idioma dos upserts do
--    seed (`cmd/seed`).
--
-- A ordem 1 → 2 é a da aplicação (`guests.Plan`). Aqui ela não é obrigatória,
-- porque o índice único só nasce no passo 3, mas é a que deixa a trilha lida
-- como a troca é lida: sai um, entra outro.
--
-- O QUE NÃO SE PERDE: cada linha tirada, incluída ou promovida vira uma linha
-- em `audit_log`, na forma das escritas da lista pela API (`entity =
-- 'reservation_guests'`, `entity_id` = a reserva, `{contact_id,
-- is_lead_guest}`, nunca nome nem documento), com `code` ao lado para quem lê a
-- trilha, ator nulo (a migration não é usuário) e `request_id` carimbado com a
-- versão. É o critério de 20261002180000. `RAISE WARNING` sozinho não
-- bastaria: aparece no log do `cmd/migrate` e some com ele. A trilha fica:
--
--     SELECT action, before, after, at
--       FROM audit_log
--      WHERE request_id = 'migration:20261013180000'
--      ORDER BY at, action;
--
-- Não há regra 7 do CLAUDE.md a ferir: a lista não é entidade financeira, e
-- nada nela foi congelado. O `down` não desfaz o reparo (ver o `.down.sql`).
DO $$
DECLARE
    v_retirados  int;
    v_incluidos  int;
    v_promovidos int;
    v_cod_ret    text[];
    v_cod_inc    text[];
    v_cod_pro    text[];
BEGIN
    -- 1. O titular antigo sai da lista.
    WITH saiu AS (
        DELETE FROM reservation_guests g
         USING reservations r
         WHERE r.id = g.reservation_id
           AND g.is_lead_guest
           AND g.contact_id <> r.contact_id
        RETURNING g.reservation_id, g.contact_id, r.property_id, r.code
    ), trilha AS (
        INSERT INTO audit_log (property_id, actor_id, action, entity, entity_id,
                               before, after, request_id)
        SELECT s.property_id, NULL, 'reservation_guests.titular_antigo_retirado',
               'reservation_guests', s.reservation_id,
               jsonb_build_object('code', s.code, 'contact_id', s.contact_id,
                                  'is_lead_guest', true),
               NULL,
               'migration:20261013180000'
          FROM saiu s
        RETURNING before->>'code' AS code
    )
    SELECT count(*), array_agg(code ORDER BY code)
      INTO v_retirados, v_cod_ret
      FROM trilha;

    -- 2. O titular da reserva entra, ou é promovido de acompanhante.
    WITH entrou AS (
        INSERT INTO reservation_guests AS g (reservation_id, contact_id, is_lead_guest)
        SELECT r.id, r.contact_id, true
          FROM reservations r
        ON CONFLICT (reservation_id, contact_id) DO UPDATE
           SET is_lead_guest = true
         WHERE NOT g.is_lead_guest
        RETURNING g.reservation_id, g.contact_id, (xmax = 0) AS inserido
    ), trilha AS (
        INSERT INTO audit_log (property_id, actor_id, action, entity, entity_id,
                               before, after, request_id)
        SELECT r.property_id, NULL,
               CASE WHEN e.inserido THEN 'reservation_guests.titular_incluido'
                    ELSE 'reservation_guests.titular_promovido' END,
               'reservation_guests', e.reservation_id,
               CASE WHEN e.inserido THEN NULL
                    ELSE jsonb_build_object('code', r.code, 'contact_id', e.contact_id,
                                            'is_lead_guest', false) END,
               jsonb_build_object('code', r.code, 'contact_id', e.contact_id,
                                  'is_lead_guest', true),
               'migration:20261013180000'
          FROM entrou e
          JOIN reservations r ON r.id = e.reservation_id
        RETURNING action, after->>'code' AS code
    )
    SELECT count(*) FILTER (WHERE action = 'reservation_guests.titular_incluido'),
           count(*) FILTER (WHERE action = 'reservation_guests.titular_promovido'),
           array_agg(code ORDER BY code) FILTER (WHERE action = 'reservation_guests.titular_incluido'),
           array_agg(code ORDER BY code) FILTER (WHERE action = 'reservation_guests.titular_promovido')
      INTO v_incluidos, v_promovidos, v_cod_inc, v_cod_pro
      FROM trilha;

    -- O aviso leva no máximo dez códigos por grupo. A lista inteira está na
    -- trilha, e um banco de desenvolvimento com centenas de reservas de
    -- fixture sem lista transformaria o log do deploy num inventário.
    IF v_retirados + v_incluidos + v_promovidos > 0 THEN
        RAISE WARNING
            'F2-22: a lista de hóspedes foi reparada — % titular(es) antigo(s) retirado(s) [%], % titular(es) incluído(s) [%], % promovido(s) de acompanhante [%]',
            v_retirados,  coalesce(array_to_string(v_cod_ret[1:10], ', ') || CASE WHEN v_retirados  > 10 THEN ', …' ELSE '' END, ''),
            v_incluidos,  coalesce(array_to_string(v_cod_inc[1:10], ', ') || CASE WHEN v_incluidos  > 10 THEN ', …' ELSE '' END, ''),
            v_promovidos, coalesce(array_to_string(v_cod_pro[1:10], ', ') || CASE WHEN v_promovidos > 10 THEN ', …' ELSE '' END, '')
            USING HINT = 'cada mudança está em audit_log, request_id = ''migration:20261013180000''; o titular é reservations.contact_id, e o anterior não volta como acompanhante (spec §6)';
    END IF;
END;
$$;

-- ═══════════════════════ 3. No máximo um titular ══════════════════════════════
--
-- Índice único PARCIAL: só as linhas de titular competem, e a lista acumula
-- quantos acompanhantes o `guests_count` permitir.
--
-- NÃO é adiável, porque índice não se adia, e isso é deliberado. A recusa sai
-- na própria instrução, com 23505 neste nome. Na troca de titular, apaga-se a
-- linha do anterior ANTES de gravar ou promover a do novo (`guests.Plan`, passo
-- 1 → 2). Promover antes de apagar levanta 23505 aqui.
--
-- A FK do passo 4 sozinha já implicaria, no COMMIT, um titular no máximo: `id`
-- é único em `reservations`, então só um `contact_id` casa, e a chave primária
-- impede a mesma pessoa duas vezes. O índice antecipa a recusa para a
-- instrução que erra, com o nome da regra, em vez de deixá-la para o COMMIT
-- com o nome de outra. E serve à leitura do titular pela reserva.
CREATE UNIQUE INDEX reservation_guests_titular_idx
    ON reservation_guests (reservation_id)
    WHERE is_lead_guest;

COMMENT ON INDEX reservation_guests_titular_idx IS
    'No máximo um titular (is_lead_guest) por reserva. Imediato: na troca de titular, apaga-se a linha do anterior antes de gravar a do novo.';

-- ═══════════ 4. A linha de titular é a do `contact_id` da reserva ═════════════
--
-- A coluna gerada é o que permite dizer isso com uma FK. A linha de titular
-- carrega o `contact_id` em `lead_contact_id`, e a de acompanhante carrega
-- NULL. Em MATCH SIMPLE (o padrão), basta uma coluna nula para a FK não
-- conferir a linha, então o acompanhante fica fora dela por construção.
-- `STORED` porque o Postgres 16 só tem coluna gerada armazenada. Ninguém a
-- escreve: `INSERT` ou `UPDATE` com valor nela é 428C9.
--
-- A tabela vai de 3 para 4 colunas. `reservations` continua com 23: a `UNIQUE`
-- abaixo é índice, não coluna.
ALTER TABLE reservation_guests
    ADD COLUMN lead_contact_id uuid
        GENERATED ALWAYS AS (CASE WHEN is_lead_guest THEN contact_id END) STORED;

COMMENT ON COLUMN reservation_guests.lead_contact_id IS
    'Gerada: contact_id na linha de titular, NULL na de acompanhante. Existe para a FK adiada reservation_guests_titular_fkey conferir que o titular da lista é reservations.contact_id. Não se escreve.';

-- Alvo da FK. `id` sozinho já é único, então esta constraint não restringe
-- nada em `reservations`. Existe porque FK só aponta para colunas cobertas por
-- UNIQUE, como `brokers_id_user_id_key` (20261002180000) e
-- `unit_rooms_id_unit_id_key` (20261009100000). Não é adiável, e nem poderia:
-- o Postgres exige alvo não adiável.
--
-- Efeito colateral, dito aqui para ninguém tropeçar nele: com `contact_id`
-- numa UNIQUE que é alvo de FK, trocar o titular passa a ser atualização de
-- CHAVE. O `UPDATE` toma `FOR UPDATE` na linha da reserva, e não mais `FOR NO
-- KEY UPDATE`, e espera quem segura `FOR KEY SHARE` nela: as FKs filhas, ao
-- inserir evento, noite ou bloco da mesma reserva. Mudar o estado, as datas
-- ou qualquer outra coluna continua sendo `FOR NO KEY UPDATE`. A troca de
-- titular já trava a reserva em `FOR UPDATE` (spec §6), então nada passa a
-- esperar que já não esperasse.
ALTER TABLE reservations
    ADD CONSTRAINT reservations_id_contact_id_key UNIQUE (id, contact_id);

COMMENT ON CONSTRAINT reservations_id_contact_id_key ON reservations IS
    'Só alvo da FK reservation_guests_titular_fkey: o titular da lista é o contact_id da reserva.';

-- A FK.
--
--   * ADIADA (`DEFERRABLE INITIALLY DEFERRED`). A troca de titular escreve em
--     duas tabelas, e entre as duas escritas a lista e a reserva discordam.
--     Adiada, a ordem entre elas é livre na transação: a lista primeiro e a
--     reserva depois, ou o contrário. O que não pode é COMITAR discordando,
--     com 23503 neste nome. `SET CONSTRAINTS ... IMMEDIATE` antecipa a
--     conferência, e quem a pede no meio da troca recebe o 23503 na hora.
--   * `ON DELETE CASCADE`, como a FK simples de `reservation_id`
--     (20260820130000). Apagar a reserva leva a lista. Ação referencial que não
--     é NO ACTION não se adia, então a cascata é imediata. A FK simples apaga as
--     outras linhas, e as duas convivem.
--   * `ON UPDATE NO ACTION` (o padrão). É a conferência adiada da troca.
--     `CASCADE` aqui reescreveria a lista sozinho quando a reserva mudasse de
--     titular, e o Postgres o recusa sobre coluna gerada. Pior: promoveria em
--     silêncio uma linha que a regra manda apagar.
--
-- Índice do lado que referencia: a chave primária `(reservation_id,
-- contact_id)` resolve a conferência pelo prefixo, `reservation_id = $1`,
-- filtrando `lead_contact_id = $2` entre os poucos nomes de uma lista. A
-- consulta de docs/db.md §14 conta esta FK como sem índice, porque não há
-- índice que comece pelas duas colunas. É o mesmo falso positivo de
-- `users_broker_id_fkey`.
ALTER TABLE reservation_guests
    ADD CONSTRAINT reservation_guests_titular_fkey
        FOREIGN KEY (reservation_id, lead_contact_id)
        REFERENCES reservations (id, contact_id)
        ON DELETE CASCADE
        DEFERRABLE INITIALLY DEFERRED;

COMMENT ON CONSTRAINT reservation_guests_titular_fkey ON reservation_guests IS
    'A linha de titular é a do contact_id da reserva. Adiada: a troca de titular escreve a lista e a reserva em qualquer ordem, e só não pode comitar com as duas discordando (23503).';

-- ═══════════ 5. A régua de ~25 colunas, conferida e não só prometida ═════════
--
-- Mesmo bloco de 20261013120000, sobre as duas tabelas que esta migration toca
-- (4 e 23 colunas).
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
           AND c.relname IN ('reservation_guests','reservations')
           AND a.attnum > 0
           AND NOT a.attisdropped
         GROUP BY c.relname
        HAVING count(*) > 25
      ) t;

    IF v_excesso IS NOT NULL THEN
        RAISE EXCEPTION 'tabela acima do teto de 25 colunas: %', v_excesso
            USING HINT = 'extraia uma tabela satélite 1:1 — a crm_opportunities de 118 colunas do portal_amimoveis é o antiexemplo do projeto (CLAUDE.md, regra 9)';
    END IF;
END;
$$;
