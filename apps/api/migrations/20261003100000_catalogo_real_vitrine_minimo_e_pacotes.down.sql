-- Inverso exato da `up`, na ordem contrária. Índices, CHECKs e comentários
-- caem junto com as tabelas e a coluna.
DROP TABLE IF EXISTS rate_packages;
DROP TABLE IF EXISTS unit_type_min_nights;
ALTER TABLE unit_types DROP COLUMN IF EXISTS public_name;
