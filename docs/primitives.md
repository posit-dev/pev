# Primitives reference

A check's `primitive:` field selects which executor runs the check. The
catalog registers fifteen primitives at init() time:

- system probes: `cmd`, `file`, `dir`, `pkg`, `sysctl`, `sizing`
- network probes: `port`, `dns`, `http`, `bind`, `postgres`
- service-state probes: `systemd`, `selinux`, `apparmor`
- TLS validation: `x509`

## `cmd`

Run a shell command via `/bin/sh -c` and match its output.

```yaml
primitive: cmd
with:
  cmd: "rstudio-server license-manager status"
  expect_exit: 0
  expect_stdout_regex: 'Status:\s+Activated'
  expect_stderr_regex: ''             # optional
  timeout_seconds: 30
```

| Key | Default | Notes |
|---|---|---|
| `cmd` | — | required |
| `expect_exit` | unset | when set, `exit code != value` ⇒ fail |
| `expect_stdout_regex` | unset | Go regex; mismatch ⇒ fail |
| `expect_stderr_regex` | unset | Go regex; mismatch ⇒ fail |
| `skip_exit` | unset | when set, `exit code == value` ⇒ skip (script says "not applicable on this host"); first non-blank stdout/stderr line becomes the reason |
| `timeout_seconds` | 30 | command is killed and check fails |

## `file`

Single-file inspection.

```yaml
primitive: file
with:
  path: /etc/rstudio/rserver.conf
  must_exist: true
  mode_max: "0644"            # fail if mode is more permissive
  contains_regex: "(?m)^ssl-enabled=1"
```

## `dir`

Directory presence and optional glob match count.

```yaml
primitive: dir
with:
  path: /var/lib/rstudio-server
  must_exist: true
  glob: "*.lic"
  glob_min_matches: 1
```

## `port`

Built-in `nc -vz`. Opens a TCP connection.

```yaml
primitive: port
with:
  host: "{{ .Inputs.connect_smtp_host }}"
  port: 587
  timeout_seconds: 5
```

## `dns`

Forward resolution with an optional reverse-match check.

```yaml
primitive: dns
with:
  name: "{{ .Inputs.workbench_hostname }}"
  type: A
  must_resolve: true
  reverse_match_hostname: false
  timeout_seconds: 5
```

## `http`

GET (or configured method) and accept any 2xx by default.

```yaml
primitive: http
with:
  url: https://cdn.posit.co/
  method: GET
  timeout_seconds: 5
  accept_status: [200, 204, 301, 302]   # optional; otherwise any 2xx
```

## `x509`

Validate a PEM certificate: chain against the system trust store, hostname match, expiration cushion, and RSA cert↔key pairing via modulus comparison (no openssl shell-out needed).

```yaml
primitive: x509
with:
  cert_path: "{{ .Inputs.workbench_cert }}"
  key_path:  "{{ .Inputs.workbench_key }}"
  verify_chain: true
  match_hostname: "{{ .Inputs.workbench_hostname }}"
  not_after_min_days: 30
```

EC private keys are not supported in v1 — file an issue if you need one.

Outcomes: an empty `cert_path` SKIPs (the SE declined the cert prompt). A real
chain/hostname/expiry/pairing failure is a FAIL. But when `key_path` or
`match_hostname` is present in the `with:` payload yet expands to an empty
string — the cert was supplied but its partner key or hostname was not — that
verification cannot run, so the result is an advisory **WARN** naming what
could not be confirmed rather than a misleading PASS. Omitting the key entirely
(the property was never requested) still PASSes on the cert alone.

## `pkg`

Distro package presence via `dpkg-query` (Ubuntu) or `rpm` (RHEL family).

```yaml
primitive: pkg
with:
  manager: auto                # auto | dpkg | rpm
  any_of: [libssl-dev, openssl-devel]   # any present ⇒ pass
  # all_of: [pkg1, pkg2]                # all present ⇒ pass
```

`auto` picks `dpkg` on Ubuntu and `rpm` on RHEL family. Provide both Ubuntu and RHEL package names in `any_of` for cross-distro checks.

## `systemd`

Systemd unit state. Replaces the older `proc` primitive (deleted; same
behavior was achievable here with `expect: active`).

```yaml
primitive: systemd
with:
  unit: rstudio-server
  expect: active        # one of: installed | active | inactive | absent
  timeout_seconds: 5
```

| Key | Default | Notes |
|---|---|---|
| `unit` | — | required, no `.service` suffix |
| `expect` | `active` | `installed`/`active`/`inactive`/`absent` |
| `timeout_seconds` | 5 | per `systemctl` call |

## `bind`

Open a TCP listener on `host:port` and immediately close it. PASS means
the port is bindable; FAIL covers `EADDRINUSE`, missing
`CAP_NET_BIND_SERVICE` for ports below 1024, or SELinux denies. When the
expected listener IS already running (a Posit product on its install
port), the `owned_by` allowlist demotes the in-use error to PASS.

```yaml
primitive: bind
with:
  host: 0.0.0.0          # default; override with the bind address Posit will use
  port: 8787
  owned_by: [rstudio-server, rstudio-pm]
  timeout_seconds: 2
```

## `postgres`

TCP-connect probe of an external PostgreSQL host. Does NOT issue a SQL
query — auth/role/database errors surface during the install. Empty
host SKIPs (the SE didn't supply postgres inputs).

```yaml
primitive: postgres
with:
  host: "{{ .Inputs.postgres_host }}"
  port: 5432
  timeout_seconds: 5
```

## `selinux`

Reports SELinux status by parsing `getenforce`. `expect`: `any` |
`disabled` | `permissive` | `enforcing` | `absent` | `not_enforcing`.

```yaml
primitive: selinux
with:
  expect: not_enforcing       # PASS for permissive | disabled | absent
  timeout_seconds: 5
```

## `apparmor`

Reports AppArmor kernel-module status from
`/sys/module/apparmor/parameters/enabled`. `expect`: `any` | `enabled` |
`disabled` | `absent` | `not_enabled`.

```yaml
primitive: apparmor
with:
  expect: not_enabled         # PASS for disabled | absent
  timeout_seconds: 5
```

## `sysctl`

Read a kernel parameter from `/proc/sys/<dotted/path>`.

```yaml
primitive: sysctl
with:
  key: net.ipv4.tcp_keepalive_time
  expect_int_min: 60
  # expect_equals: "1"
```

## `sizing`

Check host capacity against thresholds. Reads from `HostFacts` (no shell-out).

```yaml
primitive: sizing
with:
  cpus_min: 4
  mem_gb_min: 8
  disk_gb_min: { /: 100, /var: 50 }
```

Disk values are *available* GB to non-root (matches `df -h`'s Available column).
