-- Inverso exato da `up`: a coluna cai e leva junto o CHECK
-- `inventory_count_lines_custo_positivo` e o COMMENT. Os dois pertencem a ela,
-- e `DROP COLUMN` não deixa nenhum para trás. O backfill não tem o que desfazer
-- além da própria coluna, e o bloco DO da régua não criou nada.
--
-- LOSSY: o custo congelado das conferências fechadas some. Um `up` seguinte
-- refaz o backfill a partir do custo ATUAL do catálogo, que é a aproximação de
-- desenvolvimento descrita na `up`. Ela deixa de ser inofensiva no dia em que
-- existir conferência fechada em produção: a partir daí, esta `down` destrói
-- perda apurada, e o `up` seguinte a substitui pelo custo do dia.

ALTER TABLE inventory_count_lines DROP COLUMN IF EXISTS replacement_cost_cents;
