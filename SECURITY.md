# Security

Sparkbase runs other people's data on shared hosts, so we treat a cross-tenant leak as the worst bug the project can have.

## Reporting a vulnerability

Please do not open a public issue. Use GitHub's private vulnerability reporting on this repository (Security → Report a vulnerability). Include what you found, how to reproduce it and the version.

We aim to acknowledge reports within 3 days and to ship a fix or a mitigation within 30 days for serious issues. We credit reporters in the release notes unless you ask us not to.

## Supported versions

Security fixes go into the latest release. Upgrade with `docker compose pull && docker compose up -d`.

## Scope

In scope: isolation between services and projects, authentication and session handling, key handling, the project API, the Docker install as shipped. Out of scope: issues that need an admin account on the install, or a compromised host.
