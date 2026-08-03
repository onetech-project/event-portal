# syntax=docker/dockerfile:1

# ---- Stage 1: build ---------------------------------------------------------
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Dependencies are copied and downloaded first so this layer is reused whenever
# only application code changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# CGO_ENABLED=0 produces a static binary, which is what lets the final stage be a
# scratch-based distroless image with no libc.
# -trimpath and the ldflags strip local paths, the symbol table, and DWARF, which
# both shrinks the binary and keeps build-host paths out of the image.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/seedadmin ./cmd/seedadmin

# ---- Stage 2: runtime -------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

# Distroless static carries no shell and no package manager: nothing to exec into,
# and a far smaller attack surface than an alpine base. It already ships CA
# certificates (needed for the HTTPS call to the payment gateway) and tzdata.

WORKDIR /app

COPY --from=builder /out/api /app/api
COPY --from=builder /out/seedadmin /app/seedadmin

# The migrations are deliberately NOT shipped here. Nothing in this image could
# apply them — no shell, no psql, no migrate binary — and the schema is owned by
# the migrate/migrate container in docker-compose.yml, which mounts them directly.

# The nonroot tag runs as uid 65532; declared explicitly so it survives a base
# image change.
USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/app/api"]
