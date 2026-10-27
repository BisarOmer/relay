.PHONY: setup engine desktop-install dev dev-browser test test-integration test-integration-local lint build brokers-up brokers-down

ROOT := $(shell pwd)
ENGINE := $(ROOT)/engine
DESKTOP := $(ROOT)/apps/desktop
TRIPLE := $(shell rustc -vV | sed -n 's/^host: //p')
ENGINE_BIN := $(DESKTOP)/src-tauri/binaries/relay-engine-$(TRIPLE)
# ~/.cargo is root-owned on this machine; keep a writable project-local cache.
export CARGO_HOME := $(ROOT)/.cargo-home

setup: desktop-install engine
	@mkdir -p "$(CARGO_HOME)"
	@echo "Setup complete. Run: make brokers-up && make dev"

desktop-install:
	cd $(DESKTOP) && npm install

engine:
	mkdir -p $(DESKTOP)/src-tauri/binaries
	cd $(ENGINE) && go build -o $(ENGINE_BIN) ./cmd/relay-engine
	@echo "Built $(ENGINE_BIN)"

dev: engine
	mkdir -p "$(CARGO_HOME)"
	cd $(DESKTOP) && npm run tauri dev

# Runs the UI in a normal browser against the real engine (development only).
dev-browser: engine
	node scripts/dev-bridge.mjs --engine $(ENGINE_BIN) & \
	sleep 1 && cd $(DESKTOP) && npx vite --port 1420 --strictPort; \
	kill %1 2>/dev/null || true

test:
	cd $(ENGINE) && go test -race ./...
	cd $(DESKTOP) && npm run lint && npm test

test-integration: brokers-up
	cd $(ENGINE) && go test -tags=integration ./... -count=1
	@$(MAKE) brokers-down

# Integration tests against an already running nats-server (no Docker), e.g.
#   nats-server -js -m 8222 & make test-integration-local
test-integration-local:
	cd $(ENGINE) && RELAY_NATS_MONITOR_URL=$${RELAY_NATS_MONITOR_URL:-http://127.0.0.1:8222} go test -tags=integration ./... -count=1

lint:
	cd $(ENGINE) && go vet ./... && test -z "$$(gofmt -l .)" || (gofmt -l .; echo "gofmt: files need formatting"; exit 1)
	cd $(DESKTOP) && npm run lint

build: engine
	mkdir -p "$(CARGO_HOME)"
	cd $(DESKTOP) && npm run tauri build

brokers-up:
	docker compose -f docker/docker-compose.yml up -d

brokers-down:
	docker compose -f docker/docker-compose.yml down -v
