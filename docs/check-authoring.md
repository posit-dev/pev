# Authoring custom YAML check packs

`pev` accepts custom YAML packs via `--checks-file path.yaml` (repeatable) and from `~/.config/pev/checks.d/*.yaml` (when `--include-user-checks` is set, default true). Packs use the same schema as the built-in catalog under `/checks`.

## Minimum viable pack

```yaml
schema_version: 3
checks:
  - id: mycorp.example.binary
    title: My corp's wrapper binary is installed
    short_description: Checking mycorp wrapper binary is installed
    primitive: file
    why: |
      Our prod images ship a wrapper at /opt/mycorp/bin/wrap. If it's missing,
      Posit content can't authenticate against our internal vault.
    with:
      path: /opt/mycorp/bin/wrap
      must_exist: true
```

Validate it before shipping:

```bash
pev lint-checks ./mycorp-pack.yaml
```

## All check fields

| Field | Required | Description |
|------|----------|-------------|
| `id` | yes | Globally unique; convention `<area>.<topic>.<facet>`. |
| `title` | yes | One-line, present-tense; appears in tables and the Markdown report. |
| `short_description` | recommended | Friendly label shown in the engine's per-check progress line as `[i/N] <short_description> (<id>)`. Keep it human-readable and a handful of words long; admins watching a hung run should be able to tell at a glance which check is in flight without decoding the dotted ID. Falls back to the ID alone when omitted. |
| `tags` | no | Free-form labels for `--tags`/`--skip-tags`. |
| `applies_to.os` | no | Canonical OS ids: `ubuntu-22.04`, `ubuntu-24.04`, `rhel-8`, `rhel-9`, `rhel-10`. |
| `applies_to.products` | no | `workbench` \| `connect` \| `packagemanager`. |
| `applies_to.arch` | no | `amd64` \| `arm64`. |
| `applies_to.roles` | no | Reserved for future use. |
| `requires_root` | no | If true and pev runs as non-root, the check is `SKIPPED`. |
| `why` | yes | Rationale shown to users; two sentences, plain English. |
| `remediation` | no | Free-text fix hint; reserved for the v2 `pev fix` flow. |
| `references` | no | List of authoritative doc URLs. |
| `primitive` | yes | One of the registered primitive names — see `docs/primitives.md`. |
| `with` | yes | Primitive-specific payload. |

## Choosing an outcome: FAIL vs WARN vs UNKNOWN vs SKIP

A check resolves to one of five statuses. Picking the right one is the single
most important authoring decision — it is what an SE acts on. The catalog does
**not** have graduated severity tiers; the choice is deliberately coarse.

- **FAIL** — the install will likely fail, break, or be unsupported if this is
  not fixed. This is the default for any prerequisite. A FAIL trips a non-zero
  exit from `pev assess` and is rendered red. When in doubt, use FAIL.
- **WARN** — the host can be installed on as-is, but the SE should note this:
  a non-standard-but-valid layout, a soft recommendation not met, or a
  condition that matters only for some deployment shapes. A WARN is rendered
  yellow, counted separately in the summary, and is **not** exit-fatal — the
  run still exits 0. WARN is *not* "FAIL but I'm not confident": if a check
  cannot decide, that is UNKNOWN; if it does not apply, that is SKIP. Treating
  WARN as a hedge erodes it into noise and defeats the point of the tier.
- **UNKNOWN** — the check could not reach a verdict (a primitive errored, a
  tool was missing mid-run, output was unparseable). The engine assigns this;
  authors rarely return it deliberately. It renders as a failure on screen but
  does not trip the exit code today.
- **SKIP** — the check does not apply to this host/product/arch selection, a
  required input was not supplied, or `requires_root` was set and pev is
  non-root. The engine assigns SKIP automatically from `applies_to`,
  `requires`, `requires_root`, and missing-input template failures; authors
  gate rather than emit it.

> Note: as of `schema_version: 3` the model is PASS / WARN / FAIL / SKIP /
> UNKNOWN. Most built-in primitives (`cmd`, `sizing`, …) still only return
> PASS or FAIL; a YAML author cannot yet *request* WARN from them. The tier
> exists in the model and report; the mechanism for a check to emit it is a
> separate, later piece of work.

## Templating

Any string in `with:` may use Go `text/template` syntax against:

- `{{ .Facts.Hostname }}`, `{{ .Facts.FQDN }}`, `{{ .Facts.OS }}`, `{{ .Facts.Arch }}`, `{{ .Facts.CPUs }}`, `{{ .Facts.MemMB }}`
- `{{ .Inputs.<key> }}` — populated by CLI flags (`--license-file`, `--hostnames`, `--idp`) and (in v0.2+) survey prompts

Missing-key errors during template expansion become `SKIPPED (missing or invalid input: <details>)` rather than aborting the run.

## ID conventions

- Use lower-case, dotted segments: `<area>.<topic>.<facet>`.
- Areas: `os`, `net`, `sizing`, `pkg`, `workbench`, `connect`, `ppm`, `mycorp`.
- Avoid timestamps and version numbers in IDs — checks evolve, IDs persist.

## Tagging guidance

Tags shape `--tags`/`--skip-tags` filters. Reuse what's in the built-in catalog where applicable: `os`, `network`, `egress`, `license`, `ssl`, `sizing`, `packages`, `auth`, `quarto`, `r`, `python`. Add corp-specific prefixes like `mycorp.*` for tags you don't expect to share upstream.
