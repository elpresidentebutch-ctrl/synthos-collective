$ErrorActionPreference = "Stop"

param(
  [string]$DeploymentFile = "",
  [string]$AgentUrl = "http://127.0.0.1:8788"
)

$repo = Split-Path -Parent $PSScriptRoot
if ($DeploymentFile -eq "") {
  $DeploymentFile = Join-Path $repo "contracts\deployments\latest.json"
}

if (!(Test-Path $DeploymentFile)) {
  throw "Deployment file not found: $DeploymentFile"
}

$deployment = Get-Content -Raw -Path $DeploymentFile | ConvertFrom-Json
$payload = @{
  network = $deployment.network
  chain_rpc = if ($env:SYNTHOS_REWARD_CHAIN_RPC) { $env:SYNTHOS_REWARD_CHAIN_RPC } else { "http://127.0.0.1:8545" }
  syn_coin = $deployment.contracts.synCoin
  adopter_rewards = $deployment.contracts.adopterRewards
  activation_reward = $deployment.adopterRewards.activationReward
  heartbeat_reward = $deployment.adopterRewards.heartbeatReward
  heartbeat_interval_seconds = $deployment.adopterRewards.heartbeatIntervalSeconds
  max_heartbeat_claims = $deployment.adopterRewards.maxHeartbeatClaims
  registration_status = "configured_not_registered"
  updated_at = (Get-Date).ToUniversalTime().ToString("o")
} | ConvertTo-Json

Invoke-RestMethod -Method Post -Uri "$AgentUrl/agent/rewards/config" -ContentType "application/json" -Body $payload | ConvertTo-Json -Depth 8
Write-Host "Desktop reward config applied from $DeploymentFile"
