#!/bin/bash

# ════════════════════════════════════════════════════════════════
#  02-build-push.sh
#  Run every time you change Go code and want to redeploy
#  Does: go mod tidy → docker build → tag → push to ECR
# ════════════════════════════════════════════════════════════════

set -e
source "$(dirname "$0")/common.sh"

print_banner
echo -e "${BOLD}  Running: 02-build-push.sh${NC}\n"

# ── Preflight ─────────────────────────────────────────────────────
print_step "1" "Preflight Checks"
require_tool aws
require_tool docker
require_tool go
check_aws_auth

[[ -d "$INVENTORY_DIR" ]] || print_fail "inventory-service not found at: $INVENTORY_DIR"
[[ -d "$ORDER_DIR" ]]     || print_fail "order-service not found at: $ORDER_DIR"
print_ok "Service directories found"

# ── Re-authenticate with ECR (tokens expire after 12 hours) ───────
print_step "2" "Refresh ECR Login"

aws ecr get-login-password --region "$AWS_REGION" | \
    docker login \
        --username AWS \
        --password-stdin \
        "$ECR_BASE" 2>&1 | grep -v "WARNING" || true
print_ok "ECR login refreshed"

# ── Go mod tidy ───────────────────────────────────────────────────
print_step "3" "Download Go Dependencies"

print_info "go mod tidy → inventory-service"
cd "$INVENTORY_DIR" && go mod tidy
print_ok "inventory-service dependencies ready"

print_info "go mod tidy → order-service"
cd "$ORDER_DIR" && go mod tidy
print_ok "order-service dependencies ready"

# ── Docker Build ──────────────────────────────────────────────────
print_step "4" "Build Docker Images"

# Enable BuildKit for faster, cached builds
export DOCKER_BUILDKIT=1

print_info "Building inventory-service..."
docker build \
    -t movievault-inventory \
    --cache-from "$ECR_BASE/$INVENTORY_IMAGE:latest" \
    "$INVENTORY_DIR"
print_ok "inventory-service image built"

print_info "Building order-service..."
docker build \
    -t movievault-order \
    --cache-from "$ECR_BASE/$ORDER_IMAGE:latest" \
    "$ORDER_DIR"
print_ok "order-service image built"

# ── Tag & Push ────────────────────────────────────────────────────
print_step "5" "Tag & Push to ECR"

GIT_TAG=$(get_git_tag)
print_info "Tagging images with: latest + $GIT_TAG"

push_image() {
    local local_tag=$1
    local ecr_repo=$2

    # Tag with both latest and git SHA
    docker tag "$local_tag" "$ECR_BASE/$ecr_repo:latest"
    docker tag "$local_tag" "$ECR_BASE/$ecr_repo:$GIT_TAG"

    print_info "Pushing $ecr_repo..."
    docker push "$ECR_BASE/$ecr_repo:latest"
    docker push "$ECR_BASE/$ecr_repo:$GIT_TAG"
    print_ok "Pushed → $ECR_BASE/$ecr_repo:latest"
    print_ok "Pushed → $ECR_BASE/$ecr_repo:$GIT_TAG"
}

push_image "movievault-inventory" "$INVENTORY_IMAGE"
push_image "movievault-order"     "$ORDER_IMAGE"

# ── Summary ───────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}${BOLD}  ✅ Build & push complete! (git tag: $GIT_TAG)${NC}"
echo -e "  Next step: run ${CYAN}./scripts/03-deploy.sh${NC}"
echo ""