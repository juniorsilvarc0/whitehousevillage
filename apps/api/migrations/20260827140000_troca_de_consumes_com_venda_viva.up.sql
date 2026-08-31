-- Trocar o que um produto CONSOME com venda viva passa a ser recusado pelo banco.
--
-- ═════════════════════════ O QUE FOI MEDIDO ═════════════════════════════════
--
-- Com o stack no ar e a invariante da casa inteira (20260827100000) aplicada:
--
--     -- reserva confirmada da White House Completa, 8 de 8 unidades alocadas
--     UPDATE unit_types SET consumes='one_member' WHERE code='completa';
--     -- UPDATE 1
--
-- Nenhuma recusa. E o passo seguinte, também medido, é o estrago inteiro:
--
--     venda NOVA da "Completa"  →  1 unidade alocada, 8 unidades na composição
--
-- Ou seja: quem paga pela casa inteira recebe UM apartamento, e os outros sete
-- vão para o mercado. É o mesmo cenário que três revisões adversariais
-- encontraram, entrando por uma porta que ficou aberta.
--
-- ═══════════════════ POR QUE A GUARDA DE 20260827100000 NÃO PEGA ════════════
--
-- O gatilho `unit_types_composicao_completa` existe e está correto — mas ele é
-- `WHEN (NEW.consumes = 'all_members')`, isto é, defende a ENTRADA no regime da
-- casa inteira. A troca perigosa é a SAÍDA:
--
--     all_members → one_member
--
-- e ela é auto-imunizante: no instante em que o produto deixa de ser
-- `all_members`, `conferir_composicao_completa` passa a não encontrar linha
-- para ele e devolve `RETURN` sem conferir nada. Trocar o tipo TIRA o produto
-- do alcance da própria invariante que o protegia. Guarda que o alvo consegue
-- desligar mudando um campo não é guarda.
--
-- ═════════════════ POR QUE NÃO BASTA A GUARDA DA APLICAÇÃO ══════════════════
--
-- `internal/modules/inventario` já recusa a troca por `PATCH` e por `PUT`, com
-- mensagem boa. Ela continua sendo a primeira linha — e continua sendo a que o
-- usuário lê. Mas ela vale para quem entra pela porta da frente: um `psql` de
-- manutenção, um script de importação, uma correção "rápida" em produção e
-- qualquer módulo futuro que escreva em `unit_types` passam por fora dela. A
-- regra 2 do CLAUDE.md já disse o que fazer com invariante que só a aplicação
-- defende, e a lição das três rodadas foi exatamente esta: a exclusividade da
-- Completa tem de ser fato do banco.
--
-- ═════════════════════ A FORMA, E POR QUE ESTA ══════════════════════════════
--
-- `CREATE CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED`, coerente com
-- as quatro que já defendem a casa inteira. Adiar aqui não é detalhe de estilo,
-- é o que torna a regra JUSTA: a transação legítima que encerra as vendas e
-- reclassifica o produto —
--
--     BEGIN;
--       UPDATE reservations SET status='cancelled' WHERE ...;
--       UPDATE unit_types  SET consumes='one_member' WHERE ...;
--     COMMIT;
--
-- — é aceita, porque no COMMIT já não há venda viva; e é aceita nas duas
-- ordens, porque a conferência não olha o instante da instrução, olha o estado
-- final. Um gatilho imediato reprovaria a mesma transação só por causa da ordem
-- em que as duas linhas foram escritas.
--
-- O preço, dito por inteiro: reclassificar o produto e vendê-lo na MESMA
-- transação é recusado (no commit existe venda viva). É uma transação que não
-- acontece na operação real, e recusá-la é o lado seguro do erro.

CREATE FUNCTION conferir_troca_de_consumes(p_produto uuid, p_de text, p_para text)
RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
    v_codigo  text;
    v_nome    text;
    v_vivas   int;
    v_amostra text;
