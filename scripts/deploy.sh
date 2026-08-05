#!/usr/bin/env bash
#
# Build, push, and deploy the frontend and backend images.
#
# Config is read from the environment, or from scripts/.env.deploy if present.
# See scripts/.env.deploy.example for the full list of variables.
#
# Usage:
#   ./scripts/deploy.sh --env uat                 # build + push + deploy both
#   ./scripts/deploy.sh --env prod --backend-only # only jive-be
#   ./scripts/deploy.sh --env uat --skip-deploy   # build and push only
#   ./scripts/deploy.sh --help
#
# The frontend's NEXT_PUBLIC_* values come from frontend/.env.<env> and are
# compiled into the bundle, so --env is required to build it.
#
# The backend deploy also uploads backend/migrations/ to the server, since the
# migrate container bind-mounts them instead of getting them from the image.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
ENV_FILE="${ENV_FILE:-${SCRIPT_DIR}/.env.deploy}"

# --- options -----------------------------------------------------------------

DO_FRONTEND=true
DO_BACKEND=true
DO_BUILD=true
DO_PUSH=true
DO_DEPLOY=true
DO_MIGRATIONS=true
PRUNE_MIGRATIONS=false
NO_CACHE="--no-cache"
CLI_APP_ENV=""

usage() {
  sed -n '2,18p' "${BASH_SOURCE[0]}" | sed 's/^#\s\?//'
  cat <<'EOF'

Options:
  -e, --env <name>   Target environment: uat or prod. Selects frontend/.env.<name>,
                     whose NEXT_PUBLIC_* values are baked into the frontend image
                     at build time. Required whenever the frontend is built.
                     Also becomes the default image tag.
  --frontend-only    Only handle jive-fe
  --backend-only     Only handle jive-be
  --skip-build       Don't build images (assumes they already exist locally)
  --skip-push        Don't push images to the registry
  --skip-deploy      Don't run the remote pull/up step
  --skip-migrations  Don't upload backend/migrations to the server
  --prune-migrations Delete remote migration files that no longer exist locally
                     (destructive on the server; off by default)
  --cache            Allow Docker layer cache (default is --no-cache)
  -h, --help         Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -e|--env)
      [[ $# -ge 2 ]] || { echo "error: $1 requires a value (uat or prod)" >&2; exit 2; }
      CLI_APP_ENV="$2"; shift ;;
    --env=*)         CLI_APP_ENV="${1#*=}" ;;
    --frontend-only) DO_BACKEND=false ;;
    --backend-only)  DO_FRONTEND=false ;;
    --skip-build)    DO_BUILD=false ;;
    --skip-push)     DO_PUSH=false ;;
    --skip-deploy)   DO_DEPLOY=false ;;
    --skip-migrations)  DO_MIGRATIONS=false ;;
    --prune-migrations) PRUNE_MIGRATIONS=true ;;
    --cache)         NO_CACHE="" ;;
    -h|--help)       usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

# --- config ------------------------------------------------------------------

if [[ -f "${ENV_FILE}" ]]; then
  log_env="${ENV_FILE}"
  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a
fi

# The flag wins over anything the config file set.
[[ -n "${CLI_APP_ENV}" ]] && APP_ENV="${CLI_APP_ENV}"
APP_ENV="${APP_ENV:-}"

FRONTEND_ENV_FILE="${FRONTEND_ENV_FILE:-${ROOT_DIR}/frontend/.env.${APP_ENV}}"

# Defaults for anything not pinned by the environment.
FE_IMAGE_NAME="${FE_IMAGE_NAME:-jive-fe}"
BE_IMAGE_NAME="${BE_IMAGE_NAME:-jive-be}"
# Tags default to the environment name, not "latest" — uat and prod images live
# in the same registry path, and a shared :latest would have each deploy
# overwrite the other's image.
TAG_FE="${TAG_FE:-${APP_ENV:-latest}}"
TAG_BE="${TAG_BE:-${APP_ENV:-latest}}"
SSH_PORT="${SSH_PORT:-22}"
REMOTE_FE_DIR="${REMOTE_FE_DIR:-/opt/jive/frontend}"
REMOTE_BE_DIR="${REMOTE_BE_DIR:-/opt/jive/backend}"

# The migrations are not baked into the backend image (see backend/Dockerfile) —
# the migrate/migrate container bind-mounts them, so they have to exist on the
# server as plain files before the stack comes up.
LOCAL_MIGRATIONS_DIR="${LOCAL_MIGRATIONS_DIR:-${ROOT_DIR}/backend/migrations}"
REMOTE_MIGRATIONS_DIR="${REMOTE_MIGRATIONS_DIR:-${REMOTE_BE_DIR}/migrations}"

