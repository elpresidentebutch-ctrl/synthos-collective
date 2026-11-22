<#
.SYNOPSIS
  Starts an isolated SYNTHOS EVM Devnet on localhost for development and experiments.
  Never touches mainnet, live validators, or chain ID 20260702.

.DESCRIPTION
  Chain ID:       synthos-evm-devnet-1
  EVM Chain ID:   20260799 (Dedicated isolated devnet)
  Default RPC:    tcp://127.0.0.1:26757
  Default P2P:    tcp://127.0.0.1:26756
#>
param(
  [string]$HomeDir = "$PSScriptRoot\..\data-evm-devnet",
  [string]$RPC = "tcp://127.0.0.1:26757",
  [string]$P2P = "tcp://127.0.0.1:26756",
  [string]$BlockTime = "500ms"
)

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  SYNTHOS COLLECTIVE - ISOLATED EVM DEVNET (Phase 1)      " -ForegroundColor Green
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "CometBFT Chain ID:  synthos-evm-devnet-1"
Write-Host "EVM / Tx Chain ID:  20260799 (ISOLATED - Never 20260702)"
Write-Host "Devnet Data Home:   $HomeDir"
Write-Host "RPC Listen:         $RPC"
Write-Host "P2P Listen:         $P2P"
Write-Host "Block Pace:         $BlockTime"
Write-Host "Pre-funded Faucet:  0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266 (5,000,000,000 SYN)"
Write-Host "Faucet Key Note:    Hardhat/Anvil test account #0 (publicly known private key) - LOCAL DEVNET ONLY; NEVER send real SYN/ETH!" -ForegroundColor Yellow
Write-Host "==========================================================" -ForegroundColor Cyan

Push-Location "$RepoRoot\cometapp"
try {
  go run . evm-devnet -home $HomeDir -rpc $RPC -p2p $P2P -block-time $BlockTime
} finally {
  Pop-Location
}
