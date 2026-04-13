<#
.SYNOPSIS
    Flood the SYNTHOS mempool with transactions.
    Sends 200 TXs of 100 SYNTHOS each = 20,000 SYNTHOS total
    via the gossip tx-batch endpoint (single HTTP request).
#>

$targetNode = "https://synthos-validator-11.jamesishamwilliams.workers.dev"
$allNodes = @(
    "https://synthos-validator-11.jamesishamwilliams.workers.dev",
    "https://synthos-validator-12.jamesishamwilliams.workers.dev",
    "https://synthos-validator-13.jamesishamwilliams.workers.dev",
    "https://synthos-validator-14.jamesishamwilliams.workers.dev",
    "https://synthos-validator-15.jamesishamwilliams.workers.dev"
)

$fromAddr = "agent-0"
$toAddr   = "0x4ab2003c0391f25013f15539c9123e770d3c5a67"
$txCount  = 200
$amountPerTx = 100
$totalTokens = $txCount * $amountPerTx

Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "  SYNTHOS MEMPOOL FLOOD" -ForegroundColor Cyan
Write-Host "  $txCount TXs x $amountPerTx tokens = $totalTokens SYNTHOS" -ForegroundColor Cyan
Write-Host "========================================`n" -ForegroundColor Cyan

# ── Get current nonce ──
Write-Host "  Fetching current nonce for $fromAddr..." -ForegroundColor DarkGray
$acct = Invoke-RestMethod -Uri "$targetNode/account?address=$fromAddr" -TimeoutSec 10
$startNonce = [int]$acct.nonce
$balance = $acct.balance
Write-Host "  Current nonce: $startNonce  Balance: $balance" -ForegroundColor Green

# ── Pre-check balance ──
if ($balance -lt $totalTokens) {
    Write-Host "  ERROR: Insufficient balance ($balance < $totalTokens)" -ForegroundColor Red
    exit 1
}

# ── Build TX array with pre-computed IDs ──
Write-Host "  Building $txCount transactions..." -ForegroundColor DarkGray
$sha256 = [System.Security.Cryptography.SHA256]::Create()
$transactions = @()