BEGIN
    -- ── A trava, e por que ela é indispensável ────────────────────────
    --
    -- Sem esta linha a guarda teria o mesmo TOCTOU que ela veio fechar, só que
    -- deslocado para o commit: uma venda em voo, ainda não comitada, é
    -- INVISÍVEL para a conferência, e a troca passaria por cima dela.
    --
    -- Por que `FOR UPDATE` resolve, e por que não havia serialização antes:
    -- criar reserva insere em `reservations`, cuja FK `unit_type_id` faz o
    -- Postgres travar a linha do produto em `FOR KEY SHARE`. O `UPDATE ... SET
    -- consumes` toma `FOR NO KEY UPDATE`, e esses DOIS MODOS NÃO CONFLITAM —
    -- é exatamente por isso que a venda e a troca correm lado a lado hoje.
    -- `FOR UPDATE` conflita com `FOR KEY SHARE`, e é o único modo que conflita.
    -- Então: ou a venda comita antes e a conferência a enxerga, ou a venda
    -- espera a troca decidir. Não há terceira ordem.
    --
    -- Custo: uma trava de linha numa operação de CADASTRO, rara e feita por
    -- gente. A venda, que é o caminho quente, só sente algo se estiver
    -- disputando o mesmo produto no mesmo segundo em que alguém o reclassifica.
    -- Se a espera estourar o `lock_timeout` da sessão, o erro é `55P03`, que a
    -- aplicação já traduz como disputa retentável.
    PERFORM 1 FROM unit_types WHERE id = p_produto FOR UPDATE;

    SELECT ut.code, ut.name INTO v_codigo, v_nome
      FROM unit_types ut WHERE ut.id = p_produto;

    -- Produto apagado na mesma transação: não há venda a proteger.
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- "Viva" é a mesma definição das outras guardas e da spec §5: os três
    -- estados em que existe promessa de estadia de pé. `quote` fica de fora
    -- (orçamento não é venda), e os terminais também.
    SELECT count(*), string_agg(x.code, ', ' ORDER BY x.code)
      INTO v_vivas, v_amostra
      FROM (SELECT r.code
              FROM reservations r
             WHERE r.unit_type_id = p_produto
               AND r.status IN ('hold','confirmed','checked_in')
             ORDER BY r.code
             LIMIT 5) x;

    IF v_vivas = 0 THEN
        RETURN;
    END IF;

    RAISE EXCEPTION
        '% tem % reserva(s) de pé (%): mudar de "%" para "%" mudaria o que já foi vendido a essas pessoas',
        coalesce(v_nome, v_codigo), v_vivas, v_amostra, p_de, p_para
        USING ERRCODE    = '23514',
              CONSTRAINT = 'unit_types_consumes_com_venda_viva',
              TABLE      = 'unit_types',
              HINT       = 'quem comprou comprou o que o produto consumia no dia da venda — a Completa vendida é a casa inteira, e um apartamento vendido é um apartamento. Espere as estadias terminarem, cancele-as com a gestão, ou cadastre um produto NOVO com o outro tipo em vez de reescrever este.';
END;
$$;

COMMENT ON FUNCTION conferir_troca_de_consumes(uuid, text, text) IS
    'Recusa mudar unit_types.consumes enquanto o produto tem reserva viva (hold, confirmed, checked_in). Trava a linha do produto em FOR UPDATE para serializar contra a venda concorrente, que só toma FOR KEY SHARE pela FK.';

CREATE FUNCTION trg_troca_de_consumes() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM conferir_troca_de_consumes(NEW.id, OLD.consumes, NEW.consumes);
    RETURN NULL;
END;
$$;

-- As duas direções, e não só a saída de `all_members`.
--
-- `one_member → all_members` também é troca proibida com venda viva, e por um
-- motivo simétrico: quem comprou "um apartamento" passaria a ocupar a casa
-- toda, e as outras unidades sairiam do mercado sem ninguém decidir isso. O
-- gatilho de 20260827100000 só reprova esse caso quando as CONTAGENS não batem
-- — um produto de composição unitária atravessaria a troca sem violar contagem
-- nenhuma e mudaria de significado em silêncio.
CREATE CONSTRAINT TRIGGER unit_types_consumes_com_venda_viva
    AFTER UPDATE OF consumes ON unit_types
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW WHEN (OLD.consumes IS DISTINCT FROM NEW.consumes)
    EXECUTE FUNCTION trg_troca_de_consumes();

