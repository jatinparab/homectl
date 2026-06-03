#!/usr/bin/env bash
# e2e/run.sh — lifecycle helper for the e2e DinD cluster
#
# Subcommands:
#   ci            Start cluster if needed, run Go tests, always tear down (used by `make e2e`)
#   up            Build + start cluster; wait until agents are registered
#   down          Tear down cluster and remove volumes
#   test          Run Go tests against an already-running cluster
#   logs [svc]    Tail docker compose logs
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
MANAGER_URL="http://localhost:1338"

# ─── helpers ────────────────────────────────────────────────────────────────

log()  { echo "[e2e] $*"; }
pass() { echo "[e2e] PASS: $*"; }
fail() { echo "[e2e] FAIL: $*" >&2; exit 1; }

wait_for() {
    local desc="$1" url="$2" timeout="${3:-60}"
    log "Waiting for $desc (${url}) ..."
    local deadline=$(( $(date +%s) + timeout ))
    while [[ $(date +%s) -lt $deadline ]]; do
        if curl -sf "$url" >/dev/null 2>&1; then
            pass "$desc is up"
            return 0
        fi
        sleep 2
    done
    fail "$desc did not become ready within ${timeout}s"
}

check_agents() {
    local expected="$1"
    log "Checking that $expected agent(s) are registered ..."
    local deadline=$(( $(date +%s) + 120 ))
    while [[ $(date +%s) -lt $deadline ]]; do
        local count
        count=$(curl -sf "$MANAGER_URL/agents" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo 0)
        if [[ "$count" -ge "$expected" ]]; then
            pass "$count agent(s) registered"
            return 0
        fi
        log "  ... $count/$expected agents registered, retrying"
        sleep 5
    done
    fail "Only $count/$expected agents registered after 120s"
}

check_apps_assigned() {
    log "Checking that apps are assigned to agents ..."
    local deadline=$(( $(date +%s) + 120 ))
    while [[ $(date +%s) -lt $deadline ]]; do
        local resp
        resp=$(curl -sf "$MANAGER_URL/api/v1/apps" 2>/dev/null || echo "[]")
        local assigned
        assigned=$(echo "$resp" | python3 -c "
import sys, json
apps = json.load(sys.stdin)
assigned = [a for a in apps if a.get('assigned_agent')]
print(len(assigned))
" 2>/dev/null || echo 0)
        if [[ "$assigned" -ge 2 ]]; then
            pass "$assigned apps assigned to agents"
            return 0
        fi
        log "  ... $assigned apps assigned, waiting for reconcile"
        sleep 5
    done
    fail "Apps were not assigned within 120s"
}

# ─── helpers ─────────────────────────────────────────────────────────────────

cluster_is_up() {
    curl -sf "$MANAGER_URL/health" >/dev/null 2>&1
}

# ─── lifecycle ───────────────────────────────────────────────────────────────

CMD="${1:-ci}"

case "$CMD" in
ci)
    # ── detect whether the cluster is already running ──────────────────────
    STARTED_CLUSTER=0
    if cluster_is_up; then
        log "Cluster already running at $MANAGER_URL — skipping build/up"
    else
        log "Starting e2e cluster ..."
        cd "$SCRIPT_DIR"
        docker compose up --build -d
        STARTED_CLUSTER=1
        wait_for "manager HTTP API" "$MANAGER_URL/health" 120
        check_agents 3
        check_apps_assigned
    fi

    # ── run Go tests, capture exit code ────────────────────────────────────
    TEST_EXIT=0
    cd "$REPO_DIR"
    go test -tags=e2e -v -timeout 15m ./e2e/ || TEST_EXIT=$?

    # ── tear down only if we started the cluster ────────────────────────────
    if [[ "$STARTED_CLUSTER" -eq 1 ]]; then
        log "Tearing down e2e cluster ..."
        cd "$SCRIPT_DIR"
        docker compose down -v
        log "Cluster removed."
    else
        log "Cluster was pre-existing — leaving it running."
    fi

    exit "$TEST_EXIT"
    ;;

up)
    log "Building and starting e2e cluster ..."
    cd "$SCRIPT_DIR"
    docker compose up --build -d

    wait_for "manager HTTP API" "$MANAGER_URL/health" 60
    check_agents 3
    check_apps_assigned

    log ""
    log "Cluster is up. Manager API: $MANAGER_URL"
    log "  Run tests:  make e2e-tests"
    log "  Logs:       make e2e-logs"
    log "  Teardown:   make e2e-down"
    ;;

down)
    log "Tearing down e2e cluster ..."
    cd "$SCRIPT_DIR"
    docker compose down -v
    log "Done."
    ;;

test)
    # Run Go e2e tests against an already-running cluster
    wait_for "manager HTTP API" "$MANAGER_URL/health" 10
    cd "$(dirname "$SCRIPT_DIR")"
    exec go test -tags=e2e -v -timeout 15m ./e2e/
    ;;

logs)
    cd "$SCRIPT_DIR"
    docker compose logs -f "${2:-}"
    ;;

*)
    echo "Usage: $0 {ci|up|down|test|logs [service]}"
    exit 1
    ;;
esac
