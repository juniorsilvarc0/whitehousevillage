-- Restaura o estado anterior COMO ELE ERA, inclusive a FK para `reservations`:
-- um `down` que "melhora" o passado deixa de ser o inverso do `up` e vira uma
-- migration não declarada.
ALTER TABLE crm_opportunities
    ADD COLUMN quote_id uuid REFERENCES reservations(id) ON DELETE SET NULL;

DROP INDEX IF EXISTS contacts_doc_unico_idx;
