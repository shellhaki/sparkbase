# Sparkbase dashboard and docs: Next.js standalone build.
# Build from the repository root so the docs/ folder is included:
#   docker build -f apps/dashboard/Dockerfile .
FROM oven/bun:1.4-alpine AS build
WORKDIR /app
COPY apps/dashboard/package.json apps/dashboard/bun.lock ./
RUN bun install --frozen-lockfile
COPY apps/dashboard/ .
COPY docs ./content/docs
ENV NEXT_TELEMETRY_DISABLED=1
RUN bun run build

FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public
USER node
EXPOSE 3000
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD wget -qO- http://127.0.0.1:3000/login >/dev/null || exit 1
CMD ["node", "server.js"]
