#!/usr/bin/env python3
"""PTY driver for pev CLI fuzzing.

Spawns `pev` under a real pseudo-terminal (survey/v2 refuses to prompt
without one) and drives its interactive menu with a scripted keystroke
sequence. The whole point is responsiveness: every run is bounded by a
hard wall-clock deadline, so a prompt that hangs (or a signal that is
swallowed instead of terminating the process) is detected as a FAIL
rather than blocking the harness forever.

Keystrokes are sent on *quiescence*: after the child's output goes quiet
for `quiet_ms`, the next scripted action is delivered. This self-syncs
with prompts appearing without parsing survey's ANSI stream.

Special action tokens (instead of a literal string to type):
    ENTER      -> "\r"
    DOWN/UP     -> arrow-key escape sequences (survey Select/MultiSelect)
    SPACE      -> " " (MultiSelect toggle)
    CTRL_C      -> 0x03 (SIGINT via the tty)
    CTRL_Z      -> 0x1a (SIGTSTP via the tty)
    Y / N       -> "y"/"n" (Confirm)
    a bare str  -> typed verbatim (no trailing newline unless it ends in \\n)

Exit classification (printed as a single JSON line on stdout):
    completed   -> child exited on its own before the deadline
    interrupted -> child exited after we sent a signal (CTRL_C/CTRL_Z), good
    hung        -> deadline hit, child still alive -> killed (BAD)
    error       -> spawn/IO failure in the harness itself
"""
import json
import os
import pty
import select
import signal
import sys
import termios
import time

ACTION_BYTES = {
    "ENTER": b"\r",
    "DOWN": b"\x1b[B",
    "UP": b"\x1b[A",
    "SPACE": b" ",
    "CTRL_C": b"\x03",
    "CTRL_Z": b"\x1a",
    "Y": b"y",
    "N": b"n",
    "TAB": b"\t",
    "BACKSPACE": b"\x7f",
}


def to_bytes(action):
    if action in ACTION_BYTES:
        return ACTION_BYTES[action]
    if isinstance(action, str):
        return action.encode("utf-8", "replace")
    return bytes(action)


