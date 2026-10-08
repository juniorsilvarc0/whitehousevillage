-- Fase 5: identidade estável do cômodo, `unit_rooms.code` (docs/db.md §11).
--
-- O defeito ficou escrito em §11 quando a tabela nasceu (20261007170000): a
-- única chave natural de `unit_rooms` era `(unit_id, name)`, e `name` é
-- justamente o campo que o gestor edita na tela. Duas coisas aprovadas dependem
-- de reencontrar o MESMO cômodo depois de um rename, e por nome isso não dá:
--
--   * A importação do levantamento fotográfico de GV-01 e CV-01 (36 ambientes,
--     759 itens). Se ela reencontra o cômodo por `name` e alguém renomeia
--     "Quarto grande" para "Suíte Master" antes da reimportação, nasce um
--     "Quarto grande" fantasma na lista de conferência, com o enxoval partido
--     entre os dois.
--   * A cópia de inventário entre unidades (`POST /units/{id}/inventory/copy`).
--     Os 6 duplex têm a mesma planta, e casar "a cozinha do AP-01" com "a
--     cozinha do AP-02" por string de nome quebra justamente quando uma das
--     duas foi renomeada.
--
-- `code` é para o cômodo o que `units.code` é para a unidade: minúsculo, sem
-- acento, separado por hífen, único na unidade e NÃO EDITÁVEL pela API depois de
-- criado. `name` continua sendo o rótulo, editável e único na unidade
-- (`unit_rooms_nome_unico` fica: dois "Suíte 1" na mesma unidade continuam
-- sendo o defeito de contar a mesma cama duas vezes).
--
-- ───────────────────────────────────────────────────────────────────────────
-- O QUE O BANCO NÃO GARANTE, DITO PARA NINGUÉM ACHAR QUE GARANTE
-- ───────────────────────────────────────────────────────────────────────────
--
-- "Não muda depois de criado" é garantia da API: `PUT` e `PATCH` não aceitam o
-- campo (openapi, `AmbienteSubstituir`/`AmbienteAtualizar`). Nada no banco
-- impede um `UPDATE unit_rooms SET code = …` pelo psql. Fechar isso exigiria
-- gatilho, e o único escritor é a API. É a mesma decisão de `expected_qty` e do
-- custo congelado (20261007200000).
--
-- A DERIVAÇÃO do `code` a partir do `name` (`POST` sem `code`) também é da API.
-- Não há DEFAULT nem gatilho que a faça aqui: DEFAULT não lê outra coluna, e
-- regra mantida em dois lugares acaba divergindo. O bloco 2 aplica a regra uma
-- única vez, às linhas que já existem, e está escrito para ser a especificação
-- que o Go copia, passo a passo.

-- ═════════════════════════════ 1. A coluna ═════════════════════════════════
--
-- Nasce anulável só para o backfill caber entre o ADD e o SET NOT NULL
-- (bloco 3). Nenhuma linha sai desta migration com `code` nulo.
ALTER TABLE unit_rooms ADD COLUMN code text;

-- ═════════ 2. Backfill, e a REGRA DE DERIVAÇÃO que a API repete ════════════
--
-- Só o banco de desenvolvimento tem linha aqui. 20261007170000 nunca chegou a
-- produção, e lá as três migrations do módulo chegam no mesmo `migrate up`, com
-- a tabela vazia. A regra vai por extenso mesmo assim, porque é a MESMA que o
-- `POST` de ambiente aplica quando o `code` não vem, e as duas implementações
-- precisam dar o mesmo resultado, caractere a caractere.
--
-- base(name), nesta ordem:
--
--   1. Remover todo code point de U+0300 a U+036F (marcas diacríticas
--      combinantes). Elas aparecem quando o nome chega decomposto (NFD: "A" +
--      U+0301 em vez de "Á"), como o macOS às vezes entrega. Sem este passo,
--      o acento solto viraria hífen no passo 3 ("Área" → "a-rea").
--   2. Trocar caractere por caractere pela tabela abaixo, e nada além dela:
--        À Á Â Ã Ä à á â ã ä → a        Ç ç → c
--        È É Ê Ë è é ê ë     → e        Ñ ñ → n
--        Ì Í Î Ï ì í î ï     → i        A … Z → a … z  (só ASCII)
--        Ò Ó Ô Õ Ö ò ó ô õ ö → o
--        Ù Ú Û Ü ù ú û ü     → u
--      Não se usa `lower()` nem minúscula Unicode. O `lower()` do Postgres
--      depende do locale do banco, o `strings.ToLower` do Go segue a tabela
--      Unicode, e os dois discordam fora do ASCII. Letra fora da tabela ("ø",
--      "ß", "ª", "º", "Ý") não é traduzida: cai no passo 3 como qualquer
--      símbolo.
--   3. Cada sequência MÁXIMA de caracteres fora de [a-z0-9] (espaço,
--      pontuação, emoji, letra que o passo 2 não traduziu) vira UM hífen.
--   4. Tirar os hífens das duas pontas.
--   5. Se a string passar de 60 caracteres, ficar com os 60 primeiros e tirar
--      de novo o hífen do fim, se houver. Depois do passo 3 a string é ASCII
--      puro, então aqui caractere = byte.
--   6. Se a string ficou vazia (nome só de emoji ou de pontuação), a base é
--      `comodo`.
--
-- code(name): o primeiro candidato LIVRE NA UNIDADE da sequência
--
--   base, base-2, base-3, …
--
-- "Livre" é contra todos os cômodos da unidade, ativos ou não, porque a
-- `UNIQUE` também não distingue. No candidato com sufixo "-n", a base é antes
-- cortada em 60 − len("-n") caracteres, sem hífen sobrando no fim, e só então
-- recebe o sufixo. O resultado nunca passa de 60.
--
-- No backfill, os cômodos de cada unidade são processados em ordem de
-- (created_at, id), e cada um enxerga os códigos já dados aos anteriores: o
-- mais antigo fica com o código sem sufixo. É o que a API teria produzido se o
-- `code` existisse desde o primeiro `POST`.
--
-- Exemplos: "Área da churrasqueira" → area-da-churrasqueira;
-- "Suíte 1 (térreo)" → suite-1-terreo; "Sala" e depois "SALA!" na mesma
-- unidade → sala e sala-2; "🛏️" → comodo.
--
-- `updated_at` não é tocado: a linha não foi editada, só ganhou uma coluna (a
-- mesma regra do backfill de 20261007200000). A tabela não tem gatilho.
DO $$
DECLARE
    r       record;
    v_base  text;
    v_code  text;
    v_n     int;
