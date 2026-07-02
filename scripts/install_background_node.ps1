$ErrorActionPreference = "Stop"

param(
  [string]$InstallDir = "",
  [string]$RelayUrls = "",
  [switch]$NoLaunchAtLogin,
  [switch]$NoDesktopShortcut,
  [switch]$NoStart
)

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

if ($InstallDir -eq "") {
  $InstallDir = Join-Path $env:LOCALAPPDATA "SynthosCollective\BackgroundNode"
}
if ($RelayUrls -eq "") {
  $RelayUrls = if ($env:SYNTHOS_RELAY_URLS) { $env:SYNTHOS_RELAY_URLS } else { "http://127.0.0.1:8090" }
}

$exeName = "synthos-silent-node.exe"
$builtExe = Join-Path $repo $exeName
$installedExe = Join-Path $InstallDir $exeName
$envFile = Join-Path $InstallDir "node.env.ps1"
$startScript = Join-Path $InstallDir "Start Synthos Node.ps1"
$stopScript = Join-Path $InstallDir "Stop Synthos Node.ps1"
$statusScript = Join-Path $InstallDir "Synthos Node Status.ps1"
$statusPath = Join-Path $env:APPDATA "SynthosCollective\silent-node-status.json"

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $statusPath) | Out-Null

Write-Host "Building SYNTHOS background node..."
go build -o $builtExe ./cmd/silentnode
Copy-Item -Path $builtExe -Destination $installedExe -Force

@"
`$env:SYNTHOS_RELAY_URLS = "$RelayUrls"
`$env:SYNTHOS_SILENT_STATUS_PATH = "$statusPath"
"@ | Set-Content -Path $envFile -Encoding UTF8

@"
`$ErrorActionPreference = "Stop"
. "$envFile"
`$exe = "$installedExe"
`$running = Get-CimInstance Win32_Process | Where-Object { `$_.ExecutablePath -eq `$exe }
if (`$running) {
  Write-Host "SYNTHOS background node is already running."
  exit 0
}
Start-Process -FilePath `$exe -WorkingDirectory "$InstallDir" -WindowStyle Hidden
Write-Host "SYNTHOS background node started."
Write-Host "Status file: $statusPath"
"@ | Set-Content -Path $startScript -Encoding UTF8

@"
`$ErrorActionPreference = "Stop"
`$exe = "$installedExe"
`$procs = Get-CimInstance Win32_Process | Where-Object { `$_.ExecutablePath -eq `$exe }
if (!`$procs) {
  Write-Host "SYNTHOS background node is not running."
  exit 0
}
foreach (`$proc in `$procs) {
  Stop-Process -Id `$proc.ProcessId -Force
}
Write-Host "SYNTHOS background node stopped."
"@ | Set-Content -Path $stopScript -Encoding UTF8

@"
`$ErrorActionPreference = "Stop"
. "$envFile"
`$exe = "$installedExe"
`$running = Get-CimInstance Win32_Process | Where-Object { `$_.ExecutablePath -eq `$exe }
Write-Host "SYNTHOS Background Node"
Write-Host "Install dir: $InstallDir"
Write-Host "Running: " (`$running -ne `$null)
Write-Host "Relay URLs: " `$env:SYNTHOS_RELAY_URLS
Write-Host "Status file: " `$env:SYNTHOS_SILENT_STATUS_PATH
if (Test-Path `$env:SYNTHOS_SILENT_STATUS_PATH) {
  Get-Content -Raw `$env:SYNTHOS_SILENT_STATUS_PATH
} else {
  Write-Host "No status file yet. Start the node and wait for the first heartbeat."
}
"@ | Set-Content -Path $statusScript -Encoding UTF8

function New-Shortcut {
  param(
    [string]$Path,
    [string]$Target,
    [string]$Arguments,
    [string]$WorkingDirectory,
    [string]$Description
  )
  $shell = New-Object -ComObject WScript.Shell
  $shortcut = $shell.CreateShortcut($Path)
  $shortcut.TargetPath = $Target
  $shortcut.Arguments = $Arguments
  $shortcut.WorkingDirectory = $WorkingDirectory
  $shortcut.Description = $Description
  $shortcut.Save()
}

$powershell = (Get-Command powershell.exe).Source
$shortcutArgs = "-NoProfile -ExecutionPolicy Bypass -File `"$startScript`""

$programsDir = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Synthos Collective"
New-Item -ItemType Directory -Force -Path $programsDir | Out-Null
New-Shortcut -Path (Join-Path $programsDir "Start Synthos Node.lnk") -Target $powershell -Arguments $shortcutArgs -WorkingDirectory $InstallDir -Description "Start the SYNTHOS background node"
New-Shortcut -Path (Join-Path $programsDir "Stop Synthos Node.lnk") -Target $powershell -Arguments "-NoProfile -ExecutionPolicy Bypass -File `"$stopScript`"" -WorkingDirectory $InstallDir -Description "Stop the SYNTHOS background node"
New-Shortcut -Path (Join-Path $programsDir "Synthos Node Status.lnk") -Target $powershell -Arguments "-NoProfile -ExecutionPolicy Bypass -NoExit -File `"$statusScript`"" -WorkingDirectory $InstallDir -Description "Show SYNTHOS background node status"

if (!$NoDesktopShortcut) {
  $desktop = [Environment]::GetFolderPath("Desktop")
  New-Shortcut -Path (Join-Path $desktop "Start Synthos Node.lnk") -Target $powershell -Arguments $shortcutArgs -WorkingDirectory $InstallDir -Description "Start the SYNTHOS background node"
}

if (!$NoLaunchAtLogin) {
  $startup = [Environment]::GetFolderPath("Startup")
  New-Shortcut -Path (Join-Path $startup "Synthos Background Node.lnk") -Target $powershell -Arguments $shortcutArgs -WorkingDirectory $InstallDir -Description "Launch SYNTHOS background node at login"
}

if (!$NoStart) {
  & $startScript
}

Write-Host ""
Write-Host "SYNTHOS background node installed."
Write-Host "Install dir: $InstallDir"
Write-Host "Desktop shortcut: " (!$NoDesktopShortcut)
Write-Host "Launch at login: " (!$NoLaunchAtLogin)
Write-Host "Run status: powershell -NoProfile -ExecutionPolicy Bypass -File `"$statusScript`""
