#!/bin/bash

# ════════════════════════════════════════════════════════════════
#  01-setup-ecr.sh
#  Run ONCE (or when setting up a new environment)
#  Does: preflight checks → ECR login → create repositories
# ════════════════════════════════════════════════════════════════

set -e
source "$(dirname "$0")/common.sh"

print_banner
echo -e "${BOLD}  Running: 01-setup-ecr.sh${NC}\n"

# ── Preflight ─────────────────────────────────────────────────────
print_step "1" "Preflight Checks"
require_tool aws
require_tool docker
check_aws_auth

# Check Docker daemon is running
docker info &>/dev/null || print_fail "Docker is not running. Please start Docker."
print_ok "Docker daemon is running"

# ── ECR Login ─────────────────────────────────────────────────────
print_step "2" "Login to Amazon ECR"

aws ecr get-login-password --region "$AWS_REGION" | \
    docker login \
        --username AWS \
        --password-stdin \
        "$ECR_BASE" 2>&1 | grep -v "WARNING" || true

print_ok "Authenticated with ECR: $ECR_BASE"

# ── Create ECR Repositories ───────────────────────────────────────
print_step "3" "Create ECR Repositories"

create_repo() {
    local name=$1
    if aws ecr describe-repositories \
            --repository-names "$name" \
            --region "$AWS_REGION" &>/dev/null; then
        print_info "Repo '$name' already exists — skipping"
    else
        aws ecr create-repository \
            --repository-name "$name" \
            --region "$AWS_REGION" \
            --image-scanning-configuration scanOnPush=true \
            --output json > /dev/null
        print_ok "Created: $name"
    fi
}

create_repo "$INVENTORY_IMAGE"
create_repo "$ORDER_IMAGE"

# ── Grant node role permission to pull from ECR ───────────────────
print_step "4" "Attach ECR Pull Policy to Node IAM Role"

aws iam attach-role-policy \
    --role-name AmazonEKSAutoNodeRole \
    --policy-arn arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly \
    2>/dev/null && print_ok "ECR read policy attached to AmazonEKSAutoNodeRole" || \
    print_info "Policy already attached — skipping"

# ── Done ──────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}${BOLD}  ✅ ECR setup complete!${NC}"
echo -e "  Next step: run ${CYAN}./scripts/02-build-push.sh${NC}"
echo ""