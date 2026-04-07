.PHONY: dev dev-ui build build-ui install deploy sqlc db-migrate \
        test-env-setup test-env test-run test-bats test-api test-e2e test \
        test-artifacts-download test-artifacts-clean test-push \
        demo

BINARY     := devctl
# Install into the site user's devctl directory.
# When invoked via sudo (make install), SUDO_USER is the non-root caller.
# Fall back to USER for non-sudo environments.
SITE_USER  ?= $(if $(SUDO_USER),$(SUDO_USER),$(USER))
SITE_HOME  := $(shell getent passwd $(SITE_USER) | cut -d: -f6)
# Resolve npx from the site user's nvm if present, fall back to PATH.
NPX        := $(shell find $(SITE_HOME)/.nvm/versions/node -maxdepth 3 -name npx 2>/dev/null | sort -V | tail -1 || which npx 2>/dev/null || echo npx)
# Prepend the nvm bin dir to PATH so node is available when running as root.
NPX_BINDIR := $(dir $(NPX))
# Resolve go from the site user's local install if present, fall back to PATH.
GO         := $(shell find $(SITE_HOME)/.local/share/go/bin $(SITE_HOME)/go/bin /usr/local/go/bin -maxdepth 1 -name go 2>/dev/null | head -1 || which go 2>/dev/null || echo go)
INSTALL_DIR := $(SITE_HOME)/ddev/sites/server/devctl
BIN_DIR    := $(SITE_HOME)/ddev/sites/server/bin
SERVICE_DIR := /etc/systemd/system
VERSION    ?= dev

# Run the Go server in dev mode
dev:
	go run .

# Run the Vite HMR dev server (proxies /api and /ws to :4000)
dev-ui:
	cd frontend && npm run dev

# Build the Vue frontend into ui/dist/
build-ui:
	cd frontend && npm run build

# Build the full binary (frontend first, then Go)
build: build-ui
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) .

# Install the binary and systemd system service (requires root).
# Builds the UI and Go binary as the current (non-root) user first,
# then only uses sudo for the binary copy and service install steps.
# This prevents ui/dist/ from being owned by root.
# Usage: make install   (no sudo needed — the recipe calls sudo internally)
install: build-ui
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) .
	mkdir -p $(INSTALL_DIR)
	sudo install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	sudo $(INSTALL_DIR)/$(BINARY) install --yes --user $(SITE_USER)

# Force-install the service file (use this when you intentionally want to update it).
install-service:
	sudo install -m 644 devctl.service $(SERVICE_DIR)/devctl.service
	sudo systemctl daemon-reload

# Deploy without rebuilding (just copy binary + reload); useful when already built.
# Does NOT overwrite the service file.
deploy:
	mkdir -p $(INSTALL_DIR)
	sudo install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	sudo $(INSTALL_DIR)/$(BINARY) install --yes --user $(SITE_USER)
	@echo "Deployed and restarted devctl."

# Run sqlc code generation
sqlc:
	cd db && sqlc generate

# Run goose migrations against dev DB
db-migrate:
	$(shell go env GOPATH)/bin/goose -dir db/migrations sqlite3 $(SITE_HOME)/sites/server/devctl/devctl.db up

# ─── Demo environment ──────────────────────────────────────────────────────────

# Create a fresh devctl-demo container with seed data and take all screenshots.
# Requires Incus + devctl-ubuntu-base image (make test-env-setup).
demo: build
	@bash scripts/demo.sh

# ─── Test environment ──────────────────────────────────────────────────────────

# One-time setup: pull the ubuntu base image for Incus and bake in prerequisites.
# Re-run this whenever you want to refresh the cached image.
test-env-setup:
	@which incus > /dev/null 2>&1 || (echo "Incus is not installed. See: https://linuxcontainers.org/incus/docs/main/installing/" && exit 1)
	@bash scripts/test-env-setup.sh
	@echo "Setup complete. Run 'make build && make test-env' to launch a test container."

# Download all service binaries/archives into the persistent Incus artifact cache.
# Run once after test-env-setup; re-run to update stale files.
# Requires: sudo (writes to Incus storage pool).
test-artifacts-download:
	@which incus > /dev/null 2>&1 || (echo "Incus is not installed." && exit 1)
	@bash scripts/download-artifacts.sh

