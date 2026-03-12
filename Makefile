.PHONY: dev dev-ui build build-ui install deploy sqlc db-migrate

BINARY     := devctl
INSTALL_DIR := /usr/local/bin
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
	sudo install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@if [ ! -f $(SERVICE_DIR)/devctl.service ]; then \
		sudo install -m 644 devctl.service $(SERVICE_DIR)/devctl.service; \
		echo "Service file installed."; \
	else \
		echo "Service file already exists — skipping (run 'make install-service' to force overwrite)."; \
	fi
	sudo systemctl daemon-reload
	@echo "Installed. Run: systemctl enable --now devctl"

# Force-install the service file (use this when you intentionally want to update it).
install-service:
	sudo install -m 644 devctl.service $(SERVICE_DIR)/devctl.service
	sudo systemctl daemon-reload

# Deploy without rebuilding (just copy binary + reload); useful when already built.
# Does NOT overwrite the service file.
deploy:
	sudo install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	sudo systemctl daemon-reload
	sudo systemctl restart devctl
	@echo "Deployed and restarted devctl."

# Run sqlc code generation
sqlc:
	cd db && sqlc generate

# Run goose migrations against dev DB
db-migrate:
	$(shell go env GOPATH)/bin/goose -dir db/migrations sqlite3 /etc/devctl/devctl.db up
