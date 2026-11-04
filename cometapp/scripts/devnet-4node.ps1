# devnet-4node.ps1: Bootstraps and orchestrates a local 4-validator CometBFT devnet on Windows
param (
    [string]$DevnetDir = "$env:USERPROFILE\.synthos\devnet-4node"
)

$ErrorActionPreference = "Stop"
Write-Host "=== Bootstrapping SYNTHOS 4-Node CometBFT Devnet in $DevnetDir ===" -ForegroundColor Cyan

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location (Split-Path -Parent $scriptDir)

# Build synthos-comet.exe if not present
if (-not (Test-Path ".\cometapp.exe")) {
    Write-Host "Compiling cometapp.exe..." -ForegroundColor Yellow
    go build -o cometapp.exe .
}

# Initialize node directories
1..4 | ForEach-Object {
    $nodeHome = Join-Path $DevnetDir "node$_"
    New-Item -ItemType Directory -Force -Path (Join-Path $nodeHome "config") | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $nodeHome "data") | Out-Null
    Write-Host "Initializing Node $_..."
    .\cometapp.exe validator-key --home $nodeHome | Out-File -FilePath (Join-Path $nodeHome "val_key.txt")
}

Write-Host "Devnet nodes initialized successfully." -ForegroundColor Green
Write-Host "To start the cluster with Docker Compose:" -ForegroundColor White
Write-Host "docker compose -f docker-compose.devnet.yml up -d" -ForegroundColor Yellow
