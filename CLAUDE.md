# CLAUDE.md — Contributor Playbook

Guidance for AI-assisted (and human-assisted) contributions to `pev`. Read top-to-bottom before your first PR; bookmark the *Authoring* sections for repeat use.

## 1. Project mission

`pev` is a single-binary Linux CLI that assesses a host's readiness **before** any Posit product is installed. It is **read-only by default**, ships as a static binary with no runtime deps on the target system, runs as root or non-root, and produces a Markdown report (human) plus a JSON sidecar (diffable). The check catalog is authored as YAML and embedded at build time; customers and SEs can add custom YAML packs without recompiling.

**Scope boundary:** validation of an *installed* Posit product (license activation, parsing rserver.conf / rstudio-connect.gcfg / rstudio-pm.gcfg, deployed-content tests, rstudio-server license-manager) is the responsibility of [`vip`](https://github.com/posit-dev/vip), **not pev**. If a check requires a Posit product to already be installed, it does not belong in this repo.

## 2. Architecture map

| Directory | Responsibility |
|---|---|
| `cmd/` | Cobra subcommands. Persistent `--loglevel` and `--out-dir` live here. |
| `internal/system/` | Wrappers around `os/exec`, file/dir helpers, root detection. **All shell-outs route through here.** |
| `internal/operatingsystem/` | OS detection. `/etc/os-release` is authoritative; Alma/Rocky/CentOS/Oracle collapse onto `rhel-<major>`. |
| `internal/discover/` | Read-only probes that build `HostFacts` (sizing, hostname, products, languages). |
| `internal/logging/` | Logrus JSON file logger. |
| `internal/checks/` | The engine: data model, YAML loader, primitive registry, applies-to filter, lint. |
| `internal/primitives/` | One file per primitive (`cmd`, `file`, `dir`, `port`, `dns`, `http`, `x509`, `pkg`, `proc`, `sysctl`, `sizing`). Each registers itself via `init()`. |
| `internal/report/` | Pure rendering: Markdown, JSON, and `pev diff`. No exec, no network. |
| `checks/` | YAML check packs. Embedded into the binary via `embed.go`. |
| `test/e2e/` | Docker matrix runner (`ubuntu:22.04`, `ubuntu:24.04`, `almalinux:9`, `almalinux:10`). |
| `docs/` | Architecture, primitives reference, runbook mapping, release process. |

## 3. Where the truth lives

- **Modifying a check's behavior** → edit YAML in `checks/`. Run `pev lint-checks` then `make test`.
- **Adding a primitive** → Go code in `internal/primitives/` + register in the same file's `init()` + table tests + a section in `docs/primitives.md`.
- **Changing the report shape** → `internal/report/`. Bump `checks.SchemaVersion` if the JSON keys change incompatibly; `pev diff` rejects mismatched majors.
- **Changing the CLI surface** → `cmd/`. Persistent flags belong on the root command.

## 4. Conventions copied from sol-eng/wbi

- **cobra + viper** for commands and flags. Persistent `--loglevel` is bound to viper at root setup.
- **AlecAivazis/survey/v2** for interactive prompts (planned for v0.2; not yet wired).
- **logrus JSON file logger** at `pev-log-<TS>.log` under `--out-dir`. Stdout stays plain.
- **`system.RunCaptured(ctx, cmd, timeout)`** is the only sanctioned `os/exec` entry point.
- **No color** in default output. Output is plain text so report Markdown round-trips through email and PR comments cleanly.
- **OS detection** was upgraded vs. wbi: `/etc/os-release` is parsed first; `/etc/redhat-release` and `/etc/issue` are fallbacks. Alma/Rocky/CentOS/Oracle normalize to `rhel-<major>`.

## 5. Authoring a new built-in check

**Scope test before anything else.** A check is in scope for pev only if a customer can satisfy it *before* installing any Posit product. If the check inspects state that exists only after install (license-manager output, parsing product config files, deployed-content fetches, product binaries on disk), it belongs in [`vip`](https://github.com/posit-dev/vip), not here. Reject the check at PR review.

Checklist:

1. Confirm the check is a pre-install prerequisite (see scope test above).
2. Pick an `id` using dotted convention `<area>.<topic>.<facet>` (e.g. `workbench.idp.metadata`). Ids are forever — duplicates cause load failure.
3. Write a `why:` block — this rationale is shown in the report. Two sentences, plain English, customer-readable. Every FAIL is treated as worth investigating; pev does not classify checks into severity tiers.
4. Add a `short_description:` — the friendly label admins see in the engine's per-check progress line (`[i/N] <short_description> (<id>)`). Keep it human-readable and a handful of words long. This is what an SE staring at a hung run looks at to figure out what the engine is actually doing.
5. Pick a `primitive:` and the matching `with:` payload. Run `pev list-checks --tags <existing-tag>` to find similar examples.
6. List `references:` URLs from Posit docs. The docs.posit.co AI assistant is a useful way to surface the right page when unsure.
7. Gate via `applies_to.os/products/arch` and `requires_root` as appropriate.
8. If the check derives from a runbook prereq, add a row to `docs/runbook-mapping.md`.
9. `make lint && make test`. If you added a new primitive too, see §6.

## 6. Authoring a new primitive

Only when an existing primitive cannot express the check.

1. Implement `Runner` in `internal/primitives/<name>.go`.
2. Register in the file's `init()`: `checks.Register(name, runner, allowedKeys)`. The lint pass uses `allowedKeys` to catch typos in YAML.
3. Document the primitive (purpose, fields, examples) in `docs/primitives.md`.
4. Add a positive + negative table test in `internal/primitives/primitives_test.go`.
5. Update `internal/checks/lint.go` if the primitive needs cross-field validation beyond key whitelisting.

## 7. Verification expectations

- Every code change runs `make test` (race + shuffle).
- Every catalog change runs `make e2e` against at least one Ubuntu and one RHEL-family container.
- New checks should ship with an end-to-end fixture proving both PASS and FAIL paths before merging.

## 8. What not to do

- Never call `os/exec` outside `internal/system`. The wrapper centralizes timeouts and stdout/stderr capture.
- Never log secrets to stdout or the log file. The only secret-shaped input today is `postgres_password`; it's gated by `cmd/assess.secretInputKeys` + `redactSecrets()` (JSON report) and the `Password()` prompt's `(redacted)` Q/A entry. Add new secret-shaped inputs to that map and to the prompt path.
- Never write outside `--out-dir`. pev is read-only on the target system.
- Never do raw network I/O outside the `http` / `port` / `dns` primitives. Centralization keeps timeouts honest.
- Never abort the run on a missing input. Emit `SKIPPED (missing input X)` instead — the engine already does this when a `{{ .Inputs.X }}` template fails to expand.
- Never add `_test.go` fixtures that depend on internet access; mock or skip with `t.Skip("requires internet")`.

## 9. External context

- User check packs live at `~/.config/pev/checks.d/*.yaml`, loaded when `--include-user-checks` is set (default true).
- pev only writes inside `--out-dir`; it never reads or writes `$HOME/.posit/*` or any product config dir.

## 10. Release process

See `docs/release-process.md`. Tag format `vX.Y.Z`. Conventional Commits drive the changelog. `goreleaser` + cosign produce signed artifacts. The `e2e` workflow must be green on the tagged commit before promoting the release from draft to published.

## 11. Open questions / non-goals

- Windows / macOS — not in scope.
- Kubernetes / containerized Posit deployments — not in v1.
- Workbench Launcher / Slurm / K8s session-cluster validation — Phase 2 catalog (still pre-install gates only).
- Pro Drivers depth beyond presence — Phase 2.
- Remediation (`pev fix`) — explicitly v2.
- **Anything that runs against an installed Posit product** — that is [`vip`](https://github.com/posit-dev/vip)'s job. License activation, rserver.conf parsing, deployed-content tests, product version checks, etc. all belong there.
