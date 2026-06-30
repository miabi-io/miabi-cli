# miabi CLI

The imperative client for a [Miabi](https://github.com/miabi-io/miabi) control panel.
Drive the deploy flow from a terminal or CI — `miabi deploy --app web --tag $SHA --wait`
updates the image, deploys, blocks until the deployment is terminal, and **exits
non‑zero on failure**.

It is a pure consumer of the documented `/api/v1` HTTP API; it imports nothing from the
server.

## Install

**Go:**

```bash
go install github.com/miabi-io/miabi-cli@latest   # installs the `miabi` binary
```

**Prebuilt binary** — download from the [GitHub Releases](https://github.com/miabi-io/miabi-cli/releases/latest)
page, or grab the archive for your platform directly (Linux x86_64 shown):

```bash
VERSION=0.1.0
curl -fsSL "https://github.com/miabi-io/miabi-cli/releases/download/v${VERSION}/miabi_${VERSION}_linux_amd64.tar.gz" \
  | tar -xz miabi && sudo mv miabi /usr/local/bin/
miabi --version
```

(Swap `linux_amd64` for `linux_arm64`, `darwin_amd64`, `darwin_arm64`, or use the `.zip` for Windows.)

**Docker** (GitHub Container Registry):

```bash
docker run --rm -e MIABI_URL -e MIABI_TOKEN ghcr.io/miabi-io/miabi-cli:latest whoami
```

## Authenticate

The CLI resolves its context as **flags → env → `~/.miabi/config.yaml`**:

```bash
export MIABI_URL=https://miabi.example.com
export MIABI_TOKEN=mb_…            # an API key with the `deploy` scope

# …or persist it:
miabi --url "$MIABI_URL" --token "$MIABI_TOKEN" login
```

`~/.miabi/config.yaml` (written by `login` / `workspace switch`, mode `0600`):

```yaml
url: https://miabi.example.com
token: mb_…
workspace:
  id: 42
  name: acme-prod
  display_name: Acme Prod
```

## Commands

```
miabi whoami                       # identity, scopes, bound workspace
miabi workspace list|show|switch   # set the active workspace context
miabi apps                         # list applications
miabi deploy   --app web --tag $SHA [--strategy rolling] [--wait] [--timeout 10m]
miabi status   --app web [--deployment 123]
miabi logs     --app web --deployment 123 [--follow]
miabi releases --app web
miabi rollback --app web (--release 12 | --to-previous)
miabi env set    --app web KEY=VALUE [--secret]
miabi env import --app web --file .env [--secret]
miabi apply  -f stack.yaml [--prune] [--dry-run]  # declarative: converge to a manifest bundle
miabi delete -f stack.yaml [--dry-run]            # delete exactly the resources the bundle names
```

### Declarative apply

`miabi apply` converges a workspace to a bundle of `miabi.io/v1` manifests (the same
contract GitOps uses). See [`docs/stack.yaml`](docs/stack.yaml) for a complete, valid
example (volume, generated secret, Postgres, app with mounts + `{{ .databases.* }}` /
`{{ .secrets.* }}` interpolation, domain, route):

```bash
miabi apply -f docs/stack.yaml --dry-run  # preview the plan (+ creates, ~ updates, - deletes)
miabi apply -f app.yaml -f db.yaml        # multiple files → one bundle
cat stack.yaml | miabi apply -f -         # stdin
miabi apply -f docs/stack.yaml --prune    # also delete managed resources absent from the bundle
```

It prints the plan, applies it, and exits non-zero if any resource fails to converge.

`miabi delete -f` is the inverse: it removes exactly the resources the bundle names
(in dependency-safe order, dependents first), skipping any that don't exist:

```bash
miabi delete -f docs/stack.yaml --dry-run   # preview what would be deleted
miabi delete -f docs/stack.yaml             # delete them
```

Each document is `{ apiVersion: miabi.io/v1, kind, metadata: { name }, spec: { … } }`.
Kinds: `Application`, `Stack`, `Database`, `Volume`, `Secret`, `Route`, `Domain`,
`Project`. Names match `^[a-z0-9][a-z0-9-]*$` (Domain names are FQDNs); use a
hyphen-free name for anything referenced via dotted `{{ .secrets.<name> }}` /
`{{ .databases.<name>.* }}` interpolation.

- `--app` takes a **slug** (or numeric id); the workspace comes from `--workspace`,
  the active workspace, or a workspace‑bound token.
- `--json` on read commands gives `jq`‑friendly output; human tables otherwise.
- `--verbose` logs every HTTP request to stderr.

## CI example (GitHub Actions)

```yaml
- run: |
    go install github.com/miabi-io/miabi-cli@latest
    miabi deploy --app web --tag "${{ github.sha }}" --wait
  env:
    MIABI_URL:   ${{ vars.MIABI_URL }}
    MIABI_TOKEN: ${{ secrets.MIABI_DEPLOY_TOKEN }}
```

`--wait` makes the step fail when the deployment fails.

## Notes on the current API

A few server capabilities the long‑term design assumes are not yet in the panel; the
CLI adapts client‑side so it works against today's API:

- **`current` addressing** isn't in the URL scheme yet; the CLI addresses a
  workspace by its **name** in the URL and resolves an **app slug → numeric id**
  before each call.
- **Server‑side `wait`** isn't available, so `--wait` **polls** the deployment status
  until it is terminal.
- **`--image`** override and an **`Idempotency‑Key`** for retry‑safe deploys depend on
  upcoming machine‑API work; `--tag` (the common CI flow) is supported today.


---

## License

Apache License 2.0

## Copyright

Copyright (c) 2026 Jonas Kaninda