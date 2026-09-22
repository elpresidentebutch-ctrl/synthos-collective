# SYNTHOS validator service installer
# Delegates to the live backend installer, which installs SynthosNode as an
# Administrator Windows Service and starts real Ed25519 signed heartbeats.
#
# $base used to point at https://synthos-collective.onrender.com, which is
# this repo's own name, not a real deployed hostname -- render.yaml's
# actual service name for the registry/website backend is "synthos-www"
# (https://synthos-www.onrender.com), so every download of this script was
# fetching a URL that has never resolved to anything, silently breaking the
# entire Windows node installer flow.
$ErrorActionPreference = "Stop"
$base = "https://synthos-www.onrender.com"
Invoke-Expression (Invoke-RestMethod "$base/api/node/windows-installer.ps1")
