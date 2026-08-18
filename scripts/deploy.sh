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
# The frontend image carries no environment in it: API_BASE_URL and the QRIS
# identity are read when the container starts. So building it needs no --env,
# and the deploy ships frontend/.env.<env> to the server instead — which is what
# lets the image that passed uat be the image prod runs, with only that file
# different.
#
# The tag built here is also the tag the server runs. The remote compose files
# take their image from `${IMAGE_NAME}:${IMAGE_TAG}`, so the deploy rewrites
# those two keys in the .env beside each one — together with the frontend's
# runtime config — exports the image keys for the compose invocation, and aborts
# if the service still resolves to a different image than the one just pushed,
# or if it would never receive that config at all.
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
CHECK_REMOTE_IMAGE=true
WRITE_REMOTE_ENV=true
WRITE_FRONTEND_ENV=true
CHECK_REMOTE_ENV=true
NO_CACHE="--no-cache"
CLI_APP_ENV=""

usage() {
  # The banner above, minus the shebang: every leading comment line up to the
  # first line of code. Derived rather than a line range, so editing the banner
  # cannot silently truncate the help.
  awk 'NR == 1 { next } !/^#/ { exit } { sub(/^#[[:space:]]?/, ""); print }' "${BASH_SOURCE[0]}"
  cat <<'EOF'

Options:
  -e, --env <name>   Target environment: uat or prod. Selects frontend/.env.<name>,
                     which is shipped to the server as the frontend's runtime
                     config. Required whenever the frontend is deployed — the
                     image itself is environment-agnostic, so nothing needs it
                     at build time. Also becomes the default image tag.
  --frontend-only    Only handle jive-fe
  --backend-only     Only handle jive-be
  --skip-build       Don't build images (assumes they already exist locally)
  --skip-push        Don't push images to the registry
  --skip-deploy      Don't run the remote pull/up step
  --skip-migrations  Don't upload backend/migrations to the server
  --prune-migrations Delete remote migration files that no longer exist locally
                     (destructive on the server; off by default)
  --no-image-check   Deploy even if the remote compose service resolves to a
                     different image than the one built here (escape hatch;
                     the server then runs whatever tag its compose file pins)
  --no-remote-env    Don't rewrite IMAGE_NAME/IMAGE_TAG in the remote .env.
                     This run still deploys the configured tag, but a reboot
                     or a manual `docker compose up -d` reverts to the old one
  --no-frontend-env  Don't write frontend/.env.<env> into the remote .env.
                     The server then keeps whatever runtime config it already
                     has, and it is not checked
  --no-env-check     Deploy even if the frontend service never receives the
                     runtime config (escape hatch; the app then falls back to
                     its built-in defaults, which point at localhost)
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
    --no-image-check)   CHECK_REMOTE_IMAGE=false ;;
    --no-remote-env)    WRITE_REMOTE_ENV=false ;;
    --no-frontend-env)  WRITE_FRONTEND_ENV=false ;;
    --no-env-check)     CHECK_REMOTE_ENV=false ;;
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
# The compose service to pull and restart on the server. Not necessarily the
# image name, though it defaults to it because that is how the remote files are
# currently written.
FE_SERVICE="${FE_SERVICE:-${FE_IMAGE_NAME}}"
BE_SERVICE="${BE_SERVICE:-${BE_IMAGE_NAME}}"
# The two keys the remote compose files interpolate their image from:
#   image: ${IMAGE_NAME}:${IMAGE_TAG}
# The deploy owns both, in the .env next to each remote compose file.
REMOTE_IMAGE_NAME_VAR="${REMOTE_IMAGE_NAME_VAR:-IMAGE_NAME}"
REMOTE_IMAGE_TAG_VAR="${REMOTE_IMAGE_TAG_VAR:-IMAGE_TAG}"
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

# The build is environment-agnostic now, so only the deploy needs to know which
# environment this is. Without it there is no frontend/.env.<env> to ship, and
# the container would come up on the Dockerfile's localhost default.
if [[ "${DO_FRONTEND}" == true && "${DO_DEPLOY}" == true \
      && "${WRITE_FRONTEND_ENV}" == true && -z "${APP_ENV}" ]]; then
  echo "error: deploying the frontend requires --env uat or --env prod" >&2
  echo "       the image carries no configuration; frontend/.env.<env> is" >&2
  echo "       shipped to the server as the runtime config it reads at start-up." >&2
  echo "       Pass --no-frontend-env to leave the server's existing config alone." >&2
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
if [[ "${DO_FRONTEND}" == true && "${DO_DEPLOY}" == true && "${WRITE_FRONTEND_ENV}" == true ]]; then
  info "frontend env: ${FRONTEND_ENV_FILE} -> ${REMOTE_FE_DIR}/.env"
