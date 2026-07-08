#!/usr/bin/env bash
# Refresh the pinned posit/workbench and posit/connect digests used by the
# Posit-image CI test. Pulls the requested tag, prints the resolved
# repository digest, and tells the human where to paste it.
#
# Usage:
#   scripts/refresh-posit-images.sh                       # use defaults below
#   scripts/refresh-posit-images.sh 2026.04.0 2026.05.1   # explicit tags
set -euo pipefail

WORKBENCH_TAG="${1:-2026.04.0}"
CONNECT_TAG="${2:-2026.05.1}"

resolve() {
  local repo="$1" tag="$2"
  docker pull -q "${repo}:${tag}" >/dev/null
  docker image inspect "${repo}:${tag}" -f '{{ index .RepoDigests 0 }}'
}

wb=$(resolve docker.io/posit/workbench "$WORKBENCH_TAG")
cn=$(resolve docker.io/posit/connect   "$CONNECT_TAG")

cat <<EOF
Resolved digests:
  workbench: ${wb}
  connect:   ${cn}

Update the following files to match (search for "docker.io/posit/"):
  - .github/workflows/posit-images.yml
  - test/posit-images/run.sh

Then run \`bash test/posit-images/run.sh\` locally. If the new images
introduce new fail/unknown checks, decide whether each is a real
regression or a stable allowlist entry, and update the allowlists in
both posit-images.yml and test/posit-images/run.sh before opening the PR.
EOF
