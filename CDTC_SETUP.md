# `cdtc` Shared Contract Repo — Setup

How to consume the shared struct/contract repo
[`company/shared/cdtc`](https://gitlab.pg-poppay.com/company/shared/cdtc) as a Go
dependency in `backend/`.

Two things make this more involved than a normal `go get`:

- **It's private, on a GitLab subgroup.** Go authenticates module discovery separately
  from git, and GitLab answers unauthenticated probes misleadingly.
  → [Why authentication is awkward](#why-authentication-is-awkward)
- **The module's declared name doesn't match where it lives.** It sits at
  `gitlab.pg-poppay.com/company/shared/cdtc` in the `go/` subdirectory, but declares
  itself `github.com/pgauto/cdtc`. A `replace` bridges the two.
  → [Why the replace is needed](#why-the-replace-is-needed)

Want just the working answer? → [Step 5](#step-5--wire-up-the-dependency).

---

## Prerequisites

A GitLab personal access token (PAT) with **both** scopes:

| Scope | Needed for |
| --- | --- |
| `read_repository` | `git ls-remote` / `git fetch` of the module source |
| `read_api` | (recommended) API access |

Your account must be able to see `company/shared/cdtc` in the GitLab UI. A **project
access token** issued on `cdtc` itself works equally well — it authenticates as a
project bot user.

> The token is the **password** in HTTP Basic auth. GitLab ignores the username, so
> `oauth2` is a fine placeholder. GitLab rejects account passwords outright.

---

## Step 1 — Mark the host as private

```fish
go env -w GOPRIVATE='gitlab.pg-poppay.com/*'
```

Makes Go skip `proxy.golang.org` and `sum.golang.org` for this host and fetch directly
over git. Implies `GONOPROXY` and `GONOSUMDB`. Verify with
`go env GOPRIVATE GONOPROXY GONOSUMDB`.

> This is **global**, not per-project — `go env -w` writes to `~/.config/go/env`, the
> only Go env file there is. That's fine: it's a host pattern, so it changes nothing
> for any other project. Environment variables override it if you ever need to scope
> it (`GOENV=$PWD/go.env`, direnv, or a CI variable).

## Step 2 — Give git the token

```fish
git config --global credential.helper store
```

> Must be `--global`. The `git` subprocess Go spawns runs inside
> `~/go/pkg/mod/cache/vcs/<hash>`, **not** in your project, so it only reads
> `~/.gitconfig` and system config. A repo-local helper is invisible to it.

> `credential.helper` takes the name of a **helper program**, not a credential.
> Putting a token there produces `git: 'credential-glpat-…' is not a git command`.

Seed the store with one interactive auth. The token goes at the **password prompt** —
never in a config value or on a command line, where it lands in shell history:

```fish
git ls-remote https://gitlab.pg-poppay.com/company/shared/cdtc.git
# Username: oauth2
# Password: <paste PAT>
```

Printing refs means the token is valid and `~/.git-credentials` now exists. For a
keyring instead of a plaintext file, use `credential.helper libsecret`.

## Step 3 — Give **Go** the token

Go does not reuse git's credentials for repo discovery — it makes its own HTTPS
request, authenticated via `GOAUTH`.

> **`GOAUTH='git <dir>'` does not work against this GitLab instance.** Per
> `go help goauth`, that mode only re-invokes the credential helper *"if the server
> responds with any 4xx code"*, and GitLab replies **200**. See
> [below](#why-authentication-is-awkward).

### Option A — `netrc` — *currently configured on this machine*

```fish
read -s -P "PAT: " tok
printf 'machine gitlab.pg-poppay.com\n  login oauth2\n  password %s\n' $tok > ~/.netrc
chmod 600 ~/.netrc
set -e tok
go env -w GOAUTH=netrc
```

`read -s` keeps the token out of shell history. This file is also read by git, so it
covers Step 2 as well if you'd rather skip the credential helper.

> **Keep `~/.netrc` and `~/.git-credentials` in sync.** If they hold different tokens,
> git succeeds while Go fails, and GitLab reports the rejected credential as a **404** —
> which reads like "the path doesn't exist" rather than "your token is bad." This is the
> single most confusing failure mode here. See
> [Troubleshooting](#troubleshooting).

### Option B — `command` (single source of truth)

Derives the header from `~/.git-credentials` so the token lives in one place only —
which also makes the drift above impossible. Create `~/.config/go/gitlab-goauth.sh`:

```sh
#!/bin/sh
host=gitlab.pg-poppay.com

cred=$(printf 'protocol=https\nhost=%s\n\n' "$host" | GIT_TERMINAL_PROMPT=0 git credential fill 2>/dev/null)
u=$(printf '%s\n' "$cred" | sed -n 's/^username=//p')
p=$(printf '%s\n' "$cred" | sed -n 's/^password=//p')
[ -n "$p" ] || exit 0

printf 'https://%s\n\nAuthorization: Basic %s\n\n' \
  "$host" "$(printf '%s:%s' "$u" "$p" | base64 -w0)"
```

```fish
chmod 700 ~/.config/go/gitlab-goauth.sh
go env -w GOAUTH='command /home/dev3/.config/go/gitlab-goauth.sh'
```

> `GOAUTH` entries are split on whitespace, so the path must contain **no spaces**.
> Anything under `/home/dev3/01 Work/…` fails with
> `GOAUTH=git dir method requires an absolute path to the git working directory`.

## Step 4 — Verify auth

```fish
go list -mod=mod -m gitlab.pg-poppay.com/company/shared/cdtc/go@latest
```

Expected — a pseudo-version off `main`:

```
gitlab.pg-poppay.com/company/shared/cdtc/go v0.0.0-20260810054158-27225c7fa75d
```

`-mod=mod` is required because `backend/vendor/` exists, which auto-enables
`-mod=vendor` and blocks module queries.

- Reports `company/shared` (the *subgroup*) → credentials aren't reaching discovery.
- Reports `404 Not Found` → credentials are reaching it and being **rejected**.

Both point at Step 3. This command is also how you get the version string for Step 5.

## Step 5 — Wire up the dependency

You cannot `go get` this module by its real location — its `go.mod` declares a
different identity, and Go enforces the match:

```
module declares its path as: github.com/pgauto/cdtc
        but was required as: gitlab.pg-poppay.com/company/shared/cdtc/go
```

So require it under its **declared** name and redirect that name to its **real**
location. Add only the `replace` by hand, in `backend/go.mod`:

```
replace github.com/pgauto/cdtc => gitlab.pg-poppay.com/company/shared/cdtc/go v0.0.0-20260810054158-27225c7fa75d
```

Then write an import (Step 6) and run `go mod tidy`. Go adds the matching `require`
itself:

```
github.com/pgauto/cdtc v0.0.0-00010101000000-000000000000
```

That all-zeros version is **correct, not a bug**. Go cannot query a version for
`github.com/pgauto/cdtc` because no such repo exists, so it writes a placeholder; the
`replace` supplies the real source and version. Leave it alone — don't hand-edit it to
look like a real version.

Confirm the redirect is live:

```fish
go list -m github.com/pgauto/cdtc
# github.com/pgauto/cdtc v0.0.0-00010101000000-000000000000 => gitlab.pg-poppay.com/company/shared/cdtc/go v0.0.0-…-27225c7fa75d
```

> **The `require` line only exists while something imports the package.** `go mod tidy`
> prunes unused requirements, and a `replace` alone doesn't count as usage. The
> `replace` survives on its own, so the pair re-forms as soon as you add an import.

### Bumping the version

**Only the `replace` line's version matters** — edit that one and leave the `require`
placeholder untouched. There's no `go get` for the right-hand side of a `replace`, so
get the new pseudo-version from the Step 4 command, paste it in, then:

```fish
go mod tidy
go mod vendor
```

## Step 6 — Import it

Import under the **declared** name. This matches how cdtc imports itself internally, so
it's consistent with the upstream source:

```go
import (
    "github.com/pgauto/cdtc/paynet"
    "github.com/pgauto/cdtc/callback"
)
```

| Import path (prefix `github.com/pgauto/cdtc/`) | Package | Notable types |
| --- | --- | --- |
| `paynet` | `paynet` | `Account`, `InquiryRequest`, `InquiryBankRequest`, `InquiryResponse`, `InquiryBankResponse`, `TransactionResponse`, `ResponseData` |
| `callback` | `callback` | `Request`, `TrxType`, `AdditionalInfo` |
| `method` | `method` | `Method`, `QRPayload`, `VirtualAccountPayload`, `EWalletPayload`, `DebitCardPayload`, `CreditCardPayload`, `CryptoPayload`, `BankTransferPayload` |
| `status` | `status` | `Status` |
| `merchant` | **`paynet`** | `MerchantSettingRequest`, `MerchantSettingUpdateRequest`, `MerchantBulkRequest` |
| `checkout` | `checkout` | `Data`, `RawData`, `Detail` |
| `dashboard` | `dashboard` | `Request`, `Response`, `ResponseDataObj`, `PaymentNotification`, `VirtualAccountPayload`, `CreditCardPayload` |
| `aggregator` | `aggregator` | `CallbackPayload` |
| `snap/va` | `va` | `VARequest`, `VAResponse`, `Response`, `Amount`, `Type`, `AdditionalInfoObj` |
| `snap/va/status` | **`status_va`** | `Status` |

> **There is no package at the module root.** `github.com/pgauto/cdtc` holds only
> `go.mod` and subdirectories, so importing it bare fails with
> `no required module provides package`. Every import needs a subpackage on the end.

> **Two naming traps.** `merchant/` declares `package paynet`, colliding with the real
> `paynet`; and `snap/va/status` declares `package status_va`, colliding with `status`.
> Alias them:
>
> ```go
> import (
>     "github.com/pgauto/cdtc/paynet"
>     merchant "github.com/pgauto/cdtc/merchant"
>     vastatus "github.com/pgauto/cdtc/snap/va/status"
> )
> ```

### The contract types

`callback.Request` is the webhook payload. Its JSON tags are heavily abbreviated:

```go
type Request struct {
	RefID          string         `json:"ri"`
	NetTrxID       string         `json:"nti"`
	Status         status.Status  `json:"s"`
	TrxDate        string         `json:"td"`
	TrxFromOrTo    string         `json:"tft"`
	TrxType        TrxType        `json:"tt"`
	AdditionalInfo AdditionalInfo `json:"ai,omitempty"`
}
```

The enums are integer types with `stringer`-generated `String()`, each exposing an
`Enum()` that returns all values:

| Type | Values, in order from `0` |
| --- | --- |
| `status.Status` | `Pending`, `Reject`, `Cancel`, `Expired`, `Obscure`, `Completed` |
| `callback.TrxType` | `DEPOSIT`, `WITHDRAW` |
| `method.Method` | `QR`, `VirtualAccount`, `EWallet`, `DebitCard`, `CreditCard`, `Crypto`, `BankTransfer`, `Cash` |

> **Zero values are meaningful.** `Pending`, `DEPOSIT`, and `QR` are all `0`, so a
> webhook that omits `"s"` unmarshals to `Pending` rather than erroring. Validate
> presence explicitly if that distinction matters to the order state machine.

`TransactionResponse` is **generic**, and its constraint is a concrete type — so
`method.Method` is the only permitted type argument:

```go
type TransactionResponse[M method.Method] struct { … }

var resp paynet.TransactionResponse[method.Method]
```

## Step 7 — Re-vendor

`backend/vendor/` exists locally and is gitignored (root `.gitignore:5`), so regenerate
it after any dependency change. Order matters — `go mod vendor` copies *imported*
packages only, so an import must exist first:

```fish
go mod tidy
go mod vendor
```

Vendored source lands under `vendor/github.com/pgauto/cdtc/`, one directory per
imported subpackage. Each new subpackage you import needs another `go mod vendor`.

## Step 8 — Docker builds

Whether the image build needs credentials depends entirely on whether `vendor/` is in
the build context.

`.dockerignore` does **not** exclude `vendor/`, so when it exists locally `COPY . .`
copies it in, the compile runs in vendor mode, and the build needs no network and no
token. That's the normal case on a developer machine, and it's why `docker compose up`
works after a local `go mod vendor`.

On a **fresh clone or CI checkout** there is no `vendor/` (it's gitignored), so the
compile has to fetch the private module and fails without credentials.

> **Two separate failures hide here, and the first one bites even when credentials
> are present.** `golang:1.26-alpine` ships **no `git`**, and the `replace` directive
> points at a VCS path whose pseudo-version can only be resolved by a git subprocess.
> That produced
> `unable to resolve git version: failed to execute git version: exec: "git": executable file not found in $PATH`
> from `go mod download` — so the builder stage now installs git with `apk add`.
>
> The dependency layer also used to run `go mod download` *before* `COPY . .`, which
> forced module-mode resolution of `cdtc` even on machines that had a perfectly good
> `vendor/` tree sitting in the context. The Dockerfile now copies the source first
> and only downloads when `vendor/` is absent.

**Simplest fix — commit the vendor tree.** Drop `vendor/` from the root `.gitignore`
and check `backend/vendor/` in. No credentials anywhere in the build, and the image is
reproducible from the repo alone.

**Otherwise, mount a netrc as a build secret**, which keeps it out of the image
layers. This is what `backend/Dockerfile` does today:

```dockerfile
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=secret,id=netrc,target=/root/.netrc \
    export GOPRIVATE='gitlab.pg-poppay.com/*' GOAUTH=netrc && \
    if [ ! -d vendor ]; then go mod download; fi && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/api ./cmd/api
```

```fish
docker build --secret id=netrc,src=$HOME/.netrc .
```

The secret is optional to BuildKit (`required=false` is the default), so a vendored
build needs no `--secret` flag at all. `scripts/deploy.sh` passes it automatically
when `backend/vendor/` is missing, and fails loudly if neither is available.

In GitLab CI, write the secret from a masked variable rather than mounting `$HOME`.

---

## Why authentication is awkward

Fetching a private module involves **two** network steps with **two** separate auth
systems:

| Step | Who authenticates | Config read |
| --- | --- | --- |
| `?go-get=1` discovery | Go's own HTTP client | `GOAUTH` |
| `git ls-remote` / fetch | a spawned `git` process | git config |

Configuring only git leaves discovery unauthenticated. When GitLab receives an
unauthenticated `?go-get=1` for a project it can't see, it does **not** return 401 or
404 — it returns **HTTP 200** with a fallback meta tag naming the *subgroup*:

```
<meta name="go-import" content="gitlab.pg-poppay.com/company/shared git .../company/shared.git">
```

Go believes it and runs `git ls-remote` against `company/shared.git`, which doesn't
exist. Authenticated, the same request correctly returns
`.../company/shared/cdtc git .../company/shared/cdtc.git`.

That 200 is also exactly why `GOAUTH='git <dir>'` cannot work here: it only retries
after a 4xx, so the credential store is never consulted no matter how correctly it's
configured. Only `netrc` and `command` attach credentials proactively.

A **rejected** credential behaves differently again — GitLab answers `404`, not `401`,
so a stale token looks like a missing repository.

## Why the replace is needed

`cdtc` is a polyglot contract repo — `go/`, `php/`, `python/`, `rust/` — with **no
`go.mod` at the repo root**. The Go code is a self-contained module at `go/go.mod`
declaring:

```
module github.com/pgauto/cdtc
```

That address doesn't resolve, and it isn't where the code lives. Go verifies that a
module's declared path matches how it was required, so requiring it by its real
location fails outright. The `require`/`replace` pair satisfies both halves: the
`require` uses the declared name, and the `replace` says where to actually fetch it.

Go also **strips nested modules from the parent's zip**, so fetching the repo root
(`gitlab.pg-poppay.com/company/shared/cdtc`, no `/go`) yields an archive containing
`php/`, `python/`, and `rust/` but no Go code at all.

### The cleaner alternative

Two one-time changes upstream would remove the `replace` entirely — neither affects
`php/`, `python/`, or `rust/`:

1. **Correct the module path** in `go/go.mod` to
   `gitlab.pg-poppay.com/company/shared/cdtc/go`.
2. **Tag the nested module** with a subdirectory-prefixed tag — `go/v1.0.0`, not
   `v1.0.0`. An unprefixed tag is read as a tag of the *root* module and fails with
   `found, but does not contain package …/cdtc/go`.

Consumers would then reduce to one line with a real version:

```fish
go get gitlab.pg-poppay.com/company/shared/cdtc/go@v1.0.0
```

Worth raising with whoever maintains the repo. Beyond the tidiness, a `replace` only
applies to the **main module** — if another Go service ever imports `backend` as a
library, it won't inherit the redirect and will fail on `github.com/pgauto/cdtc`.

---

## Troubleshooting

| Symptom | Cause |
| --- | --- |
| `404 Not Found` on `?go-get=1`, but `git ls-remote` works | `~/.netrc` and `~/.git-credentials` hold **different** tokens. Go's is stale; GitLab reports rejection as 404. Step 3. |
| `fatal: repository '.../company/shared.git/' not found` | Discovery unauthenticated — subgroup fallback. Step 3. |
| `module declares its path as: github.com/pgauto/cdtc` | Required by its GitLab location instead of its declared name. Step 5. |
| `no required module provides package github.com/pgauto/cdtc` | Imported the module root; there's no package there. Add a subpackage. Step 6. |
| `require` line disappears after `go mod tidy` | Nothing imports it yet. Expected — write the import first. Step 5. |
| `require` shows `v0.0.0-00010101000000-000000000000` | Correct. Go can't version a non-existent repo; the `replace` carries the real one. Step 5. |
| `go get` succeeds but nothing to import | Fetched the repo root, which holds no Go code. Step 5. |
| `found, but does not contain package …/cdtc/go` | Pinned to an unprefixed tag. Nested modules need `go/vX.Y.Z`. |
| `HTTP Basic: Access denied` | Bad/expired token, or an account password used instead of a PAT. |
| `git: 'credential-glpat-…' is not a git command` | Token placed in `credential.helper`, which expects a program name. Step 2. |
| `Inconsistent vendoring detected` | `go.mod` changed without re-running `go mod vendor`. Step 7. |
| `cannot query module due to -mod=vendor` | `vendor/` forces vendor mode; add `-mod=mod` for queries. |
| `GOAUTH=git dir method requires an absolute path…` | Whitespace in the `GOAUTH` path. Step 3. |
| `could not read Username … No such device or address` | Non-interactive shell with no stored credential. Run Step 2 in a real terminal. |
| `paynet redeclared` / `status redeclared` | `merchant` is `package paynet`; `snap/va/status` is `package status_va`. Alias the imports. Step 6. |

Compare the two credential files without printing the secrets:

```fish
sed -n 's/^[[:space:]]*password[[:space:]]\+//p' ~/.netrc | head -1 | sha256sum
sed -E 's#^https://[^:]*:([^@]*)@.*#\1#' ~/.git-credentials | head -1 | sha256sum
```

Clear a cached bad resolution — the cache is keyed by remote URL, so a wrong one
persists until removed. These are **bare** repos, so config sits directly in the cache
directory. Run the `grep` alone first to see what would be deleted:

```fish
grep -rl 'company/shared\.git' ~/go/pkg/mod/cache/vcs/*/config | xargs -r dirname | xargs -r rm -rf
```

---

## Security notes

- The token is a credential. It belongs at an interactive prompt or in a `600` file —
  never in a config value, a command-line argument, or a committed file.
- `credential.helper store` and `~/.netrc` are both **plaintext**. Use
  `credential.helper libsecret` to keep it in the keyring instead.
- Never embed a token in a `url.<base>.insteadOf` rule — it writes the secret into
  `~/.gitconfig` in cleartext.
- If a token has been pasted on a command line, assume it is in shell history
  (`~/.local/share/fish/fish_history`) and rotate it.
