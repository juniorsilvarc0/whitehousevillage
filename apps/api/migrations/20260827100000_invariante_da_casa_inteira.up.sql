-- A invariante da White House Completa vira regra DO BANCO.
--
-- ═════════════════════════ O CENÁRIO QUE ISTO IMPEDE ═════════════════════════
--
-- Em linguagem de negócio, e é este o cenário que a revisão mediu três vezes:
--
--   Uma família fecha a White House Completa para o casamento — a casa INTEIRA,
--   as oito unidades, do dia 20 ao 23. O sistema aloca sete. AP-03 fica de fora
--   por qualquer um dos três motivos abaixo. No dia 21, um casal que reservou
--   "Apartamento 2 Suítes" entra pelo portão, sobe a escada e abre a porta do
--   apartamento que fica em cima da festa. Os dois lados pagaram, os dois lados
--   estão certos, e o banco não tinha como saber que alguém estava errado.
--
-- Os três caminhos por onde o buraco nasce — todos JÁ reproduzidos:
--
--   (a) A VENDA insere N−1. Bug de aplicação, unidade inativa, `INSERT` que
--       devolveu menos linha do que devia. A `stay_no_overlap` não vê nada
--       porque o defeito é a AUSÊNCIA de uma linha, e constraint nenhuma vê o
--       que não foi escrito. Sete linhas não colidem com coisa alguma.
--
--   (b) A COMPOSIÇÃO cresce DEPOIS da venda. A gestão cadastra a nova suíte e a
--       inclui na Completa (`INSERT INTO unit_type_members`). Todas as reservas
--       vivas da Completa passam, no mesmo instante, a cobrir 8 de 9 unidades —
--       e ninguém recebe erro nenhum, porque nada foi violado no momento da
--       escrita: a promessa é que envelheceu.
--
--   (c) O `consumes` MUDA com venda viva. Um produto de uma unidade vira
--       `all_members` por edição de cadastro; as reservas em `hold`/`confirmed`
--       que tinham 1 unidade continuam com 1 e agora deviam ter N.
--
-- A guarda que já existe em `internal/modules/inventario` roda DENTRO da
-- transação de quem altera a composição e serializa contra OUTRA alteração de
-- composição — não contra uma VENDA concorrente. Em `READ COMMITTED`, entre o
-- `SELECT` que ela faz e o `COMMIT` dela cabe um `POST /reservations` inteiro.
-- É o mesmo TOCTOU que a regra 2 do CLAUDE.md proíbe, um nível acima.
--
-- ═══════════════════ POR QUE ESTA FORMA, E NÃO AS OUTRAS DUAS ════════════════
--
-- A invariante é: para toda reserva VIVA (`hold`, `confirmed`, `checked_in`) de
-- produto `consumes = 'all_members'`,
--
--     count(reservation_units da reserva) = count(unit_type_members do produto)
--
-- Três caminhos foram avaliados, com o custo de cada um escrito:
--
-- 1. CONSTRAINT ADIÁVEL (`DEFERRABLE INITIALLY DEFERRED`) na forma pura — NÃO
--    EXISTE no Postgres para este predicado, e por dois motivos independentes:
--    `CHECK` nunca é adiável (só `UNIQUE`, `PRIMARY KEY`, `FOREIGN KEY` e
--    `EXCLUDE` aceitam `DEFERRABLE`), e nenhuma delas sabe expressar "a
--    contagem de uma tabela é igual à contagem de outra". O que a opção 1 tem
--    de certo é o MOMENTO: contagem só faz sentido no commit. Esse acerto foi
--    aproveitado — veja o parágrafo do `CREATE CONSTRAINT TRIGGER`.
--
-- 2. COLUNA MATERIALIZADA com `CHECK` (`reservations.units_count` +
--    `reservations.units_required`) — recusada por três custos somados:
--    (i) o `CHECK` continua não sendo adiável, então ele reprovaria a própria
--    ordem de escrita legítima da aplicação (a linha de `reservations` nasce
--    ANTES das unidades — a FK exige — e portanto nasce com contagem zero);
--    (ii) `units_required` teria de ser refanado para TODAS as reservas vivas
--    do produto a cada `INSERT`/`DELETE` em `unit_type_members`, que é
--    exatamente o trigger da opção 3 mais uma coluna denormalizada por cima;
--    (iii) duas fontes de verdade para a mesma contagem é como elas divergem.
--
-- 3. TRIGGER nas três tabelas — necessária, porque só um trigger enxerga
--    AUSÊNCIA de linha, e são três os lados por onde o buraco nasce.
--
-- A forma entregue é a 3 executada com a semântica da 1: um
-- `CREATE CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED`, que é o único
-- objeto do Postgres que junta as duas coisas — código arbitrário (vê a
-- ausência) avaliado no COMMIT (a hora em que a contagem está fechada).
--
-- ADIAR NÃO É LUXO, É REQUISITO. Sem `DEFERRED` o banco recusaria transações
-- CORRETAS, e não só as erradas:
--   · `criarReserva` insere a linha de `reservations` com status `hold` e só
--     depois os oito `stay_blocks` e os oito `reservation_units` — a FK obriga
--     essa ordem. Um trigger imediato reprovaria no primeiro `INSERT`.
--   · `Realocar` faz `DELETE` da unidade antiga e `INSERT` da nova na mesma
--     transação; entre as duas instruções a reserva legitimamente tem N−1.
-- O estado intermediário inconsistente é normal e temporário; o que não pode
-- existir é estado inconsistente COMITADO. É essa a diferença que `DEFERRED`
-- expressa, e é por isso que ela é a peça central desta migration.

