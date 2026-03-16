#!/bin/bash

# ════════════════════════════════════════════════════════════════
#  05-deploy-ui.sh
#  Deploys the MovieVault UI to AWS S3 as a static website
#
#  What is S3?
#  ─────────────
#  S3 (Simple Storage Service) is AWS's file storage.
#  Think of it like Google Drive for AWS — you store files in it.
#  But S3 has a special feature: "Static Website Hosting"
#  which lets it serve HTML files directly to browsers,
#  just like a real web server — but with ZERO servers to manage.
#
#  Why S3 for a UI?
#  ─────────────────
#  ✓ Free (or nearly free for low traffic)
#  ✓ No server to manage or patch
#  ✓ Scales to millions of users automatically
#  ✓ 99.999999999% durability (AWS stores 3 copies)
#  ✓ Deploy = just copy a file (aws s3 cp)
# ════════════════════════════════════════════════════════════════

set -e
source "$(dirname "$0")/common.sh"

print_banner
echo -e "${BOLD}  Running: 05-deploy-ui.sh${NC}\n"

# ── Config ────────────────────────────────────────────────────────
BUCKET_NAME="movievault-ui-cliff"
UI_FILE="$REPO_ROOT/movievault-ui.html"
REGION="$AWS_REGION"
POLICY_FILE="$(dirname "$0")/../s3/bucket-policy.json"

# ════════════════════════════════════════════════════════════════
#  STEP 1 — PREFLIGHT
# ════════════════════════════════════════════════════════════════
print_step "1" "Preflight Checks"

require_tool aws
check_aws_auth

# Check the HTML file exists
if [[ ! -f "$UI_FILE" ]]; then
    print_fail "UI file not found at: $UI_FILE
    Make sure movievault-ui.html is at the root of your repo."
fi
print_ok "UI file found: $UI_FILE"

# ════════════════════════════════════════════════════════════════
#  STEP 2 — CREATE THE S3 BUCKET
#
#  What is a bucket?
#  A bucket is a container for files in S3.
#  Like a hard drive partition — you store files inside it.
#  Bucket names must be GLOBALLY unique across ALL AWS accounts.
#  That is why we use "movievault-ui-cliff" — your name makes it unique.
# ════════════════════════════════════════════════════════════════
print_step "2" "Create S3 Bucket"

if aws s3api head-bucket --bucket "$BUCKET_NAME" 2>/dev/null; then
    print_info "Bucket '$BUCKET_NAME' already exists — skipping creation"
else
    print_info "Creating bucket: $BUCKET_NAME in $REGION..."

    # us-east-1 is special — it does NOT use LocationConstraint
    if [[ "$REGION" == "us-east-1" ]]; then
        aws s3api create-bucket \
            --bucket "$BUCKET_NAME" \
            --region "$REGION"
    else
        aws s3api create-bucket \
            --bucket "$BUCKET_NAME" \
            --region "$REGION" \
            --create-bucket-configuration LocationConstraint="$REGION"
    fi

    print_ok "Bucket created: s3://$BUCKET_NAME"
fi

# ════════════════════════════════════════════════════════════════
#  STEP 3 — DISABLE "BLOCK PUBLIC ACCESS"
#
#  Why do we need this?
#  By default AWS blocks ALL public access to S3 buckets.
#  This is a safety feature to prevent accidental data leaks.
#  Since our UI is intentionally PUBLIC (anyone can visit it),
#  we need to turn this protection off for this specific bucket.
#
#  We are NOT making your AWS account public —
#  just this one bucket that contains only the UI HTML file.
# ════════════════════════════════════════════════════════════════
print_step "3" "Disable Block Public Access"

print_info "Disabling public access block on bucket..."
aws s3api put-public-access-block \
    --bucket "$BUCKET_NAME" \
    --public-access-block-configuration \
        "BlockPublicAcls=false,IgnorePublicAcls=false,BlockPublicPolicy=false,RestrictPublicBuckets=false"

print_ok "Public access block disabled"

