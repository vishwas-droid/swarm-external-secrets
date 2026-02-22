#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

# Variables

PROVIDER="${1:-vault}"
STACK_NAME="smoke_stack_$(date +%s)"
SERVICE_NAME="smoke_service"
SECRET_NAME="smoke"
VAULT_CONTAINER="smoke-vault"
EXPECTED_SECRET="smoke_test_value_123"

# Logging

log() {
  echo "[SMOKE TEST] $1"
}

# Cleanup (must be early)

cleanup() {
  log "Cleaning up..."
  docker service rm "$SERVICE_NAME" 2>/dev/null || true
  docker secret rm "$SECRET_NAME" 2>/dev/null || true
  #docker plugin disable swarm-external-secrets:latest --force 2>/dev/null || true
  #docker plugin rm swarm-external-secrets:latest --force 2>/dev/null || true
  docker rm -f "$VAULT_CONTAINER" 2>/dev/null || true
  #docker swarm leave --force >/dev/null 2>&1 || true
}

trap cleanup EXIT

# Wait for Vault readiness

wait_for_vault() {
  log "Waiting for Vault to become ready..."

  for i in {1..20}; do
    if docker exec "$VAULT_CONTAINER" \
      env VAULT_ADDR="http://127.0.0.1:8200" \
      VAULT_TOKEN="root" \
      vault status >/dev/null 2>&1; then
      log "Vault is ready."
      return 0
    fi
    sleep 1
  done

  log "Vault failed to start."
  docker logs "$VAULT_CONTAINER"
  exit 1
}

# Initialize Docker Swarm

init_swarm() {
  log "Checking Docker Swarm status..."

  if ! docker info 2>/dev/null | grep -q "Swarm: active"; then
    log "Docker Swarm not active. Initializing..."
    docker swarm init >/dev/null
  else
    log "Docker Swarm already active"
  fi
}

# Setup Vault (dev mode)

setup_vault() {
  log "Starting Vault container (dev mode)..."

  docker run -d \
    --name "$VAULT_CONTAINER" \
    -e VAULT_DEV_ROOT_TOKEN_ID=root \
    -e VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200 \
    -p 8200:8200 \
    hashicorp/vault:1.16 \
    server -dev -dev-root-token-id=root >/dev/null

  wait_for_vault

  log "Writing test secret to Vault..."

  docker exec \
    -e VAULT_ADDR="http://127.0.0.1:8200" \
    -e VAULT_TOKEN="root" \
    "$VAULT_CONTAINER" \
    vault kv put secret/smoke_service/smoke password="$EXPECTED_SECRET" >/dev/null
}

build_plugin() {
  log "Building Docker plugin..."

  ./scripts/build.sh >/dev/null

  if ! docker plugin inspect swarm-external-secrets:latest >/dev/null 2>&1; then
    log "Plugin build failed."
    exit 1
  fi

  docker plugin disable swarm-external-secrets:latest --force >/dev/null 2>&1 || true

  log "Configuring plugin for Vault..."

  docker plugin set swarm-external-secrets:latest \
    SECRETS_PROVIDER="vault" \
    VAULT_ADDR="http://127.0.0.1:8200" \
    VAULT_AUTH_METHOD="token" \
    VAULT_TOKEN="root" \
    VAULT_MOUNT_PATH="secret" \
    VAULT_ENABLE_ROTATION="false" \
    ENABLE_ROTATION="false" \
    ENABLE_MONITORING="false" >/dev/null

  log "Enabling plugin..."

  docker plugin enable swarm-external-secrets:latest
}

create_secret() {
  log "Ensuring old secret does not exist..."

  docker secret rm "$SECRET_NAME" 2>/dev/null || true

  log "Creating Docker secret using plugin..."

  docker secret create \
    --driver swarm-external-secrets:latest \
    "$SECRET_NAME" ""
}

deploy_service() {
  log "Deploying test service..."

  docker service rm "$SERVICE_NAME" 2>/dev/null || true

  docker service create \
    --name "$SERVICE_NAME" \
    --secret "$SECRET_NAME" \
    alpine:latest \
    sh -c "sleep 30" >/dev/null

  log "Waiting for service to be running..."

  for i in {1..20}; do
    RUNNING=$(docker service ps "$SERVICE_NAME" \
      --format '{{.CurrentState}}' | grep -c "Running" || true)

    if [ "$RUNNING" -gt 0 ]; then
      log "Service is running."
      return 0
    fi

    sleep 1
  done

  log "Service failed to start."
  docker service ps "$SERVICE_NAME"
  exit 1
}
verify_secret() {
  log "Verifying injected secret..."

  TASK_ID=$(docker service ps "$SERVICE_NAME" \
    --format '{{.ID}}' | head -n1)

  CONTAINER_ID=$(docker inspect \
    --format '{{.Status.ContainerStatus.ContainerID}}' \
    "$TASK_ID")

  SECRET_VALUE=$(docker exec "$CONTAINER_ID" \
    cat "/run/secrets/$SECRET_NAME")

  if [ "$SECRET_VALUE" = "$EXPECTED_SECRET" ]; then
    log "Smoke test PASSED."
  else
    log "Smoke test FAILED."
    log "Expected: $EXPECTED_SECRET"
    log "Got: $SECRET_VALUE"
    exit 1
  fi
}

# Main Execution

main() {
  init_swarm
  setup_vault
  build_plugin
  create_secret
  deploy_service
  verify_secret
}
main
