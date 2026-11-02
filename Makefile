GOWORK ?= off
export GOWORK
GO = sh scripts/go.sh

.PHONY: up down test test-web test-browser build build-web smoke proto format-tools format format-check
up:
	sh start.sh
down:
	sh scripts/down.sh
test:
	$(GO) test ./...
test-web:
	sh scripts/test-frontend.sh
test-browser:
	sh scripts/test-browser.sh
build-web:
	node web/build.cjs
build:
	$(GO) build -buildvcs=false -o bin/server ./cmd/server
	$(GO) build -buildvcs=false -o bin/gateway ./cmd/gateway
smoke:
	sh scripts/smoke.sh
proto:
	python3 scripts/generate-service-rpc.py
	protoc --go_out=. --go_opt=module=github.com/davveo/lumo --go-grpc_out=. --go-grpc_opt=module=github.com/davveo/lumo api/mall/v1/*.proto
format-tools:
	python3 scripts/format.py --install
format:
	python3 scripts/format.py
format-check:
	python3 scripts/format.py --check
