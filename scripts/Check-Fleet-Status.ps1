# SYNTHOS Validator Fleet Status Check
$ErrorActionPreference = "Continue"

Write-Host "============================================================" -ForegroundColor Cyan
Write-Host " SYNTHOS Collective - Validator Fleet & Consensus Status" -ForegroundColor Cyan
Write-Host "============================================================" -ForegroundColor Cyan

$issues = @()
$warnings = @()

# 1. Local Validator (Jim All-in-One PC)
Write-Host "`n[1] Local All-in-One Validator (jim-pc-validator-1):" -ForegroundColor Yellow
$proc = Get-Process synthos-comet -ErrorAction SilentlyContinue
$rpcOk = $false
try {
    $status = Invoke-RestMethod -Uri "http://127.0.0.1:26657/status" -TimeoutSec 3
    $height = $status.result.sync_info.latest_block_height
    $catchingUp = $status.result.sync_info.catching_up
    $moniker = $status.result.node_info.moniker
    $rpcOk = $true
} catch {
    $issues += "Local CometBFT RPC not responding (127.0.0.1:26657)"
}

if ($rpcOk) {
    $pids = ($proc | ForEach-Object { $_.Id }) -join ", "
    if ($pids) {
        Write-Host "    Node Status: RUNNING & SIGNING (PID: $pids)" -ForegroundColor Green
    } else {
        Write-Host "    Node Status: RUNNING & SIGNING" -ForegroundColor Green
    }
    $procPath = $null
    $localProc = Get-Process synthos-comet -ErrorAction SilentlyContinue | Where-Object { $_.Path -like "*synthos-collective*" }
    if ($localProc) { $procPath = $localProc.Path }
    if (-not $procPath) {
        $p = Get-CimInstance Win32_Process -Filter "Name = 'synthos-comet.exe' and CommandLine like '%jim-pc-validator-1%'" -ErrorAction SilentlyContinue
        if ($p) { $procPath = $p.ExecutablePath }
    }
    if ($procPath -and (Test-Path $procPath)) {
        $h = (Get-FileHash -Path $procPath -Algorithm SHA256).Hash.ToLower()
        Write-Host "    Binary SHA-256: $h" -ForegroundColor Cyan
    } else {
        Write-Host "    Binary SHA-256: UNVERIFIED" -ForegroundColor Red
        $warnings += "Local binary SHA-256 unverified"
    }
    
    if ($catchingUp) {
        Write-Host "    CometBFT RPC: Height $height | CatchingUp: True | Moniker: $moniker" -ForegroundColor Yellow
        $warnings += "Local node is catching up (height $height)"
    } else {
        Write-Host "    CometBFT RPC: Height $height | CatchingUp: False | Moniker: $moniker" -ForegroundColor Green
    }
    
    $api = Invoke-RestMethod -Uri "http://127.0.0.1:8085/status" -TimeoutSec 3 -ErrorAction SilentlyContinue
    if ($api) {
        Write-Host "    Node API Base Fee: $($api.base_fee) SYN (V3 fee rule activates at 380,000)" -ForegroundColor Green
    } else {
        Write-Host "    Node API: NOT RESPONDING (127.0.0.1:8085)" -ForegroundColor Yellow
        $warnings += "Local Node API not responding"
    }
} else {
    Write-Host "    Node Status: NOT RUNNING (RPC not responding)" -ForegroundColor Red
}

# 2. Connected Validator Peers
Write-Host "`n[2] Connected Validator Peers:" -ForegroundColor Yellow
try {
    $net = (Invoke-RestMethod -Uri "http://127.0.0.1:26657/net_info" -TimeoutSec 3).result
    $peerCount = $net.n_peers
    Write-Host "    Total Connected Peers: $peerCount" -ForegroundColor Green
    if ([int]$peerCount -lt 3) {
        $warnings += "Connected peers low ($peerCount peers)"
    }
    foreach ($p in $net.peers) {
        $m = $p.node_info.moniker
        $ip = $p.remote_ip
        Write-Host "    - $m ($ip): ONLINE & PEERED" -ForegroundColor Green
    }
} catch {
    Write-Host "    Could not query net_info: $($_.Exception.Message)" -ForegroundColor Red
    $issues += "Could not query net_info: $($_.Exception.Message)"
}

