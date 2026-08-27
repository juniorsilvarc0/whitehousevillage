import next from "eslint-config-next";

// O Next 16 removeu `next lint`: a configuração passou a ser flat config
// chamando o eslint direto, e o `eslint-config-next` já é flat nativo — não
// precisa de FlatCompat (usá-lo estoura o validador do formato antigo).
export default [
  { ignores: [".next/**", "node_modules/**", "next-env.d.ts", "*.config.mjs"] },
  ...next,
];
