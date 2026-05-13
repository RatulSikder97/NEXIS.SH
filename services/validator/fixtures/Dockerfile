# Fixture image: a tiny Python project + pytest + pytest-json-report. The
# control-plane's validator spawns one container per /v1/validate call with
# `docker run --rm --network=none --read-only` and reads the JSON report from
# stdout.
#
# Build:
#   docker build -t nexis/validator-fixture:latest services/validator/fixtures
FROM python:3.12-slim

# git is needed for `git apply <patch>` inside the container; --no-install-
# recommends keeps the image small.
RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir pytest pytest-json-report

WORKDIR /app
COPY pyproject.toml pytest.ini /app/
COPY src /app/src
COPY tests /app/tests

# Initialise a git repo so the patch tool inside the validator (`git apply`)
# has a working tree to apply against. Phase 4 patches are typically empty,
# but Phase 5 patches will be real diffs.
RUN cd /app \
    && git init -q \
    && git add . \
    && git -c user.email=fix@nx -c user.name=fix commit -q -m "fixture base"

CMD ["pytest", "-q"]
