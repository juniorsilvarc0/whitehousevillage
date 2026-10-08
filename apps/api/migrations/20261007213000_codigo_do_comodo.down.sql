-- Inverso exato da `up`. As três constraints caem por nome e antes da coluna.
-- `DROP COLUMN` as levaria junto (a `UNIQUE (unit_id, code)` inclusive, porque
-- envolve a coluna), mas o nome explícito deixa o `down` legível e falha alto se
-- alguém um dia tirar o `IF EXISTS`. O índice da `UNIQUE` cai com ela, e o
-- COMMENT cai com a coluna. O backfill não tem o que desfazer além da própria
-- coluna, e o bloco DO da régua não criou nada.
--
-- LOSSY: os códigos somem. O `up` seguinte os deriva de novo a partir do `name`
-- ATUAL, e cômodo renomeado depois de criado ganha código diferente do que
-- tinha. É exatamente a identidade que a importação e a cópia usam. Hoje isso
-- é inofensivo, porque só o banco de desenvolvimento tem cômodos. No dia em que
-- uma importação real tiver rodado, este `down` seguido de `up` faz a próxima
-- reimportação criar cômodo fantasma, que é o defeito que a `up` corrige.

ALTER TABLE unit_rooms
    DROP CONSTRAINT IF EXISTS unit_rooms_code_unico,
    DROP CONSTRAINT IF EXISTS unit_rooms_code_tamanho,
    DROP CONSTRAINT IF EXISTS unit_rooms_code_formato,
    DROP COLUMN     IF EXISTS code;
