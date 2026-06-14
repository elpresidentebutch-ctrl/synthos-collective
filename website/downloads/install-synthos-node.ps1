$ErrorActionPreference = "Stop"

Write-Host "SYNTHOS Background Node Installer"
Write-Host "This installer is for source builds. Run it from the SYNTHOS Collective repository root."
Write-Host ""

$script = Join-Path (Get-Location) "scripts\install_background_node.ps1"
if (!(Test-Path $script)) {
  throw "scripts\install_background_node.ps1 was not found. Download or clone the SYNTHOS Collective repository, then run this installer from the repository root."
}

& $script
