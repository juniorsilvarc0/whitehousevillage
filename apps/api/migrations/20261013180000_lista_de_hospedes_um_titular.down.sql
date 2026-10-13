-- Inverso da `up`, na ordem contrária. A FK cai antes da `UNIQUE` que é o alvo
-- dela. Com a FK de pé, o DROP CONSTRAINT do alvo falharia por dependência, e
-- é bom que falhe, porque CASCADE ali levaria a FK junto em silêncio. A coluna
-- gerada cai depois da FK que a usa, e o índice parcial por último. Os
-- comentários caem com os objetos, e o bloco DO da régua não criou nada.
--
-- O que este `down` NÃO faz, e por quê (o critério de 20261002180000):
--
--   · NÃO devolve à lista os titulares antigos que a `up` retirou. Eles eram a
--     pessoa que já não é o cliente, mostrada como cliente no cartão do painel.
--     Recolocá-los seria reabrir, à mão, o defeito que a migration fechou, e o
--     schema anterior aceitar a linha não a torna verdadeira. Eles não se
--     perderam: estão em `audit_log` (`request_id =
--     'migration:20261013180000'`, ação
--     `reservation_guests.titular_antigo_retirado`, com `contact_id` em
--     `before`). A trilha também fica: é histórico do que aconteceu.
--
--   · NÃO apaga nem rebaixa as linhas de titular que a `up` incluiu ou
--     promoveu. São a projeção verdadeira de `reservations.contact_id`, e o
--     schema anterior as aceita como estão.
--
-- Depois do `down` o banco volta a aceitar dois titulares e um titular que não
-- é o da reserva. A API que sincroniza a troca continua escrevendo a lista
-- certa, mas nada mais a confere.
ALTER TABLE reservation_guests DROP CONSTRAINT IF EXISTS reservation_guests_titular_fkey;
ALTER TABLE reservations       DROP CONSTRAINT IF EXISTS reservations_id_contact_id_key;
ALTER TABLE reservation_guests DROP COLUMN     IF EXISTS lead_contact_id;
DROP INDEX IF EXISTS reservation_guests_titular_idx;
