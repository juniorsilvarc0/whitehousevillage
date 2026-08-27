FROM node:22-alpine AS deps
WORKDIR /app
RUN corepack enable
COPY apps/admin/package.json apps/admin/pnpm-lock.yam[l] ./
# `node-linker=hoisted` é obrigatório aqui, não preferência.
#
# O `output: standalone` do Next copia só as dependências que o rastreamento
# encontra. Com o layout padrão do pnpm (symlinks para `.pnpm/`), o rastreador
# não atravessa os links e transitivas ficam de fora — a imagem CONSTRÓI e o
# contêiner morre no boot com `Cannot find module '@swc/helpers/...'`.
# Descoberto rodando o stack: o job de CI só faz `docker build`, nunca executa a
# imagem, então o defeito passou verde.
RUN pnpm install --config.node-linker=hoisted --frozen-lockfile \
 || pnpm install --config.node-linker=hoisted

FROM node:22-alpine AS build
WORKDIR /app
RUN corepack enable
COPY --from=deps /app/node_modules ./node_modules
COPY apps/admin/ ./
RUN pnpm build

FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
RUN addgroup -g 1001 nodejs && adduser -S -u 1001 -G nodejs nextjs
COPY --from=build --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=build --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=build --chown=nextjs:nodejs /app/public ./public
USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
