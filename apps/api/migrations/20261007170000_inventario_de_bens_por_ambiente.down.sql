-- Inverso exato da `up`, na ordem contrária: quem referencia cai antes de quem
-- é referenciado. Índices, CHECKs, UNIQUEs e comentários caem com as tabelas, e
-- o bloco DO da régua de colunas não deixa nada no banco para desfazer.
--
-- LOSSY, e sem alternativa: ambientes, catálogo de bens, colocação, fotos,
-- conferências e avarias somem. Não há para onde salvá-los — nenhuma tabela do
-- schema anterior sabe representar um cômodo ou um prato. Os ARQUIVOS no volume
-- de mídia não são tocados: o banco não é dono deles, como em
-- 20261003120000.

DROP TABLE IF EXISTS inventory_issues;
DROP TABLE IF EXISTS inventory_count_lines;
DROP TABLE IF EXISTS inventory_counts;
DROP TABLE IF EXISTS inventory_item_media;
DROP TABLE IF EXISTS inventory_media;
DROP TABLE IF EXISTS room_inventory;
DROP TABLE IF EXISTS inventory_items;
DROP TABLE IF EXISTS unit_rooms;
