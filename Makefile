.PHONY: test-backend vet-backend build-backend test-frontend build-frontend test

test-backend:
	cd backend && go test -race ./...

vet-backend:
	cd backend && go vet ./...

build-backend:
	cd backend && go build -o bin/tsfera-server ./cmd/server

test-frontend:
	cd frontend && npm test

build-frontend:
	cd frontend && npm run build

test: vet-backend test-backend test-frontend build-frontend

# URLs are read from environment; dump paths are never interpolated as shell code.
export MIGRATION_DUMP = $(DUMP)
.PHONY: migrate-plan migrate-dump test-migration
migrate-plan:
	python3 scripts/migration/migrate.py

migrate-dump:
	python3 scripts/migration/migrate.py --apply

test-migration:
	python3 -B -m unittest discover -s scripts/migration -p 'test_*.py'

# Explicit exceptions for a metadata-only rehearsal or restricted imported accounts.
export MIGRATION_ALLOW_MISSING_FILES = $(ALLOW_MISSING_FILES)
export MIGRATION_ALLOW_UNMAPPED_PERMISSIONS = $(ALLOW_UNMAPPED_PERMISSIONS)
export MIGRATION_PERMISSION_MAP = $(PERMISSION_MAP)
