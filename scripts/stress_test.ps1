<#
.SYNOPSIS
    SYNTHOS Validator Node Stress Test (PowerShell)
    Tests all deployed Cloudflare Workers validators.
#>

$nodes = @(
    "https://synthos-validator-11.jamesishamwilliams.workers.dev",
    "https://synthos-validator-12.jamesishamwilliams.workers.dev",
    "https://synthos-validator-13.jamesishamwilliams.workers.dev",
    "https://synthos-validator-14.jamesishamwilliams.workers.dev",
    "https://synthos-validator-15.jamesishamwilliams.workers.dev"
)

$duration = 30  # seconds
$fromAddr = "agent-0"
$toAddr = "0x4ab2003c0391f25013f15539c9123e770d3c5a67"

# ── Health Check ──
Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "  SYNTHOS NODE HEALTH CHECK" -ForegroundColor Cyan
Write-Host "========================================`n" -ForegroundColor Cyan

$healthyNodes = @()
foreach ($node in $nodes) {
    $name = ($node -split "//")[1] -split "\." | Select-Object -First 1
    try {
        $t0 = Get-Date
        $health = Invoke-RestMethod -Uri "$node/health" -TimeoutSec 10
        $status = Invoke-RestMethod -Uri "$node/status" -TimeoutSec 10
        $latency = [math]::Round(((Get-Date) - $t0).TotalMilliseconds, 0)
        Write-Host "  OK    $name  height=$($status.height)  chain=$($status.chain_id)  latency=${latency}ms" -ForegroundColor Green
        $healthyNodes += $node
    } catch {
        Write-Host "  FAIL  $name  $($_.Exception.Message)" -ForegroundColor Red
    }
}

Write-Host "`n  $($healthyNodes.Count)/$($nodes.Count) nodes healthy`n"

if ($healthyNodes.Count -eq 0) {
    Write-Host "  No healthy nodes. Aborting." -ForegroundColor Red
    exit 1
}

# ── Stress Test ──
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  STRESS TEST STARTED" -ForegroundColor Cyan
Write-Host "  $($healthyNodes.Count) nodes | ${duration}s duration" -ForegroundColor Cyan
Write-Host "========================================`n" -ForegroundColor Cyan

$stats = @{
    total = 0; ok = 0; fail = 0
    txSubmitted = 0; txAccepted = 0; blocksProposed = 0
    latencies = [System.Collections.ArrayList]::new()
    errors = @{}
}

$startTime = Get-Date
# Global nonce: gossip keeps all nodes in sync, so one nonce for all
$globalNonce = 0
try {
    $acct = Invoke-RestMethod -Uri "$($healthyNodes[0])/account?address=$fromAddr" -TimeoutSec 10
    $globalNonce = [int]$acct.nonce
    Write-Host "  Global nonce: $globalNonce (from $($healthyNodes[0].Split('//')[1].Split('.')[0]))" -ForegroundColor DarkGray
} catch { $globalNonce = 0 }

$nodeIdx = 0
$requestNum = 0

while (((Get-Date) - $startTime).TotalSeconds -lt $duration) {
    $node = $healthyNodes[$nodeIdx % $healthyNodes.Count]
    $nodeIdx++
    $requestNum++

    # Workload: 85% submitTx (to single node, gossip distributes), 15% status (any node)
    $roll = $requestNum % 20

    $t0 = Get-Date
    try {
        if ($roll -lt 17) {
            # Submit TX — always to one node (gossip distributes to the rest)
            $submitNode = $healthyNodes[0]

            # Fetch live nonce (blocks may have been proposed since last TX)
            try {
                $acct = Invoke-RestMethod -Uri "$submitNode/account?address=$fromAddr" -TimeoutSec 10
                $globalNonce = [int]$acct.nonce
            } catch {}

            $tx = @{
                from = $fromAddr
                to = $toAddr
                amount = 1
                fee = 0
                nonce = $globalNonce
            } | ConvertTo-Json

            $resp = Invoke-RestMethod -Uri "$submitNode/submitTx" -Method Post -Body $tx -ContentType "application/json" -TimeoutSec 15
            $stats.txSubmitted++
            if ($resp.ok) { $stats.txAccepted++; $globalNonce++ }
            $stats.ok++
        } else {
            # Status check
            $null = Invoke-RestMethod -Uri "$node/status" -TimeoutSec 10
            $stats.ok++
        }
    } catch {
        $stats.fail++
        $errKey = $_.Exception.Message.Substring(0, [math]::Min($_.Exception.Message.Length, 60))
        if (-not $stats.errors.ContainsKey($errKey)) { $stats.errors[$errKey] = 0 }
        $stats.errors[$errKey]++
    }

    $latency = [math]::Round(((Get-Date) - $t0).TotalMilliseconds, 1)
    [void]$stats.latencies.Add($latency)
    $stats.total++

    # Progress every 50 requests
    if ($stats.total % 50 -eq 0) {
        $elapsed = [math]::Round(((Get-Date) - $startTime).TotalSeconds, 0)
        $avgLat = [math]::Round(($stats.latencies | Measure-Object -Average).Average, 0)
        Write-Host "  [${elapsed}s] requests=$($stats.total)  ok=$($stats.ok)  fail=$($stats.fail)  avg_lat=${avgLat}ms" -ForegroundColor DarkGray
    }
}

