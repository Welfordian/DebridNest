.PHONY: test test-integration vet build

test:
	go test ./...

test-integration:
	go test -tags=integration ./test/integration/...

vet:
	go vet ./...

build:
	go build -o bin/debridnest ./cmd/debridnest