fi
[[ "${DO_FRONTEND}" == true ]] && info "frontend    : ${FE_IMAGE} (service ${FE_SERVICE})"
[[ "${DO_BACKEND}"  == true ]] && info "backend     : ${BE_IMAGE} (service ${BE_SERVICE})"
[[ "${DO_DEPLOY}"   == true ]] && info "server      : ${SSH_USER}@${SSH_HOST}:${SSH_PORT}"
if [[ "${DO_DEPLOY}" == true && "${WRITE_REMOTE_ENV}" == true ]]; then
  info "remote .env : ${REMOTE_IMAGE_NAME_VAR}, ${REMOTE_IMAGE_TAG_VAR}"
fi
if [[ "${DO_DEPLOY}" == true && "${DO_FRONTEND}" == true && "${WRITE_FRONTEND_ENV}" == false ]]; then
  info "remote .env : frontend runtime config left as-is (--no-frontend-env)"
fi
if [[ "${DO_DEPLOY}" == true && "${DO_BACKEND}" == true && "${DO_MIGRATIONS}" == true ]]; then
  info "migrations  : ${LOCAL_MIGRATIONS_DIR} -> ${REMOTE_MIGRATIONS_DIR}"
fi

# --- registry login ----------------------------------------------------------

step "Logging in to ${REGISTRY_PATH}"
printf '%s' "${REGISTRY_PASSWORD}" \
  | docker login "${REGISTRY_PATH}" --username "${REGISTRY_USERNAME}" --password-stdin

# --- build & push ------------------------------------------------------------

# The frontend's runtime configuration, as KEY=VALUE lines, ready to be merged
# into the .env beside the remote compose file.
FE_ENV_PAIRS=""

