# Hypothesis property suite

Phase 6 Hypothesis property-based tests, copied into the validator image
at `/srv/hypothesis-tests/` and executed by the sidecar at
`hypothesis-sidecar/sidecar.py`.

The suite is intentionally minimal: 5 properties over
`nexis_fixture.add/sub/mul/safe_div`. A patch from the Synthesiser that
breaks an invariant (e.g. a `safe_div` rewrite that swallows the
`b == 0` branch) will be falsified here before Approval Gate runs.

Phase 7 replaces this hand-rolled suite with an auto-generator that
walks the patched repo's typed signatures.
