FROM node:22-alpine AS deps
WORKDIR /app
RUN corepack enable
COPY apps/admin/package.json apps/admin/pnpm-lock.yam[l] ./
RUN pnpm install --frozen-lockfile || pnpm install

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
