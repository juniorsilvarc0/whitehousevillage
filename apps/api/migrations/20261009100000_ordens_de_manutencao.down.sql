-- Inverso exato da `up`, na ordem contrária: a tabela cai antes das duas UNIQUE
-- que as FKs compostas dela usam como alvo (com a tabela de pé, o DROP
-- CONSTRAINT falharia por dependência — e é bom que falhe, porque CASCADE ali
-- levaria a FK junto em silêncio). Índices, CHECKs, FKs e comentários caem com
-- a tabela, e o bloco DO da régua não criou nada.
--
-- LOSSY: as ordens somem — título, custo, quem abriu e fechou. Não há para onde
-- salvá-las. As linhas de `stay_blocks` que elas criaram NÃO são tocadas: são
-- calendário, e o calendário não pertence a esta migration. Depois do `down`
-- elas continuam ocupando a unidade (as `confirmed`) como bloqueio operacional
-- comum, `source = 'maintenance'`, igual aos que `POST /blocks` cria — quem
-- quiser soltá-los o faz pelo calendário. As avarias também ficam, com o
-- desfecho `consertado` que a conclusão de uma ordem lhes deu.

DROP TABLE IF EXISTS maintenance_orders;

ALTER TABLE inventory_issues DROP CONSTRAINT IF EXISTS inventory_issues_id_room_id_item_id_key;
ALTER TABLE unit_rooms       DROP CONSTRAINT IF EXISTS unit_rooms_id_unit_id_key;
