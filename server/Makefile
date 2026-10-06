.PHONY: build test unit lint run services

build:
	go build -o bin/sparkbase-server ./cmd/sparkbase-server
	go build -o bin/sparkbase ./cmd/sparkbase

# Integration tests need the dev services: ../scripts/dev-services.sh start
test:
	SB_REQUIRE_SERVICES=1 go test -count=1 ./...

unit:
	go test ./internal/config ./internal/logx ./internal/sbcrypto ./internal/jwt

lint:
	gofmt -l . | (! grep .)
	go vet ./...

run:
	../scripts/dev.sh

services:
	../scripts/dev-services.sh start
