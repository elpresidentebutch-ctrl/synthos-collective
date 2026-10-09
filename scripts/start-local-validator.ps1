# SYNTHOS Local Validator Node Launcher
# Node ID: synthos-home-1
# Connects to Render cluster: rpc.ishamwilliamsblockchains.com, validator-12, validator-13

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$RootDir = Split-Path -Parent $ScriptDir
Set-Location $RootDir

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  Starting SYNTHOS Local Block-Producing Validator Node   " -ForegroundColor Yellow
Write-Host "  Node ID: synthos-home-1                                 " -ForegroundColor Cyan
Write-Host "  Port: 8091                                              " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

$env:SYNTHOS_CONFIG = "config/home-validator.json"
$env:SYNTHOS_BLOCK_PRODUCER = "true"
$env:SYNTHOS_BLOCK_INTERVAL_SECONDS = "10"
$env:SYNTHOS_PRODUCE_EMPTY_BLOCKS = "true"
$env:SYNTHOS_PRODUCER_ROTATION = "true"
$env:SYNTHOS_PRODUCER_ROUND_SECONDS = "30"
$env:SYNTHOS_PRODUCER_ROTATION_LOCK_SECONDS = "60"

.\synthosd.exe
