#!/bin/bash

# ════════════════════════════════════════════════════════════════
#  run-all.sh — Master orchestrator
#  Runs all scripts in sequence for a full fresh deployment
#  Usage: ./run-all.sh
# ════════════════════════════════════════════════════════════════

set -e
SCRIPTS_DIR="$(cd "$(dirname "$0")/scripts" && pwd)"

echo ""
echo "  🎬 MovieVault — Full Deployment Pipeline"
echo "  ─────────────────────────────────────────"
echo "  This will run:"
echo "    1. 01-setup-ecr.sh    (ECR login + create repos)"
echo "    2. 02-build-push.sh   (go build + docker push)"
echo "    3. 03-deploy.sh       (kubectl apply + wait)"
echo ""
read -p "  Press ENTER to start, or Ctrl+C to cancel..."

bash "$SCRIPTS_DIR/01-setup-ecr.sh"
bash "$SCRIPTS_DIR/02-build-push.sh"
bash "$SCRIPTS_DIR/03-deploy.sh"

echo ""
echo "  ✅ Full pipeline complete!"
echo ""