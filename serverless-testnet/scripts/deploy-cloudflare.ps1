$ErrorActionPreference = "Stop"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$Cloudflare = Join-Path $Root "cloudflare"
$Secrets = Join-Path $Root "generated/cloudflare-1.env"
if (-not (Test-Path $Secrets)) {
  throw "Run: deno run -A scripts/generate-network.ts endpoints.json"
}

Push-Location $Cloudflare
try {
  npm install
  npx wrangler deploy
  npx wrangler secret bulk $Secrets
  npx wrangler deploy
  Write-Host "Cloudflare validator deployed with Durable Object persistence and 15-second alarm scheduling."
} finally {
  Pop-Location
}
