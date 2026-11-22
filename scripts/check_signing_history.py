#!/usr/bin/env python3
"""
SYNTHOS Collective — Reusable Validator Signing History & Consensus Audit Tool
=============================================================================
Strictly read-only inspection tool querying CometBFT RPC endpoints.
No configuration modification, no key access, no node mutation.

Features:
- Audits validator signing performance over arbitrary height ranges (--from/--to) or latest N blocks (--blocks).
- Detects absent validators (flag 1), signed commits (flag 2), and nil votes (flag 3).
- Flags potential consensus risks (blocks with >= 2 missing validators, rounds > 0).
- Pinpoints specific blocks (--target-block) with forensic signature breakdowns.
- Analyzes round-1 escalations: round-0 proposers vs. actual proposers and nil votes.
- Supports saving baseline metrics (--save-baseline) and comparing post-switch runs (--compare).
- Supports JSON output mode (--json) for automation.
"""

import argparse
import json
import sys
import time
import urllib.request
import urllib.error
from concurrent.futures import ThreadPoolExecutor, as_completed

DEFAULT_RPC = "http://127.0.0.1:26657"

KNOWN_VALIDATORS = {
    "2EFA14D4903A1644185FA63E9E5F1B1872F664F6": {
        "name": "Salt Lake City",
        "moniker": "hosteons-validator-1",
        "role": "Remote VPS (Linux)"
    },
    "6ADC0C616AD18CAE49713A06C253DF88227CBC11": {
        "name": "Namecheap",
        "moniker": "namecheap-validator-1",
        "role": "Remote VPS (Linux)"
    },
    "80756DB8674D0D8F08843626B18051236979E7E8": {
        "name": "Jim All-in-One PC",
        "moniker": "jim-pc-validator-1",
        "role": "Local Home PC (Windows)"
    },
    "8872611711A0542169FABC1E44574A4F6EA62BF9": {
        "name": "New York",
        "moniker": "hosteons-validator-ny-1",
        "role": "Remote VPS (Linux)"
    },
    "FADB813BD6A969C0BCF70E9FFA75957C6CDE63AE": {
        "name": "HP Tower",
        "moniker": "jim-tower-validator-4",
        "role": "Home Tower PC (Windows)"
    }
}

FLAG_NAMES = {
    1: "Absent (Flag 1)",
    2: "Commit (Flag 2)",
    3: "Nil (Flag 3)"
}

class CometClient:
    def __init__(self, rpc_url):
        self.rpc_url = rpc_url.rstrip("/")

    def get(self, endpoint, timeout=6):
        url = f"{self.rpc_url}/{endpoint}"
        req = urllib.request.Request(url, headers={"User-Agent": "Synthos-SigningHistoryAudit/1.0"})
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            if "error" in data and data["error"]:
                raise RuntimeError(f"CometBFT RPC error: {data['error']}")
            return data.get("result", {})

    def fetch_status(self):
        return self.get("status")

    def fetch_commit(self, height):
        try:
            res = self.get(f"commit?height={height}")
            signed_header = res.get("signed_header", {})
            header = signed_header.get("header", {})
            commit = signed_header.get("commit", {})
            return {
                "height": int(height),
                "time": header.get("time", ""),
                "proposer": header.get("proposer_address", ""),
                "round": commit.get("round", 0),
                "signatures": commit.get("signatures", [])
            }
        except Exception as e:
            return {"height": int(height), "error": str(e)}

    def fetch_validators(self, height=None):
        endpoint = f"validators?height={height}" if height else "validators"
        try:
            res = self.get(endpoint)
            return res.get("validators", [])
        except Exception:
            return []

def resolve_val(addr):
    if addr in KNOWN_VALIDATORS:
        return KNOWN_VALIDATORS[addr]["name"], KNOWN_VALIDATORS[addr]["moniker"]
    return addr[:12] if addr else "Unknown", "unknown"

