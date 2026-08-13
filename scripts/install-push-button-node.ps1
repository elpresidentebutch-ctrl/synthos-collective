param(
  [string]$InstallDir = "$env:USERPROFILE\Documents\SYNTHOS\synthos-collective",
  [string]$RepoUrl = "https://gitlab.com/synthos-collective-group/synthos-collective.git",
  [switch]$StartValidators,
  [switch]$SkipValidators
)

$ErrorActionPreference = "Stop"

function Require-Command {
  param(
    [string]$Name,
    [string]$InstallHint
  )

  if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
    throw "$Name is required. $InstallHint"
  }
}

function Write-Step {
  param([string]$Message)
  Write-Host ""
  Write-Host "==> $Message" -ForegroundColor Cyan
}

Write-Host "SYNTHOS Push-Button Node Installer" -ForegroundColor Green
Write-Host "This installer clones/updates the SYNTHOS repo locally and starts a local node."
Write-Host "Private node keys stay on this computer under .synthos/push-button-node."

Require-Command git "Install Git for Windows from https://git-scm.com/download/win, then rerun this command."
Require-Command go "Install Go from https://go.dev/dl/, then rerun this command."
Require-Command node "Install Node.js LTS from https://nodejs.org/, then rerun this command."

if (-not $SkipValidators) {
  Require-Command docker "Install Docker Desktop from https://www.docker.com/products/docker-desktop/, start it, then rerun this command."
  docker info *> $null
  if ($LASTEXITCODE -ne 0) {
    throw "Docker Desktop is installed, but the Docker engine is not running. Start Docker Desktop, wait for it to say it is running, then rerun this command."
  }
}

$parent = Split-Path -Parent $InstallDir
if (-not (Test-Path $parent)) {
  New-Item -ItemType Directory -Force -Path $parent | Out-Null
}

if (Test-Path (Join-Path $InstallDir ".git")) {
  Write-Step "Updating SYNTHOS repo at $InstallDir"
  Set-Location $InstallDir
  git fetch origin main
  git checkout main
  git pull --ff-only origin main
} elseif (Test-Path $InstallDir) {
  throw "$InstallDir exists but is not a Git repository. Move it aside or choose a different -InstallDir."
} else {
  Write-Step "Cloning SYNTHOS repo into $InstallDir"
  git clone $RepoUrl $InstallDir
  Set-Location $InstallDir
}

if (-not $SkipValidators) {
  if ($StartValidators) {
    Write-Step "Starting local validators 11-15"
    powershell.exe -ExecutionPolicy Bypass -File .\scripts\start-validator-11-15.ps1
  } elseif (-not (Test-Path ".synthos\open-network-11-15\genesis.json")) {
    Write-Step "Validator network material not found; starting local validators 11-15"
    powershell.exe -ExecutionPolicy Bypass -File .\scripts\start-validator-11-15.ps1
  }
}

Write-Step "Starting push-button SYNTHOS node"
if ($SkipValidators) {
  powershell.exe -ExecutionPolicy Bypass -File .\scripts\start-push-button-node.ps1
} else {
  powershell.exe -ExecutionPolicy Bypass -File .\scripts\start-push-button-node.ps1
}

Write-Step "SYNTHOS node ready"
Write-Host "Local node RPC: http://127.0.0.1:8120"
Write-Host "Verify any time with:"
Write-Host "  node .\scripts\verify-push-button-node.mjs"
Write-Host ""
Write-Host "Stop the node with:"
Write-Host "  powershell.exe -ExecutionPolicy Bypass -File .\scripts\stop-push-button-node.ps1"