COMMENT ON COLUMN unit_types.consumes IS
    'one_member = a venda ocupa UMA unidade da composição; all_members = ocupa TODAS (a White House Completa). Imutável enquanto houver reserva viva do produto — constraint trigger adiado unit_types_consumes_com_venda_viva.';

-- ═══════════════ O gatilho antigo continua de pé, e por quê ═════════════════
--
-- Com esta guarda, `unit_types_composicao_completa` (20260827100000) passa a
-- ser alcançável apenas quando NÃO há venda viva — e aí o laço dele não
-- encontra reserva nenhuma. Ele fica, e a escolha é deliberada: os dois
-- gatilhos dizem coisas diferentes. Este recusa a TRANSIÇÃO; aquele confere a
-- INVARIANTE. No dia em que alguém afrouxar a transição (por exemplo, para
-- permitir a troca quando a alocação por acaso já bate), é a invariante que
-- continua impedindo a casa de ser vendida pela metade. Remover o de baixo por
-- ser "redundante hoje" é como as guardas somem.

-- ═══════════════════════════ O ESTADO LEGADO ════════════════════════════════
--
-- Trigger não roda sobre linha que já existe: um produto trocado ANTES desta
-- migration continua trocado, e as reservas vivas dele continuam fora do
-- alcance da invariante. Diferente de 20260827100000, aqui a migration NÃO
-- repara e NÃO aborta — e os dois "nãos" têm motivo:
--
--   · reparar seria devolver `consumes` para o valor antigo, e ninguém sabe se
--     a troca foi um erro ou uma decisão comercial legítima tomada quando não
--     havia venda viva. Adivinhar reescreveria o cadastro da casa.
--   · abortar pararia o deploy por uma situação que pode ser antiga, conhecida
--     e já resolvida na operação.
--
-- Então ela AVISA, com nome e sobrenome, para que a gestão confira. O sinal
-- escolhido é o produto `one_member` cuja reserva viva ocupa mais de uma
-- unidade: isso não acontece por venda normal — é a assinatura de um produto
-- que já foi `all_members` e mudou de lado com venda em pé.
--
-- ONDE O AVISO APARECE, porque aviso que ninguém lê não é aviso: `RAISE
-- WARNING` sai no LOG DO SERVIDOR Postgres (o `log_min_messages` padrão é
-- `warning`), e sai no terminal de quem aplicar o arquivo por `psql`.
-- CONFERIDO nesta árvore, aplicando a migration sobre um banco com o defeito:
--
--   WARNING: reserva WH-2026-0001 do produto completa ocupa 8 unidades num
--            produto de UMA unidade: sinal de que o consumes foi trocado (...)
--
-- O `cmd/migrate`, esse, ENGOLE avisos do banco — o driver do golang-migrate
-- não instala tratador de notice. Está anotado para o integrador; a evidência
-- fica no log do Postgres de qualquer jeito.
DO $$
DECLARE
    v_linha  record;
    v_achou  boolean := false;
BEGIN
    FOR v_linha IN
        SELECT r.code, ut.code AS produto, count(ru.*) AS unidades
          FROM reservations r
          JOIN unit_types ut ON ut.id = r.unit_type_id
          JOIN reservation_units ru ON ru.reservation_id = r.id
         WHERE ut.consumes = 'one_member'
           AND r.status IN ('hold','confirmed','checked_in')
         GROUP BY r.code, ut.code
        HAVING count(ru.*) > 1
         ORDER BY r.code
    LOOP
        v_achou := true;
        RAISE WARNING
            'reserva % do produto % ocupa % unidades num produto de UMA unidade: sinal de que o consumes foi trocado com a venda em pé. Confira com a gestão antes de vender este produto de novo.',
            v_linha.code, v_linha.produto, v_linha.unidades;
    END LOOP;

    IF v_achou THEN
        RAISE WARNING
            'a partir desta migration a troca de consumes com reserva viva é recusada pelo banco; o que está acima é anterior a ela e precisa de decisão humana.';
    END IF;
END;
$$;
