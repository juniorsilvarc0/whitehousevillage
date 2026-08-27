-- Ordem inversa da `up`: gatilhos antes das funções.
--
-- A reversão é silenciosa por natureza — sem os gatilhos o banco volta a não
-- avisar ninguém, e o mapa e o kanban voltam a depender de F5. Nenhum dado é
-- perdido: `NOTIFY` não persiste nada, o barramento é volátil por definição.

DROP TRIGGER IF EXISTS crm_opportunities_notificar_mudanca ON crm_opportunities;
DROP TRIGGER IF EXISTS crm_opportunities_notificar         ON crm_opportunities;
DROP TRIGGER IF EXISTS reservations_notificar_mudanca      ON reservations;
DROP TRIGGER IF EXISTS reservations_notificar              ON reservations;
DROP TRIGGER IF EXISTS stay_blocks_notificar_mudanca       ON stay_blocks;
DROP TRIGGER IF EXISTS stay_blocks_notificar               ON stay_blocks;

DROP FUNCTION IF EXISTS trg_notificar_oportunidade();
DROP FUNCTION IF EXISTS trg_notificar_reserva();
DROP FUNCTION IF EXISTS trg_notificar_stay_block();
