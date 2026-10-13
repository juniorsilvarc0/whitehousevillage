import { describe, expect, it } from "vitest";

import type { AberturaDoContato } from "@/components/contatos/modal-de-contato";
import type { Contato, ContatoNaLista } from "@/lib/contatos/tipos";

/**
 * **A linha da lista não passa por ficha — e quem garante é o compilador.**
 *
 * Este arquivo é lido pelo `tsc --noEmit` (o `tsconfig` inclui os testes). Cada
 * `@ts-expect-error` abaixo afirma que a linha seguinte **não compila**. Se
 * alguém tornar `ContatoNaLista` atribuível a `Contato` — acrescentando
 * `birth_date`/`notes` opcionais "para facilitar", ou alargando o tipo que o
 * modal aceita —, a linha passa a compilar, a diretiva fica sem erro para
 * esperar e o `tsc` reprova. É a D11 voltando, pega antes do teste de
 * componente.
 *
 * O `vitest` não confere tipos (o esbuild só os apaga); o `it` abaixo existe
 * para o arquivo ser uma suíte, e as atribuições são o que importa.
 */

const LINHA: ContatoNaLista = {
  id: "c-1",
  name: "Fernanda Lima",
  email: "f***@gmail.com",
  phone_e164: "+*********0000",
  doc_type: "cpf",
  doc_number: "***.***.247-25",
  city: "Fortaleza",
  state: "CE",
  lgpd_basis: "contrato",
  marketing_opt_in: false,
  consent_at: null,
  anonymized_at: null,
  created_at: "2026-01-10T12:00:00Z",
  updated_at: "2026-09-01T12:00:00Z",
};

describe("ContatoNaLista não é Contato", () => {
  it("o compilador recusa a linha onde se espera a ficha", () => {
    // @ts-expect-error — faltam `birth_date` e `notes`: a linha não é a ficha.
    const comoFicha: Contato = LINHA;

    // @ts-expect-error — o modal não abre com a linha: abre com a ficha…
    const abrirComALinha: AberturaDoContato = { ficha: LINHA };

    // …ou com o id, para buscá-la. Isto compila, e é o que a lista faz.
    const abrirPeloId: AberturaDoContato = { buscar: { id: LINHA.id, nome: LINHA.name } };

    // @ts-expect-error — a linha não tem `notes`: lê-la é erro, não `undefined`.
    const anotacao = LINHA.notes;

    expect([comoFicha, abrirComALinha, abrirPeloId, anotacao]).toHaveLength(4);
  });
});
