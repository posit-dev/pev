#!/usr/bin/env python3
"""Generate the exhaustive pev CLI scenario matrix.

Emits a JSON array of run specs (the shape pty_driver.run consumes) on
stdout. Each spec is one combination of:

  * subcommand + flag permutation (the non-interactive / flag surface), and
  * an interactive keystroke script (blanks, multi-value selects, opt-ins,
    and CTRL_C / CTRL_Z injected at every meaningful point in the chain).

Design rules that keep every scenario terminating (so "hung" means a real
wedge, never an under-driven script):

  * Every interactive scenario ends with a long ENTER *drain tail*. ENTER
    accepts the default at every survey prompt type (Confirm=No, Input=
    default, Select=highlighted, MultiSelect=current), so no matter how deep
    a branch goes the chain is driven to completion.
  * Signals (CTRL_C / CTRL_Z) are injected BEFORE the drain tail, so the
    follow-on ENTERs prove the app stayed responsive after the signal.
  * The harness pins --tags sizing (or a fast tag) by default so each run
    executes one cheap check, not the whole catalog — we are testing the
    UI/driver surface for hangs, not re-running the full assessment N times.

Output dir is unique per scenario so parallel runs never clobber each other.
"""
import itertools
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
FIX = os.path.join(HERE, "fixtures")
RESULTS = os.path.join(HERE, "results")

PEV = "pev"
# A long ENTER tail guarantees the prompt chain reaches completion no matter
# which opt-in branches a scenario walks into.
DRAIN = ["ENTER"] * 30
# Pin a single fast check so each run is cheap; the surface under test is the
# prompt/flag driver, not the catalog.
FAST = ["--tags", "sizing"]

# MultiSelect product-toggle building blocks. The cursor starts on the
# pre-checked "system configuration checks" sentinel; DOWN then SPACE toggles
# the product on that row.
SEL_WB = ["DOWN", "SPACE"]                     # +workbench
SEL_CONNECT = ["DOWN", "DOWN", "SPACE"]        # +connect
SEL_ALL = ["DOWN", "SPACE", "DOWN", "SPACE", "DOWN", "SPACE"]
SEL_NONE = []                                  # accept the sentinel-only default
SEL_DESELECT_SENTINEL = ["SPACE", "DOWN", "SPACE"]  # uncheck system, check workbench


def outdir(name):
    return os.path.join(RESULTS, "out", name)


def spec(name, argv, actions, deadline=60, quiet_ms=300, env=None):
    return {
        "name": name,
        "argv": argv,
        "actions": actions,
        "deadline_s": deadline,
        "quiet_ms": quiet_ms,
        "env": env or {},
    }


