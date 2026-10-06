.PHONY: dev backend frontend stop test

# Backend + dashboard together. Ctrl+C stops both.
dev:
	scripts/dev-all.sh

# Backend only (API + worker on :8787, starts local Postgres and Redis).
backend:
	scripts/dev.sh

# Dashboard only (:3000), talks to the backend on :8787.
frontend:
	cd web/dashboard && bun install && SPARKBASE_API_URL=http://localhost:8787 bun run dev

# Stop the local Postgres and Redis.
stop:
	scripts/dev-services.sh stop

test:
	cd server && $(MAKE) test
