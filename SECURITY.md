# Security

## Reporting a vulnerability

**Do not** file a public GitHub issue for security problems. Use GitHub's private disclosure flow:

1. Open https://github.com/posit-dev/pev/security/advisories/new
2. Describe the issue, affected versions, and reproduction steps.
3. We will respond within 5 business days with a triage decision and disclosure timeline.

## What's in scope

- The pev binary as published to GitHub Releases.
- The check catalog YAML files shipped with each release.
- The build pipeline (.goreleaser.yaml, .github/workflows/*).

Out of scope:

- Customer-authored YAML packs in `~/.config/pev/checks.d/`. pev executes those packs by design; treat them as code.
- The Posit products being assessed.

## Verifying release artifacts

Each release ships a `pev_<VERSION>_checksums.txt` plus a cosign keyless signature on that file. Verify:

```bash
cosign verify-blob \
  --certificate-identity-regexp 'github.com/posit-dev/pev' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --signature pev_<VERSION>_checksums.txt.sig pev_<VERSION>_checksums.txt
sha256sum --check --ignore-missing pev_<VERSION>_checksums.txt
```

The first command attests the checksum file was produced by the official GitHub Actions release workflow on the `posit-dev/pev` repo. The second confirms your downloaded binary's hash matches.
