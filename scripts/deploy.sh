#!/bin/bash
set -euo pipefail

DIR="$(cd "$(dirname "$0")/.." && pwd)"
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}=== SkillHub One-Click Deploy ===${NC}"

# Check prerequisites
for cmd in docker go git; do
  if ! command -v $cmd &>/dev/null; then
    echo "Error: $cmd is required but not installed."
    exit 1
  fi
done

# Step 0: Setup .env
if [ ! -f "$DIR/.env" ]; then
  if [ -f "$DIR/.env.example" ]; then
    echo -e "${YELLOW}[0] Creating .env from .env.example${NC}"
    cp "$DIR/.env.example" "$DIR/.env"
    echo "  -> Edit $DIR/.env to add API keys, then re-run."
    echo "  -> Proceeding without LLM API keys."
  fi
fi
if [ -f "$DIR/.env" ]; then
  set -a
  source "$DIR/.env"
  set +a
fi

# Step 1: Start PostgreSQL & Redis
echo -e "${GREEN}[1/4] Starting PostgreSQL & Redis...${NC}"
docker compose -f "$DIR/docker-compose.yml" up -d --pull never 2>/dev/null || \
  docker compose -f "$DIR/docker-compose.yml" up -d

# Wait for PostgreSQL
echo -e "${GREEN}[2/4] Waiting for PostgreSQL...${NC}"
for i in $(seq 1 30); do
  if docker compose -f "$DIR/docker-compose.yml" exec -T postgres \
    pg_isready -U skillhub -d skillhub 2>/dev/null; then
    echo "  PostgreSQL ready"
    break
  fi
  sleep 1
done

# Step 3: Build binaries
echo -e "${GREEN}[3/4] Building binaries...${NC}"

echo "  Building discovery..."
(cd "$DIR/discovery" && go build -o "$DIR/discovery/discovery" ./cmd/discovery/)

echo "  Building skillhub CLI..."
(cd "$DIR/skillhub" && go build -o "$DIR/skillhub/skillhub" ./cmd/skillhub/)

SKILLHUB_BIN="$DIR/skillhub/skillhub"
DISCOVERY_BIN="$DIR/discovery/discovery"

# Step 4: Start discovery server (daemon)
echo -e "${GREEN}[4/4] Starting discovery server...${NC}"

# Kill existing discovery if running
pkill -f "$DISCOVERY_BIN" 2>/dev/null || true
sleep 1

export DATABASE_URL="${DATABASE_URL:-postgres://skillhub:skillhub@localhost:5432/skillhub}"
export REDIS_ADDR="${REDIS_ADDR:-localhost:6379}"
export DISCOVERY_PORT="${DISCOVERY_PORT:-8399}"
export LLM_PROVIDER="${LLM_PROVIDER:-}"
export OPENAI_API_KEY="${OPENAI_API_KEY:-}"
export OPENAI_BASE_URL="${OPENAI_BASE_URL:-}"
export OPENAI_MODEL="${OPENAI_MODEL:-}"
export ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:-}"
export ANTHROPIC_MODEL="${ANTHROPIC_MODEL:-}"

nohup "$DISCOVERY_BIN" > /tmp/discovery.log 2>&1 &
DISCOVERY_PID=$!
echo "  Discovery PID: $DISCOVERY_PID"

# Wait for discovery health
PORT="${DISCOVERY_PORT:-8399}"
for i in $(seq 1 15); do
  if curl -sf "http://localhost:$PORT/health" >/dev/null 2>&1; then
    echo "  Discovery ready on :$PORT"
    break
  fi
  sleep 1
done

# Step 5: Install MCP configs for Codex and OpenCode
echo "  Installing MCP server config..."

CODEX_CONFIG="$HOME/.codex/config.toml"
OPENCODE_CONFIG="$HOME/.config/opencode/opencode.json"

# Codex (TOML format)
if [ -f "$CODEX_CONFIG" ]; then
  if ! grep -q '\[mcp_servers.skillhub\]' "$CODEX_CONFIG" 2>/dev/null; then
    cat >> "$CODEX_CONFIG" <<TOML

[mcp_servers.skillhub]
enabled = true
command = "$SKILLHUB_BIN"
args = ["serve"]
TOML
    echo "  Codex: skillhub MCP installed."
  else
    echo "  Codex: skillhub MCP already registered, skipping."
  fi
fi

# OpenCode (JSON format)
if [ -f "$OPENCODE_CONFIG" ]; then
  if ! python3 -c "
import json
with open('$OPENCODE_CONFIG') as f:
    c = json.load(f)
exit(0 if c.get('mcp', {}).get('skillhub') else 1)
" 2>/dev/null; then
    python3 -c "
import json
with open('$OPENCODE_CONFIG') as f:
    c = json.load(f)
c.setdefault('mcp', {})['skillhub'] = {
    'type': 'local',
    'command': ['$SKILLHUB_BIN', 'serve'],
    'enabled': True,
}
with open('$OPENCODE_CONFIG', 'w') as f:
    json.dump(c, f, indent=2)
    f.write('\n')
"
    echo "  OpenCode: skillhub MCP installed."
  else
    echo "  OpenCode: skillhub MCP already registered, skipping."
  fi
fi

echo ""
echo -e "${GREEN}=== Deploy complete ===${NC}"
echo ""
echo "  Discovery API: http://localhost:$PORT"
echo "  Discovery PID: $DISCOVERY_PID"
echo "  Logs:          tail -f /tmp/discovery.log"
echo ""
echo "  MCP servers installed for:"
echo "    - Codex (terminal tool)"
echo "    - OpenCode"
echo "  Restart the agent to use skillhub_search / skillhub_load tools."
echo ""
echo "  To stop:"
echo "    kill $DISCOVERY_PID"
echo "    docker compose -f $DIR/docker-compose.yml down"
