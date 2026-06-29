#!/bin/bash
# Deploy SYNTHOS validators after secrets have been provisioned.

set -euo pipefail

VALIDATORS=(
    "validator1" "validator2" "validator3" "validator4" "validator5"
    "validator6" "validator7" "validator8" "validator9" "validator10"
    "validator11" "validator12" "validator13" "validator14" "validator15"
)

echo "SYNTHOS VALIDATOR DEPLOYMENT"
echo "Private keys must already be stored as Wrangler secrets."
echo "Example: wrangler secret put PRIVATE_KEY --env validator1"

if ! command -v wrangler >/dev/null 2>&1; then
    echo "wrangler is required" >&2
    exit 1
fi

wrangler r2 bucket create synthos-validators || echo "R2 bucket already exists"

for validator in "${VALIDATORS[@]}"; do
    echo "Deploying ${validator} without exposing secret material..."
    wrangler deploy --env "${validator}"
done

echo "Deployment complete. Verify each worker before enabling validator membership."
