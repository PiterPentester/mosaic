# Define supported operating systems and architectures
GOOS = linux windows darwin
GOARCH = amd64 arm64

# Define Go version and other variables
GO_VERSION = 1.24
CGO_ENABLED = 0
BINARY_NAME = mosaic
BUILD_DIR = build

# Generate all combinations of OS and architecture, excluding windows/arm64
COMBINATIONS = $(foreach os,$(GOOS),$(foreach arch,$(GOARCH),$(if $(filter-out windows/arm64,$(os)/$(arch)),$(os)-$(arch))))

# Default target
all: build

# Install Go and GitHub CLI if not already installed (Linux or macOS only)
prepare_env:
	@echo "Checking for Go $(GO_VERSION)..."
	@command -v go >/dev/null 2>&1 && go version | grep -q "$(GO_VERSION)" && { echo "Go $(GO_VERSION) already installed"; go version; } || { \
		echo "Installing Go $(GO_VERSION)..."; \
		if [ "$$(uname -s)" = "Linux" ]; then \
			sudo apt-get update && sudo apt-get install -y wget; \
			wget https://golang.org/dl/go$(GO_VERSION).linux-amd64.tar.gz; \
			sudo tar -C /usr/local -xzf go$(GO_VERSION).linux-amd64.tar.gz; \
			rm go$(GO_VERSION).linux-amd64.tar.gz; \
			if ! grep -q "/usr/local/go/bin" $$$$PATH; then \
				echo "export PATH=$$$$PATH:/usr/local/go/bin" >> ~/.bashrc; \
				export PATH=$$$$PATH:/usr/local/go/bin; \
			fi; \
		elif [ "$$(uname -s)" = "Darwin" ]; then \
			brew install go@$(GO_VERSION); \
		else \
			echo "Unsupported OS for installing Go. Only Linux and macOS are supported."; \
			exit 1; \
		fi; \
		go version; \
	}
	@echo "Checking for GitHub CLI..."
	@command -v gh >/dev/null 2>&1 && { echo "GitHub CLI already installed"; gh --version; exit 0; } || { \
		echo "Installing GitHub CLI..."; \
		if [ "$$(uname -s)" = "Linux" ]; then \
			sudo apt-get update && sudo apt-get install -y gh; \
		elif [ "$$(uname -s)" = "Darwin" ]; then \
			brew install gh; \
		else \
			echo "Unsupported OS for installing GitHub CLI. Only Linux and macOS are supported."; \
			exit 1; \
		fi; \
		gh --version; \
	}

# Run Go tests
test:
	@echo "Running tests..."
	@go test ./... -v
	@echo "All tests passed."

# Create build directory
$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

# Build binaries for all combinations (requires tests to pass)
build: prepare_env test $(BUILD_DIR) $(COMBINATIONS)

# Rule to build binary for each OS/ARCH combination
$(COMBINATIONS):
	GOOS=$(word 1,$(subst -, ,$@)) GOARCH=$(word 2,$(subst -, ,$@)) CGO_ENABLED=$(CGO_ENABLED) go build -o $(BUILD_DIR)/$(BINARY_NAME)-$(word 1,$(subst -, ,$@))-$(word 2,$(subst -, ,$@)) .

# Clean up build artifacts
clean:
	rm -rf $(BUILD_DIR)

# Create GitHub release and upload binaries (requires tests and build to pass)
release: build
	@echo "Creating GitHub release with tag $(TAG)..."
	@if [ -z "$(TAG)" ]; then \
		echo "Error: TAG variable is not set. Please specify a tag (e.g., make release TAG=v1.0.0)"; \
		exit 1; \
	fi
	@if [ -n "$(TOKEN)" ]; then \
		echo "Authenticating with GitHub CLI using GITHUB_TOKEN..."; \
		echo "$(TOKEN)" | gh auth login --with-token; \
		gh auth status; \
	else \
		echo "Checking GitHub CLI authentication..."; \
		gh auth status || { echo "Error: GitHub CLI not authenticated. Set GITHUB_TOKEN or run 'gh auth login' manually."; exit 1; }; \
	fi
	gh release create $(TAG) --title "Release $(TAG)" --notes "Automated release for $(TAG)" $(BUILD_DIR)/$(BINARY_NAME)-*
	@echo "Release $(TAG) created and binaries uploaded."

# Docker related targets
# Define variables
REGISTRY = ghcr.io
IMAGE_NAME = piterpentester/mosaic
TAG ?= latest # Default tag if not provided

# Log in to the container registry (requires DOCKER_USERNAME and DOCKER_PASSWORD env vars)
docker-login:
	@echo "Logging in to $(REGISTRY)..."
	@echo "$$DOCKER_PASSWORD" | docker login $(REGISTRY) -u $$DOCKER_USERNAME --password-stdin

# Set up QEMU for multi-architecture support
setup-qemu:
	docker run --rm --privileged multiarch/qemu-user-static --reset -p yes

# Set up Docker Buildx
setup-buildx:
	docker buildx create --name mybuilder --use || true
	docker buildx inspect --bootstrap

# Build Docker image for multiple platforms
docker-build: setup-qemu setup-buildx
	docker buildx build --platform linux/amd64,linux/arm64 --tag $(REGISTRY)/$(IMAGE_NAME):$(TAG) .

# Build and push Docker image for multiple platforms
docker-build-push: setup-qemu setup-buildx docker-login
	docker buildx build --platform linux/amd64,linux/arm64 --tag $(REGISTRY)/$(IMAGE_NAME):$(TAG) --push .

# Clean up Buildx builder
docker-clean:
	docker buildx rm mybuilder || true

.PHONY: all prepare_env test build clean release docker-login setup-qemu setup-buildx docker-build docker-build-push docker-clean