require() {
  local missing=()
  for var in "$@"; do
    [[ -n "${!var:-}" ]] || missing+=("${var}")
  done
  if (( ${#missing[@]} > 0 )); then
    echo "error: missing required config: ${missing[*]}" >&2
    echo "       set them in the environment or in ${ENV_FILE}" >&2
    exit 1
  fi
}

require REGISTRY_PATH REGISTRY_USERNAME REGISTRY_PASSWORD
if [[ "${DO_DEPLOY}" == true ]]; then
  require SSH_HOST SSH_USER
fi

# Without an environment the frontend build would silently fall back to the
# Dockerfile's localhost default and ship a bundle that points nowhere.
if [[ "${DO_FRONTEND}" == true && "${DO_BUILD}" == true && -z "${APP_ENV}" ]]; then
  echo "error: building the frontend requires --env uat or --env prod" >&2
  echo "       NEXT_PUBLIC_* values are baked into the image at build time," >&2
  echo "       so the target environment must be known before docker build." >&2
  exit 1
fi

FE_IMAGE="${REGISTRY_PATH}/${FE_IMAGE_NAME}:${TAG_FE}"
BE_IMAGE="${REGISTRY_PATH}/${BE_IMAGE_NAME}:${TAG_BE}"

# --- helpers -----------------------------------------------------------------

step() { printf '\n\033[1;34m==>\033[0m \033[1m%s\033[0m\n' "$*"; }
info() { printf '    %s\n' "$*"; }
die()  { printf '\n\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# ssh_run <remote command string> [env assignments...]
# Runs the command on the server. Reads stdin, so it can also be the sink of a
# local pipe (used to stream the migrations tarball across).
# Uses sshpass when SSH_PASSWORD is set, otherwise relies on key-based auth.
ssh_run() {
  local cmd="$1"
  local -a base=(ssh -o StrictHostKeyChecking=accept-new -p "${SSH_PORT}" -l "${SSH_USER}" "${SSH_HOST}")
  if [[ -n "${SSH_PASSWORD:-}" ]]; then
    command -v sshpass >/dev/null 2>&1 \
      || die "SSH_PASSWORD is set but sshpass is not installed (apt install sshpass)"
    SSHPASS="${SSH_PASSWORD}" sshpass -e "${base[@]}" "${cmd}"
  else
    "${base[@]}" "${cmd}"
  fi
}

command -v docker >/dev/null 2>&1 || die "docker is not installed"

step "Configuration"
[[ -n "${log_env:-}" ]] && info "config file : ${log_env}"
info "environment : ${APP_ENV:-<none>}"
info "registry    : ${REGISTRY_PATH}"
if [[ "${DO_FRONTEND}" == true && "${DO_BUILD}" == true ]]; then
  info "frontend env: ${FRONTEND_ENV_FILE}"
fi
[[ "${DO_FRONTEND}" == true ]] && info "frontend    : ${FE_IMAGE}"
[[ "${DO_BACKEND}"  == true ]] && info "backend     : ${BE_IMAGE}"
[[ "${DO_DEPLOY}"   == true ]] && info "server      : ${SSH_USER}@${SSH_HOST}:${SSH_PORT}"
if [[ "${DO_DEPLOY}" == true && "${DO_BACKEND}" == true && "${DO_MIGRATIONS}" == true ]]; then
  info "migrations  : ${LOCAL_MIGRATIONS_DIR} -> ${REMOTE_MIGRATIONS_DIR}"
fi

# --- registry login ----------------------------------------------------------

step "Logging in to ${REGISTRY_PATH}"
printf '%s' "${REGISTRY_PASSWORD}" \
  | docker login "${REGISTRY_PATH}" --username "${REGISTRY_USERNAME}" --password-stdin

# --- build & push ------------------------------------------------------------

BUILD_ARGS=()

# Turns an env file into --build-arg pairs.
#
# The values cannot simply be copied into the image: .dockerignore excludes
# .env.* from the build context on purpose, and NEXT_PUBLIC_* is inlined into
# the client bundle by `next build`. So the file is read here and handed to
# docker build as arguments.
load_build_args() {
  local file="$1" dockerfile="$2"
  BUILD_ARGS=()

  [[ -f "${file}" ]] || die "no env file at ${file}"

  local line key val
  while IFS= read -r line || [[ -n "${line}" ]]; do
    line="${line%$'\r'}"                              # tolerate CRLF
    [[ "${line}" =~ ^[[:space:]]*(#|$) ]] && continue
    line="${line#export }"
    [[ "${line}" == *=* ]] || continue

    key="${line%%=*}"
    val="${line#*=}"
    key="${key//[[:space:]]/}"
    [[ "${key}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue

    # Strip one layer of matching quotes, the way a shell would.
    if [[ "${val}" == \"*\" && ${#val} -ge 2 ]]; then
      val="${val:1:${#val}-2}"
    elif [[ "${val}" == \'*\' && ${#val} -ge 2 ]]; then
      val="${val:1:${#val}-2}"
    fi

    # A NEXT_PUBLIC_* value that has no matching ARG is not inlined by the
    # build — the app would fall back to a default at run time with no error.
    if ! grep -qE "^[[:space:]]*ARG[[:space:]]+${key}([[:space:]]|=|$)" "${dockerfile}"; then
      printf '    \033[1;33mwarning:\033[0m %s is set in %s but no matching ARG in %s — it will NOT reach the build\n' \
        "${key}" "$(basename "${file}")" "$(basename "${dockerfile}")" >&2
      continue
    fi

    BUILD_ARGS+=(--build-arg "${key}=${val}")
    info "${key}=${val}"
  done < "${file}"

  (( ${#BUILD_ARGS[@]} > 0 )) || die "${file} yielded no usable build args"
}

build_and_push() {
  local name="$1" context="$2" image="$3"
  shift 3
  local -a extra_args=("$@")

  if [[ "${DO_BUILD}" == true ]]; then
    step "Building ${name} (${image})"
    [[ -f "${context}/Dockerfile" ]] || die "no Dockerfile in ${context}"
    # The ${a[@]+"${a[@]}"} form keeps an empty array from tripping `set -u`.
    # shellcheck disable=SC2086
    docker build ${NO_CACHE} ${extra_args[@]+"${extra_args[@]}"} -t "${image}" "${context}"
  fi

  if [[ "${DO_PUSH}" == true ]]; then
    step "Pushing ${name}"
    docker push "${image}"
  fi
}

if [[ "${DO_FRONTEND}" == true ]]; then
  BUILD_ARGS=()
  if [[ "${DO_BUILD}" == true ]]; then
    step "Frontend build args from ${FRONTEND_ENV_FILE}"
    load_build_args "${FRONTEND_ENV_FILE}" "${ROOT_DIR}/frontend/Dockerfile"
  fi
  build_and_push "frontend" "${ROOT_DIR}/frontend" "${FE_IMAGE}" ${BUILD_ARGS[@]+"${BUILD_ARGS[@]}"}
fi

if [[ "${DO_BACKEND}" == true ]]; then
  build_and_push "backend" "${ROOT_DIR}/backend" "${BE_IMAGE}"
fi

# --- migrations --------------------------------------------------------------

# Streamed as a tarball over the existing ssh connection rather than via
# scp/rsync: no extra tool has to be installed on either side, and it works the
# same whether auth is key- or password-based.
upload_migrations() {
  step "Uploading migrations to ${REMOTE_MIGRATIONS_DIR}"

  [[ -d "${LOCAL_MIGRATIONS_DIR}" ]] || die "no migrations directory at ${LOCAL_MIGRATIONS_DIR}"

  local count
  count="$(find "${LOCAL_MIGRATIONS_DIR}" -maxdepth 1 -name '*.sql' -type f | wc -l)"
  (( count > 0 )) || die "${LOCAL_MIGRATIONS_DIR} contains no .sql files"
  info "${count} file(s) from ${LOCAL_MIGRATIONS_DIR}"

  local remote="set -euo pipefail; mkdir -p '${REMOTE_MIGRATIONS_DIR}';"
  if [[ "${PRUNE_MIGRATIONS}" == true ]]; then
    # Stale files are not harmless: a migration that was renamed locally would
    # otherwise linger and be applied twice under two different versions.
    info "pruning remote .sql files not present locally"
    remote+=" rm -f '${REMOTE_MIGRATIONS_DIR}'/*.sql;"
  fi
  remote+=" tar -xzf - -C '${REMOTE_MIGRATIONS_DIR}';"

  tar -czf - -C "${LOCAL_MIGRATIONS_DIR}" . | ssh_run "bash -c \"${remote}\""

  info "done"
}

if [[ "${DO_DEPLOY}" == true && "${DO_BACKEND}" == true && "${DO_MIGRATIONS}" == true ]]; then
  upload_migrations
fi

# --- remote deploy -----------------------------------------------------------

if [[ "${DO_DEPLOY}" == true ]]; then
  step "Deploying to ${SSH_HOST}"

  # Built on the remote side as a single non-interactive command so that a
  # failure anywhere aborts the whole deploy instead of silently continuing.
  remote_script="set -euo pipefail;"
  remote_script+=" printf '%s' \"\$REGISTRY_PASSWORD\" | docker login ${REGISTRY_PATH} --username ${REGISTRY_USERNAME} --password-stdin;"

  if [[ "${DO_BACKEND}" == true ]]; then
    remote_script+=" cd ${REMOTE_BE_DIR};"
    remote_script+=" docker compose pull ${BE_IMAGE_NAME};"
    remote_script+=" docker compose up -d ${BE_IMAGE_NAME};"
  fi
  if [[ "${DO_FRONTEND}" == true ]]; then
    remote_script+=" cd ${REMOTE_FE_DIR};"
    remote_script+=" docker compose pull ${FE_IMAGE_NAME};"
    remote_script+=" docker compose up -d ${FE_IMAGE_NAME};"
  fi

  # REGISTRY_PASSWORD is passed through the remote env rather than interpolated
  # into the command line, so it never shows up in the remote process list.
  ssh_run "REGISTRY_PASSWORD='${REGISTRY_PASSWORD}' bash -c '${remote_script}'"
fi

step "Done"
[[ "${DO_FRONTEND}" == true ]] && info "frontend : ${FE_IMAGE}"
[[ "${DO_BACKEND}"  == true ]] && info "backend  : ${BE_IMAGE}"
exit 0
