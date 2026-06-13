.PHONY: help build build-web build-manager build-agent test clean \
        docker-manager docker-agent docker-images \
        release release-manager release-agent \
        dev-release dev-release-manager dev-release-agent \
        dev-deploy dev-deploy-agent dev-deploy-manager \
        dev-up dev-down deploy-logs dev-logs

IMAGE_REPO ?= homectl
IMAGE_TAG ?= local
CROWDSEC_VERSION ?= v1.7.8

GHCR_REPO ?= ghcr.io/jatinparab/homectl

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
	go build -o ./bin/manager ./manager/cmd

build-agent:
	go build -o ./bin/agent ./agent/cmd

test:
	go test ./...

docker-manager:
	docker build -f Dockerfile.manager --build-arg CROWDSEC_VERSION=$(CROWDSEC_VERSION) -t $(IMAGE_REPO)/manager:$(IMAGE_TAG) .

docker-agent:
	docker build -f Dockerfile.agent -t $(IMAGE_REPO)/agent:$(IMAGE_TAG) .

docker-images: docker-manager docker-agent

# ── Release (push to ghcr.io) ────────────────────────────────────────────

release: release-manager release-agent

release-manager:
	docker build --platform linux/amd64 -f Dockerfile.manager \
		--build-arg CROWDSEC_VERSION=$(CROWDSEC_VERSION) \
		-t $(GHCR_REPO)/manager:latest .
	docker push $(GHCR_REPO)/manager:latest

release-agent:
	docker buildx build --platform linux/amd64,linux/arm64 -f Dockerfile.agent \
		-t $(GHCR_REPO)/agent:latest --push .

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

dev-deploy:
	scripts/dev-deploy-agents.sh $(DEV_USER) $(DEV_COMPOSE_DIR) $(DEV_AGENT_HOSTS)
	echo "==> Restarting manager on $(DEV_MANAGER_HOST)..."
	ssh $(DEV_USER)@$(DEV_MANAGER_HOST) 'cd $(DEV_COMPOSE_DIR) && HOMECTL_MANAGER_IMAGE=homectl/manager:local docker compose down && HOMECTL_MANAGER_IMAGE=homectl/manager:local docker compose up -d'

dev-deploy-agent:
	scripts/dev-deploy-agents.sh $(DEV_USER) $(DEV_COMPOSE_DIR) $(DEV_AGENT_HOSTS)

dev-deploy-manager:
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
