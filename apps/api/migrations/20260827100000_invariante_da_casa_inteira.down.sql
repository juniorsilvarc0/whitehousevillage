-- Ordem inversa da `up`: gatilhos primeiro, funções depois (uma função com
-- gatilho pendurado não cai).
--
-- A reversão é LOSSY no ponto que importa, e está dito aqui: sem os gatilhos, a
-- casa inteira volta a poder ser vendida pela metade — a `stay_no_overlap`
-- continua sem enxergar a AUSÊNCIA de linha. O que a `up` REPAROU não é
-- desfeito: as unidades que ela alocou são a entrega do que o hóspede comprou,
-- e devolvê-las ao estoque recriaria exatamente a venda incompleta que a
-- migration existiu para fechar.

DROP TRIGGER IF EXISTS unit_types_composicao_completa        ON unit_types;
DROP TRIGGER IF EXISTS unit_type_members_composicao_completa ON unit_type_members;
DROP TRIGGER IF EXISTS reservations_composicao_completa      ON reservations;
DROP TRIGGER IF EXISTS reservation_units_composicao_completa ON reservation_units;

DROP FUNCTION IF EXISTS trg_composicao_do_proprio_produto();
DROP FUNCTION IF EXISTS trg_composicao_do_produto();
DROP FUNCTION IF EXISTS trg_composicao_da_propria_reserva();
DROP FUNCTION IF EXISTS trg_composicao_da_reserva();
DROP FUNCTION IF EXISTS conferir_composicao_completa(uuid);

COMMENT ON TABLE reservation_units IS NULL;
