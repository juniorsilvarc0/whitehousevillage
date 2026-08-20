import type { NextConfig } from "next";

const config: NextConfig = {
  output: "standalone",
  reactStrictMode: true,
  // O navegador nunca fala direto com a API Go: as Route Handlers repassam a
  // chamada com o JWT que vive em cookie httpOnly. Resolve CORS e mantém o
  // token fora do alcance de qualquer script.
  env: { API_INTERNAL_URL: process.env.API_INTERNAL_URL ?? "http://localhost:8080" },
};

export default config;
