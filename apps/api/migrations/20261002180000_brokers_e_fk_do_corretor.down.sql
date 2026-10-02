-- Inverso da `up`: as duas FKs primeiro, a tabela depois. Índices, CHECK,
-- UNIQUE e COMMENT de `brokers` caem junto com o DROP TABLE.
--
-- O que este `down` NÃO faz, e por quê:
--
--   · NÃO devolve os `broker_id` órfãos que a `up` anulou. O valor antigo era um
--     UUID que não identificava ninguém — inválido por definição, e é por isso
--     que a `up` o tirou. Recolocá-lo seria reabrir, à mão, a porta que a
--     migration fechou, e o schema anterior aceitar o valor não o torna
--     verdadeiro. Ele não se perdeu: está em `audit_log`
--     (`request_id = 'migration:20261002180000'`), e a trilha também fica — é
--     histórico do que aconteceu, e apagar auditoria no `down` faria o banco
--     esquecer uma escrita que de fato ocorreu.
--
--   · NÃO anula os `broker_id` que apontam para cadastros que estão sendo
--     derrubados aqui (o do corretor do seed, por exemplo). Eles ficam como
--     estão: o schema anterior não tem FK e os aceita, e apagá-los aqui seria
--     destruir em silêncio uma atribuição que pode ser real. Se a `up` voltar a
--     rodar, ela os encontra órfãos (a tabela renasce vazia), anula e registra o
--     valor em `audit_log` — a informação passa pela trilha em vez de sumir.
--     Num banco de desenvolvimento, `make seed` religa a conta do corretor.
ALTER TABLE users        DROP CONSTRAINT IF EXISTS users_broker_id_fkey;
ALTER TABLE reservations DROP CONSTRAINT IF EXISTS reservations_broker_id_fkey;

DROP TABLE IF EXISTS brokers;
