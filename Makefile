.PHONY: dev dev-ui build build-ui install deploy sqlc db-migrate \
        test-env-setup test-env test-run test-bats test-api test-e2e test \
        test-artifacts-download test-artifacts-clean

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
# The service file is only copied if /etc/systemd/system/devctl.service does not
# already exist, so re-running install does not clobber a configured service.
install: build
	mkdir -p $(INSTALL_DIR)
	sudo install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@if [ ! -f $(SERVICE_DIR)/devctl.service ]; then \
		sudo install -m 644 devctl.service $(SERVICE_DIR)/devctl.service; \
		echo "Service file installed."; \
	else \
		echo "Service file already exists — skipping (run 'make install-service' to force overwrite)."; \
	fi
	mkdir -p $(BIN_DIR)
	ln -sf $(INSTALL_DIR)/$(BINARY) $(BIN_DIR)/$(BINARY)
	printf '# Added by devctl — do not edit manually\nexport PATH="%s:$$PATH"\n' "$(BIN_DIR)" | sudo tee /etc/profile.d/devctl.sh > /dev/null
	sudo systemctl daemon-reload
	@echo "Installed to $(INSTALL_DIR)/$(BINARY). Run: systemctl enable --now devctl"

# Force-install the service file (use this when you intentionally want to update it).
install-service:
	sudo install -m 644 devctl.service $(SERVICE_DIR)/devctl.service
	sudo systemctl daemon-reload

# Deploy without rebuilding (just copy binary + reload); useful when already built.
# Does NOT overwrite the service file.
deploy:
	mkdir -p $(INSTALL_DIR)
	sudo install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	mkdir -p $(BIN_DIR)
	ln -sf $(INSTALL_DIR)/$(BINARY) $(BIN_DIR)/$(BINARY)
	printf '# Added by devctl — do not edit manually\nexport PATH="%s:$$PATH"\n' "$(BIN_DIR)" | sudo tee /etc/profile.d/devctl.sh > /dev/null
	sudo systemctl daemon-reload
	sudo systemctl restart devctl
	@echo "Deployed and restarted devctl."

# Run sqlc code generation
sqlc:
	cd db && sqlc generate

# Run goose migrations against dev DB
db-migrate:
	$(shell go env GOPATH)/bin/goose -dir db/migrations sqlite3 $(SITE_HOME)/sites/server/devctl/devctl.db up

# ─── Test environment ──────────────────────────────────────────────────────────

# One-time setup: pull the ubuntu base image for Incus and bake in prerequisites.
# Re-run this whenever you want to refresh the cached image.
test-env-setup:
	@which incus > /dev/null 2>&1 || (echo "Incus is not installed. See: https://linuxcontainers.org/incus/docs/main/installing/" && exit 1)
	@bash scripts/test-env-setup.sh
	@echo "Setup complete. Run 'make test-env' to launch a test container."

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
	  CACHE_DIR=$$(incus storage volume get "$$POOL" "$$VOLUME" volatile.rootfs.path 2>/dev/null || echo "/var/lib/incus/storage-pools/$$POOL/custom/$$VOLUME"); \
	  if [ -d "$$CACHE_DIR" ]; then rm -rf "$$CACHE_DIR"/*; echo "Artifact cache cleared."; else echo "Cache dir not found: $$CACHE_DIR"; fi

# Launch an ephemeral test container (interactive — Ctrl+C to destroy).
# Requires the binary to be pre-built: run 'make build' first.
test-env:
	@test -f ./devctl || (echo "Binary not found — run 'make build' first." && exit 1)
	@bash scripts/test-env.sh

# Launch container, run all tests, destroy when done.
# Exit code mirrors the test result.
test-run:
	@test -f ./devctl || (echo "Binary not found — run 'make build' first." && exit 1)
	@bash scripts/test-env.sh --run-tests

# Run BATS integration tests (DEVCTL_CONTAINER must be set by the test env).
test-bats:
	@test -n "$$DEVCTL_CONTAINER" || (echo "DEVCTL_CONTAINER not set — start a test env first with 'make test-env'." && exit 1)
	PATH="$(NPX_BINDIR):$$PATH" $(NPX) bats tests/integration/

# Run Go API integration tests (DEVCTL_BASE_URL must be set by the test env).
test-api:
	@test -n "$$DEVCTL_BASE_URL" || (echo "DEVCTL_BASE_URL not set — start a test env first with 'make test-env'." && exit 1)
	$(GO) test -tags=integration -v ./tests/api/...

# Run Playwright e2e tests (DEVCTL_BASE_URL must be set by the test env).
test-e2e:
	@test -n "$$DEVCTL_BASE_URL" || (echo "DEVCTL_BASE_URL not set — start a test env first with 'make test-env'." && exit 1)
	PLAYWRIGHT_BROWSERS_PATH="$(SITE_HOME)/.cache/ms-playwright" PATH="$(NPX_BINDIR):$$PATH" $(NPX) playwright test

# Run all three test layers (BATS + Go API + Playwright).
test: test-bats test-api test-e2e