-- ─────────────────────── A função de conferência ────────────────────────
--
-- Uma instrução só. A ordem dos filtros é a ordem do custo: a reserva é lida
-- pela PK, o produto pela PK, e as duas contagens só acontecem se o produto for
-- `all_members` E a reserva estiver viva — que é uma fração pequena das
-- escritas do sistema. Reserva de produto simples sai daqui depois de dois
-- lookups de índice.
--
-- `SECURITY INVOKER` (o padrão) de propósito: a função não concede leitura que
-- o chamador já não tenha.
CREATE FUNCTION conferir_composicao_completa(p_reserva uuid) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
    v_codigo    text;
    v_produto   text;
    v_alocadas  int;
    v_exigidas  int;
BEGIN
    SELECT r.code,
           ut.code,
           (SELECT count(*) FROM reservation_units ru WHERE ru.reservation_id = r.id),
           (SELECT count(*) FROM unit_type_members m  WHERE m.unit_type_id   = ut.id)
      INTO v_codigo, v_produto, v_alocadas, v_exigidas
      FROM reservations r
      JOIN unit_types  ut ON ut.id = r.unit_type_id
     WHERE r.id = p_reserva
       AND ut.consumes = 'all_members'
       AND r.status IN ('hold','confirmed','checked_in');

    -- Sem linha: ou a reserva não é da casa inteira, ou já não está viva, ou
    -- foi apagada nesta mesma transação (o `ON DELETE CASCADE` de
    -- `reservation_units` chega aqui com a reserva já ausente). Nos três casos
    -- não há invariante a defender.
    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF v_alocadas <> v_exigidas THEN
        RAISE EXCEPTION
            'reserva % do produto % ocupa % de % unidades: a casa inteira foi vendida sem estar inteira',
            v_codigo, v_produto, v_alocadas, v_exigidas
            USING ERRCODE   = '23514',
                  CONSTRAINT = 'reservation_units_composicao_completa',
                  TABLE      = 'reservation_units',
                  HINT       = 'produto all_members exige uma linha em reservation_units para cada unidade de unit_type_members; alocação parcial não existe';
    END IF;
END;
$$;

COMMENT ON FUNCTION conferir_composicao_completa(uuid) IS
    'Invariante da White House Completa: reserva viva de produto all_members ocupa TODAS as unidades da composição. Chamada por constraint triggers adiados — avalia no COMMIT, que é quando a contagem está fechada.';

-- Gatilho dos lados `reservations` e `reservation_units`: os dois falam de UMA
-- reserva, e o id sai direto da linha.
CREATE FUNCTION trg_composicao_da_reserva() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM conferir_composicao_completa(
        CASE WHEN TG_OP = 'DELETE' THEN OLD.reservation_id ELSE NEW.reservation_id END);
    RETURN NULL;
END;
$$;

CREATE FUNCTION trg_composicao_da_propria_reserva() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM conferir_composicao_completa(NEW.id);
    RETURN NULL;
END;
$$;

-- Gatilho do lado `unit_type_members` e do lado `unit_types.consumes`: a linha
-- que mudou fala de um PRODUTO, e o estrago se espalha por todas as reservas
-- vivas dele. É o único ponto do arquivo que varre mais de uma reserva — e é
-- barato porque alteração de composição é operação de cadastro, rara, feita por
-- gente, não pelo fluxo de venda.
CREATE FUNCTION trg_composicao_do_produto() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_produto uuid := CASE WHEN TG_OP = 'DELETE' THEN OLD.unit_type_id ELSE NEW.unit_type_id END;
    v_reserva uuid;
BEGIN
    FOR v_reserva IN
        SELECT r.id FROM reservations r
         WHERE r.unit_type_id = v_produto
           AND r.status IN ('hold','confirmed','checked_in')
    LOOP
        PERFORM conferir_composicao_completa(v_reserva);
    END LOOP;
    RETURN NULL;
