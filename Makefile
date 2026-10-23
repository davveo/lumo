GOWORK ?= off
export GOWORK

.PHONY: up down test test-web build smoke proto
up:
	sh start.sh
down:
	sh scripts/down.sh
test:
	go test ./...
test-web:
	sh scripts/test-frontend.sh
build:
	go build -buildvcs=false -o bin/server ./cmd/server
	go build -buildvcs=false -o bin/gateway ./cmd/gateway
smoke:
	sh scripts/smoke.sh
proto:
	protoc --go_out=. --go_opt=module=github.com/davveo/lumo --go-grpc_out=. --go-grpc_opt=module=github.com/davveo/lumo api/mall/v1/domain.proto
