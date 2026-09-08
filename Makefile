# goStatic development tasks.
#
# `make help` lists everything. The Go targets need only a Go toolchain; the
# docker, registry and act targets need a running Docker daemon.

BINARY  ?= goStatic
IMAGE   ?= gostatic
TAG     ?= dev
WWW     ?= $(CURDIR)/testdata/www
PORT    ?= 8043
GO      ?= go

# Every platform the published image is built for. Kept in step with
# .github/workflows/build.yml and .github/workflows/docker-push.yml.
PLATFORMS ?= linux/amd64,linux/arm64,linux/arm/v5,linux/arm/v6,linux/arm/v7,darwin/amd64,darwin/arm64,windows/amd64

# Publishing to the local registry is limited to the platforms worth running
# here; override to match PLATFORMS if you want the full set.
LOCAL_PLATFORMS ?= linux/amd64,linux/arm64

# The buildx builder runs in its own container, so it reaches the registry by
# container name over a shared docker network, not through a published port.
# Port 5000 is taken by AirPlay Receiver on macOS, so publish on 5001 for the
# host side (docker pull, imagetools).
REGISTRY_PORT ?= 5001
REGISTRY_NAME ?= gostatic-registry
REGISTRY_NET  ?= gostatic-net
REGISTRY      ?= $(REGISTRY_NAME):5000
REGISTRY_HOST ?= 127.0.0.1:$(REGISTRY_PORT)
BUILDER       ?= gostatic-builder

.DEFAULT_GOAL := help

.PHONY: help
help: ## List the available targets
	@printf "goStatic — make targets\n\n"
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_.-]+:.*## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ---------------------------------------------------------------- Go

.PHONY: build
build: ## Compile the binary to ./goStatic
	$(GO) build -ldflags="-s" -o $(BINARY) .

.PHONY: run
run: build ## Serve testdata/www on http://localhost:8043
	./$(BINARY) --port $(PORT) --path $(WWW) --fallback index.html --enable-gzip --log-level debug

.PHONY: test
test: ## Run the test suite
	$(GO) test ./...

.PHONY: test-race
test-race: ## Run the test suite under the race detector
	$(GO) test -race -count=1 ./...

.PHONY: cover
cover: ## Write an HTML coverage report to coverage.html
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.html"

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format the source in place
	gofmt -w -l .

.PHONY: fmt-check
fmt-check: ## Fail if any file needs gofmt
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "these files need gofmt:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy

.PHONY: check
check: fmt-check vet test-race ## Run every check CI runs, except the image build

# ---------------------------------------------------------------- Docker

.PHONY: docker-build
docker-build: ## Build a single-arch image for this machine as gostatic:dev
	docker build -t $(IMAGE):$(TAG) .

.PHONY: docker-build-all
docker-build-all: ## Build all 8 release platforms exactly as CI does, discarding the result
	docker buildx build --platform $(PLATFORMS) -f Dockerfile -o type=cacheonly .

.PHONY: docker-run
docker-run: docker-build ## Run gostatic:dev, serving testdata/www on port 8043
	docker run --rm -p $(PORT):8043 -v $(WWW):/srv/http $(IMAGE):$(TAG)

# ------------------------------------------------- Local image publishing

.PHONY: registry-up
registry-up: ## Start a throwaway image registry, published on 127.0.0.1:5001
	@docker network inspect $(REGISTRY_NET) >/dev/null 2>&1 || \
		docker network create $(REGISTRY_NET) >/dev/null
	@docker inspect $(REGISTRY_NAME) >/dev/null 2>&1 || \
		docker run -d --restart=always --network $(REGISTRY_NET) \
			-p $(REGISTRY_PORT):5000 --name $(REGISTRY_NAME) registry:2 >/dev/null
	@echo "registry listening on $(REGISTRY_HOST) (as $(REGISTRY) inside $(REGISTRY_NET))"

.PHONY: registry-down
registry-down: ## Stop and remove the local registry and its network
	-docker rm -f $(REGISTRY_NAME)
	-docker network rm $(REGISTRY_NET)

.PHONY: builder
builder: registry-up ## Create the buildx builder used for local multi-arch pushes
	@docker buildx inspect $(BUILDER) >/dev/null 2>&1 || \
		docker buildx create --name $(BUILDER) --driver docker-container \
			--driver-opt network=$(REGISTRY_NET) --config hack/buildkitd.toml >/dev/null
	@echo "builder $(BUILDER) ready"

.PHONY: builder-down
builder-down: ## Remove the buildx builder
	-docker buildx rm $(BUILDER)

.PHONY: publish-local
publish-local: registry-up builder ## Push a multi-arch image to 127.0.0.1:5001
	docker buildx build --builder $(BUILDER) --platform $(LOCAL_PLATFORMS) \
		-t $(REGISTRY)/$(IMAGE):$(TAG) --push .
	@echo "pushed; pull it with: docker pull $(REGISTRY_HOST)/$(IMAGE):$(TAG)"

.PHONY: publish-local-check
publish-local-check: ## Show the manifest of the image pushed to 127.0.0.1:5001
	docker buildx imagetools inspect $(REGISTRY_HOST)/$(IMAGE):$(TAG)

# ---------------------------------------------------------------- act

.PHONY: ci
ci: ## Run the build workflow locally with act
	# Scoped to build.yml on purpose: docker-push.yml publishes to GHCR, which
	# is not something a local run should ever attempt.
	act push -W .github/workflows/build.yml

.PHONY: ci-list
ci-list: ## List the jobs act would run
	act -l

.PHONY: ci-dry
ci-dry: ## Walk the build workflow without executing any step
	act push -n -W .github/workflows/build.yml

# ---------------------------------------------------------------- misc

.PHONY: clean
clean: ## Remove build artefacts
	rm -f $(BINARY) coverage.out coverage.html
