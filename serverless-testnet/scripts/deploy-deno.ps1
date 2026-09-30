param(
  [Parameter(Mandatory = $true)] [string] $Organization
)

$ErrorActionPreference = "Stop"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$Generated = Join-Path $Root "generated"
if (-not (Test-Path (Join-Path $Generated "genesis.json"))) {
  throw "Run: deno run -A scripts/generate-network.ts endpoints.json"
}

for ($i = 1; $i -le 5; $i++) {
  $Id = "deno-$i"
  $App = "synthos-validator-deno-$i"
  $Database = "synthos-validator-kv-$i"
  $EnvFile = Join-Path $Generated "$Id.env"

  Write-Host "Provisioning $App"
  deno deploy create $Root `
    --org $Organization `
    --app $App `
    --source local `
    --runtime-mode dynamic `
    --entrypoint deno/main.ts `
    --working-directory . `
    --build-timeout 5 `
    --build-memory-limit 1024 `
    --region global `
    --no-wait

  deno deploy database provision $Database --kind denokv --org $Organization
  deno deploy database assign $Database --org $Organization --app $App
  deno deploy env load $EnvFile --org $Organization --app $App
  deno deploy $Root --org $Organization --app $App --prod
}

Write-Host "Five Deno validator apps deployed. Run verify-network.ts next."
