#!/usr/bin/env python3
"""Stop ONLY the development processes this project started.

Why this exists: `internal/api` tests and the local API/worker share one database and one
River queue, so a worker left running steals the tests' jobs and produces failures that look
like product bugs. Broad process matching is unsafe here — `pkill -f workforce`, `ps | grep`,
and cmdline scans match the checking shell's own command line and kill the caller (observed
twice during development).

So: match on /proc/<pid>/exe against the built binary, and exclude this process and its
ancestors. Never match on the command line.

  python3 scripts/dev_stop.py list   # show what would be stopped
  python3 scripts/dev_stop.py stop   # terminate them, escalating to SIGKILL if needed
"""
import os
import signal
import sys
import time

# The compiled server binary is the only thing that consumes the River queue.
BINARY_NAMES = {"workforce"}


def _ancestry() -> set[int]:
    """This process plus its ancestors, so we can never kill our own shell."""
    own = {0, 1, os.getpid()}
    pid = os.getpid()
    for _ in range(10):
        try:
            with open(f"/proc/{pid}/stat") as fh:
                pid = int(fh.read().split(") ", 1)[1].split()[1])
        except Exception:
            break
        if pid in own:
            break
        own.add(pid)
    return own


def matching() -> list[tuple[int, str]]:
    own = _ancestry()
    found = []
    for entry in os.listdir("/proc"):
        if not entry.isdigit():
            continue
        pid = int(entry)
        if pid in own:
            continue
        try:
            exe = os.readlink(f"/proc/{pid}/exe")
        except Exception:
            continue
        # /proc/<pid>/exe reads "path (deleted)" for go-run child binaries; take the basename
        # of the first token so that still matches.
        if os.path.basename(exe.split(" (deleted)")[0]) in BINARY_NAMES:
            found.append((pid, exe))
    return found


def main() -> int:
    action = sys.argv[1] if len(sys.argv) > 1 else "list"
    found = matching()
    if action == "list":
        if not found:
            print("none")
        for pid, exe in found:
            print(f"{pid} {exe}")
        return 0
    if action != "stop":
        print(__doc__)
        return 2

    if not found:
        print("none running")
        return 0
    for pid, _ in found:
        print("TERM", pid)
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        except PermissionError:
            print("  permission denied")
    time.sleep(3)
    left = matching()
    for pid, _ in left:
        print("KILL", pid)
        try:
            os.kill(pid, signal.SIGKILL)
        except Exception:
            pass
    time.sleep(1)
    remaining = matching()
    print("remaining:", "none" if not remaining else [p for p, _ in remaining])
    return 0 if not remaining else 1


if __name__ == "__main__":
    raise SystemExit(main())