# Remove all cached artifacts so the next test-artifacts-download re-downloads everything.
test-artifacts-clean:
	@which incus > /dev/null 2>&1 || (echo "Incus is not installed." && exit 1)
	@POOL=default; VOLUME=devctl-test-artifacts; \
	  CACHE_DIR=$$(incus storage volume get "$$POOL" "$$VOLUME" volatile.rootfs.path 2>/dev/null || echo ""); \
	  if [ -z "$$CACHE_DIR" ]; then CACHE_DIR="/var/lib/incus/storage-pools/$$POOL/custom/$${POOL}_$$VOLUME"; fi; \
	  if [ ! -d "$$CACHE_DIR" ]; then CACHE_DIR="/var/lib/incus/storage-pools/$$POOL/custom/$$VOLUME"; fi; \
	  if [ -d "$$CACHE_DIR" ]; then rm -rf "$$CACHE_DIR"/*; echo "Artifact cache cleared."; else echo "Cache dir not found: $$CACHE_DIR"; fi

# Launch an ephemeral test container (interactive — Ctrl+C to destroy).
# Tests run inside the container; the host devctl is never stopped.
# Requires the binary to be pre-built: run 'make build' first.
test-env:
	@test -f ./devctl || (echo "Binary not found — run 'make build' first." && exit 1)
	@bash scripts/test-env.sh

# Launch container, run all tests, destroy when done.
# Exit code mirrors the test result.
test-run:
	@test -f ./devctl || (echo "Binary not found — run 'make build' first." && exit 1)
	@bash scripts/test-env.sh --run-tests

# Run BATS integration tests inside the container (DEVCTL_CONTAINER must be set).
test-bats:
	@test -n "$$DEVCTL_CONTAINER" || (echo "DEVCTL_CONTAINER not set — start a test env first with 'make test-env'." && exit 1)
	incus exec "$$DEVCTL_CONTAINER" -- mkdir -p /tmp/tests
	tar -czf - -C tests integration/ | incus exec "$$DEVCTL_CONTAINER" -- tar -xzf - -C /tmp/tests/
	incus exec "$$DEVCTL_CONTAINER" -- bats /tmp/tests/integration/

# Compile the Go API test binary on the host, push it into the container, run it.
# No Go toolchain needed inside the container.
test-api:
	@test -n "$$DEVCTL_CONTAINER" || (echo "DEVCTL_CONTAINER not set — start a test env first with 'make test-env'." && exit 1)
	$(GO) test -c -tags=integration -o devctl.test ./tests/api/
	incus exec "$$DEVCTL_CONTAINER" -- rm -f /tmp/devctl.test
	incus file push devctl.test "$$DEVCTL_CONTAINER/tmp/devctl.test"
	incus exec "$$DEVCTL_CONTAINER" -- chmod 755 /tmp/devctl.test
	rm -f devctl.test
	incus exec "$$DEVCTL_CONTAINER" -- env DEVCTL_BASE_URL=http://127.0.0.1:4000 DEVCTL_SITE_USER=testuser /tmp/devctl.test -test.v

# Run Playwright e2e tests inside the container.
# Playwright and Chromium are pre-baked into the devctl-ubuntu-base image.
test-e2e:
	@test -n "$$DEVCTL_CONTAINER" || (echo "DEVCTL_CONTAINER not set — start a test env first with 'make test-env'." && exit 1)
	tar -czf - -C tests e2e/ | incus exec "$$DEVCTL_CONTAINER" -- tar -xzf - -C /tmp/tests/
	incus exec "$$DEVCTL_CONTAINER" -- rm -f /tmp/playwright.config.ts
	incus file push playwright.config.ts "$$DEVCTL_CONTAINER/tmp/playwright.config.ts"
	incus exec "$$DEVCTL_CONTAINER" --cwd /tmp -- \
	  env DEVCTL_BASE_URL=http://127.0.0.1:4000 NODE_PATH=/usr/lib/node_modules npx playwright test

# Run all three test layers inside the container.
test: test-bats test-api test-e2e

# Push a new devctl binary into a running test container and re-run all tests.
# Requires: DEVCTL_CONTAINER=<name>   (or exported from 'make test-env')
# Example:  DEVCTL_CONTAINER=devctl-test-1234567890 make test-push
test-push:
	@test -n "$$DEVCTL_CONTAINER" || (echo "DEVCTL_CONTAINER not set — specify the container name: DEVCTL_CONTAINER=devctl-test-xxx make test-push" && exit 1)
	$(MAKE) build
	incus file push ./devctl "$$DEVCTL_CONTAINER/usr/local/bin/devctl"
	incus exec "$$DEVCTL_CONTAINER" -- chmod 755 /usr/local/bin/devctl
	incus exec "$$DEVCTL_CONTAINER" -- systemctl restart devctl
	@echo "Waiting for devctl to restart..."
	@sleep 2
	incus exec "$$DEVCTL_CONTAINER" -- curl -sf http://127.0.0.1:4000/api/settings/resolved > /dev/null \
	  || (echo "devctl did not respond after restart — check: incus exec $$DEVCTL_CONTAINER -- journalctl -u devctl -n 20 --no-pager" && exit 1)
	@echo "devctl restarted successfully. Running tests..."
	$(MAKE) test
