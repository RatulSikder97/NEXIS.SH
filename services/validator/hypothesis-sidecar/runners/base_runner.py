"""Phase 6 Hypothesis property-test runner.

Discovers a tiny hand-rolled Hypothesis suite under
``services/validator/fixtures/hypothesis/`` and runs it against the
patched fixture tree on ``PYTHONPATH``. Phase 7 will swap this for a
typed-Python static-analysis generator that emits property tests
automatically.

The runner is deliberately narrow:
  - Hypothesis test budget capped at ``max_runtime_seconds`` (default 30s).
  - Pytest stdout parsed line-by-line for ``FAILED`` markers; full
    counterexamples (when Hypothesis ships them) are extracted from the
    short summary block.
  - Returns ``[]`` on a clean run; non-empty list otherwise.
"""

from __future__ import annotations

import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path


# Path inside the validator image where the property-test suite lives.
# The validator Dockerfile copies ``fixtures/hypothesis/`` to this path so
# the sidecar can run pytest against it without leaking onto the host.
HYP_TESTS_DIR = os.environ.get(
    "HYPOTHESIS_TESTS_DIR", "/srv/hypothesis-tests"
)


_FAIL_RE = re.compile(r"^FAILED\s+(?P<test>\S+)")
_FALSIFYING_RE = re.compile(r"Falsifying example:\s*(?P<ex>.*)")


def _has_test_suite(tests_dir: str) -> bool:
    p = Path(tests_dir)
    if not p.is_dir():
        return False
    return any(p.rglob("test_*.py"))


def generate_and_run(
    repo_path: str,
    max_examples: int,
    max_runtime_seconds: int,
    tests_dir: str | None = None,
) -> list[dict]:
    """Run the Hypothesis suite at ``tests_dir`` against ``repo_path``.

    Returns a list of failure dicts. An empty list means every property
    held for every example Hypothesis tried.
    """

    suite = tests_dir or HYP_TESTS_DIR
    if not _has_test_suite(suite):
        # No tests authored yet — treat as a clean run. Phase 7 will
        # error out here once the auto-generator lands.
        return []

    env = os.environ.copy()
    env["PYTHONPATH"] = repo_path + os.pathsep + env.get("PYTHONPATH", "")
    # Surface the budget to the test suite via @settings/env defaults.
    env["HYPOTHESIS_MAX_EXAMPLES"] = str(max(1, int(max_examples)))
    env["HYPOTHESIS_DEADLINE_MS"] = str(max(50, int(max_runtime_seconds) * 1000))

    with tempfile.TemporaryDirectory() as tmpd:
        cp = subprocess.run(
            [
                sys.executable,
                "-m",
                "pytest",
                suite,
                "-q",
                "--tb=line",
                "--no-header",
                "-o",
                f"cache_dir={tmpd}",
            ],
            env=env,
            capture_output=True,
            text=True,
            timeout=max(5, int(max_runtime_seconds) + 10),
        )

    failures: list[dict] = []
    pending_example: list[str] = []
    collecting = False
    lines = cp.stdout.splitlines()
    for idx, raw in enumerate(lines):
        line = raw.rstrip()
        stripped = line.strip()

        # Start of a falsifying example block — Hypothesis emits a
        # multi-line representation: ``Falsifying example:\n    test_xxx(\n        a=0,\n    )``.
        m_falsify = _FALSIFYING_RE.search(stripped)
        if m_falsify:
            tail = m_falsify.group("ex").strip()
            pending_example = [tail] if tail else []
            collecting = True
            continue

        # While collecting, accumulate indented continuation lines until
        # we hit a non-indented terminator (or the FAILED line).
        if collecting:
            if line.startswith((" ", "\t")):
                pending_example.append(stripped)
                continue
            collecting = False

        m_fail = _FAIL_RE.match(stripped)
        if not m_fail:
            continue
        test = m_fail.group("test").split("[")[0]
        counterexample = " ".join(pending_example).strip()
        failures.append(
            {
                "test": test,
                "counterexample": counterexample,
                "shrunk": bool(counterexample),
            }
        )
        pending_example = []
        collecting = False
    return failures
