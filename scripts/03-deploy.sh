#!/bin/bash

# ════════════════════════════════════════════════════════════════
#  03-deploy.sh
#  Run every time you want to apply changes to EKS
#  Does: connect kubectl → apply K8s manifests → wait → verify
# ════════════════════════════════════════════════════════════════

set -e
source "$(dirname "$0")/common.sh"

print_banner
echo -e "${BOLD}  Running: 03-deploy.sh${NC}\n"

# ── Preflight ─────────────────────────────────────────────────────
print_step "1" "Preflight Checks"
require_tool kubectl
require_tool aws
check_aws_auth
connect_kubectl

[[ -d "$K8S_DIR" ]] || print_fail "k8s manifests not found at: $K8S_DIR"
print_ok "K8s manifests directory found"

# ── Apply Manifests ───────────────────────────────────────────────
print_step "2" "Apply Kubernetes Manifests"

# Ordered — namespace first, infra second, services last
K8S_FILES=(
    "00-namespace.yaml"
    "01-postgres.yaml"
    "02-kafka.yaml"
    "03-inventory-service.yaml"
    "04-order-service.yaml"
)

for file in "${K8S_FILES[@]}"; do
    filepath="$K8S_DIR/$file"
    if [[ -f "$filepath" ]]; then
        print_info "Applying $file..."
        kubectl apply -f "$filepath"
        print_ok "$file applied"
    else
        print_warn "Skipping $file — not found at $filepath"
    fi
done

# ── Wait for Pods ─────────────────────────────────────────────────
print_step "3" "Wait for Pods to be Ready"

wait_for() {
    local label=$1
    local timeout=$2
    print_info "Waiting for $label (timeout: ${timeout}s)..."
    kubectl wait --for=condition=ready pod \
        -l "app=$label" \
        -n "$NAMESPACE" \
        --timeout="${timeout}s" 2>/dev/null && \
        print_ok "$label is Ready" || \
        print_warn "$label not ready yet — run: kubectl get pods -n $NAMESPACE"
}

wait_for "postgres"          120
wait_for "kafka"             180
wait_for "inventory-service" 120
wait_for "order-service"     120

# ── Force rollout of services to pick up new images ──────────────
print_step "4" "Rollout New Image Versions"

print_info "Rolling out inventory-service..."
kubectl rollout restart deployment inventory-service -n "$NAMESPACE" 2>/dev/null || true

print_info "Rolling out order-service..."
kubectl rollout restart deployment order-service -n "$NAMESPACE" 2>/dev/null || true

print_info "Waiting for rollouts to complete..."
kubectl rollout status deployment inventory-service -n "$NAMESPACE" --timeout=120s
kubectl rollout status deployment order-service     -n "$NAMESPACE" --timeout=120s
print_ok "Rollouts complete"

# ── Print Summary ─────────────────────────────────────────────────
print_step "5" "Deployment Summary"

echo -e "${BOLD}📦 Pods:${NC}"
kubectl get pods -n "$NAMESPACE"

echo ""
echo -e "${BOLD}🌐 Services:${NC}"
kubectl get services -n "$NAMESPACE"

echo ""
echo -e "${BOLD}🖥️  Worker Nodes:${NC}"
kubectl get nodes

# Get load balancer URLs
INVENTORY_URL=$(kubectl get svc inventory-service -n "$NAMESPACE" \
    -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null)
ORDER_URL=$(kubectl get svc order-service -n "$NAMESPACE" \
    -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null)

echo ""
echo -e "${GREEN}${BOLD}  ╔════════════════════════════════════════╗${NC}"
echo -e "${GREEN}${BOLD}  ║       🎉 Deployment Complete!          ║${NC}"
echo -e "${GREEN}${BOLD}  ╚════════════════════════════════════════╝${NC}"

if [[ -n "$INVENTORY_URL" ]]; then
    echo -e "  ${CYAN}Inventory API:${NC}  http://$INVENTORY_URL"
    echo -e "  ${CYAN}Test health:${NC}    curl http://$INVENTORY_URL/health"
else
    echo -e "  ${YELLOW}Inventory LB still provisioning — wait 2-3 min then:${NC}"
    echo -e "  kubectl get svc inventory-service -n $NAMESPACE"
fi

if [[ -n "$ORDER_URL" ]]; then
    echo -e "  ${CYAN}Order API:${NC}      http://$ORDER_URL"
else
    echo -e "  ${YELLOW}Order LB still provisioning...${NC}"
fi
echo ""