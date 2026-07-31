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

# NEXT_PUBLIC_* values are inlined into the client bundle at build time, so the
# API URL has to be supplied here rather than at run time.
ARG NEXT_PUBLIC_API_BASE_URL=http://localhost:8080/api/v1
ENV NEXT_PUBLIC_API_BASE_URL=${NEXT_PUBLIC_API_BASE_URL}

ENV NEXT_TELEMETRY_DISABLED=1

RUN npm run build

# ---- Stage 3: runtime -------------------------------------------------------
FROM node:24-alpine AS runtime

WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

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
