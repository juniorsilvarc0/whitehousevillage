-- Reverte a extração: devolve as dez colunas a `reservations`, copia os valores
-- de volta e derruba o satélite. Depois desta `down` o schema é exatamente o de
-- 20260820140000, incluindo o CHECK de `discount_pct`.
ALTER TABLE reservations
    ADD COLUMN subtotal_cents         bigint NOT NULL DEFAULT 0,
    ADD COLUMN discount_pct           numeric(5,2) NOT NULL DEFAULT 0,
    ADD COLUMN discount_cents         bigint NOT NULL DEFAULT 0,
    ADD COLUMN cleaning_cents         bigint NOT NULL DEFAULT 0,
    ADD COLUMN event_deposit_cents    bigint NOT NULL DEFAULT 0,
    ADD COLUMN total_cents            bigint NOT NULL DEFAULT 0,
    ADD COLUMN deposit_cents          bigint NOT NULL DEFAULT 0,
    ADD COLUMN rate_table_id          uuid REFERENCES rate_tables(id),
    ADD COLUMN policy_version         int,
    ADD COLUMN cancellation_policy_id uuid REFERENCES cancellation_policies(id);

UPDATE reservations r
   SET subtotal_cents         = p.subtotal_cents,
       discount_pct           = p.discount_pct,
       discount_cents         = p.discount_cents,
       cleaning_cents         = p.cleaning_cents,
       event_deposit_cents    = p.event_deposit_cents,
       total_cents            = p.total_cents,
       deposit_cents          = p.deposit_cents,
       rate_table_id          = p.rate_table_id,
       policy_version         = p.policy_version,
       cancellation_policy_id = p.cancellation_policy_id
  FROM reservation_pricing p
 WHERE p.reservation_id = r.id;

-- Só depois de copiar: o CHECK original não tolera lixo, e restaurá-lo antes do
-- UPDATE recusaria qualquer valor fora da faixa em vez de apontar o problema.
ALTER TABLE reservations
    ADD CONSTRAINT reservations_discount_pct_check CHECK (discount_pct BETWEEN 0 AND 100);

DROP TABLE reservation_pricing;
