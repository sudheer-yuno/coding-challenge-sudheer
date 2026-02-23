#!/bin/bash
# =============================================================================
# Kaveri Market - Batch Payout Engine Demo Script
# Demonstrates: batch processing, status tracking, resumability, retry
# =============================================================================

set -e

BASE_URL="${BASE_URL:-http://localhost:8080}"
BOLD="\033[1m"
GREEN="\033[32m"
YELLOW="\033[33m"
CYAN="\033[36m"
RESET="\033[0m"

log() { echo -e "${CYAN}[DEMO]${RESET} $1"; }
step() { echo -e "\n${BOLD}${GREEN}=== STEP $1: $2 ===${RESET}\n"; }
pause() { echo -e "${YELLOW}Press Enter to continue...${RESET}"; read -r; }

# Health check
log "Checking server health..."
curl -s "$BASE_URL/health" | python3 -m json.tool
echo ""

# =====================================================================
step 1 "Create a small batch (20 payouts) for quick demo"
# =====================================================================

RESPONSE=$(curl -s -X POST "$BASE_URL/api/v1/batches" \
  -H "Content-Type: application/json" \
  -d "$(python3 -c "
import json, random
payouts = []
for i in range(20):
    region = random.choice(['ID','PH','VN'])
    payouts.append({
        'vendor_id': f'demo-vendor-{i+1:03d}',
        'vendor_name': f'Demo Vendor {region} #{i+1}',
        'amount': round(random.uniform(100, 5000), 2),
        'currency': random.choice(['IDR','PHP','VND']),
        'bank_account': f'{region}****{random.randint(1000,9999)}',
        'bank_name': random.choice(['BCA','BDO','Vietcombank']),
        'transaction_ids': [f'TXN-DEMO-{i+1:03d}-{j}' for j in range(random.randint(1,3))]
    })
print(json.dumps({'payouts': payouts}))
")")

echo "$RESPONSE" | python3 -m json.tool
BATCH_ID=$(echo "$RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['batch_id'])")
log "Batch ID: $BATCH_ID"
pause

# =====================================================================
step 2 "Start processing the batch"
# =====================================================================

curl -s -X POST "$BASE_URL/api/v1/batches/$BATCH_ID/start" | python3 -m json.tool
log "Waiting 5 seconds for processing..."
sleep 5

# =====================================================================
step 3 "Check batch status and statistics"
# =====================================================================

curl -s "$BASE_URL/api/v1/batches/$BATCH_ID" | python3 -m json.tool
pause

# =====================================================================
step 4 "View failed payouts with failure reasons"
# =====================================================================

log "Fetching failed payouts..."
curl -s "$BASE_URL/api/v1/batches/$BATCH_ID/payouts?status=failed" | python3 -m json.tool
pause

# =====================================================================
step 5 "Retry failed payouts"
# =====================================================================

log "Retrying retryable failures..."
curl -s -X POST "$BASE_URL/api/v1/batches/$BATCH_ID/retry-failed" | python3 -m json.tool
sleep 3
log "Status after retry:"
curl -s "$BASE_URL/api/v1/batches/$BATCH_ID" | python3 -m json.tool
pause

# =====================================================================
step 6 "Resumability Demo - Create large batch, stop mid-processing"
# =====================================================================

log "Creating a batch of 500 payouts for resumability test..."

RESPONSE2=$(curl -s -X POST "$BASE_URL/api/v1/batches" \
  -H "Content-Type: application/json" \
  -d "$(python3 -c "
import json, random
payouts = []
for i in range(500):
    region = random.choice(['ID','PH','VN'])
    payouts.append({
        'vendor_id': f'resume-vendor-{i+1:04d}',
        'vendor_name': f'Resume Test Vendor #{i+1}',
        'amount': round(random.uniform(50, 10000), 2),
        'currency': random.choice(['IDR','PHP','VND']),
        'bank_account': f'{region}****{random.randint(1000,9999)}',
        'bank_name': random.choice(['BCA','Mandiri','BDO','Techcombank']),
        'transaction_ids': [f'TXN-RESUME-{i+1:04d}-{j}' for j in range(random.randint(1,4))]
    })
print(json.dumps({'payouts': payouts}))
")")

BATCH_ID2=$(echo "$RESPONSE2" | python3 -c "import sys,json; print(json.load(sys.stdin)['batch_id'])")
log "Resumability Batch ID: $BATCH_ID2"

log "Starting processing..."
curl -s -X POST "$BASE_URL/api/v1/batches/$BATCH_ID2/start" | python3 -m json.tool

log "Waiting 3 seconds (partial processing)..."
sleep 3

log "Sending STOP signal..."
curl -s -X POST "$BASE_URL/api/v1/batches/$BATCH_ID2/stop" | python3 -m json.tool

sleep 1
log "Status after STOP (should show partial progress):"
curl -s "$BASE_URL/api/v1/batches/$BATCH_ID2" | python3 -m json.tool
pause

# =====================================================================
step 7 "Resume the stopped batch"
# =====================================================================

log "Resuming batch processing..."
curl -s -X POST "$BASE_URL/api/v1/batches/$BATCH_ID2/start" | python3 -m json.tool

log "Waiting for completion..."
sleep 15

log "Final status (all should be processed, no duplicates):"
FINAL=$(curl -s "$BASE_URL/api/v1/batches/$BATCH_ID2")
echo "$FINAL" | python3 -m json.tool

# Verify no duplicates
TOTAL=$(echo "$FINAL" | python3 -c "import sys,json; d=json.load(sys.stdin); s=d['statistics']; print(s['completed']+s['failed'])")
log "Total processed: $TOTAL / 500 (should equal 500, proving no duplicates)"

echo -e "\n${BOLD}${GREEN}========================================${RESET}"
echo -e "${BOLD}${GREEN}  DEMO COMPLETE!${RESET}"
echo -e "${BOLD}${GREEN}========================================${RESET}"
echo ""
echo "Summary of what was demonstrated:"
echo "  ✅ Batch creation with transaction IDs"
echo "  ✅ Concurrent processing with individual error handling"
echo "  ✅ Real-time status tracking and statistics"
echo "  ✅ Failed payout inspection with failure reasons"
echo "  ✅ Retry of retryable failures"
echo "  ✅ Stop/resume without duplicate payments"
