# Changelog

All notable changes are listed here. Versions follow [semantic versioning](https://semver.org/). Every release notes any steps needed to upgrade; database migrations run on their own at startup.

## Unreleased

First release candidate.

- Projects with a project URL and publishable and secret keys.
- Services: Postgres, cache (Redis), storage (Garage or local disk), vector (pgvector), queues, auth (email and password, JWT sessions).
- Isolation per service: own role or ACL user or bucket key, with isolation test suites.
- Limits per plan with warnings, write locks and usage graphs; backups with restore.
- Dashboard, CLI (`sparkbase`) and TypeScript SDK (`@sparkbase/sdk`).
- Docker install with Traefik (TLS and SNI routing for Postgres and Redis), PgBouncer and Garage.
