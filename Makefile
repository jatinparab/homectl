.PHONY: help build build-web build-manager build-agent test clean docker-manager docker-agent docker-images dev-up dev-down deploy-logs dev-logs dev-deploy

IMAGE_REPO ?= homectl
IMAGE_TAG ?= $(shell head -n1 VERSION 2>/dev/null || echo local)
CROWDSEC_VERSION ?= v1.7.8

# Build metadata injected via ldflags.
VERSION_PKG = github.com/jatinparab/homectl/buildinfo
GO_LDFLAGS = -s -w \
	-X $(VERSION_PKG).Version=$(IMAGE_TAG) \
	-X $(VERSION_PKG).Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) \
	-X $(VERSION_PKG).BuildDate=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

DEV_DEPLOY_HOST ?= 192.168.50.101
DEV_DEPLOY_USER ?= jatin
DEV_COMPOSE_DIR ?= homectl-compose
DEV_MANAGER_HOST ?= 203.0.113.10
DEV_MANAGER_USER ?= jatin

help:
	@echo "Homectl"
	@echo ""
	@echo "Development:"
	@echo "  build          Build web UI, manager, and agent binaries"
	@echo "  build-web      Build web UI into manager/ui/dist"
	@echo "  test           Run Go tests"
	@echo "  clean          Remove build artifacts"
	@echo ""
	@echo "Docker:"
	@echo "  docker-manager Build manager image"
	@echo "  docker-agent   Build agent image"
	@echo "  docker-images  Build both images"
	@echo ""
	@echo "Local clusters:"
	@echo "  dev-up         Start hot-reload dev cluster"
	@echo "  dev-down       Tear down dev cluster"
	@echo "  deploy-logs    Tail local dev cluster logs"
	@echo "  dev-deploy     Build & deploy agent to $$DEV_DEPLOY_HOST"
	@echo "  dev-deploy-manager Build & deploy manager to $$DEV_MANAGER_HOST"
	@echo "  dev-logs       Tail remote deployment logs"

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

dev-up:
	cd dev && docker compose up --build -d

dev-down:
	cd dev && docker compose down -v

deploy-logs:
	cd dev && docker compose logs -f

dev-logs:
	ssh $(DEV_DEPLOY_USER)@$(DEV_DEPLOY_HOST) 'cd $(DEV_COMPOSE_DIR) && docker compose logs --tail 50 -f'

dev-deploy:
	docker build --platform linux/amd64 -f Dockerfile.agent \
		--build-arg HOMECTL_VERSION=$(IMAGE_TAG) \
		--build-arg HOMECTL_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) \
		--build-arg HOMECTL_BUILD_DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) \
		-t $(IMAGE_REPO)/agent:$(IMAGE_TAG) .
	docker save $(IMAGE_REPO)/agent:$(IMAGE_TAG) | ssh $(DEV_DEPLOY_USER)@$(DEV_DEPLOY_HOST) 'docker load'
	ssh $(DEV_DEPLOY_USER)@$(DEV_DEPLOY_HOST) 'cd $(DEV_COMPOSE_DIR) && docker compose down && docker compose up -d'

dev-deploy-manager:
	docker build --platform linux/amd64 -f Dockerfile.manager \
		--build-arg CROWDSEC_VERSION=$(CROWDSEC_VERSION) \
		--build-arg HOMECTL_VERSION=$(IMAGE_TAG) \
		--build-arg HOMECTL_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) \
		--build-arg HOMECTL_BUILD_DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) \
		-t $(IMAGE_REPO)/manager:$(IMAGE_TAG) .
	docker save $(IMAGE_REPO)/manager:$(IMAGE_TAG) | ssh $(DEV_MANAGER_USER)@$(DEV_MANAGER_HOST) 'docker load'
	ssh $(DEV_MANAGER_USER)@$(DEV_MANAGER_HOST) 'cd $(DEV_COMPOSE_DIR) && docker compose down && docker compose up -d'

clean:
	rm -rf ./bin manager/ui/dist
	go clean -cache