def run_target_audit(client, target_height, val_map, quiet=False):
    if not quiet:
        print(f"\n[TARGET AUDIT: BLOCK {target_height:,}]")
    commit = client.fetch_commit(target_height)
    if "error" in commit:
        if not quiet:
            print(f"  Error fetching block {target_height:,}: {commit['error']}")
        return None

    prop_addr = commit["proposer"]
    prop_name, prop_mon = resolve_val(prop_addr)
    if not quiet:
        print(f"  Block Timestamp:   {commit['time']}")
        print(f"  Consensus Round:   {commit['round']}")
        print(f"  Block Proposer:    {prop_name} ({prop_addr})")
        print(f"  Validator Signatures ({len(commit['signatures'])} slots):")

    missing = []
    for idx, sig in enumerate(commit["signatures"]):
        v_info = val_map.get(idx, {})
        v_name = v_info.get("name", f"Validator #{idx}")
        v_mon = v_info.get("moniker", "unknown")
        flag = sig.get("block_id_flag", 0)
        
        status_desc = FLAG_NAMES.get(flag, f"Flag {flag}")
        if flag == 2:
            if not quiet:
                print(f"    [{idx}] {v_name:<18} ({v_mon:<26}) : SIGNED [{status_desc}]")
        elif flag == 1:
            if not quiet:
                print(f"    [{idx}] {v_name:<18} ({v_mon:<26}) : MISSED [{status_desc}]")
            missing.append((idx, v_name, v_mon, "Absent"))
        elif flag == 3:
            if not quiet:
                print(f"    [{idx}] {v_name:<18} ({v_mon:<26}) : VOTED NIL [{status_desc}]")
            missing.append((idx, v_name, v_mon, "Nil"))
        else:
            if not quiet:
                print(f"    [{idx}] {v_name:<18} ({v_mon:<26}) : OTHER [{status_desc}]")
            missing.append((idx, v_name, v_mon, f"Flag {flag}"))

    if not quiet:
        if not missing:
            print(f"\n  Result: All {len(commit['signatures'])} validators signed block {target_height:,}.")
        else:
            for idx, m_name, m_mon, m_stat in missing:
                print(f"\n  Result: Missing validator in block {target_height:,}: {m_name} ({m_mon})")
                print(f"          Slot Index: {idx} | Observed Status: {m_stat}")

    return commit