# Reads frontend/.env.<env> into FE_ENV_PAIRS.
#
# The file cannot simply be copied into the image — .dockerignore excludes
# .env.* from the build context on purpose, and an image that carried one
# environment's configuration could not be promoted to another. It is parsed
# here and shipped to the server instead, where the container reads it at
# start-up (frontend/lib/runtime-config.ts).
load_frontend_env() {
  local file="$1"
  FE_ENV_PAIRS=""

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

    # NEXT_PUBLIC_* is the build-time spelling. The app still reads it as a
    # fallback, so this deploys and works — but Next inlined that value into the
    # bundle of whatever image is running, and shipping it here cannot change
    # it. Renaming the key is what makes the setting take effect at run time.
    if [[ "${key}" == NEXT_PUBLIC_* ]]; then
      printf '    \033[1;33mwarning:\033[0m %s in %s is the build-time spelling; rename it to %s so the server can change it without a rebuild\n' \
        "${key}" "$(basename "${file}")" "${key#NEXT_PUBLIC_}" >&2
    fi

    FE_ENV_PAIRS+="${key}=${val}"$'\n'
    info "${key}=${val}"
  done < "${file}"

  [[ -n "${FE_ENV_PAIRS}" ]] || die "${file} defines no configuration"

  # A frontend that cannot reach the API is not worth deploying, and the failure
  # would otherwise be a browser-side 404 rather than a deploy error.
  grep -qE '^(NEXT_PUBLIC_)?API_BASE_URL=' <<<"${FE_ENV_PAIRS}" \
    || die "${file} sets no API_BASE_URL"
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
  # No build args: the image is the same for every environment, which is the
  # only reason a uat image can be promoted to prod without being rebuilt.
  build_and_push "frontend" "${ROOT_DIR}/frontend" "${FE_IMAGE}"

  if [[ "${DO_DEPLOY}" == true && "${WRITE_FRONTEND_ENV}" == true ]]; then
    step "Frontend runtime config from ${FRONTEND_ENV_FILE}"
    load_frontend_env "${FRONTEND_ENV_FILE}"
  fi
fi

if [[ "${DO_BACKEND}" == true ]]; then
  # The backend depends on the private cdtc module. A vendored tree needs no
  # credentials; without one the build fetches from GitLab, so the netrc is
  # mounted as a build secret (never a layer). See CDTC_SETUP.md.
  BE_BUILD_ARGS=()
  if [[ "${DO_BUILD}" == true && ! -d "${ROOT_DIR}/backend/vendor" ]]; then
    [[ -f "${HOME}/.netrc" ]] \
      || die "backend/vendor/ is missing and ~/.netrc does not exist; the private cdtc module cannot be fetched (run 'go mod vendor' in backend/, or see CDTC_SETUP.md)"
    BE_BUILD_ARGS+=(--secret "id=netrc,src=${HOME}/.netrc")
    info "cdtc source : GitLab (netrc build secret)"
  elif [[ "${DO_BUILD}" == true ]]; then
    info "cdtc source : backend/vendor/"
  fi
  build_and_push "backend" "${ROOT_DIR}/backend" "${BE_IMAGE}" ${BE_BUILD_ARGS[@]+"${BE_BUILD_ARGS[@]}"}
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

# The tag that actually runs on the server is decided by the remote compose
# file, which takes it from ${IMAGE_NAME}:${IMAGE_TAG} — read from the .env
# sitting next to it. So the deploy owns those two keys: it rewrites them in
# the remote .env (leaving every other line alone) and exports them for the
# compose invocation, then checks what the service resolves to before pulling.
#
# Both halves matter. The export makes this run deploy the configured tag; the
# .env write makes a reboot or a hand-run `docker compose up -d` come back on
# the same tag instead of whatever was pinned there before.
#
# The frontend's runtime config rides in that same .env, by the same rule and
# for the same reason: the image has none of it, so the file beside the compose
# file is the whole of the environment's configuration, and it has to survive a
# restart as much as the tag does.
#
# Note the values cannot be exported once for the whole session: the frontend
# and backend projects use the same two variable names with different values.
remote_deploy_block() {
  local dir="$1" service="$2" image_name="$3" tag="$4" image="$5" config="${6:-}"

  # Values first, in their own unquoted heredoc; the logic below is quoted so
  # that nothing in it is expanded here instead of on the server.
  cat <<EOF
cd "${dir}"
svc='${service}'
img_name='${image_name}'
img_tag='${tag}'
want='${image}'
name_var='${REMOTE_IMAGE_NAME_VAR}'
tag_var='${REMOTE_IMAGE_TAG_VAR}'
write_env='${WRITE_REMOTE_ENV}'
check='${CHECK_REMOTE_IMAGE}'
check_config='${CHECK_REMOTE_ENV}'
EOF

  # The runtime config, held in a variable rather than written out: nothing
  # touches the server's filesystem until the image check below has passed.
  # The heredoc is quoted, so no value is expanded by either shell.
  printf 'cfg=$(cat <<%s\n' "'JIVE_ENV_EOF'"
  printf '%s' "${config}"
  printf 'JIVE_ENV_EOF\n)\n'

  cat <<'EOF'
echo "--> $PWD"

# The environment wins over the .env file, so this is what the compose commands
# below resolve against — including when --no-remote-env leaves the file alone.
export "$name_var=$img_name" "$tag_var=$img_tag"

# Checked before anything on the server is written or pulled, so a compose file
# that ignores these variables leaves the machine exactly as it was.
if [ "$check" = true ]; then
  resolved="$(docker compose config --images "$svc" 2>/dev/null || true)"
  if ! printf '%s\n' "$resolved" | grep -Fxq "$want"; then
    echo "error: compose in $PWD resolves service $svc to: ${resolved:-<nothing>}" >&2
    echo "       but this deploy pushed $want" >&2
    echo "       the service is expected to read its image from the deploy:" >&2
    echo "         image: \${$name_var}:\${$tag_var}" >&2
    echo "       re-run with --no-image-check to deploy the pinned tag anyway." >&2
    exit 1
  fi
fi

# Rewrites each KEY= line in place and appends the ones that were not there,
# leaving every other line — comments, blanks, keys this deploy does not own —
# exactly as it found them.
merge_env() {
  touch .env
  awk -v pairs="$1" '
    BEGIN {
      while ((getline line < pairs) > 0) {
        if (line !~ /^[A-Za-z_][A-Za-z0-9_]*=/) continue
        k = substr(line, 1, index(line, "=") - 1)
        if (!(k in val)) order[++n] = k
        val[k] = line
      }
    }
    {
      key = $0
      sub(/^[[:space:]]*/, "", key)
      sub(/^export[[:space:]]+/, "", key)
      if (key ~ /^[A-Za-z_][A-Za-z0-9_]*=/) {
        k = substr(key, 1, index(key, "=") - 1)
        if (k in val) {
          if (!(k in done)) { print val[k]; done[k] = 1 }
          next
        }
      }
      print
    }
    END { for (i = 1; i <= n; i++) if (!(order[i] in done)) print val[order[i]] }
  ' .env > .env.deploy-tmp && mv .env.deploy-tmp .env
}

: > .env.deploy-pairs
if [ "$write_env" = true ]; then
  printf '%s=%s\n%s=%s\n' "$name_var" "$img_name" "$tag_var" "$img_tag" >> .env.deploy-pairs
fi
if [ -n "$cfg" ]; then
  printf '%s\n' "$cfg" >> .env.deploy-pairs
fi

if [ -s .env.deploy-pairs ]; then
  merge_env .env.deploy-pairs
  while IFS= read -r pair; do
    [ -n "$pair" ] && echo "    .env: $pair"
  done < .env.deploy-pairs
fi
rm -f .env.deploy-pairs

# Writing the file is not the same as the container reading it: compose uses
# .env for interpolation, and only passes a name into the container if the
# service asks for it. A silent miss here would leave the app on its built-in
# localhost default, which fails in the browser rather than in this deploy.
if [ -n "$cfg" ] && [ "$check_config" = true ]; then
  resolved_env="$(docker compose config "$svc" 2>/dev/null || true)"
  missing="$(printf '%s\n' "$cfg" | while IFS= read -r pair; do
    [ -n "$pair" ] || continue
    key="${pair%%=*}"
    printf '%s\n' "$resolved_env" | grep -qE "^[[:space:]]*$key:" || printf '%s ' "$key"
  done)"
  if [ -n "$missing" ]; then
    echo "error: service $svc in $PWD never receives: $missing" >&2
    echo "       the deploy wrote them to $PWD/.env, but the compose file does" >&2
    echo "       not pass them into the container. Add to the $svc service:" >&2
    echo "         env_file:" >&2
    echo "           - .env" >&2
    echo "       or map each name under environment:. Re-run with --no-env-check" >&2
    echo "       to deploy anyway — the app would then fall back to its built-in" >&2
    echo "       defaults, which point at localhost." >&2
    exit 1
  fi
fi

docker compose pull "$svc"
docker compose up -d "$svc"
docker compose ps "$svc"
EOF
}

