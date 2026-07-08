#!/usr/bin/env python3
"""Run a batch of pev CLI scenarios and classify each for responsiveness.

Usage:
    python3 run_batch.py < specs.json > results.json
    python3 run_batch.py --slice A/N < specs.json   # run shard A of N

Reads a JSON array of scenario specs (from scenarios.py) on stdin, runs each
under the PTY driver, and writes a JSON array of results on stdout. A short
human summary goes to stderr.

Verdict per scenario:
    PASS  -> terminated within deadline (completed OR interrupted), no crash.
             For expect_blocks scenarios, "still at prompt within the short
             deadline" is also a PASS (it correctly waits for input).
    HANG  -> alive at deadline and NOT an expect_blocks scenario.
    CRASH -> exited via an unexpected fatal signal (SIGSEGV/SIGABRT/etc.).
    FAIL  -> harness/IO error.

Exit code is non-zero if any scenario is HANG or CRASH, so the agent driving
this batch can flag regressions immediately.
"""
import json
import signal
import sys

import pty_driver

FATAL_SIGNALS = {
    signal.SIGSEGV, signal.SIGABRT, signal.SIGBUS, signal.SIGILL, signal.SIGFPE,
}


def verdict(spec, r):
    expect_blocks = spec.get("expect_blocks", False)
    cls = r["classification"]
    term_sig = r.get("term_sig")

    # Unexpected fatal crash signal.
    if term_sig in FATAL_SIGNALS:
        return "CRASH", f"died on signal {term_sig}"

    if cls == "hung":
        if expect_blocks:
            return "PASS", "correctly waits at prompt for input (no input given)"
        return "HANG", f"alive at deadline ({r['elapsed_s']}s), output tail logged"

    if cls in ("completed", "interrupted", "stopped"):
        return "PASS", cls

    if cls == "error":
        return "FAIL", "harness error"

    return "FAIL", f"unknown classification {cls}"


def main():
    args = sys.argv[1:]
    shard = None
    if args and args[0] == "--slice":
        a, n = args[1].split("/")
        shard = (int(a), int(n))

    specs = json.load(sys.stdin)
    if shard:
        a, n = shard
        specs = [s for i, s in enumerate(specs) if i % n == (a - 1)]

    results = []
    bad = 0
    for s in specs:
        r = pty_driver.run(
            argv=s["argv"],
            actions=s.get("actions", []),
            deadline_s=s.get("deadline_s", 60),
            quiet_ms=s.get("quiet_ms", 300),
            env=s.get("env"),
        )
        v, reason = verdict(s, r)
        rec = {
            "name": s["name"],
            "verdict": v,
            "reason": reason,
            "classification": r["classification"],
            "exit_code": r["exit_code"],
            "term_sig": r["term_sig"],
            "elapsed_s": r["elapsed_s"],
            "argv": s["argv"],
            "actions": s.get("actions", []),
        }
        if v in ("HANG", "CRASH", "FAIL"):
            rec["output_tail"] = r["output_tail"]
            bad += 1
        results.append(rec)
        sys.stderr.write(f"[{v:5}] {s['name']:32} {r['elapsed_s']:>6.1f}s  {reason}\n")

    json.dump(results, sys.stdout, indent=0)
    npass = sum(1 for r in results if r["verdict"] == "PASS")
    sys.stderr.write(f"\n{npass}/{len(results)} PASS, {bad} need attention\n")
    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    main()
