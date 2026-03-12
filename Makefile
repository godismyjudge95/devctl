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

# Install the binary and systemd system service (requires root)
install: build
	sudo install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	sudo install -m 644 devctl.service $(SERVICE_DIR)/devctl.service
	sudo systemctl daemon-reload
	@echo "Installed. Run: systemctl enable --now devctl"

# Deploy without rebuilding (just copy + reload); useful when already built
deploy:
	sudo install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	sudo install -m 644 devctl.service $(SERVICE_DIR)/devctl.service
	sudo systemctl daemon-reload
	sudo systemctl restart devctl
	@echo "Deployed and restarted devctl."

# Run sqlc code generation
sqlc:
	cd db && sqlc generate

# Run goose migrations against dev DB
db-migrate:
	$(shell go env GOPATH)/bin/goose -dir db/migrations sqlite3 /etc/devctl/devctl.db up
