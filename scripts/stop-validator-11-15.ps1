param(
  [string]$OutDir = ".synthos/open-network-11-15"
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $repoRoot

docker compose -f (Join-Path $OutDir "docker-compose.yml") down