# ════════════════════════════════════════════════════════════════
#  STEP 4 — ATTACH BUCKET POLICY
#
#  What is a bucket policy?
#  A JSON document that defines WHO can do WHAT to files in the bucket.
#  Our policy says:
#    Principal: "*"    = anyone in the world
#    Action: GetObject = can download/read files
#    Resource: bucket/* = all files in this bucket
#
#  This is what makes your website publicly accessible.
#  Without this, browsers would get "403 Access Denied".
# ════════════════════════════════════════════════════════════════
print_step "4" "Attach Public Read Policy"

print_info "Attaching bucket policy from: $POLICY_FILE"

# Inject the actual bucket name into the policy
POLICY=$(cat "$POLICY_FILE" | sed "s/movievault-ui-cliff/$BUCKET_NAME/g")

aws s3api put-bucket-policy \
    --bucket "$BUCKET_NAME" \
    --policy "$POLICY"

print_ok "Bucket policy applied — files are now publicly readable"

# ════════════════════════════════════════════════════════════════
#  STEP 5 — ENABLE STATIC WEBSITE HOSTING
#
#  What does this do?
#  Normally S3 is just a file store — you download files from it.
#  Static website hosting turns it into a web server that:
#    - Responds to HTTP requests (not just file downloads)
#    - Serves index.html when someone visits the root URL /
#    - Returns a proper error page for missing files
#
#  After this step, your bucket gets a public website URL:
#  http://BUCKET.s3-website-REGION.amazonaws.com
# ════════════════════════════════════════════════════════════════
print_step "5" "Enable Static Website Hosting"

print_info "Configuring bucket as static website..."
aws s3 website "s3://$BUCKET_NAME" \
    --index-document movievault-ui.html \
    --error-document movievault-ui.html

print_ok "Static website hosting enabled"
print_ok "Index document: movievault-ui.html"

# ════════════════════════════════════════════════════════════════
#  STEP 6 — UPLOAD THE HTML FILE
#
#  aws s3 cp = copy a file to/from S3
#  --content-type = tells browsers this is HTML, not a download
#  --cache-control = tells browsers/CDN how long to cache it
#                    no-cache = always fetch fresh version
#
#  Every time you run this script after making UI changes,
#  this is the ONLY step that matters — the bucket is already set up.
# ════════════════════════════════════════════════════════════════
print_step "6" "Upload UI File to S3"

print_info "Uploading movievault-ui.html..."
aws s3 cp "$UI_FILE" "s3://$BUCKET_NAME/movievault-ui.html" \
    --content-type "text/html" \
    --cache-control "no-cache, no-store, must-revalidate"

print_ok "File uploaded to s3://$BUCKET_NAME/movievault-ui.html"

# ════════════════════════════════════════════════════════════════
#  STEP 7 — GET YOUR LIVE URL
# ════════════════════════════════════════════════════════════════
print_step "7" "Your Live Website"

WEBSITE_URL="http://$BUCKET_NAME.s3-website-$REGION.amazonaws.com"

echo ""
echo -e "${GREEN}${BOLD}  ╔══════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}${BOLD}  ║        🌐 MovieVault UI is LIVE!                     ║${NC}"
echo -e "${GREEN}${BOLD}  ╚══════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  ${CYAN}${BOLD}URL:${NC}  $WEBSITE_URL"
echo ""
echo -e "  ${BOLD}Next steps:${NC}"
echo -e "  1. Open the URL above in your browser"
echo -e "  2. Paste your EKS Load Balancer URLs in the header inputs:"

# Try to get the LB URLs from EKS
INV_LB=$(kubectl get svc inventory-service -n movievault \
    -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null)
ORD_LB=$(kubectl get svc order-service -n movievault \
    -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null)

if [[ -n "$INV_LB" ]]; then
    echo -e "     ${YELLOW}Inventory:${NC} http://$INV_LB"
else
    echo -e "     ${YELLOW}Inventory:${NC} run: kubectl get svc inventory-service -n movievault"
fi
if [[ -n "$ORD_LB" ]]; then
    echo -e "     ${YELLOW}Orders:${NC}    http://$ORD_LB"
else
    echo -e "     ${YELLOW}Orders:${NC}    run: kubectl get svc order-service -n movievault"
fi
echo ""
echo -e "  ${MUTED}To update the UI after changes:${NC}"
echo -e "  ${CYAN}./scripts/05-deploy-ui.sh${NC}  ← just re-run this script"
echo ""