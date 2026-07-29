#!/bin/sh
# pev installer — one-line bootstrap.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/posit-dev/pev/main/scripts/install.sh | sh
#
# To install AND assess in one line, join with `&&`, never a pipe:
#   curl -fsSL .../install.sh | sh && pev assess
# Do NOT do `... | sh | pev assess`: that hands pev a non-TTY stdin, so it
# detects no terminal and silently runs in --yes mode (no prompts, all
# discovered defaults accepted).
#
# Environment overrides:
#   PEV_VERSION     — release tag to pin (default: latest, e.g. v0.0.2)
#   PEV_INSTALL_DIR — destination directory (default: ~/.local/bin for non-root,
#                     /usr/local/bin for root)
#   PEV_VERBOSE     — set to 1 to surface the SHA-256-only fallback warning
#                     printed when cosign is not on PATH (default: quiet)
#
# Behavior:
#   - Detects Linux amd64 / arm64; refuses other platforms.
#   - Downloads the matching pev_linux_<arch> binary and the published
#     pev_<version>_checksums.txt; verifies SHA-256 before install.
#   - When `cosign` is on PATH, also verifies the keyless signature on the
#     checksums file before trusting any SHA-256. Without cosign the
#     installer proceeds with SHA-256-only (set PEV_VERBOSE=1 to see the
#     downgrade warning) — set PEV_REQUIRE_COSIGN=1 to make the
#     missing-cosign case a hard error for regulated installs.
#   - Always overwrites: re-running upgrades in place.
#
# Verifying this script before running it is recommended for regulated
# environments. Save it locally, inspect, then `sh install.sh`.

set -eu

REPO="posit-dev/pev"
RELEASES_API="https://api.github.com/repos/${REPO}/releases/latest"
RELEASES_DOWNLOAD="https://github.com/${REPO}/releases/download"

err() { printf 'pev-install: %s\n' "$*" >&2; exit 1; }
log() { printf 'pev-install: %s\n' "$*"; }
# vlog prints only when PEV_VERBOSE=1; used for advisory notices (e.g. the
# cosign-absent SHA-256-only fallback) that are noise on the happy path but
# matter to operators auditing supply-chain verification.
vlog() { [ "${PEV_VERBOSE:-0}" = "1" ] && printf 'pev-install: %s\n' "$*"; return 0; }

need() { command -v "$1" >/dev/null 2>&1 || err "missing required tool: $1"; }
need curl
need sha256sum
need install
need mktemp

# -- Platform ------------------------------------------------------------------
os=$(uname -s)
[ "$os" = "Linux" ] || err "pev only supports Linux (detected: $os)"

case "$(uname -m)" in
  x86_64)  arch="amd64" ;;
  aarch64) arch="arm64" ;;
  *) err "pev only supports amd64 and arm64 (detected: $(uname -m))" ;;
esac

# -- Version -------------------------------------------------------------------
if [ -n "${PEV_VERSION:-}" ]; then
  version="$PEV_VERSION"
else
  # The "latest" endpoint returns a JSON blob; grep+sed is enough for the
  # tag_name field and avoids requiring jq.
  version=$(curl -fsSL "$RELEASES_API" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
  [ -n "$version" ] || err "could not resolve latest version from $RELEASES_API (set PEV_VERSION to override)"
fi
case "$version" in
  v*) ;;
  *) err "PEV_VERSION must start with 'v' (got: $version)" ;;
esac
version_no_v="${version#v}"

# -- Destination ---------------------------------------------------------------
if [ -n "${PEV_INSTALL_DIR:-}" ]; then
  dest="$PEV_INSTALL_DIR"
elif [ "$(id -u)" = "0" ]; then
  dest="/usr/local/bin"
else
  dest="${HOME}/.local/bin"
fi

# -- Download + verify + install ----------------------------------------------
tmp=$(mktemp -d) || err "could not create temp dir"
trap 'rm -rf "$tmp"' EXIT INT TERM

binary_name="pev_linux_${arch}"
checksums_name="pev_${version_no_v}_checksums.txt"

# cosign 3.x (shipped with the release since the --new-bundle-format switch)
# folds the signature AND signing certificate into a single Sigstore bundle.
# The release publishes only this .bundle, not the legacy .sig + .pem pair.
bundle_name="${checksums_name}.bundle"

log "downloading $binary_name ($version)"
curl -fsSL "${RELEASES_DOWNLOAD}/${version}/${binary_name}"   -o "${tmp}/${binary_name}"
log "downloading $checksums_name"
curl -fsSL "${RELEASES_DOWNLOAD}/${version}/${checksums_name}" -o "${tmp}/${checksums_name}"

# Cosign-verify the checksums file before trusting any SHA-256 in it. Without
# this step, an attacker with control of the GitHub release's binary asset
# could ship matching checksums and the SHA-256 check would happily pass.
if command -v cosign >/dev/null 2>&1; then
  log "downloading $bundle_name"
  if ! curl -fsSL "${RELEASES_DOWNLOAD}/${version}/${bundle_name}" -o "${tmp}/${bundle_name}"; then
    err "cosign bundle ${bundle_name} not found at release — refusing to install"
  fi
  log "verifying cosign signature on ${checksums_name}"
  cosign verify-blob \
    --bundle "${tmp}/${bundle_name}" \
    --new-bundle-format \
    --certificate-identity-regexp "https://github.com/${REPO}/.github/workflows/release.yml@.*" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    "${tmp}/${checksums_name}" >/dev/null \
    || err "cosign verification failed for ${checksums_name} — refusing to install"
elif [ "${PEV_REQUIRE_COSIGN:-0}" = "1" ]; then
  err "cosign not on PATH and PEV_REQUIRE_COSIGN=1 — install cosign or unset the variable"
else
  vlog "warning: cosign not on PATH; falling back to SHA-256 only (set PEV_REQUIRE_COSIGN=1 to hard-fail)"
fi

# sha256sum -c needs the checksum line in its CWD-relative form. We grep the
# row for our binary, then run -c against just that row.
expected_line=$(grep "  ${binary_name}\$" "${tmp}/${checksums_name}" || true)
[ -n "$expected_line" ] || err "no checksum entry for ${binary_name} in ${checksums_name}"
( cd "$tmp" && printf '%s\n' "$expected_line" | sha256sum -c - >/dev/null ) \
  || err "checksum mismatch for ${binary_name} — refusing to install"

mkdir -p "$dest"
install -m 0755 "${tmp}/${binary_name}" "${dest}/pev"

log "installed pev ${version} to ${dest}/pev"

# Hint about PATH if the install dir is not on PATH. Print the exact,
# copy-paste command to run pev now (the chained `... && pev assess` in the
# docs fails here because this shell's PATH won't pick up ${dest} until a new
# login), plus the export to make `pev` resolve permanently.
case ":${PATH}:" in
  *:"${dest}":*) ;;
  *)
    log "note: ${dest} is not on PATH."
    log "  run pev now with: ${dest}/pev assess"
    log "  or add it to PATH: export PATH=\"${dest}:\$PATH\""
    ;;
esac