def run(argv, actions, deadline_s, quiet_ms, env=None):
    """Run argv under a PTY, feeding actions on quiescence.

    Returns a result dict.
    """
    quiet_s = quiet_ms / 1000.0
    pid, master_fd = pty.fork()
    if pid == 0:
        # Child. Replace stdin/out/err with the slave side (already done by
        # pty.fork) and exec pev. Use a clean-ish env so discovery is stable.
        run_env = dict(os.environ)
        if env:
            run_env.update(env)
        try:
            os.execvpe(argv[0], argv, run_env)
        except Exception as e:  # pragma: no cover
            os.write(2, f"exec failed: {e}\n".encode())
            os._exit(127)

    # Parent.
    output = bytearray()
    sent = []
    start = time.monotonic()
    last_read = start
    idx = 0
    sent_signal = None
    classification = None
    exit_status = None
    stop_sig = None

    # Make master non-blocking-ish via select.
    while True:
        now = time.monotonic()
        if now - start > deadline_s:
            classification = "hung"
            break

        r, _, _ = select.select([master_fd], [], [], 0.05)
        if r:
            try:
                chunk = os.read(master_fd, 65536)
            except OSError:
                chunk = b""
            if chunk:
                output.extend(chunk)
                last_read = time.monotonic()
                # survey/v2 sizes the terminal by moving the cursor to the
                # bottom-right (ESC[999;999f) then issuing a Device Status
                # Report query (ESC[6n) and BLOCKING until the terminal
                # replies with a cursor-position report. A bare PTY has no
                # terminal behind it, so nothing answers and every prompt
                # after the first MultiSelect wedges. Emulate a real terminal
                # by replying with a plausible cursor position (24x80). This
                # is the single thing that makes deep prompt chains drivable.
                if b"\x1b[6n" in chunk:
                    try:
                        os.write(master_fd, b"\x1b[24;80R")
                    except OSError:
                        pass
                # Cap retained output to avoid unbounded memory.
                if len(output) > 1_000_000:
                    del output[: len(output) - 1_000_000]
            else:
                # EOF: child closed the pty. Reap it.
                break

        # Has the child already exited (or been stopped by SIGTSTP)?
        try:
            wpid, status = os.waitpid(pid, os.WNOHANG | os.WUNTRACED)
            if wpid == pid and os.WIFSTOPPED(status):
                # CTRL_Z (SIGTSTP) suspended the child. This is correct
                # job-control behavior, NOT a hang — the kernel stopped it,
                # the process is responsive. Record it and continue the
                # cleanup path (SIGCONT+SIGTERM below) so we don't leak a
                # stopped process.
                classification = "stopped"
                stop_sig = os.WSTOPSIG(status)
                break
            if wpid == pid:
                exit_status = status
                # Drain any final output.
                try:
                    while True:
                        r2, _, _ = select.select([master_fd], [], [], 0.02)
                        if not r2:
                            break
                        c = os.read(master_fd, 65536)
                        if not c:
                            break
                        output.extend(c)
                except OSError:
                    pass
                break
        except ChildProcessError:
            break

        # Quiescent? Feed the next action.
        if idx < len(actions) and (time.monotonic() - last_read) >= quiet_s:
            action = actions[idx]
            idx += 1
            data = to_bytes(action)
            try:
                os.write(master_fd, data)
            except OSError:
                pass
            sent.append(action)
            if action in ("CTRL_C", "CTRL_Z"):
                sent_signal = action
            # Reset the quiescence clock so we don't dump the whole script
            # at once; give the child a beat to react.
            last_read = time.monotonic()
        # Once the script is exhausted we simply wait for the child to exit
        # on its own (it may be running real checks) until the deadline. A
        # child still alive at the deadline is the ONLY "hung" verdict —
        # because every scenario's action script is built to be sufficient
        # to drive the prompt chain to completion (see scenarios.py), a
        # non-exit means the app genuinely wedged. NOTE: survey/v2 runs the
        # tty in raw mode (ISIG off), so CTRL_C is caught by survey (not the
        # kernel) and CTRL_Z is consumed as a literal byte — neither stops
        # the process, so we cannot rely on job-control signals here; the
        # follow-on completion keystrokes in the script are what prove the
        # app stayed responsive after the signal.

    elapsed = time.monotonic() - start

    # Ensure the child is gone.
    alive = False
    if exit_status is None:
        try:
            wpid, status = os.waitpid(pid, os.WNOHANG)
            if wpid == pid:
                exit_status = status
        except ChildProcessError:
            pass
    if exit_status is None:
        alive = True
        # A stopped (SIGTSTP'd) child must be continued before it can react
        # to SIGTERM, otherwise the term is queued behind the stop.
        if classification == "stopped":
            try:
                os.kill(pid, signal.SIGCONT)
            except ProcessLookupError:
                pass
        # Kill the process group / process to clean up.
        for sig in (signal.SIGTERM, signal.SIGKILL):
            try:
                os.kill(pid, sig)
            except ProcessLookupError:
                break
            time.sleep(0.2)
            try:
                wpid, status = os.waitpid(pid, os.WNOHANG)
                if wpid == pid:
                    exit_status = status
                    break
            except ChildProcessError:
                break

    try:
        os.close(master_fd)
    except OSError:
        pass

    # Decode exit.
    exit_code = None
    term_sig = None
    if exit_status is not None:
        if os.WIFEXITED(exit_status):
            exit_code = os.WEXITSTATUS(exit_status)
        if os.WIFSIGNALED(exit_status):
            term_sig = os.WTERMSIG(exit_status)

    if classification is None:
        if sent_signal:
            classification = "interrupted"
        else:
            classification = "completed"

    text = output.decode("utf-8", "replace")
    return {
        "argv": argv,
        "actions": [a if isinstance(a, str) else repr(a) for a in actions],
        "sent": sent,
        "classification": classification,
        "exit_code": exit_code,
        "term_sig": term_sig,
        "stop_sig": stop_sig,
        "sent_signal": sent_signal,
        "elapsed_s": round(elapsed, 2),
        "was_alive_at_deadline": alive,
        "output_tail": text[-2000:],
        "output_len": len(text),
    }


def main():
    spec = json.load(sys.stdin)
    result = run(
        argv=spec["argv"],
        actions=spec.get("actions", []),
        deadline_s=spec.get("deadline_s", 60),
        quiet_ms=spec.get("quiet_ms", 350),
        env=spec.get("env"),
    )
    print(json.dumps(result))


if __name__ == "__main__":
    main()
