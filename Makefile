.PHONY: dev dev-ui build build-ui install deploy sqlc db-migrate \
        test-env-setup test-env test-run test-cleanup test-cleanup-all \
        test-bats _test-bats test-api _test-api test-e2e _test-e2e \
        test test-inner test-push test-artifacts-download test-artifacts-clean \
        demo

BINARY     := devctl
# Install into the site user's devctl directory.
# When invoked via sudo (make install), SUDO_USER is the non-root caller.
# Fall back to USER for non-sudo environments.
SITE_USER  ?= $(if $(SUDO_USER),$(SUDO_USER),$(USER))
UNAME_S    := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
SITE_HOME  := $(HOME)
SITES_DIR  := $(SITE_HOME)/Code/sites
TEST_GOOS  := linux
TEST_GOARCH := arm64
else
SITE_HOME  := $(shell getent passwd $(SITE_USER) | cut -d: -f6)
SITES_DIR  := $(SITE_HOME)/ddev/sites
TEST_GOOS  := linux
TEST_GOARCH := amd64
endif
TEST_ENV_SCRIPT := scripts/test-env.sh
ifeq ($(UNAME_S),Darwin)
ifeq ($(shell command -v incus 2>/dev/null),)
TEST_ENV_SCRIPT := scripts/test-env-orb.sh
endif
endif
# Resolve npx from the site user's nvm if present, fall back to PATH.
NPX        := $(shell find $(SITE_HOME)/.nvm/versions/node -maxdepth 3 -name npx 2>/dev/null | sort -V | tail -1 || which npx 2>/dev/null || echo npx)
# Prepend the nvm bin dir to PATH so node is available when running as root.
NPX_BINDIR := $(dir $(NPX))
# Resolve go from the site user's local install if present, fall back to PATH.
GO         := $(shell find $(SITE_HOME)/.local/share/go/bin $(SITE_HOME)/go/bin /usr/local/go/bin -maxdepth 1 -name go 2>/dev/null | head -1 || which go 2>/dev/null || echo go)
INSTALL_DIR := $(SITES_DIR)/server/devctl
BIN_DIR    := $(SITES_DIR)/server/bin
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
	cd frontend && { test -x node_modules/.bin/vue-tsc || npm ci; } && npm run build

# Build the full binary (frontend first, then Go)
build: build-ui
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) .

# Install the binary and parent daemon unit.
# Builds the UI and Go binary as the current (non-root) user first.
# Linux: sudo copies the binary and writes the systemd unit.
# Darwin: no sudo; writes a LaunchAgent under ~/Library/LaunchAgents.
# Usage: make install   (never: sudo make install)
install: build-ui
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) .
	mkdir -p $(INSTALL_DIR)
ifeq ($(UNAME_S),Darwin)
	install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	$(INSTALL_DIR)/$(BINARY) install --yes --user $(SITE_USER) --sites-dir $(SITES_DIR) --path $(INSTALL_DIR)
else
	sudo install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	sudo $(INSTALL_DIR)/$(BINARY) install --yes --user $(SITE_USER) --sites-dir $(SITES_DIR) --path $(INSTALL_DIR)
endif

# Force-install the service file (use this when you intentionally want to update it).
install-service:
ifeq ($(UNAME_S),Darwin)
	$(INSTALL_DIR)/$(BINARY) install --yes --user $(SITE_USER) --sites-dir $(SITES_DIR) --path $(INSTALL_DIR)
else
	sudo install -m 644 devctl.service $(SERVICE_DIR)/devctl.service
	sudo systemctl daemon-reload
endif

# Deploy without rebuilding (just copy binary + reload); useful when already built.
# Does NOT overwrite the service file.
deploy:
	mkdir -p $(INSTALL_DIR)
ifeq ($(UNAME_S),Darwin)
	install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	$(INSTALL_DIR)/$(BINARY) install --yes --user $(SITE_USER) --sites-dir $(SITES_DIR) --path $(INSTALL_DIR)
else
	sudo install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	sudo $(INSTALL_DIR)/$(BINARY) install --yes --user $(SITE_USER) --sites-dir $(SITES_DIR) --path $(INSTALL_DIR)
endif
	@echo "Deployed and restarted devctl."

# Run sqlc code generation
sqlc:
	cd db && sqlc generate

# Run goose migrations against dev DB
db-migrate:
	$(shell go env GOPATH)/bin/goose -dir db/migrations sqlite3 $(SITES_DIR)/server/devctl/devctl.db up

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
	@test -f ./devctl -o "$(TEST_ENV_SCRIPT)" = "scripts/test-env-orb.sh" || (echo "Binary not found — run 'make build' first." && exit 1)
	@bash $(TEST_ENV_SCRIPT)

# Launch container, run all tests, destroy when done.
# Exit code mirrors the test result.
test-run:
	@test -f ./devctl -o "$(TEST_ENV_SCRIPT)" = "scripts/test-env-orb.sh" || (echo "Binary not found — run 'make build' first." && exit 1)
	@bash $(TEST_ENV_SCRIPT) --run-tests

# Destroy the test container named by DEVCTL_CONTAINER.
# Set KEEP_TEST_CONTAINER=1 to skip (used by test-push for iterative runs).
test-cleanup:
	@bash scripts/test-cleanup.sh

