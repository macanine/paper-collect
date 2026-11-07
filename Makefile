.PHONY: check test-all

# Quick pre-commit check: skips slow LibreOffice/formula tests.
check:
	cd backend && go test -short ./... && go vet ./...
	cd web && npm test && npm run build

# Full suite, as run in CI.
test-all:
	cd backend && go test ./... && go vet ./...
	cd web && npm test && npm run build