$endTime = Get-Date
$totalDuration = [math]::Round(($endTime - $startTime).TotalSeconds, 1)

# ── Latency calc ──
$sorted = $stats.latencies | Sort-Object
$avgLat = [math]::Round(($sorted | Measure-Object -Average).Average, 1)
$p50Idx = [math]::Floor($sorted.Count * 0.5)
$p95Idx = [math]::Floor($sorted.Count * 0.95)
$p99Idx = [math]::Floor($sorted.Count * 0.99)
$p50 = if ($sorted.Count -gt 0) { $sorted[$p50Idx] } else { 0 }
$p95 = if ($sorted.Count -gt 0) { $sorted[[math]::Min($p95Idx, $sorted.Count-1)] } else { 0 }
$p99 = if ($sorted.Count -gt 0) { $sorted[[math]::Min($p99Idx, $sorted.Count-1)] } else { 0 }
$rps = [math]::Round($stats.total / $totalDuration, 1)
$successRate = [math]::Round($stats.ok / [math]::Max($stats.total, 1) * 100, 1)

# ── Report ──
Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "  STRESS TEST REPORT" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Nodes tested:        $($healthyNodes.Count)"
Write-Host "  Duration:            ${totalDuration}s"
Write-Host "  Total requests:      $($stats.total)"
Write-Host "  Successful:          $($stats.ok)" -ForegroundColor Green
Write-Host "  Failed:              $($stats.fail)" -ForegroundColor $(if ($stats.fail -gt 0) { "Red" } else { "Green" })
Write-Host "  Success rate:        ${successRate}%"
Write-Host "  Throughput:          ${rps} req/s"
Write-Host "  TXs submitted:       $($stats.txSubmitted)"
Write-Host "  TXs accepted:        $($stats.txAccepted)"
Write-Host "  Blocks proposed:     $($stats.blocksProposed)"
Write-Host "  ────────────────────────────────────"
Write-Host "  Latency (avg):       ${avgLat}ms"
Write-Host "  Latency (p50):       ${p50}ms"
Write-Host "  Latency (p95):       ${p95}ms"
Write-Host "  Latency (p99):       ${p99}ms"

if ($stats.errors.Count -gt 0) {
    Write-Host "  ────────────────────────────────────"
    Write-Host "  Errors:" -ForegroundColor Yellow
    foreach ($e in $stats.errors.GetEnumerator() | Sort-Object Value -Descending) {
        Write-Host "    [$($e.Value)x] $($e.Key)" -ForegroundColor Yellow
    }
}
Write-Host "========================================`n" -ForegroundColor Cyan

# Wait for gossip to propagate all blocks
Write-Host "  Waiting 12s for gossip convergence..." -ForegroundColor DarkGray
Start-Sleep -Seconds 12

# Final chain state on each node
Write-Host "`n  Final chain state:" -ForegroundColor Cyan
$heights = @()
$roots = @()
foreach ($node in $nodes) {
    $name = ($node -split "//")[1] -split "\." | Select-Object -First 1
    try {
        $s = Invoke-RestMethod -Uri "$node/status" -TimeoutSec 15
        $heights += $s.height
        $roots += $s.state_root
        $proposer = if ($s.next_proposer) { "  next=$($s.next_proposer)" } else { "" }
        Write-Host "    $name  height=$($s.height)  mempool=$($s.mempool_size)  root=$($s.state_root)$proposer"
    } catch {
        Write-Host "    $name  ERROR" -ForegroundColor Red
    }
}

# Consensus check: are all nodes in sync?
Write-Host ""
$uniqueHeights = $heights | Sort-Object -Unique
$uniqueRoots = $roots | Sort-Object -Unique
if ($uniqueHeights.Count -eq 1 -and $uniqueRoots.Count -eq 1) {
    Write-Host "  CONSENSUS: ALL NODES IN SYNC" -ForegroundColor Green
    Write-Host "    height=$($uniqueHeights[0])  state_root=$($uniqueRoots[0])" -ForegroundColor Green
} else {
    Write-Host "  CONSENSUS: NODES DIVERGED" -ForegroundColor Red
    Write-Host "    heights: $($uniqueHeights -join ', ')  roots: $($uniqueRoots -join ', ')" -ForegroundColor Red
}
Write-Host "========================================`n" -ForegroundColor Cyan
