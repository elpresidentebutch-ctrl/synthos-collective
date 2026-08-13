param(
  [string]$OutDir = ".synthos/push-button-node"
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $repoRoot

$pidFile = Join-Path $OutDir "synthosd.pid"
if (-not (Test-Path $pidFile)) {
  Write-Host "No push-button node PID file found."
  exit 0
}

$pidText = (Get-Content $pidFile -Raw).Trim()
if ($pidText -and (Get-Process -Id $pidText -ErrorAction SilentlyContinue)) {
  Stop-Process -Id $pidText -Force
  Write-Host "Stopped push-button node PID $pidText"
}

Remove-Item -LiteralPath $pidFile -Force

