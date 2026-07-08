# Architecture

`pev` follows a small, deliberately conventional Go layout that mirrors `sol-eng/wbi` for SE familiarity.

## Scope

pev is a **pre-install** validator. Every check in the catalog must be satisfiable before any Posit product is installed on the host. Anything that requires the product to be installed first (license activation, rserver.conf parsing, deployed content, product version reads) belongs in [`vip`](https://github.com/posit-dev/vip), not here.

```
main.go ──► cmd/* (cobra)
              │
              ├─► internal/discover  ── reads /proc, os-release, hostname, /opt/{R,python,quarto}
              ├─► internal/operatingsystem  ── /etc/os-release first; Alma/Rocky → rhel-<major>
              ├─► internal/checks (engine)
              │     ├─► loader: walk go:embed FS + filesystem packs
              │     ├─► registry: primitive name -> Runner func
              │     ├─► engine: applies_to filtering, root gating, template expansion
              │     └─► lint: schema validation
              │
              ├─► internal/primitives (cmd, file, dir, port, dns, http, x509, pkg, proc, sysctl, sizing)
              │     └─► system.RunCaptured (the only os/exec entry point)
              │
              ├─► internal/logging  ── logrus JSON logger
              └─► internal/report   ── Markdown + JSON renderers + Diff
```

## Data flow per `pev assess`

1. Parse flags. Initialize logger (under `--out-dir`).
2. `discover.Gather()` populates `HostFacts` (read-only).
3. Resolve selected products from `--products` / `--profile` / discovery.
4. Load the catalog: embedded YAML packs + `--checks-file` files + `~/.config/pev/checks.d/*.yaml`.
5. Lint the catalog — fail fast if any pack is malformed.
6. Filter checks by `Products`, `Tags`, `SkipTags`, `SkipIDs`.
7. For each check (sequentially, for deterministic ordering):
   - `applies_to` gate, `requires_root` gate
   - Expand `{{ .Inputs.X }}` and `{{ .Facts.Y }}` in `with:`
   - Dispatch to the registered primitive
   - Record a `Result`
8. Sort `Results` by ID; render Markdown + JSON; write the report files.
9. Exit 1 iff `Summary.Fail > 0` — every FAIL is worth investigating. pev has no graduated severity tiers; the only advisory rung is WARN (visible, counted separately, **not** exit-fatal). See `docs/check-authoring.md` for FAIL vs WARN.

## Why the splits look the way they do

- **`internal/system` is the only `os/exec` entry point.** Every shell-out flows through `RunCaptured`, which gives us per-command timeouts, captured stdout/stderr, and a single audit hook.
- **Primitives can't see each other.** Each registers itself via `init()`. New primitives are additive; you can't accidentally make one depend on another's internals.
- **Reports never run anything.** `internal/report/` reads JSON files and produces strings. That makes `pev diff` trivially safe to run on archived sidecars and easy to unit-test.
- **The engine never aborts.** Failures, skips, and unknowns are all results — there are no error paths from the engine to the caller. The CLI uses `Summary.Fail > 0` to decide the process exit code.
