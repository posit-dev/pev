#!/usr/bin/env bash
# Diagnostic checks to investigate suspect pev assess failures on a VM.
# Run as root on the target host.
set +e

echo "=== 1) port 4242 owner ==="
ss -ltnp 'sport = :4242'

echo; echo "=== 2) setfacl on /home ==="
which setfacl || echo "setfacl MISSING"
t=/home/.pev-acltest.$$
: > "$t" && setfacl -m u:nobody:r "$t" && getfacl "$t" | grep '^user:nobody:'
rm -f "$t"
findmnt /home

echo; echo "=== 3) ubuntu user + home ==="
getent passwd ubuntu
ls -ld /home/ubuntu 2>&1
stat -c '%U %A %n' /tmp /home /home/ubuntu 2>&1

echo; echo "=== 4) uv venv as ubuntu ==="
runuser -u ubuntu -- sh -c 'cd && uv --version && D=$(mktemp -d) && uv venv --python /opt/python/cpython-3.12.12-linux-x86_64-gnu/bin/python "$D/.venv" 2>&1; rm -rf "$D"'

echo; echo "=== 5) renv install as ubuntu ==="
runuser -u ubuntu -- sh -c 'cd && D=$(mktemp -d) && /opt/R/4.5.2/bin/R --vanilla --no-save --slave -e "install.packages(\"renv\", repos=\"https://packagemanager.posit.co/cran/latest\", lib=\"$D\"); library(\"renv\", lib.loc=\"$D\")" 2>&1 | tail -20; rm -rf "$D"'

echo; echo "=== 6) packages ==="
dpkg -l libssl-dev libcurl4-openssl-dev libxml2-dev 2>&1 | tail -10