def audit_range(client, start_height, end_height, val_map, workers=20, do_round_analysis=True, quiet=False):
    total_blocks = end_height - start_height + 1
    if not quiet:
        print(f"\n[SCANNING BLOCKS {start_height:,} - {end_height:,} ({total_blocks:,} total blocks)]")
    start_t = time.time()

    commits = {}
    with ThreadPoolExecutor(max_workers=workers) as executor:
        futures = {executor.submit(client.fetch_commit, h): h for h in range(start_height, end_height + 1)}
        for f in as_completed(futures):
            res = f.result()
            commits[res["height"]] = res

    duration = time.time() - start_t
    rate = len(commits) / duration if duration > 0 else 0
    if not quiet:
        print(f"  Fetched {len(commits):,} commits in {duration:.2f}s ({rate:.0f} req/s).\n")

    val_indices = sorted(val_map.keys())
    signed_counts = {idx: 0 for idx in val_indices}
    absent_counts = {idx: 0 for idx in val_indices}
    nil_counts = {idx: 0 for idx in val_indices}
    streak_cur = {idx: 0 for idx in val_indices}
    streak_max = {idx: 0 for idx in val_indices}

    rounds_gt_zero = []
    blocks_missing_ge_2 = []
    blocks_missing_1 = []

    for h in range(start_height, end_height + 1):
        c = commits.get(h)
        if not c or "error" in c:
            continue

        sigs = c.get("signatures", [])
        if c.get("round", 0) > 0:
            rounds_gt_zero.append(c)

        missing_this_block = []
        for idx in val_indices:
            fl = sigs[idx].get("block_id_flag", 0) if idx < len(sigs) else 0
            if fl == 2:
                signed_counts[idx] += 1
                streak_cur[idx] = 0
            elif fl == 3:
                nil_counts[idx] += 1
                streak_cur[idx] += 1
                if streak_cur[idx] > streak_max[idx]:
                    streak_max[idx] = streak_cur[idx]
                missing_this_block.append((idx, "Nil"))
            else:
                absent_counts[idx] += 1
                streak_cur[idx] += 1
                if streak_cur[idx] > streak_max[idx]:
                    streak_max[idx] = streak_cur[idx]
                missing_this_block.append((idx, "Absent"))

        if len(missing_this_block) >= 2:
            blocks_missing_ge_2.append((h, c.get("time", ""), missing_this_block))
        elif len(missing_this_block) == 1:
            blocks_missing_1.append((h, missing_this_block[0]))

    if not quiet:
        print(f"  {'Validator':<20} | {'Moniker':<26} | {'Signed':<7} | {'Absent':<7} | {'Nil':<5} | {'Miss Rate':<10} | {'Max Streak':<10} | {'Status'}")
        print("  " + "-" * 106)

    validator_stats = {}
    for idx in val_indices:
        v = val_map[idx]
        s = signed_counts[idx]
        a = absent_counts[idx]
        n = nil_counts[idx]
        tot = s + a + n
        miss_rate = ((a + n) / tot * 100.0) if tot > 0 else 0.0

        if miss_rate < 1.0:
            eval_str = "NORMAL (< 1%)"
        elif miss_rate < 5.0:
            eval_str = "ELEVATED (1-5%)"
        else:
            eval_str = "HIGH (>= 5%)"

        if not quiet:
            print(f"  {v['name']:<20} | {v['moniker']:<26} | {s:<7} | {a:<7} | {n:<5} | {miss_rate:>7.2f}%   | {streak_max[idx]:<10} | {eval_str}")

        validator_stats[v["name"]] = {
            "address": v["address"],
            "moniker": v["moniker"],
            "signed": s,
            "absent": a,
            "nil": n,
            "miss_rate_pct": round(miss_rate, 3),
            "max_streak": streak_max[idx]
        }

    # Consensus Health
    if not quiet:
        print(f"\n[CONSENSUS SAFETY EVALUATION]")
        print(f"  1. Blocks Requiring Round > 0: {len(rounds_gt_zero)}")
        print(f"  2. Blocks with >= 2 Missing Validators: {len(blocks_missing_ge_2)}")
        if blocks_missing_ge_2:
            print("     WARNING: The following blocks were signed with degraded quorum:")
            for bh, bt, bmiss in blocks_missing_ge_2:
                m_names = [f"{val_map[m[0]]['name']} ({m[1]})" for m in bmiss]
                print(f"     - Block {bh:,} ({bt}): Missing [{', '.join(m_names)}]")
        else:
            print("     PERFECT. Zero blocks had >= 2 missing validators. Consensus remained >= 80% voting power.")

        print(f"  3. Blocks with Exactly 1 Missing Validator: {len(blocks_missing_1)} ({(len(blocks_missing_1)/total_blocks*100):.2f}%)")

    # Round 1 Proposer Analysis (requested for post-switch audit)
    round_details = []
    slc_nil_matches = 0
    if do_round_analysis and rounds_gt_zero:
        if not quiet:
            print(f"\n[ROUND-1 ESCALATION & PROPOSER BREAKDOWN ({len(rounds_gt_zero)} blocks)]")
            print(f"  Height | Time (UTC)    | Round | Round-0 Expected Proposer | Committed Proposer  | SLC Vote  | All Flags")
            print("  " + "-" * 102)

        for rc in rounds_gt_zero:
            h = rc["height"]
            t_str = rc["time"][11:19] if len(rc["time"]) >= 19 else rc["time"]
            rnd = rc["round"]
            committing_prop = rc["proposer"]
            c_prop_name, _ = resolve_val(committing_prop)

            sigs = rc.get("signatures", [])
            slc_flag = sigs[0].get("block_id_flag", 0) if sigs else 0
            slc_vote_desc = "Nil (Flag 3)" if slc_flag == 3 else ("Commit (Flag 2)" if slc_flag == 2 else f"Flag {slc_flag}")
            if slc_flag == 3:
                slc_nil_matches += 1

            vset = client.fetch_validators(height=h)
            if vset:
                sorted_prio = sorted(vset, key=lambda x: int(x.get("proposer_priority", 0)), reverse=True)
                r0_addr = sorted_prio[0].get("address", "")
                r0_name, _ = resolve_val(r0_addr)
            else:
                r0_name = "Unknown"

            flag_summary = " ".join(f"[{s.get('block_id_flag',0)}]" for s in sigs)
            if not quiet:
                print(f"  {h:<6} | {t_str:<13} | {rnd:^5} | {r0_name:<25} | {c_prop_name:<19} | {slc_vote_desc:<9} | {flag_summary}")

            round_details.append({
                "height": h,
                "time": rc["time"],
                "round": rnd,
                "round0_expected_proposer": r0_name,
                "committed_proposer": c_prop_name,
                "slc_vote": slc_vote_desc,
                "flags": [s.get("block_id_flag", 0) for s in sigs]
            })

        if not quiet:
            print(f"\n  SLC Nil Correlation: {slc_nil_matches} of {len(rounds_gt_zero)} round-escalated blocks match SLC Nil votes.")

    results = {
        "range": {"start": start_height, "end": end_height, "total": total_blocks},
        "validators": validator_stats,
        "rounds_gt_zero_count": len(rounds_gt_zero),
        "blocks_missing_ge_2_count": len(blocks_missing_ge_2),
        "blocks_missing_1_count": len(blocks_missing_1),
        "slc_nil_matches": slc_nil_matches,
        "round_details": round_details
    }
    return results