# Destroy all orphaned devctl-test-* containers (e.g. after a crashed test run).
test-cleanup-all:
	@if command -v incus >/dev/null 2>&1; then \
	  for name in $$(incus list --format csv -c n 2>/dev/null | grep '^devctl-test-' || true); do \
	    echo "Destroying Incus $$name..."; \
	    incus exec "$$name" -- systemctl stop devctl 2>/dev/null || true; \
	    incus delete --force "$$name" 2>/dev/null || true; \
	  done; \
	fi
	@if command -v orbctl >/dev/null 2>&1; then \
	  for name in $$(orbctl list -q 2>/dev/null | awk '{print $$1}' | grep '^devctl-test-' || true); do \
	    echo "Destroying OrbStack $$name..."; \
	    bash scripts/test-cleanup.sh "$$name"; \
	  done; \
	fi

# Run BATS integration tests inside the container (DEVCTL_CONTAINER must be set).
# Destroys the container when finished unless KEEP_TEST_CONTAINER=1.
test-bats:
	@bash scripts/with-test-cleanup.sh $(MAKE) _test-bats

_test-bats:
	@test -n "$$DEVCTL_CONTAINER" || (echo "DEVCTL_CONTAINER not set — start a test env first with 'make test-env'." && exit 1)
	bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- mkdir -p /tmp/tests
	tar -czf - -C tests integration/ | bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- tar -xzf - -C /tmp/tests/
	bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- bats /tmp/tests/integration/

# Compile the Go API test binary on the host, push it into the container, run it.
# No Go toolchain needed inside the container.
# Destroys the container when finished unless KEEP_TEST_CONTAINER=1.
test-api:
	@bash scripts/with-test-cleanup.sh $(MAKE) _test-api

_test-api:
	@test -n "$$DEVCTL_CONTAINER" || (echo "DEVCTL_CONTAINER not set — start a test env first with 'make test-env'." && exit 1)
	CGO_ENABLED=0 GOOS=$(TEST_GOOS) GOARCH=$(TEST_GOARCH) $(GO) test -c -tags=integration -o devctl.test ./tests/api/
	bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- rm -f /tmp/devctl.test
	bash scripts/test-push.sh "$$DEVCTL_CONTAINER" devctl.test /tmp/devctl.test
	bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- chmod 755 /tmp/devctl.test
	rm -f devctl.test
	bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- env DEVCTL_BASE_URL=http://127.0.0.1:4000 DEVCTL_SITE_USER=testuser /tmp/devctl.test -test.v

# Run Playwright e2e tests inside the container.
# Playwright and Chromium are pre-baked into the devctl-ubuntu-base image.
# Destroys the container when finished unless KEEP_TEST_CONTAINER=1.
test-e2e:
	@bash scripts/with-test-cleanup.sh $(MAKE) _test-e2e

_test-e2e:
	@test -n "$$DEVCTL_CONTAINER" || (echo "DEVCTL_CONTAINER not set — start a test env first with 'make test-env'." && exit 1)
	bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- mkdir -p /tmp/tests
	tar -czf - -C tests e2e/ | bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- tar -xzf - -C /tmp/tests/
	bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- rm -f /tmp/playwright.config.ts
	bash scripts/test-push.sh "$$DEVCTL_CONTAINER" playwright.config.ts /tmp/playwright.config.ts
	bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" --cwd /tmp -- \
	  env DEVCTL_BASE_URL=http://127.0.0.1:4000 NODE_PATH=/usr/lib/node_modules npx playwright test

# Run all three test layers inside the container.
# Destroys the container when finished unless KEEP_TEST_CONTAINER=1.
test:
	@bash scripts/with-test-cleanup.sh $(MAKE) test-inner

test-inner: _test-bats _test-api _test-e2e

# Push a new devctl binary into a running test container and re-run all tests.
# Requires: DEVCTL_CONTAINER=<name>   (or exported from 'make test-env')
# Example:  DEVCTL_CONTAINER=devctl-test-1234567890 make test-push
test-push:
	@test -n "$$DEVCTL_CONTAINER" || (echo "DEVCTL_CONTAINER not set — specify the container name: DEVCTL_CONTAINER=devctl-test-xxx make test-push" && exit 1)
	$(MAKE) build
	@if [ "$(UNAME_S)" = "Darwin" ]; then \
	  GOOS=$(TEST_GOOS) GOARCH=$(TEST_GOARCH) CGO_ENABLED=0 $(GO) build -ldflags "-X main.version=$(VERSION)" -o /tmp/devctl.linux .; \
	  bash scripts/test-push.sh "$$DEVCTL_CONTAINER" /tmp/devctl.linux /usr/local/bin/devctl; \
	else \
	  bash scripts/test-push.sh "$$DEVCTL_CONTAINER" ./devctl /usr/local/bin/devctl; \
	fi
	bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- chmod 755 /usr/local/bin/devctl
	bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- systemctl restart devctl
	@echo "Waiting for devctl to restart..."
	@sleep 2
	bash scripts/test-exec.sh "$$DEVCTL_CONTAINER" -- curl -sf http://127.0.0.1:4000/api/settings/resolved > /dev/null \
	  || (echo "devctl did not respond after restart — check: bash scripts/test-exec.sh $$DEVCTL_CONTAINER -- journalctl -u devctl -n 20 --no-pager" && exit 1)
	@echo "devctl restarted successfully. Running tests..."
	KEEP_TEST_CONTAINER=1 $(MAKE) test-inner
