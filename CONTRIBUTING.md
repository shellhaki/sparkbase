# Contributing

Thanks for helping. Sparkbase is built in the open and every change goes through a pull request.

## Set up

You need Go, Bun, Postgres 17+ and Redis (for example `brew install go bun postgresql@18 redis`).

```bash
make dev        # backend on :8787 and dashboard on :3000
make test       # Go unit and integration tests (starts private Postgres and Redis)
```

Optional suites:

```bash
scripts/dev-services.sh garage-start                     # Garage in Docker, for the S3 backend tests
cd packages/sdk-js && ../../scripts/with-server.sh bun test       # SDK against a live server
scripts/with-server.sh scripts/e2e-dashboard.sh          # dashboard smoke test
scripts/e2e-compose.sh                                   # full Docker install through Traefik
```

## Rules of the codebase

- Read the docs in `docs/` before changing behavior; record choices in `DECISIONS.md`.
- No ORM, no query builder. Hand-written, parameterized SQL.
- Every service keeps its own credentials. A change that weakens isolation needs a test in the isolation suites that fails without the fix.
- Never log secrets: passwords, keys, tokens, connection strings.
- Tests come with the change, against real Postgres and Redis.

## Sign your commits (DCO)

Every commit needs a `Signed-off-by` line, which certifies the [Developer Certificate of Origin](https://developercertificate.org/):

```bash
git commit -s -m "Explain what changed"
```

CI checks it on every pull request.

## Licenses

The server, drivers and dashboard are AGPL-3.0 (`LICENSE`). The SDK (`packages/sdk-js`) and the CLI (`server/cmd/sparkbase`, `server/internal/cli`) are MIT, so they can go into closed-source apps. By contributing you agree your work is released under the license of the files you change.