def non_interactive_specs():
    """Pure flag-surface coverage: --non-interactive / --yes / piped paths.

    These never prompt, so they must always exit on their own quickly. We
    permute the enumerated flags (products, profile, idp, output, tags,
    skip-*, license, hostnames, custom packs) including invalid values that
    must fail-fast (exit non-zero) rather than hang.
    """
    out = []

    # --- subcommands that never prompt ---
    for sub in (["version"], ["list-checks"], ["discover"],
                ["discover", "--format", "json"],
                ["list-checks", "--products", "workbench"],
                ["list-checks", "--tags", "network"],
                ["lint-checks", os.path.join(FIX, "custom-pack.yaml")],
                ["--help"], ["assess", "--help"], ["diff", "--help"],
                ["completion", "bash"], ["completion", "zsh"]):
        nm = "ni-" + "_".join(x for x in sub if not x.startswith("/")).replace("-", "").replace("/", "")[:40]
        out.append(spec(nm, [PEV] + sub, [], deadline=30))

    # diff with the two report fixtures (and reversed / same-file).
    base = os.path.join(FIX, "report-baseline.json")
    cur = os.path.join(FIX, "report-current.json")
    out.append(spec("ni-diff-fwd", [PEV, "diff", base, cur], [], deadline=30))
    out.append(spec("ni-diff-rev", [PEV, "diff", cur, base], [], deadline=30))
    out.append(spec("ni-diff-same", [PEV, "diff", base, base], [], deadline=30))
    out.append(spec("ni-diff-json", [PEV, "diff", base, cur, "--format", "json"], [], deadline=30))
    out.append(spec("ni-diff-missing", [PEV, "diff", "/nonexistent-a.json", "/nonexistent-b.json"], [], deadline=30))

    # assess --non-interactive across the product / idp / output / profile space.
    products_opts = [None, "workbench", "connect", "packagemanager", "ppm",
                     "workbench,connect", "workbench,connect,packagemanager",
                     "bogusproduct", ""]
    idp_opts = [None, "none", "ldap", "saml", "oidc", "bogus", ""]
    output_opts = [None, "md", "json", "md,json", "markdown", "jsn", ""]
    profile_opts = [None, "single-server", "workbench", "connect", "ppm", "bogus"]

    n = 0
    for prod in products_opts:
        for idp in idp_opts:
            argv = [PEV, "assess", "--non-interactive"] + FAST
            argv += ["--out-dir", outdir(f"ni-pi-{n}")]
            if prod is not None:
                argv += ["--products", prod]
            if idp is not None:
                argv += ["--idp", idp]
            out.append(spec(f"ni-prod-idp-{n}", argv, [], deadline=40))
            n += 1

    n = 0
    for outp in output_opts:
        for prof in profile_opts:
            argv = [PEV, "assess", "--non-interactive"] + FAST
            argv += ["--out-dir", outdir(f"ni-op-{n}")]
            if outp is not None:
                argv += ["--output", outp]
            if prof is not None:
                argv += ["--profile", prof]
            out.append(spec(f"ni-out-prof-{n}", argv, [], deadline=40))
            n += 1

    # license / hostnames / custom pack / skip flags / review-skipped / loglevel.
    lic = os.path.join(FIX, "dummy.lic")
    pack = os.path.join(FIX, "custom-pack.yaml")
    extra = [
        ("ni-license", ["--license-file", lic]),
        ("ni-license-missing", ["--license-file", "/no/such/file.lic"]),
        ("ni-hostnames", ["--hostnames", "workbench=wb.example.com,connect=c.example.com,ppm=p.example.com"]),
        ("ni-hostnames-malformed", ["--hostnames", "garbage,,=,workbench="]),
        ("ni-checksfile", ["--checks-file", pack]),
        ("ni-checksfile-missing", ["--checks-file", "/no/such/pack.yaml"]),
        ("ni-skip-checks", ["--skip-checks", "sizing.cpu.cores"]),
        ("ni-skip-tags", ["--skip-tags", "network"]),
        ("ni-tags-multi", ["--tags", "sizing"]),
        ("ni-review-skipped", ["--review-skipped"]),
        ("ni-no-user-checks", ["--include-user-checks=false"]),
        ("ni-loglevel-trace", ["--loglevel", "trace"]),
        ("ni-loglevel-bogus", ["--loglevel", "bogus"]),
        ("ni-everything", ["--products", "workbench,connect,packagemanager",
                           "--idp", "saml", "--output", "md,json",
                           "--license-file", lic, "--checks-file", pack,
                           "--hostnames", "workbench=wb.x", "--review-skipped",
                           "--profile", "single-server"]),
    ]
    for nm, flags in extra:
        argv = [PEV, "assess", "--non-interactive"] + FAST + ["--out-dir", outdir(nm)] + flags
        out.append(spec(nm, argv, [], deadline=40))

    # --yes mode (accept discovered defaults, no prompts).
    for prod in (None, "workbench", "connect,packagemanager"):
        argv = [PEV, "assess", "--yes"] + FAST + ["--out-dir", outdir(f"yes-{prod}")]
        if prod:
            argv += ["--products", prod]
        out.append(spec(f"yes-{prod or 'auto'}", argv, [], deadline=40))

    # Piped/non-TTY auto-downgrade is implicitly covered: pty_driver always
    # gives a TTY, but a bare interactive invocation with an immediate EOF
    # (empty action list, no drain) on the assess prompt is also exercised
    # below in the interactive set.
    return out


