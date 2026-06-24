.PHONY: help build build-web build-manager build-agent test clean \
        docker-manager docker-agent docker-images \
        release release-check release-manager release-agent \
        dev-release dev-release-manager dev-release-agent \
        dev-deploy dev-deploy-agent dev-deploy-manager \
        dev-up dev-down deploy-logs dev-logs

IMAGE_REPO ?= homectl
IMAGE_TAG ?= $(shell head -n1 VERSION 2>/dev/null || echo local)
GHCR_IMAGE_REPO ?= ghcr.io/jatinparab/homectl
RELEASE_VERSION ?= $(shell head -n1 VERSION 2>/dev/null | tr -d '\r\n')
RELEASE_TAG ?= v$(RELEASE_VERSION)
RELEASE_PLATFORMS ?= linux/amd64,linux/arm64
RELEASE_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
RELEASE_BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
CROWDSEC_VERSION ?= v1.7.8

# Build metadata injected via ldflags.
# shellcheck disable=SC2034
VERSION_PKG=github.com/jatinparab/homectl/buildinfo
# shellcheck disable=SC2037
GO_LDFLAGS=-s -w \
	-X $(VERSION_PKG).Version=$(IMAGE_TAG) \
	-X $(VERSION_PKG).Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) \
	-X $(VERSION_PKG).BuildDate=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

DEV_DEPLOY_HOST ?= 192.168.50.101
DEV_DEPLOY_USER ?= jatin

DEV_AGENT_HOSTS ?= 192.168.50.101 192.168.50.103 192.168.50.102
DEV_MANAGER_HOST ?= 203.0.113.10
DEV_USER ?= jatin
DEV_COMPOSE_DIR ?= homectl-compose

help:
	@echo "Homectl"
	@echo ""
	@echo "Development:"
	@echo "  build             Build web UI, manager, and agent binaries"
	@echo "  build-web         Build web UI into manager/ui/dist"
	@echo "  test              Run Go tests"
	@echo "  clean             Remove build artifacts"
	@echo ""
	@echo "Docker:"
	@echo "  docker-manager    Build manager image locally"
	@echo "  docker-agent      Build agent image locally"
	@echo "  docker-images     Build both images locally"
	@echo ""
	@echo "Release:"
	@echo "  release           Build + push manager and agent to ghcr.io"
	@echo "  release-manager   Build + push manager to ghcr.io"
	@echo "  release-agent     Build + push agent to ghcr.io"
	@echo ""
	@echo "Dev release (local network):"
	@echo "  dev-release       Build + push manager and agent to all nodes"
	@echo "  dev-deploy        Restart containers on all nodes with local images"
	@echo "  dev-logs          Tail remote deployment logs (first agent)"
	@echo ""
	@echo "Local clusters:"
	@echo "  dev-up            Start hot-reload dev cluster"
	@echo "  dev-down          Tear down dev cluster"
	@echo "  deploy-logs       Tail local dev cluster logs"

build: build-web build-manager build-agent

build-web:
	cd web && npm --loglevel=error install && npm --loglevel=error run build
	rm -rf manager/ui/dist
	mkdir -p manager/ui
	cp -r web/dist manager/ui/

build-manager: build-web
	go build -ldflags="$(GO_LDFLAGS) -X $(VERSION_PKG).Component=manager" -o ./bin/manager ./manager/cmd

build-agent:
	go build -ldflags="$(GO_LDFLAGS) -X $(VERSION_PKG).Component=agent" -o ./bin/agent ./agent/cmd

test:
	go test ./...

docker-manager:
	docker build -f Dockerfile.manager \
		--build-arg CROWDSEC_VERSION=$(CROWDSEC_VERSION) \
		--build-arg HOMECTL_VERSION=$(IMAGE_TAG) \
		--build-arg HOMECTL_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) \
		--build-arg HOMECTL_BUILD_DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) \
		-t $(IMAGE_REPO)/manager:$(IMAGE_TAG) .

docker-agent:
	docker build -f Dockerfile.agent \
		--build-arg HOMECTL_VERSION=$(IMAGE_TAG) \
		--build-arg HOMECTL_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) \
		--build-arg HOMECTL_BUILD_DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) \
		-t $(IMAGE_REPO)/agent:$(IMAGE_TAG) .

docker-images: docker-manager docker-agent

