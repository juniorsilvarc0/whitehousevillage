-- Ordem inversa da `up`. O DEFAULT de `code` sai antes da função, senão o DROP
-- é recusado por dependência.
DROP INDEX IF EXISTS idempotency_keys_created_idx;
DROP INDEX IF EXISTS stay_blocks_created_by_idx;
DROP INDEX IF EXISTS stay_blocks_property_idx;
DROP INDEX IF EXISTS reservation_nights_unit_type_night_idx;
DROP INDEX IF EXISTS reservation_guests_contact_idx;
DROP INDEX IF EXISTS reservation_units_stay_block_idx;
DROP INDEX IF EXISTS reservation_units_unit_idx;
DROP INDEX IF EXISTS reservations_created_by_idx;
DROP INDEX IF EXISTS reservations_rebooked_from_idx;
DROP INDEX IF EXISTS reservations_broker_idx;
DROP INDEX IF EXISTS reservations_unit_type_check_in_idx;
DROP INDEX IF EXISTS reservations_prop_status_check_in_idx;

ALTER TABLE commercial_policies
    DROP COLUMN IF EXISTS hold_max_extensions,
    DROP COLUMN IF EXISTS hold_extension_hours;

ALTER TABLE reservations ALTER COLUMN code DROP DEFAULT;
DROP FUNCTION IF EXISTS proximo_codigo_reserva();
DROP TABLE IF EXISTS reservation_code_counters;

DROP INDEX IF EXISTS reservations_owner_idx;
ALTER TABLE reservations DROP COLUMN IF EXISTS owner_id;
