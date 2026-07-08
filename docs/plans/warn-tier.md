# Plan: Add a `WARN` status tier

**Status:** Proposed
**Author:** Sam Cofer
**Date:** 2026-06-18
**Origin:** BfR Weekly Sync (Jun 17). Two pev commitments asked for checks to
"show a warning instead of a failure" (disk sizing when a separate data dir is
used; firewall ports 8787/3939/4242/5599 that aren't required for Connect).
pev has no warning state today, so the literal ask requires a new status tier.

This document scopes **only** the addition of the `WARN` tier itself. It does
**not** cover wiring any specific check (sizing, firewall) to emit `WARN` —
those are separate follow-ups that depend on this landing first.

---

## 1. Problem

`pev`'s outcome model is `pass / fail / skip / unknown` (`internal/checks/model.go`).
There is no way to express "this is worth surfacing to the SE, but it is not a
blocking failure." Schema v2 **deliberately removed** severity tiers:

```go
// internal/checks/model.go:14-19
// v2 (2026-06): dropped Severity. Every FAIL is treated as worth
// investigating; the catalog no longer tries to predict which failures
// will block an install.
const SchemaVersion = 2
```

Adding `WARN` is a partial reversal of that decision. The justification is that
some prerequisites are genuinely advisory (host *can* be installed on, but the
SE should note something), and folding those into `FAIL` either creates false
alarms or pressures authors to drop the check entirely. `WARN` gives a third
landing spot: visible, not green, but not counted against the run.

### Design philosophy note

The v2 comment must be updated, not ignored. The new stance: **FAIL = will
likely block or break the install; WARN = advisory, SE should read it but it is
not disqualifying.** Authors must be given clear guidance (see §6) or `WARN`
will erode into a dumping ground for "FAIL but I'm not sure," which defeats the
purpose.

---

## 2. Scope

**In scope**
- New `StatusWarn` constant and `Summary.Warn` tally.
- Rendering `WARN` across all three outputs (terminal, Markdown, JSON).
- Classifying `WARN` transitions in `pev diff`.
- `SchemaVersion` bump and its consequences.
- Test coverage for the new tier.
- Author-facing documentation of when to use `WARN`.

**Out of scope (explicit follow-ups)**
- Migrating the disk-sizing check to emit `WARN`.
- Migrating the firewall port audits to emit `WARN`.
- Any **mechanism** by which a YAML check declares "treat this failure as a
  warning" (e.g. a `downgrade_to_warn_when:` gate). See §7 — the tier alone does
  not give catalog authors a way to *emit* `WARN` from the existing `cmd` /
  `sizing` primitives. That mechanism is the larger, separate piece of work.

---

## 3. Affected sites

The tier is a small constant; the cost is that `Status` is switched on in many
places and the schema bump invalidates cross-version diffs.

| # | File / location | Change | Effort |
|---|---|---|---|
| 1 | `internal/checks/model.go:24` | Add `StatusWarn Status = "warn"` | trivial |
| 2 | `internal/checks/model.go:163` (`Summary`) | Add `Warn int` field with JSON tag | trivial |
| 3 | `internal/checks/model.go:19` | Bump `SchemaVersion` 2 → 3 | trivial code, large blast radius |
| 4 | `internal/checks/model.go:14-19` | Rewrite the "dropped Severity" comment to define FAIL vs WARN | trivial |
| 5 | `internal/report/json.go:49` (`Summarize`) | Add `case StatusWarn: s.Warn++` | trivial |
| 6 | `internal/report/markdown.go:148` (`iconFor`) | Add `case StatusWarn: return "[WARN]"` | trivial |
| 7 | `internal/report/terminal.go:57-99` | Include `WARN` in the rendered loop (today filters to Fail/Unknown), tag `[WARN]`, wrap in `yellow()`, add to totals line | moderate |
| 8 | `internal/report/terminal.go:40-48` | Add `Warn %d` to the totals line; decide whether a non-zero warn count changes the "All checks passed" footer | small |
| 9 | `internal/report/diff.go:64-69` | Classify `PASS↔WARN` and `WARN↔FAIL` transitions | **design decision** |
| 10 | tests: `engine_test.go`, `terminal_test.go`, `markdown_test.go`, `diff_test.go` | Cover the new status in each renderer + summary | moderate |
| 11 | `docs/check-authoring.md` (+ this dir) | Author guidance: when WARN vs FAIL | small |

**The color is already done.** `internal/report/terminal.go:18` defines
`ansiYellow` and it is already used for `reason:` / `fix:` labels. Rendering
`WARN` lines in yellow is reuse, not new infrastructure.

---

## 4. The two real costs

### 4.1 `SchemaVersion` bump (2 → 3)

`pev diff` rejects mismatched majors (`model.go:13-19`). Bumping to 3 means a v3
binary cannot diff against any report produced by a v2 binary. This is the
correct behavior — the key set changes (`summary.warn` is new) — but it is a
hard break for anyone with archived v2 reports. Call it out in the changelog and
release notes; it is a minor-version-worthy change at minimum.

### 4.2 Terminal renderer currently hides everything but FAIL/UNKNOWN

`RenderTerminal` (`terminal.go:57-67`) explicitly collects only `StatusFail` and
`StatusUnknown` into the per-category view; PASS and SKIP live only in the
on-disk Markdown. `WARN` must be added to that filter or it will be invisible on
the console — which would defeat the entire point of the tier (the BfR ask was
specifically that the SE *sees* it, just not as a red failure).

**Display and exit code are decoupled — both requirements hold together.**
Screen output (`RenderTerminal`, `cmd/assess.go:203`) writes to stdout and feeds
nothing into control flow; the exit code is a separate `return` keyed only on
`Summary.Fail` (`cmd/assess.go:215`, see §4.3). So WARN can be shown on the
assess screen *and* exit 0 — these do not conflict. The precedent is already in
the tree: UNKNOWN renders as a red failure line on screen (`terminal.go:55-56`)
yet the process still exits 0. WARN follows the same pattern: visible on screen,
not exit-fatal. An implementer adding WARN to the §4.2 filter does **not** need
to touch the §4.3 exit logic, and vice versa.

Decisions for the terminal view:
- `WARN` lines render in `yellow()` with a `[WARN]` tag (distinct from red
  `[FAIL]`).
- The "All checks passed." footer (`terminal.go:46-48`) should become something
  like "N warning(s) — review, not blocking." when `Summary.Warn > 0` and
  `Summary.Fail == 0`.

### 4.3 Exit code — verified

The `pev assess` exit code is gated **solely** on the failure count
(`cmd/assess.go:215-217`):

```go
if rep.Summary.Fail > 0 {
    return fmt.Errorf("%d failure(s) — see report", rep.Summary.Fail)
}
return nil
```

Consequences:
- **`WARN` already does the right thing for free.** Because the condition keys
  only on `Summary.Fail`, adding a `WARN` tier leaves the exit code at 0 as long
  as nothing adds `Summary.Warn` to this check. **The action item is therefore
  a negative one: do _not_ touch `cmd/assess.go:215`** — a warning must not be
  exit-fatal.
- **Pre-existing quirk worth noting:** today even `UNKNOWN` does *not* trip a
  non-zero exit — only `Fail` does. The terminal renderer treats UNKNOWN as a
  failure for *display* (`terminal.go:55-56`), but the process still exits 0.
  This plan does not change that, but flag it: if a future change makes UNKNOWN
  exit-fatal, be deliberate about whether WARN stays exempt (it should).
- Add a test asserting `pev assess` exits 0 when a run produces WARN results and
  zero FAIL/UNKNOWN.

---

## 5. `pev diff` classification (design decision)

`diff.go:64-69` currently buckets transitions as regression
(`PASS → FAIL/UNKNOWN`), improvement (`FAIL/UNKNOWN → PASS`), or
"other status change". Proposed treatment of `WARN`:

| Transition | Bucket | Rationale |
|---|---|---|
| `PASS → WARN` | regression | something got worse, even if not blocking |
| `WARN → PASS` | improvement | resolved |
| `WARN → FAIL` / `WARN → UNKNOWN` | regression | got worse |
| `FAIL → WARN` / `UNKNOWN → WARN` | improvement | got better (no longer blocking) |
| `WARN → SKIP` etc. | other status change | neither clearly better nor worse |

This is the one genuinely debatable piece. The table above treats `WARN` as
strictly between `PASS` and `FAIL` on a severity ladder, which matches the
intended semantics. Confirm before implementing.

---

## 6. Author guidance (must ship with the tier)

Add to `docs/check-authoring.md`:

- **FAIL** — the install will likely fail, break, or be unsupported if this is
  not fixed. Default for prerequisites.
- **WARN** — the host can be installed on as-is, but the SE should note this:
  a non-standard-but-valid layout, a soft recommendation not met, a condition
  that matters only for some deployment shapes.
- **When unsure, use FAIL.** WARN is not "FAIL but I'm not confident." If a
  check cannot decide, that is `UNKNOWN`. If a check does not apply to this
  host/product selection, that is `SKIP`.

Without this, WARN regresses into noise and the v2 "every FAIL is worth
investigating" clarity is lost for nothing.

---

## 7. Why the tier alone does not satisfy the BfR asks

Primitives return a `Status` directly. `runSizing` (`sizing.go:52-56`) returns
`StatusFail` on any threshold miss; the firewall audits are `cmd` primitives
that map a shell exit code to pass/fail. **Neither has any concept of "this
particular failure is advisory."** Adding `StatusWarn` to the model gives
primitives a value they *can* return, but it does not give a YAML author any way
to request it from the existing `sizing` / `cmd` primitives.

So landing this tier is a **prerequisite** for the two BfR check changes, not a
completion of them. The follow-up work — a per-check downgrade gate, or
primitive-specific support — is tracked separately and should be planned once
this tier is merged and its semantics are settled.

---

## 8. Verification

- `make test` (race + shuffle) green.
- New unit tests assert: `Summarize` counts `Warn`; `iconFor` returns `[WARN]`;
  the terminal renderer emits a yellow `[WARN]` line and the updated totals;
  diff buckets the transitions per §5; `pev assess` exits 0 on a WARN-only run
  (see §4.3).
- `make e2e` on at least one Ubuntu and one RHEL container to confirm the new
  totals line and report shape render cleanly (no check emits WARN yet, so this
  is a regression check that nothing broke).
- Manually confirm `pev diff` between a v2 and a v3 report fails with a clear
  major-mismatch error rather than a silent miscompare.

---

## 9. Rollout

1. Land the tier (this plan) behind no flag — it is inert until a check emits it.
2. Update changelog/release notes flagging the `SchemaVersion` 2→3 break.
3. Separately design + land the emit mechanism (§7).
4. Separately migrate the disk-sizing and firewall checks to use it.
