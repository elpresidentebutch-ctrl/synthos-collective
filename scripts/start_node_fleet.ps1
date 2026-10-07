param(
  [string]$RelayUrl = "https://synthos-www.onrender.com"
)

$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $repo "synthos-fleet.exe"

if (-not (Test-Path $exe)) {
  Write-Host "Building synthos-fleet.exe..."
  Set-Location $repo
  go build -o synthos-fleet.exe ./cmd/fleet
}

# Copy to LocalAppData for permanence
$destDir = Join-Path $env:LOCALAPPDATA "SynthosCollective\Fleet"
New-Item -ItemType Directory -Force -Path $destDir | Out-Null
$destExe = Join-Path $destDir "synthos-fleet.exe"
Copy-Item $exe $destExe -Force

# Stop any older synthos node processes
Get-Process synthos-fleet, synthos-silent-node -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Milliseconds 800

Write-Host "Starting SYNTHOS Comprehensive Multi-Node Fleet..."
Write-Host "Managing 16 distinct nodes (syn-fleet-1..5, syn-cc59..., desktop-..., synthos-home-1, and all legacy nodes)..."

# Launch detached in background outside any calling job objects
$cmd = "`"$destExe`" -relay `"$RelayUrl`""
$wshell = New-Object -ComObject WScript.Shell
try {
  Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine = $cmd} | Out-Null
} catch {
  $wshell.Run($cmd, 0, $false)
}

Start-Sleep -Seconds 3

# Update Desktop Shortcut
$desktopPath = [System.Environment]::GetFolderPath("Desktop")
$shortcutPath = Join-Path $desktopPath "Start Synthos Fleet.lnk"
$shortcut = $wshell.CreateShortcut($shortcutPath)
$shortcut.TargetPath = "powershell.exe"
$shortcut.Arguments = "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$PSScriptRoot\start_node_fleet.ps1`""
$shortcut.WindowStyle = 7 # Minimized
$shortcut.IconLocation = "$destExe,0"
$shortcut.Description = "Launch SYNTHOS Decentralized Node Fleet"
$shortcut.Save()

# Update Windows Startup Folder
$startupPath = [System.Environment]::GetFolderPath("Startup")
$startupShortcutPath = Join-Path $startupPath "Start Synthos Fleet.lnk"
Copy-Item $shortcutPath $startupShortcutPath -Force

Write-Host "Fleet startup complete. Process status:"
Get-Process synthos-fleet -ErrorAction SilentlyContinue | Select-Object Id, ProcessName, WorkingSet64
