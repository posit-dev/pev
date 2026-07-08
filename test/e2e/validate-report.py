#!/usr/bin/env python3
"""Validate a pev-report-*.json after a clean-image assess run.

The CI/local e2e harness exits zero from `pev assess` only when no checks
fail, but on a clean image we *expect* the workbench/connect/ppm presence
checks to fail. We swallow that specific exit code and instead run this
validator against the JSON sidecar to confirm the engine actually ran a
real catalog and reached real verdicts — catches regressions where every
check returns "unknown" (broken primitive registry, YAML loader crash,
etc.) that the older `test -f *.json` gate would happily ship green.

Usage:
    validate-report.py root <root-report.json>
    validate-report.py nonroot <nonroot-report.json> <root-report.json>
"""
import json
import sys


def load_summary(path):
    with open(path) as f:
        return json.load(f)["summary"]


def validate_root(path):
    s = load_summary(path)
    # Catalog size sanity. >50 catches a loader regression that drops packs.
    assert s["total"] > 50, f"unexpectedly small catalog: {s}"
    # The engine MUST have reached a verdict on most checks. >50% unknown
    # signals primitives crashing or YAML failing to bind to runners.
    assert s["unknown"] < s["total"] // 2, f"too many unknowns: {s}"
    # Clean image: workbench/connect/ppm presence MUST fail.
    assert s["fail"] >= 3, f"expected >=3 failures (product presence), got {s}"
    # Some checks always pass on a clean Linux box; zero passes signals
    # broad engine breakage.
    assert s["pass"] > 0, f"zero passes signals broken engine: {s}"
    print(f"root summary OK: {s}")


def validate_nonroot(nonroot_path, root_path):
    nr = load_summary(nonroot_path)
    rt = load_summary(root_path)
    assert nr["total"] == rt["total"], f"catalog size differs root={rt} nonroot={nr}"
    # Non-root MUST SKIP more than root because requires_root checks
    # short-circuit. Equality means the privilege gate has regressed.
    assert nr["skip"] > rt["skip"], f"non-root did not SKIP more than root: root={rt} nonroot={nr}"
    print(f"non-root summary OK: {nr}")


def main():
    if len(sys.argv) < 3:
        sys.exit(__doc__)
    mode = sys.argv[1]
    if mode == "root":
        validate_root(sys.argv[2])
    elif mode == "nonroot":
        if len(sys.argv) != 4:
            sys.exit(__doc__)
        validate_nonroot(sys.argv[2], sys.argv[3])
    else:
        sys.exit(f"unknown mode: {mode}")


if __name__ == "__main__":
    main()
