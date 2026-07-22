# SYNTHOS background node installer
# Downloads the prebuilt node and registers it to run in the background at login.
$ErrorActionPreference = "Stop"
$base = "https://synthos-www.onrender.com"
$dir = Join-Path $env:LOCALAPPDATA "SynthosNode"
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$exe = Join-Path $dir "silentnode.exe"
Write-Host "Downloading your SYNTHOS node..."
Invoke-WebRequest -Uri "$base/downloads/silentnode.exe" -OutFile $exe
Write-Host "Setting it to run quietly in the background at login..."
$action = New-ScheduledTaskAction -Execute $exe
$trigger = New-ScheduledTaskTrigger -AtLogOn
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName "SynthosNode" -Action $action -Trigger $trigger -Settings $settings -Force | Out-Null
Start-ScheduledTask -TaskName "SynthosNode"
Write-Host ""
Write-Host "Done! Your SYNTHOS node is now running in the background"
Write-Host "and will start automatically every time you log in."
Write-Host ""
Write-Host "To stop and remove it later, run:"
Write-Host "  Stop-ScheduledTask -TaskName SynthosNode; Unregister-ScheduledTask -TaskName SynthosNode -Confirm:`$false"
