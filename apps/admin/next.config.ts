import type { NextConfig } from "next";

const config: NextConfig = {
  output: "standalone",
  reactStrictMode: true,
  // O navegador nunca fala direto com a API Go: as Route Handlers repassam a
  // chamada com o JWT que vive em cookie httpOnly. Resolve CORS e mantém o
  // token fora do alcance de qualquer script.
  //
  // `API_INTERNAL_URL` NÃO entra em `env` aqui de propósito: o `env` do Next
  // INLINE o valor no build, e a imagem passaria a carregar para sempre o
  // endereço que existia na máquina de build — em Docker, `http://localhost:8080`,
  // onde não há nada. O contêiner subia, o login respondia 502 `fetch failed`, e
  // a variável de runtime era ignorada em silêncio. `src/lib/api/client.ts` lê
  // `process.env.API_INTERNAL_URL` em tempo de execução, que é o correto para
  // código que só roda no servidor.
};

export default config;
