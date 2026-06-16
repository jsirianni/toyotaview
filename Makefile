.PHONY: fmt test race vet lint gosec build run snapshot

fmt:
	goimports -w .

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

gosec:
	gosec ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/smartcar-4runner ./cmd/smartcar-4runner

run:
	go run ./cmd/smartcar-4runner

snapshot:
	goreleaser release --snapshot --clean