END;
$$;

CREATE FUNCTION trg_composicao_do_proprio_produto() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_reserva uuid;
BEGIN
    FOR v_reserva IN
        SELECT r.id FROM reservations r
         WHERE r.unit_type_id = NEW.id
           AND r.status IN ('hold','confirmed','checked_in')
    LOOP
        PERFORM conferir_composicao_completa(v_reserva);
    END LOOP;
    RETURN NULL;
END;
$$;

-- ───────────────────────────── Os gatilhos ──────────────────────────────
--
-- CUSTO EM ESCRITA — MEDIDO, não estimado. Vender a Completa insere 8 linhas em
-- `reservation_units`, e `CREATE CONSTRAINT TRIGGER` só existe `FOR EACH ROW`:
-- são 8 eventos enfileirados e 8 conferências no commit.
--
-- Números (Postgres 16, `pgbench` sobre a inserção pura da Completa, mediana de
-- 5 pares alternados de 800 transações cada):
--
--     sem os gatilhos ..... 6,24 ms por venda
--     com os gatilhos ..... 6,82 ms por venda
--     ────────────────────────────────────────
--     delta ............... +0,58 ms por venda  (+9,3%), ~72 µs por conferência
--
-- E no cenário do critério de aceite da spec §5 — 50 pedidos SIMULTÂNEOS pela
-- mesma data — a diferença DESAPARECE na medição (2,28 s vs 2,30 s de latência
-- média, ~22 tps nos dois casos, dentro do ruído entre execuções). O motivo é
-- estrutural e vale registrar: sob concorrência a venda já serializa na linha
-- do ano de `reservation_code_counters` e na GiST da `stay_no_overlap`, e 72 µs
-- de contagem por índice não aparecem ao lado disso. O teste de jornada pela
-- API (`TestCorridaPelaMesmaDataTemExatamenteUmVencedorPorUnidade`, 3 produtos
-- × 50 goroutines × 10 repetições) também não separou os dois casos: 5,34 s
-- contra 5,62 s, com desvio de ±5% entre execuções da MESMA configuração.
--
-- A OTIMIZAÇÃO QUE FOI RECUSADA: memorizar em GUC de transação
-- (`set_config(..., is_local => true)`) as reservas já conferidas, para rodar 1
-- conferência em vez de 8. Ela economizaria ~0,5 ms por venda da Completa e
-- abriria um buraco: se alguém chamar `SET CONSTRAINTS ALL IMMEDIATE` no meio
-- da transação, as conferências rodam cedo, gravam o memo, e as escritas
-- POSTERIORES da mesma transação nunca mais são checadas. Meio milissegundo num
-- produto que vende algumas vezes por mês não paga uma invariante que se
-- desliga sozinha por um comando de sessão.
--
-- Quem quiser reconferir: `pgbench` com um script que insere reserva + 8 blocos
-- + 8 unidades, alternando `ALTER TABLE ... DISABLE/ENABLE TRIGGER`.

-- (a) A venda inserindo N−1 — pega no commit, com a contagem final.
CREATE CONSTRAINT TRIGGER reservation_units_composicao_completa
    AFTER INSERT OR DELETE ON reservation_units
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION trg_composicao_da_reserva();

