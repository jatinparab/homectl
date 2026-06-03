.PHONY: help build build-web build-manager build-agent test clean docker-manager docker-agent docker-images dev-up dev-down deploy-logs dev-logs dev-deploy e2e e2e-up e2e-down e2e-tests e2e-logs

IMAGE_REPO ?= homectl
IMAGE_TAG ?= local

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
	@echo "  e2e            Start cluster, run e2e tests, then tear down"
	@echo "  e2e-tests      Run e2e tests against an existing cluster"

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
	docker build -f Dockerfile.manager -t $(IMAGE_REPO)/manager:$(IMAGE_TAG) .

docker-agent:
	docker build -f Dockerfile.agent -t $(IMAGE_REPO)/agent:$(IMAGE_TAG) .

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
	docker build --platform linux/amd64 -f Dockerfile.agent -t $(IMAGE_REPO)/agent:$(IMAGE_TAG) .
	docker save $(IMAGE_REPO)/agent:$(IMAGE_TAG) | ssh $(DEV_DEPLOY_USER)@$(DEV_DEPLOY_HOST) 'docker load'
	ssh $(DEV_DEPLOY_USER)@$(DEV_DEPLOY_HOST) 'cd $(DEV_COMPOSE_DIR) && docker compose down && docker compose up -d'

dev-deploy-manager:
	docker build --platform linux/amd64 -f Dockerfile.manager -t $(IMAGE_REPO)/manager:$(IMAGE_TAG) .
	docker save $(IMAGE_REPO)/manager:$(IMAGE_TAG) | ssh $(DEV_MANAGER_USER)@$(DEV_MANAGER_HOST) 'docker load'
	ssh $(DEV_MANAGER_USER)@$(DEV_MANAGER_HOST) 'cd $(DEV_COMPOSE_DIR) && docker compose down && docker compose up -d'

e2e:
	./e2e/run.sh ci

e2e-up:
	./e2e/run.sh up

e2e-down:
	./e2e/run.sh down

e2e-tests:
	go test -tags=e2e -v -timeout 15m ./e2e/

e2e-logs:
	./e2e/run.sh logs

clean:
	rm -rf ./bin manager/ui/dist
	go clean -cache
