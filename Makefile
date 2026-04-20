.PHONY: help build test agent manager clean deploy deploy-manager deploy-1 deploy-2 deploy-3 logs logs-1 logs-2 logs-3 status stop stop-1 stop-2 stop-3 stop-manager stop-all restart-1 restart-2 restart-3 restart-manager sync sync-1 sync-2 sync-3 setup-secrets setup-secrets-1 setup-secrets-2 setup-secrets-3 build-web web-dev e2e e2e-up e2e-down e2e-test e2e-logs dev-up dev-down dev-logs dev-hosts dev-restart-manager dev-rebuild

# Node configuration
NODE_1_HOST ?= 192.168.1.101
NODE_2_HOST ?= 192.168.1.102
NODE_3_HOST ?= 192.168.1.103
MANAGER_HOST ?= 65.20.72.36
REMOTE_PATH ?= /home/jatin/homectll

# Colors
GREEN := \033[0;32m
YELLOW := \033[0;33m
NC := \033[0m

# SSH options for online checks
SSH_OPTS ?= -o BatchMode=yes -o ConnectTimeout=3

help:
	@echo "$(GREEN)Homectl Management Commands$(NC)"
	@echo ""
	@echo "$(YELLOW)Development:$(NC)"
	@echo "  build          Build agent and manager binaries"
	@echo "  build-web      Build web UI (into manager/ui/dist)"
	@echo "  web-dev        Run web UI dev server (with proxy to manager)"
	@echo "  test           Run tests"
	@echo "  agent          Run agent locally"
	@echo "  manager        Run manager locally"
	@echo ""
	@echo "$(YELLOW)Deployment:$(NC)"
	@echo "  deploy         Deploy manager and agents to all nodes"
	@echo "  deploy-1       Deploy agent to node 1 ($(NODE_1_HOST))"
	@echo "  deploy-2       Deploy agent to node 2 ($(NODE_2_HOST))"
	@echo "  deploy-3       Deploy agent to node 3 ($(NODE_3_HOST))"
	@echo "  deploy-manager Deploy manager to VPS"
	@echo ""
	@echo "$(YELLOW)Logs:$(NC)"
	@echo "  logs           Show manager logs"
	@echo "  logs-1         Show agent logs on node 1"
	@echo "  logs-2         Show agent logs on node 2"
	@echo "  logs-3         Show agent logs on node 3"
	@echo ""
	@echo "$(YELLOW)Secrets:$(NC)"
	@echo "  setup-secrets  Deploy secrets to all nodes"
	@echo "  setup-secrets-1 Deploy secrets to node 1"
	@echo "  setup-secrets-2 Deploy secrets to node 2"
	@echo "  setup-secrets-3 Deploy secrets to node 3"
	@echo ""
	@echo "$(YELLOW)Stop Services:$(NC)"
	@echo "  stop           Stop all agents"
	@echo "  stop-1         Stop agent on node 1"
	@echo "  stop-2         Stop agent on node 2"
	@echo "  stop-3         Stop agent on node 3"
	@echo "  stop-manager   Stop manager"
	@echo "  stop-all       Stop manager and all agents"
	@echo ""
	@echo "$(YELLOW)Utilities:$(NC)"
	@echo "  sync           Sync binaries and scripts to all nodes"
	@echo "  status         Show status of all agents"
	@echo "  clean          Clean build artifacts"
	@echo ""
	@echo "$(YELLOW)Dev cluster (hot-reload):$(NC)"
	@echo "  dev-up         Start dev cluster at http://homectl.local"
	@echo "  dev-down       Tear down dev cluster"
	@echo "  dev-logs       Tail all dev cluster logs"
	@echo "  dev-rebuild    Rebuild images and restart cluster"

# Development
build: build-agent build-manager
	@echo "$(GREEN)Build complete!$(NC)"

build-agent:
	@echo "$(GREEN)Building agent (linux/amd64 + linux/arm64)...$(NC)"
	GOOS=linux GOARCH=amd64 go build -o ./bin/agent-amd64 ./agent/cmd
	GOOS=linux GOARCH=arm64 go build -o ./bin/agent-arm64 ./agent/cmd

build-manager: build-web
	@echo "$(GREEN)Building manager (linux/amd64)...$(NC)"
	GOOS=linux GOARCH=amd64 go build -o ./bin/manager-amd64 ./manager/cmd

build-amd64:
	@echo "$(GREEN)Building binaries (linux/amd64)...$(NC)"
	GOOS=linux GOARCH=amd64 go build -o ./bin/agent-amd64 ./agent/cmd
	GOOS=linux GOARCH=amd64 go build -o ./bin/manager-amd64 ./manager/cmd

build-arm64:
	@echo "$(GREEN)Building binaries (linux/arm64)...$(NC)"
	GOOS=linux GOARCH=arm64 go build -o ./bin/agent-arm64 ./agent/cmd
	GOOS=linux GOARCH=arm64 go build -o ./bin/manager-arm64 ./manager/cmd

build-all: build-amd64 build-arm64
	@echo "$(GREEN)All binaries built!$(NC)"

build-local:
	go build -o ./bin/agent ./agent/cmd
	go build -o ./bin/manager ./manager/cmd

test:
	go test ./...

agent:
	go run ./agent/cmd

manager:
	go run ./manager/cmd

# Deployment - Sync
# Node 1 (101) = amd64, Node 2 (102) = arm64, Node 3 (103) = amd64
# Manager = AMD64 (VPS)
STOP_AGENT = if systemctl is-active --quiet homectl-agent 2>/dev/null; then sudo systemctl stop homectl-agent; fi

sync: build
	@echo "$(GREEN)Syncing to all nodes...$(NC)"
	@$(MAKE) sync-1
	@$(MAKE) sync-2
	@$(MAKE) sync-3

sync-1: build-agent
	@echo "Syncing to node 1 ($(NODE_1_HOST))..."
	ssh jatin@$(NODE_1_HOST) 'mkdir -p $(REMOTE_PATH)/bin $(REMOTE_PATH)/scripts && $(STOP_AGENT)'
	scp ./bin/agent-amd64 jatin@$(NODE_1_HOST):$(REMOTE_PATH)/bin/agent
	rsync -avz ./scripts/ jatin@$(NODE_1_HOST):$(REMOTE_PATH)/scripts/

sync-2: build-agent
	@echo "Syncing to node 2 ($(NODE_2_HOST))..."
	ssh jatin@$(NODE_2_HOST) 'mkdir -p $(REMOTE_PATH)/bin $(REMOTE_PATH)/scripts && $(STOP_AGENT)'
	scp ./bin/agent-arm64 jatin@$(NODE_2_HOST):$(REMOTE_PATH)/bin/agent
	rsync -avz ./scripts/ jatin@$(NODE_2_HOST):$(REMOTE_PATH)/scripts/

sync-3: build-agent
	@echo "Syncing to node 3 ($(NODE_3_HOST))..."
	ssh jatin@$(NODE_3_HOST) 'mkdir -p $(REMOTE_PATH)/bin $(REMOTE_PATH)/scripts && $(STOP_AGENT)'
	scp ./bin/agent-amd64 jatin@$(NODE_3_HOST):$(REMOTE_PATH)/bin/agent
	rsync -avz ./scripts/ jatin@$(NODE_3_HOST):$(REMOTE_PATH)/scripts/

sync-manager: build-manager
	@echo "Syncing to manager ($(MANAGER_HOST))..."
	ssh jatin@$(MANAGER_HOST) 'mkdir -p $(REMOTE_PATH)/bin $(REMOTE_PATH)/scripts'
	scp ./bin/manager-amd64 jatin@$(MANAGER_HOST):$(REMOTE_PATH)/bin/manager
	rsync -avz ./scripts/ jatin@$(MANAGER_HOST):$(REMOTE_PATH)/scripts/

# Deployment - Deploy
deploy:
	@echo "$(GREEN)Deploying to reachable hosts...$(NC)"
	@if ssh $(SSH_OPTS) jatin@$(NODE_1_HOST) 'exit 0' >/dev/null 2>&1; then \
		echo "$(GREEN)Node 1 ($(NODE_1_HOST)) is online, deploying...$(NC)"; \
		$(MAKE) --no-print-directory deploy-1; \
	else \
		echo "$(YELLOW)Node 1 ($(NODE_1_HOST)) is offline, skipping.$(NC)"; \
	fi
	@if ssh $(SSH_OPTS) jatin@$(NODE_2_HOST) 'exit 0' >/dev/null 2>&1; then \
		echo "$(GREEN)Node 2 ($(NODE_2_HOST)) is online, deploying...$(NC)"; \
		$(MAKE) --no-print-directory deploy-2; \
	else \
		echo "$(YELLOW)Node 2 ($(NODE_2_HOST)) is offline, skipping.$(NC)"; \
	fi
	@if ssh $(SSH_OPTS) jatin@$(NODE_3_HOST) 'exit 0' >/dev/null 2>&1; then \
		echo "$(GREEN)Node 3 ($(NODE_3_HOST)) is online, deploying...$(NC)"; \
		$(MAKE) --no-print-directory deploy-3; \
	else \
		echo "$(YELLOW)Node 3 ($(NODE_3_HOST)) is offline, skipping.$(NC)"; \
	fi
	@if ssh $(SSH_OPTS) jatin@$(MANAGER_HOST) 'exit 0' >/dev/null 2>&1; then \
		echo "$(GREEN)Manager ($(MANAGER_HOST)) is online, deploying...$(NC)"; \
		$(MAKE) --no-print-directory deploy-manager; \
	else \
		echo "$(YELLOW)Manager ($(MANAGER_HOST)) is offline, skipping.$(NC)"; \
	fi
	@echo "$(GREEN)Deploy run complete (offline hosts skipped).$(NC)"

deploy-1: sync-1
	@echo "$(GREEN)Deploying agent to node 1...$(NC)"
	ssh jatin@$(NODE_1_HOST) 'cd $(REMOTE_PATH) && ./scripts/deploy-agent.sh'

deploy-2: sync-2
	@echo "$(GREEN)Deploying agent to node 2...$(NC)"
	ssh jatin@$(NODE_2_HOST) 'cd $(REMOTE_PATH) && ./scripts/deploy-agent.sh'

deploy-3: sync-3
	@echo "$(GREEN)Deploying agent to node 3...$(NC)"
	ssh jatin@$(NODE_3_HOST) 'cd $(REMOTE_PATH) && ./scripts/deploy-agent.sh'

deploy-manager: stop-manager sync-manager
	@echo "$(GREEN)Deploying manager...$(NC)"
	ssh jatin@$(MANAGER_HOST) 'cd $(REMOTE_PATH) && ./scripts/deploy-manager.sh'

# Logs
logs:
	ssh jatin@$(MANAGER_HOST) 'journalctl -u homectl-manager -f'

logs-1:
	ssh jatin@$(NODE_1_HOST) 'journalctl -u homectl-agent -f'

logs-2:
	ssh jatin@$(NODE_2_HOST) 'journalctl -u homectl-agent -f'

logs-3:
	ssh jatin@$(NODE_3_HOST) 'journalctl -u homectl-agent -f'

# Status
status:
	@echo "$(GREEN)Checking service status...$(NC)"
	@echo ""
	@echo "Manager ($(MANAGER_HOST)):"
	@ssh jatin@$(MANAGER_HOST) 'systemctl status homectl-manager --no-pager' 2>/dev/null || echo "  Not reachable"
	@echo ""
	@echo "Node 1 ($(NODE_1_HOST)):"
	@ssh jatin@$(NODE_1_HOST) 'systemctl status homectl-agent --no-pager' 2>/dev/null || echo "  Not reachable"
	@echo ""
	@echo "Node 2 ($(NODE_2_HOST)):"
	@ssh jatin@$(NODE_2_HOST) 'systemctl status homectl-agent --no-pager' 2>/dev/null || echo "  Not reachable"
	@echo ""
	@echo "Node 3 ($(NODE_3_HOST)):"
	@ssh jatin@$(NODE_3_HOST) 'systemctl status homectl-agent --no-pager' 2>/dev/null || echo "  Not reachable"

# Restart services
restart-1:
	ssh jatin@$(NODE_1_HOST) 'sudo systemctl restart homectl-agent'

restart-2:
	ssh jatin@$(NODE_2_HOST) 'sudo systemctl restart homectl-agent'

restart-3:
	ssh jatin@$(NODE_3_HOST) 'sudo systemctl restart homectl-agent'

restart-manager:
	ssh jatin@$(MANAGER_HOST) 'sudo systemctl restart homectl-manager'

# Stop services
stop: stop-1 stop-2 stop-3
	@echo "$(GREEN)All agents stopped!$(NC)"

stop-1:
	@echo "$(YELLOW)Stopping agent on node 1...$(NC)"
	ssh -tt jatin@$(NODE_1_HOST) 'sudo systemctl stop homectl-agent'

stop-2:
	@echo "$(YELLOW)Stopping agent on node 2...$(NC)"
	ssh -tt jatin@$(NODE_2_HOST) 'sudo systemctl stop homectl-agent'

stop-3:
	@echo "$(YELLOW)Stopping agent on node 3...$(NC)"
	ssh -tt jatin@$(NODE_3_HOST) 'sudo systemctl stop homectl-agent'

stop-manager:
	@echo "$(YELLOW)Stopping manager...$(NC)"
	ssh jatin@$(MANAGER_HOST) 'sudo systemctl stop homectl-manager'

stop-all: stop-manager stop
	@echo "$(GREEN)All services stopped!$(NC)"

# Secrets management
SECRETS_FILE ?= ./.env

setup-secrets: setup-secrets-1 setup-secrets-2 setup-secrets-3
	@echo "$(GREEN)Secrets deployed to all nodes!$(NC)"

setup-secrets-1:
	@echo "$(GREEN)Deploying secrets to node 1...$(NC)"
	./scripts/setup-secrets.sh $(NODE_1_HOST) $(SECRETS_FILE)

setup-secrets-2:
	@echo "$(GREEN)Deploying secrets to node 2...$(NC)"
	./scripts/setup-secrets.sh $(NODE_2_HOST) $(SECRETS_FILE)

setup-secrets-3:
	@echo "$(GREEN)Deploying secrets to node 3...$(NC)"
	./scripts/setup-secrets.sh $(NODE_3_HOST) $(SECRETS_FILE)

# Web UI
build-web:
	@echo "$(GREEN)Building web UI...$(NC)"
	cd web && npm --loglevel=error install && npm --loglevel=error run build
	rm -rf manager/ui/dist
	mkdir -p manager/ui
	cp -r web/dist manager/ui/

web-dev:
	@echo "$(GREEN)Starting web dev server...$(NC)"
	cd web && npm run dev

# E2E testing (Docker-in-Docker cluster)
# `make e2e` — start cluster if needed, run tests, always tear down.
e2e:
	@./e2e/run.sh ci

e2e-up:
	@echo "$(GREEN)Starting e2e cluster (manager + 3 DinD agent nodes)...$(NC)"
	./e2e/run.sh up

e2e-down:
	@echo "$(YELLOW)Tearing down e2e cluster...$(NC)"
	./e2e/run.sh down

e2e-test:
	@echo "$(GREEN)Running e2e Go tests against running cluster...$(NC)"
	go test -v -timeout 15m ./e2e/

e2e-logs:
	./e2e/run.sh logs

# Dev cluster (hot-reload: Air for Go binaries, Vite HMR for UI)
# ─── one-time setup ──────────────────────────────────────────────────────────
dev-hosts:
	@if grep -q "homectl.local" /etc/hosts 2>/dev/null; then \
		echo "$(GREEN)homectl.local already in /etc/hosts$(NC)"; \
	else \
		echo "$(YELLOW)Adding 127.0.0.1 homectl.local to /etc/hosts (requires sudo)...$(NC)"; \
		echo "127.0.0.1 homectl.local" | sudo tee -a /etc/hosts > /dev/null; \
		echo "$(GREEN)Done. homectl.local → 127.0.0.1$(NC)"; \
	fi

# ─── lifecycle ───────────────────────────────────────────────────────────────
dev-up: dev-hosts
	@echo "$(GREEN)Starting dev cluster (this builds images on first run)...$(NC)"
	cd dev && docker compose up --build -d
	@echo ""
	@echo "$(GREEN)Dev cluster is up!$(NC)"
	@echo "  UI + API:  http://homectl.local"
	@echo "  Logs:      make dev-logs"
	@echo "  Teardown:  make dev-down"

dev-down:
	@echo "$(YELLOW)Tearing down dev cluster...$(NC)"
	cd dev && docker compose down -v
	@echo "$(GREEN)Dev cluster removed.$(NC)"

dev-logs:
	cd dev && docker compose logs -f

# ─── convenience ─────────────────────────────────────────────────────────────
dev-restart-manager:
	cd dev && docker compose restart manager

dev-rebuild:
	@echo "$(GREEN)Rebuilding and restarting dev cluster images...$(NC)"
	cd dev && docker compose up --build -d

# Clean
clean:
	rm -rf ./bin
	rm -rf manager/ui/dist
	go clean -cache
