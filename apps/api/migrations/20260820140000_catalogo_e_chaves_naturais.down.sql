DROP INDEX IF EXISTS cancellation_tiers_ordem_idx;
DROP INDEX IF EXISTS rate_tables_nome_idx;
DROP INDEX IF EXISTS special_periods_nome_idx;

ALTER TABLE resources DROP CONSTRAINT IF EXISTS resources_actions_validas;
ALTER TABLE resources
    DROP COLUMN IF EXISTS sort_order,
    DROP COLUMN IF EXISTS actions,
    DROP COLUMN IF EXISTS supports_own;

DROP INDEX IF EXISTS users_broker_idx;
ALTER TABLE users DROP COLUMN IF EXISTS broker_id;
