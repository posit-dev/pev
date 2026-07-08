# Release process

Releases are tagged `vX.Y.Z` on `main` and published as signed GitHub Releases via `goreleaser` + cosign keyless OIDC.

## Cadence

- **Patch (`v1.2.x`)**: any time, for bug fixes.
- **Minor (`v1.x.0`)**: when new checks land or existing ones change behavior.
- **Major (`vX.0.0`)**: only when `checks.SchemaVersion` changes — `pev diff` rejects mismatched majors, so this is a real break.

## Version bump rules

- A new check is `feat:` ⇒ minor bump.
- A behavior change to an existing check that may flip a customer's CI gate is `feat!:` ⇒ minor bump with a changelog "behavior change" callout.
- Schema bump is `feat!:` ⇒ major bump; ship a migration note.

## Cutting a release

1. Confirm `main` is green (`ci`, `e2e`, `codeql`).
2. Tag locally and push:
   ```bash
   git checkout main && git pull
   git tag -a v0.2.0 -m "v0.2.0"
   git push origin v0.2.0
   ```
3. `release.yml` runs goreleaser, which:
   - Builds linux/amd64 and linux/arm64 static binaries.
   - Generates an SBOM (Syft).
   - Writes `pev_<VERSION>_checksums.txt`.
   - Cosign-signs the checksums file via GitHub Actions OIDC, emitting a single Sigstore `.bundle` (signature + cert chain) alongside it.
   - Publishes a draft GitHub Release.
4. Smoke-test the draft release on a real RHEL 9 / RHEL 10 VM (CI uses Alma; this catches anything Alma's rebuild misses — `subscription-manager`, FIPS-mode kernels, license-manager binary differences).
5. Promote the draft to published.

## Yanking a release

If a release ships with a critical bug:

1. Mark the GitHub Release as a pre-release (visually demotes it).
2. Tag and ship the patch (`vX.Y.Z+1`).
3. Add a "Known issues" section to the original release notes pointing at the patch.

Do not delete or retag tags — customers may have already pulled the binary, and silent retags break supply-chain verification.
