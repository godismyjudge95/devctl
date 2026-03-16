.PHONY: dev dev-ui build build-ui install deploy sqlc db-migrate

BINARY     := devctl
# Install into the site user's devctl directory.
# When invoked via sudo (make install), SUDO_USER is the non-root caller.
# Fall back to USER for non-sudo environments.
SITE_USER  ?= $(if $(SUDO_USER),$(SUDO_USER),$(USER))
SITE_HOME  := $(shell getent passwd $(SITE_USER) | cut -d: -f6)
INSTALL_DIR := $(SITE_HOME)/sites/server/devctl
BIN_DIR    := $(SITE_HOME)/sites/server/bin
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
