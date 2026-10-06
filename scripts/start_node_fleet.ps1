param(
  [int]$Count = 4,
  [string]$RelayUrl = "https://synthos-www.onrender.com"
)

$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $env:LOCALAPPDATA "SynthosCollective\BackgroundNode\synthos-silent-node.exe"

if (-not (Test-Path $exe)) {
  $localExe = Join-Path $repo "synthos-silent-node.exe"
  if (-not (Test-Path $localExe)) {
    Write-Host "Building synthos-silent-node.exe..."
    Set-Location $repo
    go build -o synthos-silent-node.exe ./cmd/silentnode
  }
  $exe = Join-Path $repo "synthos-silent-node.exe"
}

$fleetDir = Join-Path $env:LOCALAPPDATA "SynthosCollective\Fleet"
New-Item -ItemType Directory -Force -Path $fleetDir | Out-Null

Write-Host "Starting fleet of $Count additional SYNTHOS silent nodes..."

1..$Count | ForEach-Object {
  $idx = $_
  $nodeDir = Join-Path $fleetDir "node-$idx"
  New-Item -ItemType Directory -Force -Path $nodeDir | Out-Null
  
  $keyPath = Join-Path $nodeDir "silent-node-key.json"
  $statusPath = Join-Path $nodeDir "silent-node-status.json"
  
  # Check if a process is already running for this node
  $running = Get-CimInstance Win32_Process | Where-Object { 
    $_.CommandLine -like "*node-$idx\silent-node-key.json*"
  }
  
  if ($running) {
    Write-Host "Fleet node $idx is already running (PID: $($running.ProcessId))"
  } else {
    $args = "-key `"$keyPath`" -status `"$statusPath`" -relay `"$RelayUrl`""
    Start-Process -FilePath $exe -ArgumentList $args -WorkingDirectory $nodeDir -WindowStyle Hidden
    Write-Host "Fleet node $idx started in background."
  }
}

Start-Sleep -Seconds 3
Write-Host ""
Write-Host "Fleet startup complete. Checking active processes:"
Get-Process synthos-silent-node | Select-Object Id, ProcessName, WorkingSet64
