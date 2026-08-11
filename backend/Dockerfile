# syntax=docker/dockerfile:1

# ---- Stage 1: build ---------------------------------------------------------
FROM golang:1.26-alpine AS builder

# The base image ships no git, and `cdtc` is replaced by a VCS path
# (gitlab.pg-poppay.com/...) whose pseudo-version can only be resolved by a git
# subprocess. Vendored builds never reach that code path, but a fresh clone does.
RUN apk add --no-cache git

WORKDIR /src

# Everything is copied in one step because dependency resolution depends on
# whether vendor/ is present, which is only knowable after the copy. The module
# and build caches below carry across builds regardless, so splitting the layer
# would buy little.
COPY . .

ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# With backend/vendor/ in the build context (the normal case — .dockerignore does
# not exclude it) the compile runs in vendor mode: no network, no credentials.
# Without it, the private cdtc module has to be fetched, which needs GOPRIVATE so
# Go skips the proxy and sumdb, and a netrc mounted as a build secret so both the
# ?go-get=1 discovery and the git fetch authenticate. See CDTC_SETUP.md.
#
# CGO_ENABLED=0 produces a static binary, which is what lets the final stage be a
# scratch-based distroless image with no libc.
# -trimpath and the ldflags strip local paths, the symbol table, and DWARF, which
# both shrinks the binary and keeps build-host paths out of the image.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=secret,id=netrc,target=/root/.netrc \
    export GOPRIVATE='gitlab.pg-poppay.com/*' GOAUTH=netrc && \
    if [ ! -d vendor ]; then go mod download; fi && \
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
