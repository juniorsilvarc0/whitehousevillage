-- Inverso exato da `up`. As três tabelas não se referenciam entre si, e nenhuma
-- tabela anterior aponta para elas; caem em ordem inversa à de criação por
-- hábito. Índices, CHECKs, a EXCLUDE (e o índice GiST dela), as FKs e os
-- comentários caem com as tabelas. A `up` não criou função, gatilho, sequência
-- nem extensão (`btree_gist` é de 20260820120000 e fica), e o bloco DO da régua
-- não deixa nada no banco para desfazer.
--
-- LOSSY, e sem alternativa: os eventos da agenda (os automáticos das reservas
-- confirmadas e os marcados à mão, com responsável, anotação e conclusão), os
-- bloqueios de horário e os horários que o dono confirmou somem. Nenhuma
-- tabela do schema anterior sabe representar nada disso. Reservas, unidades,
-- contatos e usuários NÃO são tocados: a agenda aponta para eles, e não o
-- contrário. O `up` seguinte devolve a cada propriedade a linha de
-- configuração com os horários PROVISÓRIOS, e a agenda das reservas
-- confirmadas só volta pelo preenchimento (`agenda.Plan` sobre as reservas
-- confirmadas que estão sem agenda), não por esta migration.

DROP TABLE IF EXISTS agenda_blocks;
DROP TABLE IF EXISTS agenda_events;
DROP TABLE IF EXISTS agenda_settings;
