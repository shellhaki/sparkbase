# syntax=docker/dockerfile:1

# ---- deps: install from bun.lock ----
FROM node:24-bookworm-slim AS deps
COPY --from=oven/bun:1.4.2 /usr/local/bin/bun /usr/local/bin/bun
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

# ---- build: next build with output: "standalone" ----
FROM node:24-bookworm-slim AS build
COPY --from=oven/bun:1.4.2 /usr/local/bin/bun /usr/local/bin/bun
RUN npm install -g @shellhaki/envi-cli@1.1.0
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

# The landing page and /privacy are prerendered, so NEXT_PUBLIC_* and CONTACT_EMAIL are baked in at build time.
# Rebuild the image after changing any of them. They come from Envi when an ENVI_TOKEN build secret is
# provided, and otherwise from these build args.
ARG NEXT_PUBLIC_SITE_URL
ARG CONTACT_EMAIL
ARG NEXT_PUBLIC_GITHUB_URL
ARG NEXT_PUBLIC_X_URL
ARG NEXT_PUBLIC_PLAUSIBLE_DOMAIN
ARG NEXT_PUBLIC_USD_TO_NGN
ENV NEXT_PUBLIC_SITE_URL=$NEXT_PUBLIC_SITE_URL \
    CONTACT_EMAIL=$CONTACT_EMAIL \
    NEXT_PUBLIC_GITHUB_URL=$NEXT_PUBLIC_GITHUB_URL \
    NEXT_PUBLIC_X_URL=$NEXT_PUBLIC_X_URL \
    NEXT_PUBLIC_PLAUSIBLE_DOMAIN=$NEXT_PUBLIC_PLAUSIBLE_DOMAIN \
    NEXT_PUBLIC_USD_TO_NGN=$NEXT_PUBLIC_USD_TO_NGN \
    NEXT_TELEMETRY_DISABLED=1

# The token is mounted only for this step, so it never lands in an image layer or the build history.
RUN --mount=type=secret,id=ENVI_TOKEN,env=ENVI_TOKEN,required=false \
    if [ -n "$ENVI_TOKEN" ]; then envi run -- bun run build; else bun run build; fi

# ---- runtime: only the standalone server, static assets and the migration script ----
FROM node:24-bookworm-slim AS runner
RUN npm install -g @shellhaki/envi-cli@1.1.0
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0

COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static
COPY --from=build --chown=node:node /app/public ./public
# Run migrations from the same image: docker run --rm -e ENVI_TOKEN=... <image> node scripts/migrate.mjs
COPY --from=build --chown=node:node /app/scripts/migrate.mjs ./scripts/migrate.mjs
COPY --from=build --chown=node:node /app/migrations ./migrations
# Next bundles postgres.js into the server chunks, so standalone has no copy of it for the script.
COPY --from=deps --chown=node:node /app/node_modules/postgres ./node_modules/postgres
# Project and origin ids for envi (no secrets in this file).
COPY --from=build --chown=node:node /app/envi.toml ./envi.toml

USER node
EXPOSE 3000
# The slim image has no curl or wget, so check with Node. Coolify uses this to decide when the new container is healthy.
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:' + (process.env.PORT || 3000) + '/robots.txt').then(r => process.exit(r.ok ? 0 : 1), () => process.exit(1))"]
# envi fetches the secrets (DATABASE_URL, IP_HASH_SALT, CONTACT_EMAIL, ...) at start using ENVI_TOKEN
# and injects them into the command, so nothing is written to disk.
ENTRYPOINT ["envi", "run", "--"]
CMD ["node", "server.js"]