# ── Release (push to ghcr.io) ────────────────────────────────────────────

release-check:
	test -n "$(RELEASE_VERSION)" || (echo "RELEASE_VERSION is empty; set RELEASE_VERSION or create VERSION" >&2; exit 1)
	echo "$(RELEASE_VERSION)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' || (echo "RELEASE_VERSION must be X.Y.Z, got: $(RELEASE_VERSION)" >&2; exit 1)

release: release-check release-manager release-agent

release-manager: release-check
	docker buildx build --push --platform $(RELEASE_PLATFORMS) -f Dockerfile.manager \
		--build-arg CROWDSEC_VERSION=$(CROWDSEC_VERSION) \
		--build-arg HOMECTL_VERSION=$(RELEASE_VERSION) \
		--build-arg HOMECTL_COMMIT=$(RELEASE_COMMIT) \
		--build-arg HOMECTL_BUILD_DATE=$(RELEASE_BUILD_DATE) \
		-t $(GHCR_IMAGE_REPO)/manager:$(RELEASE_TAG) \
		-t $(GHCR_IMAGE_REPO)/manager:latest .

release-agent: release-check
	docker buildx build --push --platform $(RELEASE_PLATFORMS) -f Dockerfile.agent \
		--build-arg HOMECTL_VERSION=$(RELEASE_VERSION) \
		--build-arg HOMECTL_COMMIT=$(RELEASE_COMMIT) \
		--build-arg HOMECTL_BUILD_DATE=$(RELEASE_BUILD_DATE) \
		-t $(GHCR_IMAGE_REPO)/agent:$(RELEASE_TAG) \
		-t $(GHCR_IMAGE_REPO)/agent:latest .

# ── Dev release (build local images, push to all nodes) ──────────────────

dev-release: dev-release-manager dev-release-agent

dev-release-manager:
	docker build --platform linux/amd64 -f Dockerfile.manager \
		--build-arg CROWDSEC_VERSION=$(CROWDSEC_VERSION) \
		-t homectl/manager:local .
	docker save homectl/manager:local | ssh $(DEV_USER)@$(DEV_MANAGER_HOST) 'docker load'

dev-release-agent:
	docker build --platform linux/amd64 -f Dockerfile.agent \
		-t homectl/agent:local-amd64 .
	docker build --platform linux/arm64 -f Dockerfile.agent \
		-t homectl/agent:local-arm64 .
	scripts/dev-push-agent.sh $(DEV_USER) $(DEV_AGENT_HOSTS)

# ── Dev deploy (override image + force restart on all nodes) ─────────────

dev-deploy: dev-release
	scripts/dev-deploy-agents.sh $(DEV_USER) $(DEV_COMPOSE_DIR) $(DEV_AGENT_HOSTS)
	echo "==> Restarting manager on $(DEV_MANAGER_HOST)..."
	ssh $(DEV_USER)@$(DEV_MANAGER_HOST) 'cd $(DEV_COMPOSE_DIR) && HOMECTL_MANAGER_IMAGE=homectl/manager:local docker compose down && HOMECTL_MANAGER_IMAGE=homectl/manager:local docker compose up -d'

dev-deploy-agent: dev-release-agent
	scripts/dev-deploy-agents.sh $(DEV_USER) $(DEV_COMPOSE_DIR) $(DEV_AGENT_HOSTS)

dev-deploy-manager: dev-release-manager
	echo "==> Restarting manager on $(DEV_MANAGER_HOST)..."
	ssh $(DEV_USER)@$(DEV_MANAGER_HOST) 'cd $(DEV_COMPOSE_DIR) && HOMECTL_MANAGER_IMAGE=homectl/manager:local docker compose down && HOMECTL_MANAGER_IMAGE=homectl/manager:local docker compose up -d'

# ── Dev cluster & logs ───────────────────────────────────────────────────

dev-up:
	cd dev && docker compose up --build -d

dev-down:
	cd dev && docker compose down -v

deploy-logs:
	cd dev && docker compose logs -f

dev-logs:
	ssh $(DEV_USER)@$(word 1,$(DEV_AGENT_HOSTS)) 'cd $(DEV_COMPOSE_DIR) && docker compose logs --tail 50 -f'

# ── Clean ────────────────────────────────────────────────────────────────

clean:
	rm -rf ./bin manager/ui/dist
	go clean -cache