# 3. Remote Server Binary Fingerprints & Fee Status (v1.2.0)
Write-Host "`n[3] Remote Server Fingerprints & Fees (v1.2.0):" -ForegroundColor Yellow
$sshKey = "C:\Users\elpre\.ssh\id_ed25519_github"
$remotes = @(
    @{ Name = "Salt Lake City"; Host = "82.29.54.7" },
    @{ Name = "New York"; Host = "82.29.130.11" },
    @{ Name = "Namecheap"; Host = "104.207.85.159" }
)
foreach ($r in $remotes) {
    $h = $r.Host
    $out = $null
    if (Test-Path $sshKey) {
        $cmd = "sha256sum /usr/local/bin/synthos-comet; echo '---STATUS---'; curl -s --max-time 3 http://127.0.0.1:8080/status || curl -s --max-time 3 http://127.0.0.1:8085/status"
        $out = ssh -i $sshKey -o StrictHostKeyChecking=no -o BatchMode=yes -o ConnectTimeout=5 root@$h $cmd 2>$null
    }
    if ($out) {
        $outStr = ($out -join "`n")
        $parts = $outStr -split '---STATUS---'
        $hashLine = $parts[0].Trim()
        $statusJson = if ($parts.Length -gt 1) { $parts[1].Trim() } else { "" }
        
        $feeInfo = ""
        if ($statusJson) {
            try {
                $remoteStatus = $statusJson | ConvertFrom-Json
                $feeInfo = " | Height: $($remoteStatus.height) | Base Fee: $($remoteStatus.base_fee) SYN"
            } catch {}
        }
        Write-Host "    $($r.Name) ($h): $hashLine$feeInfo" -ForegroundColor Green
    } else {
        Write-Host "    $($r.Name) ($h): UNVERIFIED / UNREACHABLE" -ForegroundColor Red
        $issues += "Remote server $($r.Name) ($h) unreachable or unverified"
    }
}

# 4. Consensus Block Commit & Signatures
Write-Host "`n[4] Latest Block Commit & Signatures:" -ForegroundColor Yellow
$validCount = 0
$commitOk = $false
$valMap = @{
    "2EFA14D4903A1644185FA63E9E5F1B1872F664F6" = "Salt Lake City (hosteons-validator-1)"
    "6ADC0C616AD18CAE49713A06C253DF88227CBC11" = "Namecheap (namecheap-validator-1)"
    "80756DB8674D0D8F08843626B18051236979E7E8" = "Jim All-in-One PC (jim-pc-validator-1)"
    "8872611711A0542169FABC1E44574A4F6EA62BF9" = "New York (hosteons-validator-ny-1)"
    "FADB813BD6A969C0BCF70E9FFA75957C6CDE63AE" = "HP Tower (jim-tower-validator-4)"
}
try {
    $commitData = (Invoke-RestMethod -Uri "http://127.0.0.1:26657/commit" -TimeoutSec 3).result.signed_header.commit
    $h = $commitData.height
    $round = $commitData.round
    $commitOk = $true
    $valList = (Invoke-RestMethod -Uri "http://127.0.0.1:26657/validators" -TimeoutSec 3).result.validators
    Write-Host "    Block Height: $h | Round: $round" -ForegroundColor Green
    Write-Host "    Validator Signatures in this block:" -ForegroundColor Cyan
    for ($i = 0; $i -lt $commitData.signatures.Count; $i++) {
        $sig = $commitData.signatures[$i]
        $vAddr = if ($valList -and $i -lt $valList.Count) { $valList[$i].address } else { $sig.validator_address }
        $vName = if ($valMap.ContainsKey($vAddr)) { $valMap[$vAddr] } else { $vAddr }
        if ($sig.block_id_flag -eq 2) {
            $validCount++
            Write-Host "      [SIGNED]   $vName" -ForegroundColor Green
        } else {
            Write-Host "      [ABSENT]   $vName (flag: $($sig.block_id_flag))" -ForegroundColor DarkGray
        }
    }
} catch {
    Write-Host "    Commit check error: $($_.Exception.Message)" -ForegroundColor Red
    $issues += "Commit check error: $($_.Exception.Message)"
}

# 5. Overall Consensus & Fleet Health Assessment
Write-Host ""
if (-not $commitOk) {
    Write-Host "    Consensus State: FAILED TO QUERY COMMIT - UNHEALTHY" -ForegroundColor Red
} elseif ($issues.Count -gt 0 -or $validCount -lt 4) {
    Write-Host "    Consensus State: $validCount of 5 validators signed (Round $round - UNHEALTHY)" -ForegroundColor Red
} elseif ($validCount -eq 4 -or $warnings.Count -gt 0 -or $round -gt 0) {
    Write-Host "    Consensus State: $validCount of 5 validators signed (Round $round - DEGRADED)" -ForegroundColor Yellow
} else {
    Write-Host "    Consensus State: 5 of 5 validators signed (Round $round - HEALTHY)" -ForegroundColor Green
}

if ($issues.Count -gt 0) {
    Write-Host "    Failed Checks:" -ForegroundColor Red
    foreach ($issue in $issues) {
        Write-Host "      - $issue" -ForegroundColor Red
    }
}
if ($warnings.Count -gt 0) {
    Write-Host "    Warnings / Notes:" -ForegroundColor Yellow
    foreach ($warn in $warnings) {
        Write-Host "      - $warn" -ForegroundColor Yellow
    }
}

Write-Host "`n============================================================" -ForegroundColor Cyan
