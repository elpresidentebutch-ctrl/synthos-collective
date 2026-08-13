param(
  [string]$OutDir = ".synthos/push-button-node",
  [string]$NetworkDir = ".synthos/open-network-11-15",
  [switch]$StartValidators,
  [switch]$Reset
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $repoRoot

if ($Reset -and (Test-Path $OutDir)) {
  Write-Host "Resetting push-button node folder: $OutDir"
  Remove-Item -LiteralPath $OutDir -Recurse -Force
}

if ($StartValidators) {
  powershell.exe -ExecutionPolicy Bypass -File .\scripts\start-validator-11-15.ps1
}

if (-not (Test-Path (Join-Path $NetworkDir "genesis.json"))) {
  throw "Validator network material is missing. Run scripts/start-validator-11-15.ps1 first, or rerun this script with -StartValidators."
}

Write-Host "Initializing local push-button SYNTHOS node"
go run ./cmd/pushnode -out $OutDir -network-dir $NetworkDir

$nodeExe = Join-Path $OutDir "synthosd.exe"
$pidFile = Join-Path $OutDir "synthosd.pid"
$stdoutLog = Join-Path $OutDir "synthosd.out.log"
$stderrLog = Join-Path $OutDir "synthosd.err.log"

Write-Host "Building local node binary"
go build -o $nodeExe ./cmd/synthosd

if (Test-Path $pidFile) {
  $oldPid = Get-Content $pidFile -Raw
  $oldPid = $oldPid.Trim()
  if ($oldPid -and (Get-Process -Id $oldPid -ErrorAction SilentlyContinue)) {
    Write-Host "Push-button node is already running as PID $oldPid"
    node .\scripts\verify-push-button-node.mjs
    exit $LASTEXITCODE
  }
}

try {
  $existing = Invoke-WebRequest -Uri "http://127.0.0.1:8120/health" -UseBasicParsing -TimeoutSec 2
  if ($existing.StatusCode -eq 200) {
    Write-Host "A SYNTHOS node is already responding on http://127.0.0.1:8120"
    node .\scripts\verify-push-button-node.mjs
    exit $LASTEXITCODE
  }
} catch {
  # Port is not serving a SYNTHOS health endpoint; start our node below.
}

Write-Host "Starting local push-button node"
$process = Start-Process `
  -FilePath (Resolve-Path $nodeExe) `
  -WorkingDirectory (Resolve-Path $OutDir) `
  -WindowStyle Hidden `
  -RedirectStandardOutput $stdoutLog `
  -RedirectStandardError $stderrLog `
  -PassThru

Set-Content -Path $pidFile -Value $process.Id

Write-Host "Verifying local push-button node"
node .\scripts\verify-push-button-node.mjs
if ($LASTEXITCODE -ne 0) {
  throw "push-button node did not verify."
}
