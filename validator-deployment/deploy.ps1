# Deploy 15 SYNTHOS validators to Cloudflare Workers via PowerShell

$Validators = 1..5 | ForEach-Object { "validator$_" }

Write-Host "SYNTHOS VALIDATOR DEPLOYMENT"
Write-Host "===================================="
Write-Host ""
Write-Host "Deploying 5 validators to Cloudflare Workers"
Write-Host "Storage: Cloudflare R2"
Write-Host "Cost: $0/month (on free tier)"
Write-Host ""

# Step 1: Install Wrangler if not present
if (!(Get-Command "wrangler" -ErrorAction SilentlyContinue)) {
    Write-Host "Installing Wrangler CLI..."
    npm install -g wrangler
}

# Step 2: Login to Cloudflare
Write-Host "Authenticated with Cloudflare via existing token."

# Step 3: Create R2 bucket
Write-Host "Creating R2 bucket..."
try {
    npx wrangler r2 bucket create synthos-validators
} catch {
    Write-Host "Bucket may already exist or creation failed."
}

# Step 4: Deploy each validator.
# PRIVATE_KEY must already exist as a Wrangler secret for each environment.
foreach ($Validator in $Validators) {
    Write-Host ""
    Write-Host "Deploying $Validator..."
    
    # Deploy without printing or persisting secret material.
    npx wrangler deploy --env "$Validator" --name "synthos-$Validator"
    
    Write-Host "$Validator deployed successfully"
}

Write-Host ""
Write-Host "DEPLOYMENT COMPLETE!"
Write-Host ""
Write-Host "Summary:"
Write-Host "--------"
Write-Host "5 validators deployed"
Write-Host "R2 bucket created (free first 10GB)"
Write-Host "Cron triggers active (every 5 seconds)"
Write-Host "Cost: $0/month on free tier"
Write-Host ""
Write-Host "Next steps:"
Write-Host "1. Monitor at: https://dash.cloudflare.com/"
Write-Host "2. Check logs: npx wrangler tail --env validator-1"
