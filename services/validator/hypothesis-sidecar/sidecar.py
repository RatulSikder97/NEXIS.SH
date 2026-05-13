"""Phase 6 Hypothesis property-test sidecar.

Listens on a Unix-domain socket (default ``/tmp/hypothesis.sock``) and
runs a small Hypothesis property suite against a patched fixture tree.

Wire protocol (mirrors ``internal/runner/hypothesis.go``):

  request:  4-byte BE length prefix + JSON
    { "patch_diff": str,
      "repo_path": str,    # path inside the container
      "max_examples": int, # default 20
      "max_runtime_seconds": int }  # hard cap, default 30

  response: 4-byte BE length prefix + JSON
    { "tests_passed": bool,
      "failures": [{ "test": str, "counterexample": str, "shrunk": bool }],
      "duration_ms": int,
      "error": str | null }

The sidecar is dev-only — Phase 7 swaps the Unix-socket transport for
the Modal isolate. Until then this process MUST stay inside the
validator container's localhost-only namespace.
"""

from __future__ import annotations

import json
import os
import socket
import struct
import sys
import time
import traceback

from runners.base_runner import generate_and_run


SOCK_PATH = os.environ.get("HYPOTHESIS_SOCKET", "/tmp/hypothesis.sock")
DEFAULT_MAX_EXAMPLES = int(os.environ.get("HYPOTHESIS_MAX_EXAMPLES", "20"))
DEFAULT_MAX_RUNTIME_SECONDS = int(os.environ.get("HYPOTHESIS_MAX_RUNTIME_SECONDS", "30"))
DEFAULT_REPO_PATH = os.environ.get("HYPOTHESIS_REPO_PATH", "/srv/fixture")


def _recv_exact(conn: socket.socket, n: int) -> bytes:
    """Read exactly ``n`` bytes from ``conn`` or return b"" on EOF."""

    buf = bytearray()
    while len(buf) < n:
        chunk = conn.recv(n - len(buf))
        if not chunk:
            return b""
        buf.extend(chunk)
    return bytes(buf)


def read_frame(conn: socket.socket) -> dict | None:
    """Read one length-prefixed JSON frame. Returns None on disconnect."""

    hdr = _recv_exact(conn, 4)
    if len(hdr) != 4:
        return None
    (length,) = struct.unpack(">I", hdr)
    body = _recv_exact(conn, length)
    if len(body) != length:
        return None
    return json.loads(body)


def write_frame(conn: socket.socket, obj: dict) -> None:
    body = json.dumps(obj).encode("utf-8")
    conn.sendall(struct.pack(">I", len(body)) + body)


def handle(req: dict) -> dict:
    """Run the property suite for one request and return the response body."""

    started = time.monotonic_ns()
    repo_path = req.get("repo_path") or DEFAULT_REPO_PATH
    max_examples = int(req.get("max_examples") or DEFAULT_MAX_EXAMPLES)
    raw_runtime = req.get("max_runtime_seconds")
    if raw_runtime is None:
        max_runtime_seconds = DEFAULT_MAX_RUNTIME_SECONDS
    else:
        max_runtime_seconds = max(1, int(raw_runtime))

    try:
        failures = generate_and_run(
            repo_path=repo_path,
            max_examples=max_examples,
            max_runtime_seconds=max_runtime_seconds,
        )
        err: str | None = None
    except Exception as exc:  # noqa: BLE001 — surface every failure to the Go side
        failures = []
        err = f"{type(exc).__name__}: {exc}"
        traceback.print_exc(file=sys.stderr)

    duration_ms = (time.monotonic_ns() - started) // 1_000_000
    return {
        "tests_passed": err is None and not failures,
        "failures": failures,
        "duration_ms": int(duration_ms),
        "error": err,
    }


def serve(sock_path: str = SOCK_PATH) -> None:
    """Bind ``sock_path`` and serve frames forever."""

    try:
        os.unlink(sock_path)
    except FileNotFoundError:
        pass

    srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    srv.bind(sock_path)
    srv.listen(4)
    os.chmod(sock_path, 0o660)
    print(f"hypothesis-sidecar: listening on {sock_path}", flush=True)

    try:
        while True:
            conn, _ = srv.accept()
            try:
                req = read_frame(conn)
                if req is None:
                    continue
                resp = handle(req)
                write_frame(conn, resp)
            except Exception:  # noqa: BLE001
                traceback.print_exc(file=sys.stderr)
            finally:
                conn.close()
    finally:
        try:
            os.unlink(sock_path)
        except FileNotFoundError:
            pass


if __name__ == "__main__":
    serve()
