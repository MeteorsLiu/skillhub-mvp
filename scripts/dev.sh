#!/bin/bash
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}=== SkillHub Discovery ===${NC}"

# Load .env if exists
if [ -f "$DIR/.env" ]; then
  set -a
  source "$DIR/.env"
  set +a
fi

# Step 1: Start infra (PostgreSQL + Redis)
echo -e "${GREEN}[1/4] Starting PostgreSQL & Redis...${NC}"
docker compose -f "$DIR/docker-compose.yml" up -d 2>/dev/null || docker-compose -f "$DIR/docker-compose.yml" up -d

# Step 2: Wait for PostgreSQL
echo -e "${GREEN}[2/4] Waiting for PostgreSQL...${NC}"
for i in $(seq 1 30); do
  if docker exec skills-postgres-1 pg_isready -U skillhub -d skillhub 2>/dev/null || \
     docker exec $(docker ps -q -f name=postgres) pg_isready -U skillhub -d skillhub 2>/dev/null; then
    break
  fi
  sleep 1
done

# Step 3: Build discovery
echo -e "${GREEN}[3/4] Building discovery...${NC}"
(cd "$DIR/discovery" && go build ./cmd/discovery/)

# Step 4: Run discovery
echo -e "${GREEN}[4/4] Starting discovery server...${NC}"
exec "$DIR/discovery/discovery" "$@"
