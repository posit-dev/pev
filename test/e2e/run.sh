#!/usr/bin/env bash
# Local e2e runner: builds pev, then runs it inside Ubuntu 22/24 + Alma 9/10
# containers, exercising both root and non-root paths. Mirrors the CI workflow
# in .github/workflows/e2e.yml so behavior matches between local and CI.
set -euo pipefail

cd "$(dirname "$0")/../.."

CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o pev ./

IMAGES=(ubuntu:22.04 ubuntu:24.04 almalinux:9 almalinux:10)

for img in "${IMAGES[@]}"; do
  echo "=========================================="
  echo "  $img"
  echo "=========================================="
  docker run --rm \
    -v "$PWD":/work -w /work \
    "$img" \
    bash -c '
      set -e
      if command -v apt-get >/dev/null; then
        apt-get update -qq
        apt-get install -yqq --no-install-recommends ca-certificates curl iproute2 procps openssl >/dev/null
      elif command -v dnf >/dev/null; then
        # util-linux ships runuser/su, which the minimal alma:10 image omits.
        dnf -y --allowerasing install ca-certificates curl iproute procps-ng openssl which util-linux >/dev/null
      fi

      if ! command -v python3 >/dev/null; then
        if command -v apt-get >/dev/null; then apt-get install -yqq --no-install-recommends python3 >/dev/null
        elif command -v dnf >/dev/null; then dnf -y install python3 >/dev/null
        fi
      fi

      ./pev version
      ./pev discover --format json | head -40

      mkdir -p /tmp/pev-root
      ./pev assess --non-interactive --products workbench,connect,packagemanager \
        --out-dir /tmp/pev-root --skip-tags egress \
        && root_rc=0 || root_rc=$?
      echo "root assess exited $root_rc"
      ls /tmp/pev-root/
      root_json=$(ls /tmp/pev-root/pev-report-*.json | head -1)
      python3 test/e2e/validate-report.py root "$root_json"

      cp ./pev /tmp/pev-bin && chmod 0755 /tmp/pev-bin
      mkdir -p /tmp/pev-nobody && chmod 0777 /tmp/pev-nobody
      if command -v runuser >/dev/null; then
        runuser -u nobody -- /tmp/pev-bin assess --non-interactive \
          --products workbench,connect,packagemanager \
          --out-dir /tmp/pev-nobody --skip-tags egress \
          && nr_rc=0 || nr_rc=$?
      else
        su -s /bin/sh nobody -c "/tmp/pev-bin assess --non-interactive --products workbench,connect,packagemanager --out-dir /tmp/pev-nobody --skip-tags egress" \
          && nr_rc=0 || nr_rc=$?
      fi
      echo "non-root assess exited $nr_rc"
      ls /tmp/pev-nobody/
      nr_json=$(ls /tmp/pev-nobody/pev-report-*.json | head -1)
      python3 test/e2e/validate-report.py nonroot "$nr_json" "$root_json"
    '
done
