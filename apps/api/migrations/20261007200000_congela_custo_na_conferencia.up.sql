-- Fase 5: o custo de reposição passa a ser CONGELADO na linha da conferência
-- (docs/db.md §11).
--
-- O defeito: ao fechar uma conferência (`POST /inventory/counts/{id}/close`), a
-- API apura a perda de cada divergência como
--
--     falta × inventory_items.replacement_cost_cents
--
-- lendo o custo ATUAL do catálogo, e não guarda esse custo em lugar nenhum. O
-- contrato agora exige que `GET /inventory/counts/{id}` de uma conferência
-- `fechada` devolva o mesmo `result`, lido de volta. Sem a coluna, o único jeito
-- de responder é recalcular com o custo de hoje, e aí a perda apurada em março
-- muda quando alguém recota o prato em junho. Isso viola a regra 7 do CLAUDE.md
-- ("toda entidade financeira congela o que usou"). A tabela já cumpre a regra
-- para a outra metade da conta: `expected_qty` é congelada na ABERTURA
-- (20261007170000). O custo é congelado no FECHAMENTO, porque é no fechamento
-- que a perda é apurada.
--
-- Na linha, e não num total no cabeçalho (`inventory_counts.loss_cents`): a
-- perda é por divergência, e um total sozinho não reconstrói a de cada item. O
-- total sai da soma das linhas. Guardá-lo ao lado seria uma segunda fonte da
-- mesma verdade.
--
-- ───────────────────────────────────────────────────────────────────────────
-- O QUE O BANCO NÃO GARANTE, DITO PARA NINGUÉM ACHAR QUE GARANTE
-- ───────────────────────────────────────────────────────────────────────────
--
-- "NULL em conferência aberta e em cancelada" depende do status do PAI, e CHECK
-- não enxerga outra tabela. Quem garante é o `UPDATE` de fechamento da API, que
-- é o único escritor da coluna. E, como acontece com `expected_qty`, nada no
-- banco impede reescrever o custo depois do fechamento: a imutabilidade é da
-- API. Fechar as duas coisas exigiria gatilho, e não há defeito medido que o
-- justifique.

-- ═════════════════════════════ 1. A coluna ═════════════════════════════════
--
-- Mesmo tipo, mesma semântica de NULL e mesmo CHECK de
-- `inventory_items.replacement_cost_cents`, de onde ela é copiada: centavos em
-- bigint (regra 4), NULL = não cotado, e zero recusado para não virar um
-- segundo jeito, errado, de escrever "não sei". Aqui o zero seria ainda pior
-- que lá, porque entraria na conta como perda de R$ 0,00. Isso é uma
-- afirmação, não uma perda desconhecida.
ALTER TABLE inventory_count_lines
    ADD COLUMN replacement_cost_cents bigint
        CONSTRAINT inventory_count_lines_custo_positivo
            CHECK (replacement_cost_cents IS NULL OR replacement_cost_cents > 0);

COMMENT ON COLUMN inventory_count_lines.replacement_cost_cents IS
    'Custo de reposição em centavos CONGELADO NO FECHAMENTO da conferência (regra 7 do CLAUDE.md): copiado de inventory_items.replacement_cost_cents no mesmo UPDATE que fecha a conferência, e é dele que sai a perda da divergência (falta × este custo). Recotar o item depois não reescreve a perda apurada. NULL em conferência aberta, em cancelada e quando o bem não tinha custo cotado no instante do fechamento. Zero é recusado pelo CHECK.';

-- ═══════════════ 2. Backfill: APROXIMAÇÃO DE DESENVOLVIMENTO ═══════════════
--
-- Conferência já `fechada` recebe o custo ATUAL do item, que não é
-- necessariamente o custo do instante em que ela fechou. Esse nunca foi
-- guardado, e é exatamente o defeito que esta migration corrige. A aproximação
-- só é aceitável por um motivo: 20261007170000 nunca foi aplicada em produção.
-- Conferência fechada só existe em banco de desenvolvimento, onde não há perda
-- apurada de verdade a preservar. Em produção as duas migrations chegam no
-- mesmo `migrate up`, e este UPDATE não encontra linha.
--
-- Aberta e cancelada não recebem nada (a coluna é do fechamento), e item sem
-- custo cotado fica NULL. São as mesmas três regras do UPDATE de fechamento.
-- `updated_at` não é tocado: a linha não foi editada, só ganhou uma coluna.
UPDATE inventory_count_lines AS l
   SET replacement_cost_cents = i.replacement_cost_cents
  FROM inventory_counts AS c, inventory_items AS i
 WHERE c.id = l.count_id
   AND c.status = 'fechada'
   AND i.id = l.item_id
   AND i.replacement_cost_cents IS NOT NULL;

-- ═══════════ 3. A régua de ~25 colunas, conferida e não só prometida ═══════
--
-- Mesmo bloco de 20261007170000, restrito à tabela que esta migration alarga
-- (11 → 12 colunas). Tabela cresce até estourar a régua assim mesmo, uma coluna
-- de cada vez, e o bloco não custa nada.
DO $$
DECLARE
    v_colunas int;
BEGIN
    SELECT count(*)
      INTO v_colunas
      FROM pg_attribute a
     WHERE a.attrelid = 'inventory_count_lines'::regclass
       AND a.attnum > 0
       AND NOT a.attisdropped;

    IF v_colunas > 25 THEN
        RAISE EXCEPTION 'inventory_count_lines acima do teto de 25 colunas: % colunas', v_colunas
            USING HINT = 'extraia uma tabela satélite 1:1 — a crm_opportunities de 118 colunas do portal_amimoveis é o antiexemplo do projeto (CLAUDE.md, regra 9)';
    END IF;
END;
$$;
