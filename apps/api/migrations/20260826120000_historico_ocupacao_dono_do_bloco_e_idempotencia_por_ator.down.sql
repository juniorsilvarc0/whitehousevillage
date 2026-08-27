-- Ordem inversa da `up`. Duas reversões são LOSSY por natureza, e isso está
-- dito no ponto exato em que a perda acontece: o schema antigo não sabe
-- representar o que o novo passou a guardar.

-- ─── 3. Dono do bloco ───
DROP INDEX IF EXISTS stay_blocks_owner_idx;
ALTER TABLE stay_blocks DROP COLUMN IF EXISTS owner_id;

-- ─── 2. Idempotência por ator ───
-- LOSSY, e obrigatório: com a chave antiga `(key, endpoint)`, duas linhas de
-- atores diferentes que usaram a mesma chave na mesma rota viram duplicata e a
-- PK seria recusada. Como é cache de replay — nada referencia esta tabela —,
-- esvaziar é a reversão correta, e não um atalho: o pior efeito é um cliente
-- reexecutar um retry em voo, que é o comportamento normal de retry.
DELETE FROM idempotency_keys;

ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_pkey;
ALTER TABLE idempotency_keys
    DROP COLUMN IF EXISTS property_id,
    DROP COLUMN IF EXISTS actor_id;
ALTER TABLE idempotency_keys ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (key, endpoint);

COMMENT ON TABLE idempotency_keys IS NULL;

-- ─── 1. Estado terminal ───
DROP INDEX IF EXISTS stay_blocks_ocupacao_idx;

-- LOSSY: o CHECK antigo não tem valor para "estadia concluída", então a estadia
-- consumada volta a ser indistinguível da venda cancelada — exatamente o defeito
-- que a `up` corrigiu. `cancelled` é o destino porque é para onde o `/check-out`
-- mandava antes desta migration: a reversão devolve o banco ao estado que o
-- código anterior produzia, não a um estado que ele nunca gerou.
UPDATE stay_blocks SET status = 'cancelled' WHERE status = 'completed';

ALTER TABLE stay_blocks DROP CONSTRAINT stay_blocks_status_check;
ALTER TABLE stay_blocks ADD CONSTRAINT stay_blocks_status_check
    CHECK (status IN ('hold','confirmed','cancelled','expired'));

COMMENT ON COLUMN stay_blocks.status IS NULL;