BEGIN
    FOR r IN
        SELECT id, unit_id, name
          FROM unit_rooms
         ORDER BY unit_id, created_at, id
    LOOP
        -- Passo 1. E'…' resolve o \u na leitura da string, sem depender de
        -- standard_conforming_strings: o regex recebe os próprios code points.
        v_base := regexp_replace(r.name, E'[̀-ͯ]', '', 'g');

        -- Passo 2. As duas listas andam em paralelo, grupo a grupo, com o
        -- mesmo número de caracteres em cada par.
        v_base := translate(v_base,
            'ÀÁÂÃÄàáâãä' || 'ÈÉÊËèéêë' || 'ÌÍÎÏìíîï' || 'ÒÓÔÕÖòóôõö' || 'ÙÚÛÜùúûü'
                || 'Çç' || 'Ññ' || 'ABCDEFGHIJKLMNOPQRSTUVWXYZ',
            'aaaaaaaaaa' || 'eeeeeeee' || 'iiiiiiii' || 'oooooooooo' || 'uuuuuuuu'
                || 'cc' || 'nn' || 'abcdefghijklmnopqrstuvwxyz');

        -- Passos 3 a 6.
        v_base := regexp_replace(v_base, '[^a-z0-9]+', '-', 'g');
        v_base := btrim(v_base, '-');
        v_base := rtrim(left(v_base, 60), '-');
        IF v_base = '' THEN
            v_base := 'comodo';
        END IF;

        -- Primeiro livre na unidade.
        v_code := v_base;
        v_n    := 1;
        WHILE EXISTS (SELECT 1 FROM unit_rooms WHERE unit_id = r.unit_id AND code = v_code) LOOP
            v_n    := v_n + 1;
            v_code := rtrim(left(v_base, 60 - length('-' || v_n)), '-') || '-' || v_n;
        END LOOP;

        UPDATE unit_rooms SET code = v_code WHERE id = r.id;
    END LOOP;
END;
$$;

-- ═════════════════ 3. NOT NULL, formato, tamanho e unicidade ═══════════════
--
-- Três constraints com nome próprio, e não uma só, para a API saber QUAL regra
-- foi violada pelo nome que vem no erro: formato e tamanho viram `422` no campo
-- `code`; a `UNIQUE` vira `409 CODE_IN_USE` (`23505` em `unit_rooms_code_unico`).
--
-- O regex é o `pattern` do contrato (`Ambiente.code`). Ele recusa maiúscula,
-- acento, `_`, espaço, hífen nas pontas e hífen duplo. A faixa [a-z0-9] do
-- Postgres é por code point, e não pelo collation: "á" não casa.
--
-- `UNIQUE (unit_id, code)`, e não `(property_id, code)`: o mesmo `cozinha`
-- existe em cada um dos 6 duplex, e é exatamente por ele que a cópia casa
-- origem e destino. O índice da `UNIQUE` serve à busca da importação (cômodo
-- da unidade X com código Y) e à junção da cópia.
ALTER TABLE unit_rooms
    ALTER COLUMN code SET NOT NULL,
    ADD CONSTRAINT unit_rooms_code_formato CHECK (code ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    ADD CONSTRAINT unit_rooms_code_tamanho CHECK (length(code) <= 60),
    ADD CONSTRAINT unit_rooms_code_unico   UNIQUE (unit_id, code);

COMMENT ON COLUMN unit_rooms.code IS
    'Identidade estável do cômodo na unidade (UNIQUE (unit_id, code), unit_rooms_code_unico), como units.code é a da unidade: minúsculo, sem acento, separado por hífen, até 60 caracteres. Não é editável pela API depois de criado (PUT e PATCH não aceitam o campo); name é o rótulo que o gestor edita. É a chave pela qual a importação do levantamento fotográfico reencontra o cômodo e pela qual a cópia de inventário entre unidades casa o cômodo de origem com o de destino. Ausente no POST, a API deriva do name pela regra escrita na migration 20261007213000 (e em docs/db.md §11).';

-- ═══════════ 4. A régua de ~25 colunas, conferida e não só prometida ═══════
--
-- Mesmo bloco de 20261007200000, restrito à tabela que esta migration alarga
-- (9 → 10 colunas).
DO $$
DECLARE
    v_colunas int;
BEGIN
    SELECT count(*)
      INTO v_colunas
      FROM pg_attribute a
     WHERE a.attrelid = 'unit_rooms'::regclass
       AND a.attnum > 0
       AND NOT a.attisdropped;

    IF v_colunas > 25 THEN
        RAISE EXCEPTION 'unit_rooms acima do teto de 25 colunas: % colunas', v_colunas
            USING HINT = 'extraia uma tabela satélite 1:1 — a crm_opportunities de 118 colunas do portal_amimoveis é o antiexemplo do projeto (CLAUDE.md, regra 9)';
    END IF;
END;
$$;
