<#
.SYNOPSIS
    Deploy SYNTHOS validator Workers to Cloudflare.

.DESCRIPTION
    Deploys the validator Worker code to all 6 validator Workers (10-15).
    Requires: npm install -g wrangler
    Then:     wrangler login
    Then run: .\scripts\deploy_workers.ps1

.EXAMPLE
    .\scripts\deploy_workers.ps1
    .\scripts\deploy_workers.ps1 -Validators 11,12
#>

param(
    [int[]]$Validators = @(10, 11, 12, 13, 14, 15)
)

$ErrorActionPreference = "Stop"
$workerDir = Join-Path $PSScriptRoot "..\workers\validator"

# Check wrangler is available
if (-not (Get-Command "wrangler" -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: wrangler CLI not found." -ForegroundColor Red
    Write-Host "Install it:  npm install -g wrangler"
    Write-Host "Then login:  wrangler login"
    exit 1
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  SYNTHOS Validator Worker Deployment"
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Push-Location $workerDir

foreach ($v in $Validators) {
    $name = "synthos-validator-$v"
    Write-Host "Deploying $name ..." -ForegroundColor Yellow

    # Deploy with name override so each Worker gets its own name
    wrangler deploy --name $name 2>&1 | ForEach-Object { Write-Host "  $_" }

    if ($LASTEXITCODE -eq 0) {
        Write-Host "  OK: $name deployed" -ForegroundColor Green
    } else {
        Write-Host "  FAIL: $name deployment failed" -ForegroundColor Red
    }
    Write-Host ""
}

Pop-Location

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Deployment complete."
Write-Host "  Test with:"
Write-Host "    curl https://synthos-validator-15.jamesishamwilliams.workers.dev/health"
Write-Host "========================================" -ForegroundColor Cyan