-- (a') A reserva que nasce (ou revive) sem NENHUMA unidade. Sem este gatilho o
-- caso extremo escaparia: zero linhas em `reservation_units` é zero eventos no
-- gatilho de cima, e "vendi a casa inteira e não aloquei nada" passaria.
-- `UPDATE` entra porque `quote → hold` e `cancelled → confirmed` fazem uma
-- reserva ATRAVESSAR a fronteira do "vivo" sem que nenhuma unidade seja tocada.
CREATE CONSTRAINT TRIGGER reservations_composicao_completa
    AFTER INSERT OR UPDATE OF status, unit_type_id ON reservations
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW WHEN (NEW.status IN ('hold','confirmed','checked_in'))
    EXECUTE FUNCTION trg_composicao_da_propria_reserva();

-- (b) A composição crescendo (ou encolhendo) depois da venda.
CREATE CONSTRAINT TRIGGER unit_type_members_composicao_completa
    AFTER INSERT OR DELETE ON unit_type_members
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION trg_composicao_do_produto();

-- (c) O `consumes` mudando com venda viva.
CREATE CONSTRAINT TRIGGER unit_types_composicao_completa
    AFTER UPDATE OF consumes ON unit_types
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW WHEN (NEW.consumes = 'all_members' AND OLD.consumes IS DISTINCT FROM NEW.consumes)
    EXECUTE FUNCTION trg_composicao_do_proprio_produto();

-- ═════════════════════════ O ESTADO LEGADO ═══════════════════════════════
--
-- Trigger não é `NOT VALID`: ele simplesmente NÃO RODA sobre linha que já
-- existe. Sem o bloco abaixo, uma reserva viva já fora da invariante ficaria de
-- pé — invisível — até alguém encostar nela, e a migration teria dado a falsa
-- impressão de ter fechado o buraco.
--
-- A DECISÃO, dita por inteiro: a migration REPARA o que dá para reparar sem
-- tomar decisão comercial, e ABORTA, com nome e sobrenome, o que não dá.
--
--   · Falta unidade e ela está LIVRE no período → a migration aloca. Isto não é
--     inventar dado: é entregar o que o hóspede comprou e pagou. A casa inteira
--     é o produto; sete oitavos dela nunca foi o contrato.
--   · Falta unidade e ela está VENDIDA a outra pessoa no período → a migration
--     PARA. As duas saídas possíveis — encolher a Completa ou cancelar a venda
--     do terceiro — tiram dinheiro de um cliente identificável, e isso é
--     decisão da gestão com telefone na mão, nunca de um `UPDATE` silencioso no
--     meio de um deploy. A mensagem sai com os códigos das reservas e as
--     unidades em disputa, que é exatamente o que a gestão precisa para ligar.
--
-- Em banco recém-migrado (o caso do CI e do desenvolvimento) o bloco não
-- encontra nada e não faz nada.
DO $$
DECLARE
    v_res       record;
    v_unidade   record;
    v_bloco     uuid;
    v_reparadas int := 0;
    v_alocadas  int := 0;
    v_impasse   text[] := '{}';
BEGIN
    FOR v_res IN
        SELECT r.id, r.code, r.property_id, r.check_in, r.check_out,
               r.status, r.hold_expires_at, r.owner_id, r.created_by, ut.id AS produto
          FROM reservations r
          JOIN unit_types ut ON ut.id = r.unit_type_id
         WHERE ut.consumes = 'all_members'
           AND r.status IN ('hold','confirmed','checked_in')
           AND (SELECT count(*) FROM reservation_units ru WHERE ru.reservation_id = r.id)
             <> (SELECT count(*) FROM unit_type_members m WHERE m.unit_type_id = ut.id)
        ORDER BY r.code
    LOOP
        v_reparadas := v_reparadas + 1;

        FOR v_unidade IN
            SELECT u.id, u.code
              FROM unit_type_members m
              JOIN units u ON u.id = m.unit_id
             WHERE m.unit_type_id = v_res.produto
               AND NOT EXISTS (SELECT 1 FROM reservation_units ru
                                WHERE ru.reservation_id = v_res.id AND ru.unit_id = u.id)
             ORDER BY u.code   -- mesma ordem de aquisição de lock da aplicação
        LOOP
            BEGIN
                INSERT INTO stay_blocks (property_id, unit_id, reservation_id, source, status,
                                         period, expires_at, created_by, owner_id)
                VALUES (v_res.property_id, v_unidade.id, v_res.id, 'reservation',
                        CASE WHEN v_res.status = 'hold' THEN 'hold' ELSE 'confirmed' END,
                        daterange(v_res.check_in, v_res.check_out, '[)'),
                        CASE WHEN v_res.status = 'hold' THEN v_res.hold_expires_at END,
                        v_res.created_by, v_res.owner_id)
                RETURNING id INTO v_bloco;

                INSERT INTO reservation_units (reservation_id, unit_id, stay_block_id, locked)
                VALUES (v_res.id, v_unidade.id, v_bloco, true);

                v_alocadas := v_alocadas + 1;
            EXCEPTION WHEN exclusion_violation THEN
                -- A unidade que falta já está prometida a outra pessoa. Só a
                -- gestão pode decidir quem fica com ela.
                v_impasse := v_impasse || format('%s precisa de %s', v_res.code, v_unidade.code);
            END;
        END LOOP;
    END LOOP;

    IF array_length(v_impasse, 1) > 0 THEN
        RAISE EXCEPTION
            'há venda da casa inteira que não pode ser completada porque a unidade que falta já está vendida a outro hóspede: %',
            array_to_string(v_impasse, '; ')
            USING HINT = 'decida com a gestão quem fica com a unidade (encolher a Completa ou cancelar a outra venda), acerte o dado e rode a migration de novo';
    END IF;

    IF v_reparadas > 0 THEN
        RAISE NOTICE 'invariante da casa inteira: % reserva(s) viva(s) estavam incompletas; % unidade(s) alocada(s)',
            v_reparadas, v_alocadas;
    END IF;
END;
$$;

COMMENT ON TABLE reservation_units IS
    'Unidades físicas que a reserva ocupa. Para produto all_members a contagem TEM de bater com unit_type_members — garantido pelo constraint trigger adiado reservation_units_composicao_completa, avaliado no COMMIT.';
