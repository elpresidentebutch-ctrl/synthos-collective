param(
  [switch]$SkipContracts
)

$ErrorActionPreference = "Stop"
$repo = Resolve-Path (Join-Path $PSScriptRoot "..")

Write-Host "SYNTHOS bridge pre-audit"
Write-Host "Repository: $repo"

Push-Location $repo
try {
  Write-Host "Running Go tests..."
  go test ./...

  if (-not $SkipContracts) {
    Write-Host "Compiling contracts..."
    Push-Location (Join-Path $repo "contracts")
    try {
      npm.cmd run compile
      npm.cmd test
    } finally {
      Pop-Location
    }
  }

  Write-Host "Checking bridge source markers..."
  $requiredPatterns = @(
    "BridgeReleaseQueued",
    "setBridgeLimits",
    "processedMessages",
    "approvedBy",
    "bridge_release_native",
    "BridgeReleaseSigningMessage",
    "validator_signatures",
    "watch-evm"
  )

  foreach ($pattern in $requiredPatterns) {
    $result = rg --fixed-strings $pattern contracts cmd internal docs website
    if (-not $result) {
      throw "Required bridge marker not found: $pattern"
    }
  }

  Write-Host "Bridge pre-audit checks passed."
} finally {
  Pop-Location
}
