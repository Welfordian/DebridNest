.PHONY: test test-integration vet dashboard build

test:
	go test ./...

test-integration:
	go test -tags=integration ./test/integration/...

vet:
	go vet ./...

dashboard:
	cd web/dashboard && npm ci && npm run build

build: dashboard
	go build -o bin/debridnest ./cmd/debridnest
