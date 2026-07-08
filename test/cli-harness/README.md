# pev CLI test harness

A PTY-driven fuzzer that drives every mutation of the `pev` CLI's interactive
menu and flag surface to verify the binary stays **responsive** — it never
hangs, regardless of the combination of options, blanks, multi-value prompt
selections, or `CTRL+C` / `CTRL+Z` exits.

## Layout

| File | Role |
|---|---|
| `pty_driver.py` | Spawns `pev` under a real pseudo-terminal, drives prompts on output quiescence, answers survey's `ESC[6n` cursor-position query (without this, deep prompt chains wedge), injects keystrokes/signals, and classifies the run against a hard wall-clock deadline. |
| `scenarios.py` | Generates the exhaustive scenario matrix (product selections, idp/output/profile permutations, opt-in branches, blanks, `skip` word, and signals injected at every meaningful point). Each interactive scenario ends with a long ENTER *drain tail* so it always reaches completion — a non-exit therefore means a real wedge. |
| `run_batch.py` | Runs a batch (or `--slice A/N` shard) of specs and emits a JSON verdict array. Exits non-zero on any HANG/CRASH. |
| `fixtures/` | Dummy placeholder inputs: self-signed `dummy.crt`/`dummy.key`, `dummy.lic`, a `custom-pack.yaml` check pack, and two report JSONs for `pev diff`. |
| `results/` | `specs.json` (frozen matrix) and per-shard result JSON. |

## Verdicts

- **PASS** — terminated within deadline (`completed` or `interrupted`), no fatal signal. `expect_blocks` scenarios pass if they correctly wait at a prompt.
- **HANG** — still alive at the deadline (not an `expect_blocks` scenario). The real regression this harness exists to catch.
- **CRASH** — died on `SIGSEGV`/`SIGABRT`/etc.
- **FAIL** — harness/IO error.

## Run it

```bash
cd test/cli-harness
python3 scenarios.py > results/specs.json          # regenerate matrix
python3 run_batch.py < results/specs.json > results/all.json   # whole matrix
python3 run_batch.py --slice 3/8 < results/specs.json          # one shard
```

## Notes on signal behavior

survey/v2 puts the tty in raw mode (`ISIG` off), so `CTRL+C` (`0x03`) is caught
by survey itself and `CTRL+Z` (`0x1a`) is consumed as a literal byte — neither
is delivered to the kernel as a job-control signal. `pev` swallows the
resulting prompt error (the `_` on every `driver.X()` call) and continues, so
the harness verifies that after a signal the app still drives to a clean exit
rather than wedging.
