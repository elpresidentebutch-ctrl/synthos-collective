$ErrorActionPreference = "Stop"

param(
  [string]$InstallDir = "",
  [switch]$KeepData
)

if ($InstallDir -eq "") {
  $InstallDir = Join-Path $env:LOCALAPPDATA "SynthosCollective\BackgroundNode"
}

$exe = Join-Path $InstallDir "synthos-silent-node.exe"
$procs = Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $exe }
foreach ($proc in $procs) {
  Stop-Process -Id $proc.ProcessId -Force
}

$shortcutPaths = @(
  (Join-Path ([Environment]::GetFolderPath("Desktop")) "Start Synthos Node.lnk"),
  (Join-Path ([Environment]::GetFolderPath("Startup")) "Synthos Background Node.lnk"),
  (Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Synthos Collective\Start Synthos Node.lnk"),
  (Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Synthos Collective\Stop Synthos Node.lnk"),
  (Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Synthos Collective\Synthos Node Status.lnk")
)

foreach ($path in $shortcutPaths) {
  if (Test-Path $path) {
    Remove-Item -LiteralPath $path -Force
  }
}

$programsDir = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Synthos Collective"
if ((Test-Path $programsDir) -and -not (Get-ChildItem -Path $programsDir -Force)) {
  Remove-Item -LiteralPath $programsDir -Force
}

if (!$KeepData -and (Test-Path $InstallDir)) {
  Remove-Item -LiteralPath $InstallDir -Recurse -Force
}

Write-Host "SYNTHOS background node uninstalled."
if ($KeepData) {
  Write-Host "Data kept at: $InstallDir"
}
