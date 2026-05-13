.PHONY: build test lint vet run tidy build-fixture test-property

build:
	go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server

test:
	go test -race -timeout 60s ./...

vet:
	go vet ./...

lint: vet

run:
	go run ./cmd/server

tidy:
	go mod tidy

# Phase 4 + 6: build the sandboxed fixture image consumed by the docker
# runner. The same target is mirrored under fixtures/Makefile.
build-fixture:
	$(MAKE) -C fixtures build

# Phase 6: run the Hypothesis property suite locally (requires hypothesis
# + pytest on the active python env). Mirrors what the in-container
# sidecar runs against `/srv/fixture` — useful for iterating on properties
# without rebuilding the validator image.
#
# PYTHONPATH is set so the fixture package resolves to the on-disk source.
# We override the pythonpath ini key so the fixtures/pytest.ini config does
# not shadow whatever PYTHONPATH points at (in production the sidecar runs
# pytest with no parent pytest.ini in the rootdir, so this only matters for
# local dev).
test-property:
	HYPOTHESIS_TESTS_DIR=fixtures/hypothesis \
	PYTHONPATH=fixtures/src \
	python -m pytest fixtures/hypothesis -q --tb=line \
	  -c fixtures/hypothesis/pytest.ini