for ($i = 0; $i -lt $txCount; $i++) {
    $nonce = $startNonce + $i
    $fee = 0

    # Compute TX ID matching the worker's computeTxId function:
    # SHA-256 of JSON.stringify({from, to, amount, fee, nonce})
    $idPayload = "{`"from`":`"$fromAddr`",`"to`":`"$toAddr`",`"amount`":$amountPerTx,`"fee`":$fee,`"nonce`":$nonce}"
    $idBytes = [System.Text.Encoding]::UTF8.GetBytes($idPayload)
    $hashBytes = $sha256.ComputeHash($idBytes)
    $txId = "0x" + ($hashBytes[0..15] | ForEach-Object { $_.ToString("x2") }) -replace " ", ""

    $transactions += @{
        id     = $txId
        from   = $fromAddr
        to     = $toAddr
        amount = $amountPerTx
        fee    = $fee
        nonce  = $nonce
    }
}

Write-Host "  Built $($transactions.Count) TXs (nonce $startNonce to $($startNonce + $txCount - 1))" -ForegroundColor Green

# ── Get pre-flood status ──
Write-Host "`n  Pre-flood chain state:" -ForegroundColor Cyan
foreach ($node in $allNodes) {
    $name = ($node -split "//")[1] -split "\." | Select-Object -First 1
    try {
        $s = Invoke-RestMethod -Uri "$node/status" -TimeoutSec 10
        Write-Host "    $name  height=$($s.height)  mempool=$($s.mempool_size)  root=$($s.state_root)"
    } catch {
        Write-Host "    $name  ERROR" -ForegroundColor Red
    }
}

# ── Send the batch ──
Write-Host "`n  Sending $txCount TXs as single batch to $($targetNode.Split('//')[1].Split('.')[0])..." -ForegroundColor Yellow
$batchPayload = @{ transactions = $transactions } | ConvertTo-Json -Depth 5

$t0 = Get-Date
try {
    $resp = Invoke-RestMethod -Uri "$targetNode/gossip/tx-batch" -Method Post -Body $batchPayload -ContentType "application/json" -TimeoutSec 30
    $elapsed = [math]::Round(((Get-Date) - $t0).TotalMilliseconds, 0)
    Write-Host "  SENT! added=$($resp.added)  mempool_size=$($resp.mempool_size)  (${elapsed}ms)" -ForegroundColor Green
} catch {
    Write-Host "  SEND FAILED: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}

# ── Immediate mempool check ──
Write-Host "`n  Mempool after flood:" -ForegroundColor Cyan
foreach ($node in $allNodes) {
    $name = ($node -split "//")[1] -split "\." | Select-Object -First 1
    try {
        $s = Invoke-RestMethod -Uri "$node/status" -TimeoutSec 10
        Write-Host "    $name  mempool=$($s.mempool_size)  height=$($s.height)" -ForegroundColor $(if ($s.mempool_size -gt 0) { "Yellow" } else { "DarkGray" })
    } catch {
        Write-Host "    $name  ERROR" -ForegroundColor Red
    }
}

# ── Wait for gossip + block proposals to drain mempool ──
Write-Host "`n  Waiting for blocks to process TXs..." -ForegroundColor DarkGray
$rounds = 5
for ($r = 1; $r -le $rounds; $r++) {
    Start-Sleep -Seconds 8
    $nodeStatus = Invoke-RestMethod -Uri "$targetNode/status" -TimeoutSec 10
    Write-Host "  [${r}/${rounds}] height=$($nodeStatus.height)  mempool=$($nodeStatus.mempool_size)" -ForegroundColor DarkGray
    if ($nodeStatus.mempool_size -eq 0) {
        Write-Host "  Mempool drained!" -ForegroundColor Green
        break
    }
}

# ── Wait for gossip convergence ──
Write-Host "`n  Waiting 12s for gossip convergence..." -ForegroundColor DarkGray
Start-Sleep -Seconds 12

# ── Final state ──
Write-Host "`n  Final chain state:" -ForegroundColor Cyan
$heights = @()
$roots = @()
foreach ($node in $allNodes) {
    $name = ($node -split "//")[1] -split "\." | Select-Object -First 1
    try {
        $s = Invoke-RestMethod -Uri "$node/status" -TimeoutSec 10
        $heights += $s.height
        $roots += $s.state_root
        Write-Host "    $name  height=$($s.height)  mempool=$($s.mempool_size)  root=$($s.state_root)"
    } catch {
        Write-Host "    $name  ERROR" -ForegroundColor Red
    }
}

# ── Recipient balance ──
Write-Host ""
try {
    $recipientAcct = Invoke-RestMethod -Uri "$targetNode/account?address=$toAddr" -TimeoutSec 10
    Write-Host "  Recipient ($toAddr):" -ForegroundColor Cyan
    Write-Host "    balance = $($recipientAcct.balance) SYNTHOS" -ForegroundColor Green
} catch {}
try {
    $senderAcct = Invoke-RestMethod -Uri "$targetNode/account?address=$fromAddr" -TimeoutSec 10
    Write-Host "  Sender ($fromAddr):" -ForegroundColor Cyan
    Write-Host "    balance = $($senderAcct.balance) SYNTHOS" -ForegroundColor Green
} catch {}

# ── Consensus ──
Write-Host ""
$uniqueHeights = $heights | Sort-Object -Unique
$uniqueRoots = $roots | Sort-Object -Unique
if ($uniqueHeights.Count -eq 1 -and $uniqueRoots.Count -eq 1) {
    Write-Host "  CONSENSUS: ALL NODES IN SYNC" -ForegroundColor Green
    Write-Host "    height=$($uniqueHeights[0])  state_root=$($uniqueRoots[0])" -ForegroundColor Green
} else {
    Write-Host "  CONSENSUS: NODES STILL CONVERGING" -ForegroundColor Yellow
    Write-Host "    heights: $($uniqueHeights -join ', ')  roots: $($uniqueRoots -join ', ')" -ForegroundColor Yellow
}
Write-Host "========================================`n" -ForegroundColor Cyan
