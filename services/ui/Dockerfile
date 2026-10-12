FROM node:22-alpine3.21 AS deps
WORKDIR /app
RUN apk add --no-cache libc6-compat
COPY package.json package-lock.json ./
# ci installs exactly what the lockfile pins, so a clean clone rebuilds the same
# dependency tree instead of whatever is newest that day.
RUN npm ci

FROM node:22-alpine3.21 AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
ENV NODE_ENV=production
# Baked into the standalone build; the UI proxies /api/v1/* to this origin.
ARG CORE_ORIGIN=http://core:8000
ENV CORE_ORIGIN=$CORE_ORIGIN
RUN npm run build

FROM node:22-alpine3.21 AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

RUN addgroup --system --gid 1001 nodejs && \
    adduser --system --uid 1001 nextjs

COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
