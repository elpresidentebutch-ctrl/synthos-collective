param(
  [string]$OutDir = ".synthos/open-network-11-15",
  [switch]$Reset
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $repoRoot

docker info *> $null
if ($LASTEXITCODE -ne 0) {
  throw "Docker is installed, but the Docker engine is not running. Start Docker Desktop, wait for it to say it is running, then rerun this script."
}

if ($Reset -and (Test-Path $OutDir)) {
  Write-Host "Resetting generated network folder: $OutDir"
  Remove-Item -LiteralPath $OutDir -Recurse -Force
}

if (-not (Test-Path (Join-Path $OutDir "docker-compose.yml"))) {
  Write-Host "Generating SYNTHOS validator network 11-15 in $OutDir"
  go run ./cmd/opennet `
    -out $OutDir `
    -validators 5 `
    -start-index 11 `
    -local-base-rpc-port 8111 `
    -public-rpc "http://127.0.0.1:8111"
}

Write-Host "Starting validators 11-15"
docker compose -f (Join-Path $OutDir "docker-compose.yml") up --build -d
if ($LASTEXITCODE -ne 0) {
  throw "docker compose failed to start validators 11-15."
}

Write-Host "Verifying validators converge to one chain height"
node ./scripts/verify-validator-11-15.mjs
if ($LASTEXITCODE -ne 0) {
  throw "validators 11-15 did not converge."
}
