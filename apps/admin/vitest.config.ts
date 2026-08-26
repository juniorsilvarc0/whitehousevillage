import path from "node:path";
import { defineConfig } from "vitest/config";

/**
 * Configuração dos testes do painel.
 *
 * Só componentes de DECISÃO entram aqui — os que escondem, mostram ou recusam
 * alguma coisa. Testar um `Card` que renderiza `children` mede o React, não o
 * produto.
 *
 * Não há plugin de React: o Vite lê o `jsx: "react-jsx"` do tsconfig e usa o
 * runtime automático. Fast refresh não faz falta em suíte que roda uma vez.
 */
export default defineConfig({
  test: {
    environment: "jsdom",
    // globals liga o `afterEach` que o @testing-library/react usa para limpar o
    // DOM entre um teste e outro. Sem ele, o segundo teste renderiza por cima
    // do primeiro e passa a encontrar dois de cada elemento.
    globals: true,
    include: ["src/**/*.test.{ts,tsx}"],
    restoreMocks: true,
  },
  resolve: {
    // Mesmo alias do tsconfig; sem ele os imports "@/..." não resolvem.
    alias: { "@": path.resolve(__dirname, "src") },
  },
});
