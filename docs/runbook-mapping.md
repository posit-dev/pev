# Runbook prereq → check ID mapping

Each row in the standard customer onboarding runbook ("Prerequisites" tab) maps to one or more pev built-in checks. **pev's scope is strictly pre-install prerequisites.** Validation of an *installed* Posit product (license activation, rserver.conf SSL config, deployed-content tests) is the responsibility of [`vip`](https://github.com/posit-dev/vip), not pev.

This file is the contract — when a runbook prereq lands here, the corresponding YAML check exists in the catalog and is exercised by CI.

Every FAIL is treated as worth investigating; pev has no graduated severity
tiers beyond a single advisory rung (WARN — visible but not blocking; see
`docs/check-authoring.md`). The catalog is common-only — language and identity
checks apply to any host running Workbench, Connect, or Package Manager.

## Shared infrastructure (all products)

| Runbook prereq | pev check ID(s) |
|---|---|
| Linux VM provisioned (CPU/RAM/disk) | `sizing.workbench.minimum`, `sizing.connect.minimum`, `sizing.packagemanager.minimum` |
| OS supported | `os.supported` |
| CPU architecture | `os.architecture.amd64-or-arm64` |
| OS runtime (file limits, cgroups v2, UTF-8 locale) | `os.ulimit.nofile`, `os.cgroups.v2-available`, `os.locale.utf8` |
| RHEL subscription attached | `os.subscription-manager.attached` |
| /tmp and /home mounted exec | `os.tmp.exec`, `os.home.exec` |
| Home share (NFS) is writable, persistent, ACL-capable, no uid squash | `os.home.root-writable`, `os.home.nfs.no-root-squash`, `os.home.user-uid-preserved`, `storage.home.share.mounted`, `storage.home.fstab-persistent`, `storage.acl.posix.home-and-local`, `storage.acl.nfsv4`, `pkg.nfs-utils.installed` |
| Hostname / FQDN resolves | `net.dns.fqdn-resolves-to-self` |
| Default ports bindable | `net.bind.workbench-8787`, `net.bind.connect-3939`, `net.bind.packagemanager-4242` |
| Outbound networking — Posit CDNs | `net.egress.cdn-rstudio`, `net.egress.cdn-posit`, `net.egress.download2-rstudio` |
| Outbound networking — license activation | `net.egress.license-activation` |
| Outbound networking — package mirrors | `net.egress.packagemanager-posit-ping`, `net.egress.p3m`, `net.egress.cran`, `net.egress.bioconductor`, `net.egress.pypi`, `net.egress.pypi-files` |
| Distro package manager healthy | `pkg-mgr.apt.update`, `pkg-mgr.apt.repolist-fresh`, `pkg-mgr.dnf.repolist`, `pkg-mgr.dnf.makecache` |
| System dependencies (build deps for R/Python packages) | `pkg.openssl-dev`, `pkg.libcurl-dev`, `pkg.libxml2-dev` |
| Posit Pro Drivers installed (when declared) | `pkg.pro-drivers.installed` |
| Security posture (umask, SELinux/AppArmor, firewalls) | `sec.umask.permissive`, `sec.selinux.not-enforcing`, `sec.apparmor.not-enabled`, `sec.firewalld.inactive`, `sec.firewalld.posit-ports-allowed`, `sec.iptables.inactive`, `sec.iptables.posit-ports-allowed`, `sec.nftables.inactive`, `sec.nftables.posit-ports-allowed` |
| R installed under `/opt/R/<version>` | `lang.r.versioned-install` |
| Python installed under `/opt/python/<version>` | `lang.python.versioned-install` |
| Quarto available on PATH | `lang.quarto.present` |
| Unprivileged user can install renv / uv venv / pip venv | `lang.r.renv-user-install`, `lang.python.uv-venv`, `lang.python.pip-venv` |
| IdP metadata or discovery URL reachable | `lang.idp.metadata` |
| Customer-supplied PAM/SSO test user resolves | `auth.pam.users-resolvable` |
| Customer-declared PostgreSQL host is reachable | `db.postgres.reachable` |

## Package Manager

| Runbook prereq | pev check ID(s) |
|---|---|
| SSL certificate for PPM hostname (customer-supplied) | `ppm.ssl.cert-key-match` |
| Outbound to Posit Package Service | `ppm.egress.sync` |
| Sufficient disk for package cache | `sizing.packagemanager.minimum` |

## Connect

| Runbook prereq | pev check ID(s) |
|---|---|
| SSL certificate for Connect hostname (customer-supplied) | `connect.ssl.cert-key-match` |
| Outbound email server access | `connect.smtp.reachable` |

## Workbench

| Runbook prereq | pev check ID(s) |
|---|---|
| SSL certificate for Workbench hostname (customer-supplied) | `workbench.ssl.cert-key-match` |
| Desired editors identified | _(human-only checklist)_ |

## Out of scope (handled by `vip`, not pev)

These were intentionally removed from the catalog because they validate state that only exists *after* a Posit product has been installed, which is `vip`'s job:

- `*.license.activated` — `rstudio-server license-manager status` and the Connect/PPM equivalents
- `*.license.file-present` — globbing `/var/lib/rstudio-*/*.lic`
- `*.binary.present` — `/usr/lib/rstudio-server/bin/rserver`, `/opt/rstudio-connect/bin/rstudio-connect`, `/opt/rstudio-pm/bin/rstudio-pm`
- `workbench.ssl.config` — parsing `/etc/rstudio/rserver.conf` for `ssl-enabled=1`
- Anything that depends on `rstudio-server`, `rstudio-connect`, or `rstudio-pm` being installed

## When to add a row

Whenever you add a YAML check whose origin is a runbook prereq, add a row here in the same PR. A check is in scope for pev only if a customer can satisfy it *before* installing any Posit product. If satisfaction requires the product to already be installed, it belongs in `vip`.
