# TapBridge developer tasks. Run `make help` for a list.

BINARY  := tapbridge
PKG     := ./cmd/tapbridge
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: help build headless run test race vet fmt fmt-check check windows mac-arm64 mac-intel docker icon clean

help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

build: ## Build for this machine into ./tapbridge
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

headless: ## Build without the tray (no GTK needed) into ./tapbridge
	go build -tags notray -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

run: build ## Build and run
	./$(BINARY)

test: ## Run unit tests
	go test ./...

race: ## Run unit tests with the race detector
	go test -race ./...

vet: ## Run go vet (host and Windows)
	go vet ./...
	go vet -tags notray ./...
	GOOS=windows CGO_ENABLED=0 go vet ./...

fmt: ## Format all Go code
	gofmt -w .

fmt-check: ## Fail if any Go file needs formatting
	@test -z "$$(gofmt -l .)" || (echo "Run 'make fmt' on:"; gofmt -l .; exit 1)

check: fmt-check vet race ## Everything CI runs

windows: ## Cross-compile the Windows tray app (no console window)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS) -H windowsgui" -o dist/$(BINARY)-windows.exe $(PKG)

mac-arm64: ## Build for Apple Silicon (run on a Mac)
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-mac-arm64 $(PKG)

mac-intel: ## Build for Intel Macs (run on a Mac)
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-mac-intel $(PKG)

docker: ## Build the Docker image as tapbridge:dev
	docker build --build-arg VERSION=$(VERSION) -t tapbridge:dev .

icon: ## Regenerate assets/icon.ico from assets/icon.png
	go run scripts/gen_icon.go assets/icon.png assets/icon.ico

clean: ## Remove build output
	go clean
	rm -rf dist
	rm -f $(BINARY) $(BINARY).exe
