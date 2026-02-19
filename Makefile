.PHONY: help build test agent manager clean deploy deploy-manager deploy-1 deploy-2 deploy-3 logs logs-1 logs-2 logs-3 status stop stop-1 stop-2 stop-3 stop-manager stop-all restart-1 restart-2 restart-3 restart-manager sync sync-1 sync-2 sync-3 setup-secrets setup-secrets-1 setup-secrets-2 setup-secrets-3 build-web web-dev

# Node configuration
NODE_1_HOST ?= 192.168.50.101
NODE_2_HOST ?= 192.168.50.102
NODE_3_HOST ?= 192.168.50.103
MANAGER_HOST ?= 203.0.113.10
REMOTE_PATH ?= /home/jatin/homectll

# Colors
GREEN := \033[0;32m
YELLOW := \033[0;33m
NC := \033[0m

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
sync: build
	@echo "$(GREEN)Syncing to all nodes...$(NC)"
	@$(MAKE) sync-1
	@$(MAKE) sync-2
	@$(MAKE) sync-3

sync-1: build-agent
	@echo "Syncing to node 1 ($(NODE_1_HOST))..."
	ssh jatin@$(NODE_1_HOST) 'mkdir -p $(REMOTE_PATH)/bin $(REMOTE_PATH)/scripts'
	scp ./bin/agent-amd64 jatin@$(NODE_1_HOST):$(REMOTE_PATH)/bin/agent
	rsync -avz ./scripts/ jatin@$(NODE_1_HOST):$(REMOTE_PATH)/scripts/

sync-2: build-agent
	@echo "Syncing to node 2 ($(NODE_2_HOST))..."
	ssh jatin@$(NODE_2_HOST) 'mkdir -p $(REMOTE_PATH)/bin $(REMOTE_PATH)/scripts'
	scp ./bin/agent-arm64 jatin@$(NODE_2_HOST):$(REMOTE_PATH)/bin/agent
	rsync -avz ./scripts/ jatin@$(NODE_2_HOST):$(REMOTE_PATH)/scripts/

sync-3: build-agent
	@echo "Syncing to node 3 ($(NODE_3_HOST))..."
	ssh jatin@$(NODE_3_HOST) 'mkdir -p $(REMOTE_PATH)/bin $(REMOTE_PATH)/scripts'
	scp ./bin/agent-amd64 jatin@$(NODE_3_HOST):$(REMOTE_PATH)/bin/agent
	rsync -avz ./scripts/ jatin@$(NODE_3_HOST):$(REMOTE_PATH)/scripts/

sync-manager: build-manager
	@echo "Syncing to manager ($(MANAGER_HOST))..."
	ssh jatin@$(MANAGER_HOST) 'mkdir -p $(REMOTE_PATH)/bin $(REMOTE_PATH)/scripts'
	scp ./bin/manager-amd64 jatin@$(MANAGER_HOST):$(REMOTE_PATH)/bin/manager
	rsync -avz ./scripts/ jatin@$(MANAGER_HOST):$(REMOTE_PATH)/scripts/

# Deployment - Deploy
deploy: deploy-1 deploy-2 deploy-manager
	@echo "$(GREEN)All services deployed!$(NC)"

deploy-1: stop-1 sync-1
	@echo "$(GREEN)Deploying agent to node 1...$(NC)"
	ssh jatin@$(NODE_1_HOST) 'cd $(REMOTE_PATH) && ./scripts/deploy-agent.sh'

deploy-2: stop-2 sync-2
	@echo "$(GREEN)Deploying agent to node 2...$(NC)"
	ssh jatin@$(NODE_2_HOST) 'cd $(REMOTE_PATH) && ./scripts/deploy-agent.sh'

deploy-3: stop-3 sync-3
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
SECRETS_FILE ?= ./secrets.env

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
	cd web && npm install && npm run build
	rm -rf manager/ui/dist
	mkdir -p manager/ui
	cp -r web/dist manager/ui/

web-dev:
	@echo "$(GREEN)Starting web dev server...$(NC)"
	cd web && npm run dev

# Clean
clean:
	rm -rf ./bin
	rm -rf manager/ui/dist
	go clean -cache
