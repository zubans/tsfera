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
