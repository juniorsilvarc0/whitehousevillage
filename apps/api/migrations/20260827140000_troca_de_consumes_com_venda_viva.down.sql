-- Devolve o banco ao estado em que trocar `consumes` com venda viva era aceito
-- em silêncio. Reverter isto é reabrir a porta medida no cabeçalho da `up`:
-- venda nova da Completa alocando UMA unidade pelo preço da casa inteira.
DROP TRIGGER IF EXISTS unit_types_consumes_com_venda_viva ON unit_types;
DROP FUNCTION IF EXISTS trg_troca_de_consumes();
DROP FUNCTION IF EXISTS conferir_troca_de_consumes(uuid, text, text);

-- O comentário da coluna volta ao que era antes desta migration: não havia
-- nenhum, e deixar um texto prometendo imutabilidade sem o gatilho que a impõe
-- é pior que não ter comentário.
COMMENT ON COLUMN unit_types.consumes IS NULL;