def interactive_specs():
    """Full interactive prompt-chain coverage with signals and blanks."""
    out = []

    def iv(name, pre_actions, deadline=60, extra_flags=None):
        argv = [PEV, "assess"] + FAST + ["--out-dir", outdir("iv-" + name)]
        if extra_flags:
            argv += extra_flags
        out.append(spec("iv-" + name, argv, pre_actions + DRAIN, deadline=deadline))

    # Product-selection variants, each then drained to completion.
    iv("none-default", [])                       # accept sentinel-only
    iv("workbench", SEL_WB)
    iv("connect", SEL_CONNECT)
    iv("all-products", SEL_ALL)
    iv("deselect-sentinel-wb", SEL_DESELECT_SENTINEL)
    iv("toggle-on-off", ["DOWN", "SPACE", "SPACE"])   # toggle wb on then off

    # Opt-IN to each follow-up branch (Y) then drain. Workbench selected so
    # the deepest chain (cert/key, idp, postgres, pam, drivers, home share)
    # is reachable. Each Y is placed right after the product ENTER; the drain
    # tail handles the sub-prompts.
    iv("wb-optin-first", SEL_WB + ["ENTER", "Y"])
    iv("wb-optin-many", SEL_WB + ["ENTER", "Y", "Y", "Y", "Y", "Y"])
    iv("connect-optin", SEL_CONNECT + ["ENTER", "Y", "Y", "Y"])
    iv("all-optin", SEL_ALL + ["ENTER", "Y", "Y", "Y", "Y", "Y", "Y"])

    # Blank-input paths: opt in, then submit an empty string (ENTER on an
    # Input whose default is empty -> blank -> dependent check SKIPs).
    iv("wb-blank-hostname", SEL_WB + ["ENTER", "BACKSPACE", "BACKSPACE", "ENTER"])
    iv("postgres-y-blank-host", SEL_WB + ["ENTER", "N", "N", "N", "Y", "ENTER"])

    # 'skip' magic word on an Input prompt (bypasses one prompt -> blank).
    iv("hostname-skip-word", SEL_WB + ["ENTER", "skip", "ENTER"])

    # Select-prompt navigation: IdP type SAML vs OIDC (DOWN to pick oidc).
    iv("idp-saml", [], extra_flags=None)  # placeholder, refined below
    out.pop()  # drop the placeholder; build explicit idp scenarios next

    # IdP opt-in -> Select(saml/oidc). After SEL_NONE the prompt order is:
    # MultiSelect(ENTER) -> ... wait, idp prompt only fires when a product is
    # selected. Use workbench.
    iv("idp-confirm-saml", SEL_WB + ["ENTER", "N", "Y", "ENTER"])      # idp Y, pick saml (default)
    iv("idp-confirm-oidc", SEL_WB + ["ENTER", "N", "Y", "DOWN", "ENTER"])  # pick oidc

    # ---- Signal injection at every meaningful point ----
    # CTRL_C / CTRL_Z then a full drain: the app must still complete (survey
    # runs the tty in raw mode, so these are consumed as bytes, not signals;
    # the assertion is that the app neither crashes nor wedges).
    iv("ctrlc-at-multiselect", ["CTRL_C"])
    iv("ctrlz-at-multiselect", ["CTRL_Z"])
    iv("ctrlc-after-products", SEL_WB + ["ENTER", "CTRL_C"])
    iv("ctrlz-after-products", SEL_WB + ["ENTER", "CTRL_Z"])
    iv("ctrlc-mid-confirms", SEL_WB + ["ENTER", "N", "CTRL_C", "N"])
    iv("ctrlz-mid-confirms", SEL_WB + ["ENTER", "N", "CTRL_Z", "N"])
    iv("ctrlc-in-input", SEL_WB + ["ENTER", "CTRL_C", "ENTER"])
    iv("ctrlc-in-select", SEL_WB + ["ENTER", "N", "Y", "CTRL_C", "ENTER"])
    iv("double-ctrlc", SEL_WB + ["CTRL_C", "CTRL_C"])
    iv("ctrlc-then-ctrlz", SEL_WB + ["CTRL_C", "CTRL_Z"])

    # Immediate EOF / no input on the first prompt (empty action list, short
    # deadline): MultiSelect should hold at the prompt. This one is ALLOWED to
    # hang within a SHORT deadline and is classified separately by the runner
    # as "blocks-on-input" rather than "hung" — see runner. We mark it.
    s = spec("iv-eof-no-input", [PEV, "assess"] + FAST + ["--out-dir", outdir("iv-eof")],
             [], deadline=12, quiet_ms=400)
    s["expect_blocks"] = True
    out.append(s)

    # --yes / --non-interactive with interactive driver present (flags win,
    # no prompt shown) — confirms flags short-circuit the TTY path.
    out.append(spec("iv-yes-flag", [PEV, "assess", "--yes"] + FAST +
                    ["--out-dir", outdir("iv-yesflag")], DRAIN, deadline=40))

    return out


def main():
    specs = non_interactive_specs() + interactive_specs()
    # Dedupe by name.
    seen = {}
    for s in specs:
        seen[s["name"]] = s
    specs = list(seen.values())
    json.dump(specs, sys.stdout, indent=0)
    sys.stderr.write(f"generated {len(specs)} scenarios\n")


if __name__ == "__main__":
    main()
