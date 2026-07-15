# pev — Posit Environment Validator

Assess Linux readiness before installing **Posit Workbench**, **Posit Connect**, and **Posit Package Manager**. One static binary, no runtime dependencies on the target system, runs as root or non-root, writes a Markdown report and a JSON sidecar that diffs cleanly between runs.

[![ci](https://github.com/posit-dev/pev/actions/workflows/ci.yml/badge.svg)](https://github.com/posit-dev/pev/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/posit-dev/pev)](https://github.com/posit-dev/pev/releases/latest)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

---

## Why pev exists

Today, the prereq verification call before a Posit install is a manual checklist exercise driven by a runbook, interpreted live by an SE on a Webex call. That process is slow, inconsistent across SEs, and produces no shareable artifact. pev replaces the manual pass with an automated assessment that defaults to discovery (it shells out to the same OS commands a Linux admin would type at the terminal) and produces a report identifying every issue *before* the install session — turning the prereq meeting from "let me poke around your box" into "let's review the report you ran yesterday." Every failed check is worth investigating; pev does not try to predict which failures the customer can defer.

## Scope

**pev assesses the host's readiness BEFORE any Posit product is installed.** It checks OS support, sizing, network egress, system packages, customer-supplied SSL cert/key validity, R/Python/Quarto presence at the expected paths, IdP and SMTP reachability — every prereq the customer has to satisfy ahead of the install session.

**pev explicitly does NOT validate installed products.** License activation (`rstudio-server license-manager status`), product binary presence, post-install config files (`rserver.conf`, `rstudio-connect.gcfg`, `rstudio-pm.gcfg`), and content-deployment smoke tests are the responsibility of [`posit-dev/vip`](https://github.com/posit-dev/vip). If a check requires a Posit product to be installed first, it belongs in `vip`, not here.

## Install

### One-line install (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/posit-dev/pev/main/scripts/install.sh | sh && pev assess
```

Detects amd64 / arm64, downloads the latest release, verifies the SHA-256 against the published `pev_<version>_checksums.txt`, and installs to `~/.local/bin/pev` (or `/usr/local/bin/pev` when run as root). Re-running upgrades in place.

> **Do not pipe the installer into `pev`** (`curl ... | sh | pev assess`). That makes `pev`'s stdin the installer's pipe rather than your terminal, so `pev` detects no TTY and silently runs in `--yes` mode — accepting every discovered default without prompting. To install and then assess in one line, join them with `&&` so `pev` keeps your terminal:
>
> ```bash
> curl -fsSL https://raw.githubusercontent.com/posit-dev/pev/main/scripts/install.sh | sh && pev assess
> ```
>
> For unattended/CI runs, accept the defaults explicitly with `pev assess --non-interactive` instead of relying on TTY detection.

Pin a version or override the destination:

```bash
PEV_VERSION=v0.0.2 PEV_INSTALL_DIR=/opt/bin \
  curl -fsSL https://raw.githubusercontent.com/posit-dev/pev/main/scripts/install.sh | sh
```

If your security review requires reading the script before execution, save and inspect first:

```bash
curl -fsSLo install.sh https://raw.githubusercontent.com/posit-dev/pev/main/scripts/install.sh
less install.sh
sh install.sh
```

### Verified install (cosign)

Releases are signed with [cosign](https://github.com/sigstore/cosign) keyless OIDC. For environments that require manual signature verification:

```bash
VERSION=v0.5.0  # or whichever release you want
# Download the binary under its published name so it matches the checksum entry.
curl -fsSL https://github.com/posit-dev/pev/releases/download/${VERSION}/pev_linux_amd64                          -o pev_linux_amd64
curl -fsSL https://github.com/posit-dev/pev/releases/download/${VERSION}/pev_${VERSION#v}_checksums.txt           -o checksums.txt
curl -fsSL https://github.com/posit-dev/pev/releases/download/${VERSION}/pev_${VERSION#v}_checksums.txt.bundle    -o checksums.txt.bundle

cosign verify-blob \
  --bundle checksums.txt.bundle \
  --new-bundle-format \
  --certificate-identity-regexp 'github.com/posit-dev/pev' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

sha256sum --check --ignore-missing checksums.txt
chmod +x pev_linux_amd64 && mv pev_linux_amd64 pev && ./pev version
```

## Quickstart

```bash
./pev discover                       # what facts pev would assume — no checks run
./pev assess                         # Markdown to screen + Markdown + JSON to ./
./pev assess --out-dir /var/log/pev  # land artifacts in a stable location
./pev diff a.json b.json             # exit 1 on regressions
```

A trimmed report excerpt:

```
# pev report — db-prod-01 — 2026-06-03 14:22:05 UTC

## Summary
| Pass | Fail | Skip | Unknown |
|---:|---:|---:|---:|
|   53 |    5 |    3 |    0 |

**5 failure(s)** — investigate before proceeding.
```

## What it checks

Every built-in check maps to an authoritative Posit doc and (where applicable) to a row in the customer prereq runbook. Run `pev list-checks` to dump the catalog at any time. Every FAIL is worth investigating; pev does not classify findings into tiers.

The full built-in catalog (run `pev list-checks` for the live version):

### Operating System & Architecture

| ID | Title |
|---|---|
| `os.supported` | Operating system is supported by Posit professional products |
| `os.architecture.amd64-or-arm64` | CPU architecture is amd64 or arm64 |
| `os.ulimit.nofile` | Open-file limit (ulimit -Hn) is sufficient for Posit products |
| `os.cgroups.v2-available` | cgroups v2 (unified hierarchy) is available |
| `os.locale.utf8` | A UTF-8 locale is configured |
| `os.subscription-manager.attached` | RHEL subscription-manager reports an attached, current entitlement |

### Filesystem & Home Mount

| ID | Title |
|---|---|
| `os.tmp.exec` | /tmp is mounted exec (not noexec) |
| `os.home.exec` | /home is mounted exec (not noexec) |
| `os.home.root-writable` | Root can create and own a directory under /home |
| `os.home.nfs.no-root-squash` | NFS-mounted /home is exported with no_root_squash to this host |
| `os.home.user-uid-preserved` | Files in $HOME are owned by the running user (no NFS uid squash) |
| `storage.home.share.mounted` | Customer-supplied home NFS share is mounted writable |
| `storage.home.fstab-persistent` | Customer-supplied home share survives reboot (in /etc/fstab) |
| `storage.acl.posix.home-and-local` | POSIX ACLs are supported on /home and local mounts |
| `storage.acl.nfsv4` | NFSv4 ACLs are supported on every NFSv4 mount |
| `pkg.nfs-utils.installed` | NFS client package is installed when /home is on NFS |

### Sizing

| ID | Title |
|---|---|
| `sizing.workbench.minimum` | Host meets Workbench minimum sizing (2 cores / 4 GB / 100 GB) |
| `sizing.connect.minimum` | Host meets Connect minimum sizing (2 cores / 4 GB / 100 GB) |
| `sizing.packagemanager.minimum` | Host meets Package Manager minimum sizing (2 cores / 2 GB / 200 GB) |

### Networking — DNS, Bind, Egress

| ID | Title |
|---|---|
| `net.dns.fqdn-resolves-to-self` | Host's FQDN (or supplied Workbench URL) resolves to a local IP |
| `net.bind.workbench-8787` | Workbench default port 8787 is bindable |
| `net.bind.connect-3939` | Connect default port 3939 is bindable |
| `net.bind.packagemanager-4242` | Package Manager default port 4242 is bindable |
| `net.egress.cdn-rstudio` | HTTPS GET cdn.rstudio.com Pro Drivers installer returns 200 |
| `net.egress.cdn-posit` | HTTPS GET cdn.posit.co PPM installer returns 200 |
| `net.egress.download2-rstudio` | HTTPS GET download2.rstudio.org returns 200 |
| `net.egress.license-activation` | HTTPS GET www.wyday.com returns 200 (license activation) |
| `net.egress.packagemanager-posit-ping` | HTTPS GET packagemanager.posit.co/__ping__ returns 200 |
| `net.egress.p3m` | HTTPS GET p3m.dev/__ping__ returns 200 (Posit Public Package Manager) |
| `net.egress.cran` | HTTPS GET cran.r-project.org PACKAGES index returns 200 |
| `net.egress.bioconductor` | HTTPS GET bioconductor.org returns 200 |
| `net.egress.pypi` | HTTPS GET pypi.org/simple/ returns 200 |
| `net.egress.pypi-files` | HTTPS GET files.pythonhosted.org returns 200 |

### Database

| ID | Title |
|---|---|
| `db.postgres.reachable` | Customer-declared PostgreSQL host is reachable |

### Security

| ID | Title |
|---|---|
| `sec.umask.permissive` | Login umask permits readable site-packages (umask <= 0027) |
| `sec.selinux.not-enforcing` | SELinux is not Enforcing |
| `sec.apparmor.not-enabled` | AppArmor is not enabled |
| `sec.firewalld.inactive` | firewalld is not active (or rules permit Posit ports) |
| `sec.iptables.inactive` | iptables service is not active (or rules permit Posit ports) |
| `sec.nftables.inactive` | nftables service is not active (or rules permit Posit ports) |
| `sec.firewall.workbench-port` | Firewall permits inbound access to Workbench |
| `sec.firewall.connect-port` | Firewall permits inbound access to Connect |
| `sec.firewall.packagemanager-port` | Firewall permits inbound access to Package Manager |

### Distro Package Manager Health

| ID | Title |
|---|---|
| `pkg-mgr.apt.update` | apt-get update succeeds |
| `pkg-mgr.apt.repolist-fresh` | apt repository metadata is recent (< 30 days) |
| `pkg-mgr.dnf.repolist` | dnf repolist succeeds |
| `pkg-mgr.dnf.makecache` | dnf makecache succeeds |

### Build-Dep System Packages

| ID | Title |
|---|---|
| `pkg.openssl-dev` | openssl development headers installed |
| `pkg.libcurl-dev` | libcurl development headers installed |
| `pkg.libxml2-dev` | libxml2 development headers installed |
| `pkg.pro-drivers.installed` | Posit Pro Drivers are installed (when declared) |

### Languages & Identity

These checks apply to any host running Workbench, Connect, or Package Manager.
The user-install checks run as the unprivileged user pev auto-detects (or
prompts for, if running as root) and use the latest discovered R / Python
under `/opt`.

| ID | Title |
|---|---|
| `lang.r.versioned-install` | At least one R install at `/opt/R/<version>/bin/R` |
| `lang.r.renv-user-install` | Unprivileged user can install renv with the latest R |
| `lang.python.versioned-install` | At least one Python install at `/opt/python/<version>/bin/python3` |
| `lang.python.uv-venv` | Unprivileged user can create a uv venv with the latest Python |
| `lang.python.pip-venv` | Unprivileged user can create a venv via `python -m venv` + pip install |
| `lang.quarto.versioned-install` | At least one Quarto install at `/opt/quarto/<version>/bin/quarto` |
| `lang.quarto.path` | Quarto on PATH |
| `lang.idp.metadata` | IdP metadata or discovery URL is reachable |
| `auth.pam.users-resolvable` | Customer-supplied PAM/SSO test user resolves through nsswitch |

### Per-product (customer-supplied; opt-in)

| ID | Title |
|---|---|
| `workbench.ssl.cert-key-match` | Workbench SSL certificate and key are paired |
| `connect.ssl.cert-key-match` | Connect SSL certificate and key are paired |
| `connect.smtp.reachable` | SMTP server reachable from Connect host |
| `ppm.ssl.cert-key-match` | Package Manager SSL certificate and key are paired |
| `ppm.egress.sync` | Package Manager can reach the Posit Package Service |

Anything that requires a Posit product to be already installed (license-manager status, parsing rserver.conf, etc.) is **out of scope** — that's `vip`'s job. See [docs/runbook-mapping.md](docs/runbook-mapping.md) for the full prereq → check ID table and the explicit out-of-scope list.

## Permissions

pev runs as root or non-root. The pre-install catalog is mostly readable by any user (DNS, HTTP, /opt/* listings, sysctl). Checks that need root (e.g. reading customer-supplied SSL keys at mode 0600) are gated by `requires_root: true` and emit `SKIPPED (requires root)` when run as a normal user. The run never aborts.

```bash
sudo ./pev assess         # full coverage, including SSL-key checks
./pev assess              # everything that doesn't need root
```

## Custom checks

Drop a YAML file in `~/.config/pev/checks.d/` (loaded automatically) or pass `--checks-file path.yaml`:

```yaml
schema_version: 1
checks:
  - id: mycorp.cron.installed
    title: Internal cron job for log rotation is installed
    tags: [internal]
    primitive: file
    why: |
      Our prod boxes ship a custom logrotate.d unit. If it's missing, /var fills up.
    with:
      path: /etc/logrotate.d/posit-mycorp
      must_exist: true
```

`pev lint-checks file.yaml` validates a pack before you ship it. See [docs/check-authoring.md](docs/check-authoring.md) for the full schema and [docs/primitives.md](docs/primitives.md) for the available primitives.

## Reports & diffs

Every `pev assess` writes three files:

- `pev-report-<host>-<TS>.md` — human Markdown report
- `pev-report-<host>-<TS>.json` — machine sidecar (stable, sorted, schema-versioned)
- `pev-log-<TS>.log` — logrus JSON-lines for debugging

`pev diff a.json b.json` classifies every check as **regression** (PASS→FAIL/UNKNOWN), **improvement** (FAIL→PASS), **status changed**, **added**, **removed**, or **evidence-only changed**. Exit code 1 iff regressions exist — perfect for a CI gate during install runbook automation.

## Supported OS

| OS                       | Status                  | Notes                                                |
|--------------------------|-------------------------|------------------------------------------------------|
| Ubuntu 22.04             | Supported               |                                                       |
| Ubuntu 24.04             | Supported               |                                                       |
| Ubuntu 20.04             | **Unsupported**         | EOL across all three Posit products                   |
| RHEL 8 / 9 / 10          | Supported               | UBI requires registry auth                            |
| Alma Linux 8 / 9 / 10    | Supported               | RHEL-family rebuild; collapsed onto `rhel-<major>` ID |
| Rocky Linux 8 / 9 / 10   | Supported               | Same as Alma                                          |

CI exercises Ubuntu 22.04, Ubuntu 24.04, Alma 9, and Alma 10 containers. Real RHEL is validated pre-release on customer-representative VMs.

## Building from source

```bash
git clone https://github.com/posit-dev/pev
cd pev
make build       # CGO_ENABLED=0 -> ~12 MB static binary
make test        # go test ./... -race -shuffle=on
make lint        # golangci-lint
make snapshot    # goreleaser release --snapshot --clean
make e2e         # local docker matrix (Ubuntu 22/24, Alma 9/10)
```

Requires Go 1.22+.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). PRs use [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `chore:`); the changelog is generated from those prefixes.

## Security

See [SECURITY.md](SECURITY.md). Verify release artifacts with cosign as shown above. Report vulnerabilities privately via GitHub Security Advisories.

## Maintainers

Posit Solutions Engineering. File bugs and feature requests through [GitHub Issues](https://github.com/posit-dev/pev/issues); discussion happens in [GitHub Discussions](https://github.com/posit-dev/pev/discussions).
