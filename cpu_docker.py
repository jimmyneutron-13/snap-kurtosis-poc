#!/usr/bin/env python3
"""Print total CPU time (user+system, seconds) and RSS (MiB) of the geth
process running inside a Docker container — the external measurement for
the Kurtosis PoC (same units as the local PoC's cpu_time.py, which reads
/proc/<pid>/stat on the host).

Usage: cpu_docker.py <container-id-or-name> <geth-pid-inside-container>
Output: "<cpu_seconds> <rss_mib>"

Reads /proc/<pid>/stat (utime+stime fields) and /proc/<pid>/status (VmRSS)
from INSIDE the container via `docker exec`. The measurement itself is
computed outside the victim process (by this script, on the runner host);
only the kernel accounting read goes through docker exec.
CLK_TCK is 100 on Linux x86_64.
"""
import subprocess
import sys

cid, pid = sys.argv[1], sys.argv[2]


def docker_exec(*args):
    out = subprocess.run(
        ["docker", "exec", cid, *args],
        capture_output=True, text=True, timeout=30,
    )
    if out.returncode != 0:
        raise RuntimeError(f"docker exec {' '.join(args)}: {out.stderr.strip()}")
    return out.stdout


raw = docker_exec("cat", f"/proc/{pid}/stat")
# comm is in parentheses and may contain spaces/parens; split after last ')'.
fields = raw.rsplit(")", 1)[1].split()
# fields[0]=state, [11]=utime, [12]=stime (0-indexed after comm)
utime = int(fields[11])
stime = int(fields[12])
cpu_seconds = (utime + stime) / 100.0
rss_mib = 0.0
for line in docker_exec("cat", f"/proc/{pid}/status").splitlines():
    if line.startswith("VmRSS:"):
        rss_mib = int(line.split()[1]) / 1024.0
        break
print(f"{cpu_seconds:.2f} {rss_mib:.1f}")
