-- Reverte a persistência do orçamento.
--
-- A ordem é a do dono: os gatilhos morrem junto com as tabelas em que estão
-- (`DROP TABLE` leva os seus triggers), as funções precisam cair depois — uma
-- função ainda referenciada por um trigger vivo não some.
--
-- APAGA DADO: os orçamentos emitidos e as noites deles. É por isso que uma
-- reversão em ambiente com uso real é decisão de gestão, não de deploy. O
-- `ON DELETE SET NULL` do vínculo com o funil garante que nenhuma oportunidade
-- e nenhuma reserva vão embora junto.
DROP TABLE IF EXISTS quote_nights;
DROP TABLE IF EXISTS quotes;

DROP FUNCTION IF EXISTS trg_noites_do_proprio_orcamento();
DROP FUNCTION IF EXISTS trg_noites_do_orcamento();
DROP FUNCTION IF EXISTS conferir_noites_do_orcamento(uuid);
