#!/bin/bash

# ════════════════════════════════════════════════════════════════
#  04-teardown.sh
#  Deletes everything from EKS to stop AWS charges
#  Does: delete namespace → verify → print cost reminder
# ════════════════════════════════════════════════════════════════

set -e
source "$(dirname "$0")/common.sh"

print_banner
echo -e "${BOLD}  Running: 04-teardown.sh${NC}\n"

# ── Confirm before destroying ─────────────────────────────────────
echo -e "${RED}${BOLD}  ⚠️  WARNING: This will delete ALL MovieVault resources from EKS${NC}"
echo -e "${YELLOW}  This includes: pods, services, databases, load balancers${NC}"
echo ""
read -p "  Type 'yes' to confirm: " CONFIRM

if [[ "$CONFIRM" != "yes" ]]; then
    echo -e "\n  ${CYAN}Cancelled. Nothing was deleted.${NC}\n"
    exit 0
fi

# ── Preflight ─────────────────────────────────────────────────────
print_step "1" "Connect to Cluster"
require_tool kubectl
check_aws_auth
connect_kubectl

# ── Delete namespace (removes everything inside it) ───────────────
print_step "2" "Delete All Resources"

print_info "Deleting namespace '$NAMESPACE' and all resources inside it..."
kubectl delete namespace "$NAMESPACE" --ignore-not-found=true
print_ok "Namespace deleted — all pods, services, volumes removed"

# ── Verify ────────────────────────────────────────────────────────
print_step "3" "Verify Cleanup"

echo -e "${BOLD}Remaining namespaces:${NC}"
kubectl get namespaces

echo ""
echo -e "${BOLD}Remaining nodes (EKS Auto Mode will scale these down):${NC}"
kubectl get nodes

# ── Done ──────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}${BOLD}  ✅ Teardown complete!${NC}"
echo -e "  ${YELLOW}💰 Tip: EKS Auto Mode will terminate idle EC2 nodes automatically.${NC}"
echo -e "  ${YELLOW}   Check your AWS Cost Explorer in 1 hour to confirm.${NC}"
echo ""