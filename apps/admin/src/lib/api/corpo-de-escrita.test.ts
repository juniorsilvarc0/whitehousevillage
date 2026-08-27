import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

/**
 * O corpo que o painel manda × o corpo que a API aceita.
 *
 * Desde 26/08/2026 a API recusa campo desconhecido (`DisallowUnknownFields`):
 * uma chave a mais no corpo deixou de ser ignorada e passou a ser
 * `422 VALIDATION_ERROR`. Isso fechou um buraco de verdade — antes,
 * `PATCH /units {"ativa": false}` respondia 200 sem desativar nada — e criou
 * um risco novo do outro lado: **qualquer divergência de nome entre o painel e
 * o DTO do Go agora é uma tela que não salva**, não mais um campo ignorado em
 * silêncio.
 *
 * Foi exatamente assim que o rename `guests` → `guests_count` atravessou uma
 * rodada inteira: o contrato e o painel foram renomeados, `/quotes` não, e a
 * Fase 1 ficou sem calcular orçamento nenhum. A conferência era feita à mão,
 * chave a chave, por quem lembrasse de fazer.
 *
 * Este teste faz a conferência sozinho: lê os **tipos de entrada** do painel e
 * as **tags `json`** dos DTOs em Go, e exige que toda chave que o painel manda
 * exista do outro lado. O caminho vale porque os mapeadores
 * (`produtoParaEntrada`, `orcamentoParaPedido`, …) declaram o tipo de retorno:
 * o TypeScript já recusa chave fora do tipo, então garantir o tipo garante o
 * corpo.
 *
 * O contrário — campo que o Go aceita e o painel não manda — NÃO é erro: campo
 * opcional existe para isso.
 */

const AQUI = path.dirname(fileURLToPath(import.meta.url));
const COMERCIAL_TS = path.resolve(AQUI, "comercial.ts");
const API = path.resolve(AQUI, "../../../../api/internal/modules");

/** Tira comentários antes de procurar chave: `/** … *\/` tem `:` dentro. */
function semComentarios(fonte: string): string {
  return fonte.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");
}

/** Chaves de um `export type X = { … }` do painel. */
function chavesDoTipoTS(nome: string): string[] {
  const fonte = semComentarios(readFileSync(COMERCIAL_TS, "utf8"));
  const inicio = fonte.indexOf(`export type ${nome} = {`);
  if (inicio < 0) throw new Error(`tipo ${nome} não encontrado em comercial.ts`);

  const corpo = fonte.slice(inicio + `export type ${nome} = {`.length);
  const fim = corpo.indexOf("};");
  if (fim < 0) throw new Error(`tipo ${nome} sem fechamento em comercial.ts`);

  return [...corpo.slice(0, fim).matchAll(/(\w+)\??\s*:/g)].map((m) => m[1]);
}

/** Nomes das tags `json` de um `type X struct { … }` do Go. */
function tagsDoStructGo(arquivo: string, nome: string): string[] | null {
  const caminho = path.resolve(API, arquivo);
  if (!existsSync(caminho)) return null;

  const fonte = readFileSync(caminho, "utf8");
  const inicio = fonte.indexOf(`type ${nome} struct {`);
  if (inicio < 0) throw new Error(`struct ${nome} não encontrado em ${arquivo}`);

  const corpo = fonte.slice(inicio);
  const fim = corpo.indexOf("\n}");
  return [...corpo.slice(0, fim).matchAll(/json:"([^",]+)/g)].map((m) => m[1]);
}

/**
 * Cada linha é um corpo de escrita do painel e o DTO que o recebe.
 *
 * Rota nova com corpo novo entra aqui — e é de propósito que entrar seja
 * manual: o par (tipo do painel, struct do Go) é a decisão que se quer
 * revisada, não adivinhada por convenção de nome.
 */
const CORPOS: { rota: string; tipoTS: string; arquivoGo: string; structGo: string }[] = [
  { rota: "POST/PUT /unit-types", tipoTS: "ProdutoEntrada", arquivoGo: "inventario/dto.go", structGo: "ProdutoEntrada" },
  { rota: "POST/PUT /units", tipoTS: "UnidadeEntrada", arquivoGo: "inventario/dto.go", structGo: "UnidadeEntrada" },
  { rota: "POST/PUT /rate-tables", tipoTS: "TabelaDeTarifasEntrada", arquivoGo: "tarifario/dto.go", structGo: "TabelaEntrada" },
  { rota: "POST/PUT /holidays", tipoTS: "FeriadoEntrada", arquivoGo: "tarifario/dto.go", structGo: "FeriadoEntrada" },
  { rota: "POST/PUT /special-periods", tipoTS: "PeriodoEspecialEntrada", arquivoGo: "tarifario/dto.go", structGo: "PeriodoEntrada" },
  { rota: "POST/PUT /min-nights", tipoTS: "MinimoDeNoitesEntrada", arquivoGo: "tarifario/dto.go", structGo: "MinimoEntrada" },
  { rota: "PUT /policies/commercial", tipoTS: "PoliticaComercialEntrada", arquivoGo: "tarifario/dto.go", structGo: "PoliticaComercialEntrada" },
  { rota: "PUT /policies/cancellation", tipoTS: "PoliticaDeCancelamentoEntrada", arquivoGo: "tarifario/dto.go", structGo: "PoliticaDeCancelamentoEntrada" },
  { rota: "PUT /policies/cancellation (faixa)", tipoTS: "FaixaDeCancelamento", arquivoGo: "tarifario/dto.go", structGo: "FaixaEntrada" },
  { rota: "POST /quotes", tipoTS: "PedidoDeOrcamento", arquivoGo: "disponibilidade/dto.go", structGo: "Pedido" },
];

describe("o corpo que o painel manda é o corpo que a API aceita", () => {
  for (const corpo of CORPOS) {
    it(`${corpo.rota}: nenhuma chave do painel é desconhecida para ${corpo.structGo}`, () => {
      const tags = tagsDoStructGo(corpo.arquivoGo, corpo.structGo);
      if (tags === null) {
        // Checkout parcial do monorepo: sem `apps/api` por perto não há o que
        // comparar, e reprovar aqui seria reprovar por ausência de arquivo.
        return;
      }

      const doPainel = chavesDoTipoTS(corpo.tipoTS);
      expect(doPainel.length).toBeGreaterThan(0);

      const desconhecidas = doPainel.filter((chave) => !tags.includes(chave));
      expect(
        desconhecidas,
        `${corpo.rota}: o painel manda ${JSON.stringify(desconhecidas)} e o DTO ${corpo.structGo} ` +
          `não declara — a API recusa campo desconhecido, então esta tela responde 422 e não salva. ` +
          `O Go aceita: ${JSON.stringify(tags)}`,
      ).toEqual([]);
    });
  }

  it("a lista de corpos não encolheu sem ninguém notar", () => {
    // Uma linha removida da tabela é cobertura que some em silêncio — o mesmo
    // modo de falha do teste que "passa" porque não afirma nada.
    expect(CORPOS.length).toBeGreaterThanOrEqual(10);
  });
});
