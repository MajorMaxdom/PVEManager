# Contributing

## Setup

You need Go (version in `go.mod`) and Node.js 24.

```
make web         # build the frontend into web/dist (embedded into the binary)
make build       # go build -> bin/pve-manager
make test        # go vet + go test
make e2e         # browser tests against a mock PVE host and a mock OIDC provider
```

First run of the browser tests: `cd web/e2e && npm ci && npx playwright install chromium`.

`web/e2e/demo.sh` starts the whole stack against **mock** hosts with made-up data, which is also the quickest way to
try the UI without a Proxmox server.

## Guidelines

- Keep changes focused and add a test. Security-relevant code (authentication, sessions, API keys, SSO, backup,
  input validation towards PVE) needs a test that fails when the check is removed.
- Everything that ends up in a PVE parameter string is validated first (`internal/pve`); keep it that way.
- The UI is German and English: add both texts in `web/app/src/lib/i18n.svelte.ts`.
- Do not commit real host names, addresses, tokens or screenshots of real systems. Use `example.net` style names.
