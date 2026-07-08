#!/usr/bin/env bash
# Local Posit-image runner: builds pev, mounts it into the pinned official
# Workbench and Connect images, runs `pev assess`, and asserts that no
# fail/unknown checks appear outside the per-product allowlist below.
#
# The image digests and allowlists must mirror those in
# .github/workflows/posit-images.yml. Refresh digests with
# scripts/refresh-posit-images.sh.
set -euo pipefail

cd "$(dirname "$0")/../.."

# Posit's official, fully-installed product images. Pinned by repository
# digest (sha256) so an upstream rebuild cannot silently change pev's test
# results. Refresh with scripts/refresh-posit-images.sh.
WORKBENCH_IMAGE="docker.io/posit/workbench@sha256:3d76d4b38651287d158c59fe82fc7e7d5bb8de1a1178a922bd50a90892d0d87a"
CONNECT_IMAGE="docker.io/posit/connect@sha256:8de3d13ba5fdd46576ecdcbdff43420305407192d2c0de462545eba1ba082de9"

# Known-residual fail/unknown checks per product. Keep in sync with
# .github/workflows/posit-images.yml. Anything fail/unknown NOT in the
# allowlist is treated as a regression and fails the run.
WORKBENCH_ALLOWED=$(cat <<'EOF'
storage.acl.posix.home-and-local
EOF
)
CONNECT_ALLOWED=$(cat <<'EOF'
pkg.libcurl-dev
pkg.libxml2-dev
EOF
)

CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o pev ./

run_one() {
  local label="$1" image="$2" product="$3" allowed="$4"
  local outdir
  outdir=$(mktemp -d -t "pev-posit-${label}.XXXX")
  echo "=========================================="
  echo "  ${label}: ${image}"
  echo "=========================================="
  docker run --rm \
    --entrypoint="" \
    -v "$PWD/pev":/usr/local/bin/pev:ro \
    -v "$outdir":/out \
    "$image" \
    pev assess --non-interactive --products "$product" \
      --out-dir /out --skip-tags egress,ssl || true

  local report
  report=$(ls "$outdir"/pev-report-*.json | head -1)
  echo "report: $report"
  jq '.summary' "$report"

  local actual unexpected
  actual=$(jq -r '.results[] | select(.status=="fail" or .status=="unknown") | .id' "$report" | sort -u)
  unexpected=$(comm -23 <(printf '%s\n' "$actual") <(printf '%s\n' "$allowed" | awk 'NF' | sort -u))
  if [[ -n "$unexpected" ]]; then
    echo "FAIL: ${label} produced unexpected fail/unknown check(s):"
    while IFS= read -r id; do
      [[ -z "$id" ]] && continue
      jq -r --arg id "$id" '.results[] | select(.id==$id) | "  - \(.status)\t\(.id): \(.reason // "")"' "$report"
    done <<<"$unexpected"
    return 1
  fi
  echo "OK: ${label} — all fail/unknown checks were on the allowlist"
}

run_one workbench "$WORKBENCH_IMAGE" workbench "$WORKBENCH_ALLOWED"
run_one connect   "$CONNECT_IMAGE"   connect   "$CONNECT_ALLOWED"
