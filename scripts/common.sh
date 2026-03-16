#!/bin/bash

# ════════════════════════════════════════════════════════════════
#  common.sh — Shared config & utilities
#  Source this in every script: source ./scripts/common.sh
# ════════════════════════════════════════════════════════════════

# ── AWS / Cluster Config ─────────────────────────────────────────
export AWS_ACCOUNT_ID="100984278168"
export AWS_REGION="us-east-1"
export CLUSTER_NAME="cliff-eks-cluster"
export NAMESPACE="movievault"
export ECR_BASE="$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com"

# ── Image names ───────────────────────────────────────────────────
export INVENTORY_IMAGE="movievault/inventory-service"
export ORDER_IMAGE="movievault/order-service"

# ── Paths ─────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export REPO_ROOT="$(dirname "$SCRIPT_DIR")"
export INVENTORY_DIR="$REPO_ROOT/movievault/inventory-service"
export ORDER_DIR="$REPO_ROOT/movievault/order-service"
export K8S_DIR="$REPO_ROOT/movievault/k8s"

# ── Colors ────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# ── Logging helpers ───────────────────────────────────────────────
print_step() { echo -e "\n${BLUE}${BOLD}━━━  STEP $1: $2  ━━━${NC}\n"; }
print_ok()   { echo -e "  ${GREEN}✅  $1${NC}"; }
print_info() { echo -e "  ${CYAN}ℹ️   $1${NC}"; }
print_warn() { echo -e "  ${YELLOW}⚠️   $1${NC}"; }
print_fail() { echo -e "  ${RED}❌  $1${NC}"; exit 1; }
print_banner() {
    echo -e "${BLUE}${BOLD}"
    echo "  ╔════════════════════════════════════════╗"
    echo "  ║         🎬  MovieVault Deploy          ║"
    echo "  ║   cliff-eks-cluster  |  us-east-1      ║"
    echo "  ╚════════════════════════════════════════╝"
    echo -e "${NC}"
}

# ── Preflight: check a tool is installed ─────────────────────────
require_tool() {
    command -v "$1" &>/dev/null || print_fail "'$1' is not installed. Please install it first."
    print_ok "$1 found"
}

# ── Check AWS credentials are working ────────────────────────────
check_aws_auth() {
    print_info "Verifying AWS credentials..."
    aws sts get-caller-identity --output json &>/dev/null || \
        print_fail "AWS credentials not configured. Run: aws configure"
    print_ok "AWS credentials valid"
}

# ── Connect kubectl to EKS ────────────────────────────────────────
connect_kubectl() {
    print_info "Connecting kubectl to $CLUSTER_NAME..."

    # Explicitly set the context to EKS (you have minikube + default also configured)
    kubectl config use-context \
        "arn:aws:eks:${AWS_REGION}:${AWS_ACCOUNT_ID}:cluster/${CLUSTER_NAME}" \
        2>/dev/null || true

    # Update kubeconfig (ignore errors, may already be set)
    aws eks update-kubeconfig \
        --region "$AWS_REGION" \
        --name "$CLUSTER_NAME" 2>/dev/null || true

    # Verify connection
    if kubectl get nodes &>/dev/null; then
        print_ok "kubectl connected to $CLUSTER_NAME"
    else
        print_fail "kubectl cannot reach the cluster. Run: aws eks update-kubeconfig --region us-east-1 --name cliff-eks-cluster"
    fi
}

# ── Get current git short SHA for image tagging ──────────────────
get_git_tag() {
    git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo "manual"
}