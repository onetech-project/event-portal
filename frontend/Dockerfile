# syntax=docker/dockerfile:1

# ---- Stage 1: dependencies --------------------------------------------------
FROM node:24-alpine AS deps

WORKDIR /app

# Only the lockfile pair, so this layer is reused for every build that does not
# change dependencies. `npm ci` installs exactly what the lockfile pins.
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci

# ---- Stage 2: build ---------------------------------------------------------
FROM node:24-alpine AS builder

WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules
COPY . .

# Deliberately no NEXT_PUBLIC_* build args. Inlining configuration here would
# weld the image to one environment and make "promote the UAT image to prod"
# impossible. The server reads it all at start-up instead and hands it to the
# browser — see lib/runtime-config.ts.

ENV NEXT_TELEMETRY_DISABLED=1

RUN npm run build

# ---- Stage 3: runtime -------------------------------------------------------
FROM node:24-alpine AS runtime

WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

# What differs between deployments. Set these per environment; the same image
# then serves UAT and production without a rebuild.
#
# Only the API URL gets a default, because a wrong one fails loudly. The QRIS
# identity is left unset on purpose: an image that carried a default merchant
# name would print it on the payment frame of any environment that forgot to
# configure one, and the instructions tell the guest to trust that name.
ENV API_BASE_URL=http://localhost:8080/api/v1

# Run as an unprivileged user. node:alpine already provides uid 1000 "node".
USER node

# The standalone bundle carries its own trimmed node_modules and server.js.
# `public` and `.next/static` are not included by design and must be copied in
# alongside it, or every asset 404s.
COPY --from=builder --chown=node:node /app/.next/standalone ./
COPY --from=builder --chown=node:node /app/.next/static ./.next/static
COPY --from=builder --chown=node:node /app/public ./public

EXPOSE 3000

CMD ["node", "server.js"]
