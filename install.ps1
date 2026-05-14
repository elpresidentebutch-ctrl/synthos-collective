# Usage: irm https://your-domain.com/install.ps1 | iex
param($Relay = "http://127.0.0.1:8080")

$ErrorActionPreference = 'Stop'
Write-Host "🚀 Initializing SYNTHOS Sovereign Node..." -ForegroundColor Cyan
Write-Host "🔗 Connecting to Relay: $Relay" -ForegroundColor Gray

$workDir = "$HOME\.synthos"
if (!(Test-Path $workDir)) { New-Item -Path $workDir -ItemType Directory }
Set-Location $workDir

# 1. Download the latest node source (or binary)
Write-Host "📦 Downloading Node Source..." -ForegroundColor Gray
$repoUrl = "https://raw.githubusercontent.com/elpresidentebutch/synthos-collective/main/cmd/node/main.go"
Invoke-WebRequest -Uri $repoUrl -OutFile "main.go"

# 2. Check for Go installation
if (!(Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "⚠️  Go not found. Please install Go from https://go.dev/dl/ to continue." -ForegroundColor Red
    Write-Host "Attempting to continue with pre-compiled binary... (Coming Soon)" -ForegroundColor Yellow
    exit
}

# 3. Build and Initialize
Write-Host "🛠️  Building Agentic Core..." -ForegroundColor Gray
go mod init synthos-node 2>$null
go mod tidy 2>$null
go build -o synthos-node.exe main.go

# 4. Create Desktop Shortcut Icon
Write-Host "🎨 Creating Desktop Icon..." -ForegroundColor Gray
$WshShell = New-Object -ComObject WScript.Shell
$Shortcut = $WshShell.CreateShortcut("$HOME\Desktop\SYNTHOS Node.lnk")
$Shortcut.TargetPath = "$workDir\synthos-node.exe"
$Shortcut.WorkingDirectory = $workDir
$Shortcut.Description = "Launch your SYNTHOS Sovereign Node"
$Shortcut.Save()

# 5. Start the Node
Write-Host "✅ Installation Complete! Icon placed on your Desktop." -ForegroundColor Green
Write-Host "📡 Starting your Sovereign Node. Keep this window open." -ForegroundColor Cyan
Write-Host "--------------------------------------------------------"
$env:SYNTHOS_RELAY = $Relay
.\synthos-node.exe