def main():
    parser = argparse.ArgumentParser(
        description="SYNTHOS Collective — Reusable Validator Signing History & Consensus Audit Tool (Read-Only)"
    )
    parser.add_argument("--rpc", default=DEFAULT_RPC, help=f"CometBFT RPC endpoint (default: {DEFAULT_RPC})")
    parser.add_argument("--from", dest="from_height", type=int, default=None, help="Start block height")
    parser.add_argument("--to", dest="to_height", type=int, default=None, help="End block height")
    parser.add_argument("--blocks", type=int, default=1000, help="Number of recent blocks to inspect (default: 1000)")
    parser.add_argument("--target-block", type=int, default=None, help="Inspect a specific block height in detail (e.g. 363003)")
    parser.add_argument("--save-baseline", type=str, default=None, help="Save audit metrics to JSON baseline file")
    parser.add_argument("--compare", type=str, default=None, help="Compare current audit metrics against saved JSON baseline")
    parser.add_argument("--no-rounds", action="store_true", help="Skip detailed round escalation analysis")
    parser.add_argument("--json", action="store_true", help="Output summary in JSON format")

    args = parser.parse_args()

    client = CometClient(args.rpc)

    if not args.json:
        print("=" * 84)
        print(" SYNTHOS COLLECTIVE — VALIDATOR SIGNING AUDITOR")
        print("=" * 84)

    try:
        status = client.fetch_status()
    except Exception as e:
        print(f"ERROR: Cannot connect to CometBFT RPC at {args.rpc}: {e}", file=sys.stderr)
        sys.exit(1)

    sync_info = status.get("sync_info", {})
    node_info = status.get("node_info", {})
    latest_height = int(sync_info.get("latest_block_height", 0))

    if not args.json:
        print(f"Connected RPC:        {args.rpc}")
        print(f"Node Moniker:         {node_info.get('moniker', 'unknown')}")
        print(f"Chain ID:             {node_info.get('network', 'unknown')}")
        print(f"Current Tip Height:   {latest_height:,}")
        print(f"Catching Up:          {sync_info.get('catching_up', False)}")
        print("-" * 84)

    # Build active validator map
    vlist = client.fetch_validators()
    val_map = {}
    for idx, v in enumerate(vlist):
        addr = v.get("address", "")
        name, mon = resolve_val(addr)
        val_map[idx] = {
            "index": idx,
            "address": addr,
            "name": name,
            "moniker": mon,
            "voting_power": int(v.get("voting_power", 0))
        }

    # Target block audit if requested
    if args.target_block:
        run_target_audit(client, args.target_block, val_map, quiet=args.json)

    # Determine range
    if args.from_height and args.to_height:
        start_h = max(1, args.from_height)
        end_h = min(latest_height, args.to_height)
    elif args.from_height:
        start_h = max(1, args.from_height)
        end_h = min(latest_height, start_h + args.blocks - 1)
    else:
        end_h = latest_height
        start_h = max(1, end_h - args.blocks + 1)

    results = audit_range(
        client,
        start_h,
        end_h,
        val_map,
        workers=20,
        do_round_analysis=(not args.no_rounds),
        quiet=args.json
    )

    if args.save_baseline:
        try:
            with open(args.save_baseline, "w", encoding="utf-8") as f:
                json.dump(results, f, indent=2)
            if not args.json:
                print(f"\n[+] Baseline saved successfully to {args.save_baseline}")
        except Exception as e:
            print(f"Error saving baseline to {args.save_baseline}: {e}", file=sys.stderr)

    if args.compare:
        if not args.json:
            print(f"\n[BASELINE COMPARISON: Current vs. {args.compare}]")
        try:
            with open(args.compare, "r", encoding="utf-8") as f:
                base = json.load(f)
            b_vals = base.get("validators", {})
            c_vals = results.get("validators", {})
            if not args.json:
                print(f"  {'Validator':<20} | {'Baseline Miss %':<16} | {'Current Miss %':<16} | {'Delta':<10} | {'Verdict'}")
                print("  " + "-" * 78)
            for vname in c_vals:
                c_rate = c_vals[vname].get("miss_rate_pct", 0.0)
                b_rate = b_vals.get(vname, {}).get("miss_rate_pct", 0.0) if vname in b_vals else 0.0
                delta = c_rate - b_rate
                delta_str = f"{delta:+.3f}%"
                if delta <= 0:
                    verdict = "IMPROVED / STABLE"
                elif delta < 1.0:
                    verdict = "ACCEPTABLE (< +1%)"
                else:
                    verdict = "DEGRADED - INVESTIGATE"
                if not args.json:
                    print(f"  {vname:<20} | {b_rate:>14.3f}% | {c_rate:>14.3f}% | {delta_str:>9} | {verdict}")
        except Exception as e:
            print(f"Error reading baseline comparison file: {e}", file=sys.stderr)

    if args.json:
        print(json.dumps(results, indent=2))

if __name__ == "__main__":
    main()
