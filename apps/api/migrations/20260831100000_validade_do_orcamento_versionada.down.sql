-- Inverso exato da `up`: volta ao estado de 13 colunas, com a validade do
-- orçamento outra vez em Go.
--
-- Não há linha separada para o CHECK nem para o COMMENT: o Postgres derruba a
-- constraint de coluna e o comentário junto do DROP COLUMN. Conferido no ciclo
-- `up → down 1 → up → down -all`: `pg_constraint` e `pg_description` não ficam
-- com órfão do nome `quote_validity_days`.
ALTER TABLE commercial_policies DROP COLUMN IF EXISTS quote_validity_days;
