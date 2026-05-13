# syntax=docker/dockerfile:1.7
#
# Phase 6 causal-inference sidecar — FastAPI service exposing POST /infer.
# Phase 7 swaps this for a DoWhy-backed gRPC service; the Phase 6 image is
# deliberately small and dependency-light so docker-compose stays fast.
FROM python:3.12-slim

WORKDIR /app

# Install runtime + dev deps in one layer. pytest + httpx are shipped here
# so `make test` can run inside the same image during CI without a second
# image build. Phase 7 splits runtime vs dev when DoWhy lands.
COPY requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir -r /app/requirements.txt

COPY pyproject.toml /app/pyproject.toml
COPY src/ /app/src/

ENV PYTHONPATH=/app/src \
    PYTHONUNBUFFERED=1 \
    PORT=8090

EXPOSE 8090

# Default to uvicorn so docker-compose just works. `python -m app` also
# works (it boots the same uvicorn instance) — kept for `make run-dev`.
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8090"]