if [[ "${DO_DEPLOY}" == true ]]; then
  step "Deploying to ${SSH_HOST}"

  # Fed to the remote shell over stdin rather than embedded in the ssh command
  # line: the script quotes freely, and `set -e` still aborts the whole deploy
  # at the first failing step.
  remote_script="set -euo pipefail"
  remote_script+=$'\n'"printf '%s' \"\${REGISTRY_PASSWORD}\" | docker login ${REGISTRY_PATH} --username ${REGISTRY_USERNAME} --password-stdin"

  if [[ "${DO_BACKEND}" == true ]]; then
    remote_script+=$'\n'"$(remote_deploy_block \
      "${REMOTE_BE_DIR}" "${BE_SERVICE}" "${REGISTRY_PATH}/${BE_IMAGE_NAME}" "${TAG_BE}" "${BE_IMAGE}")"
  fi
  if [[ "${DO_FRONTEND}" == true ]]; then
    remote_script+=$'\n'"$(remote_deploy_block \
      "${REMOTE_FE_DIR}" "${FE_SERVICE}" "${REGISTRY_PATH}/${FE_IMAGE_NAME}" "${TAG_FE}" "${FE_IMAGE}" \
      "${FE_ENV_PAIRS}")"
  fi

  # REGISTRY_PASSWORD is passed through the remote env rather than interpolated
  # into the script, so it never shows up in the remote process list.
  printf '%s\n' "${remote_script}" | ssh_run "REGISTRY_PASSWORD='${REGISTRY_PASSWORD}' bash -s"
fi

step "Done"
[[ "${DO_FRONTEND}" == true ]] && info "frontend : ${FE_IMAGE}"
[[ "${DO_BACKEND}"  == true ]] && info "backend  : ${BE_IMAGE}"
exit 0
