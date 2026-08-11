.PHONY: seed-sentry migrate help

ORG_ID ?=
SECRET ?= dev-sentry-webhook-secret-32
HOST   ?= http://localhost:8080
DATABASE_URL ?= postgres://nexis:nexis_dev_password@localhost:5432/nexis?sslmode=disable

migrate:
	@echo "control-plane applies migrations automatically at boot (internal/platform/migrate)."
	@echo "This target is for replaying them manually against $(DATABASE_URL) without starting the server."
	@for f in $$(ls services/control-plane/migrations/*.up.sql | sort); do \
		echo "[up] $$f"; \
		psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f "$$f" || exit 1; \
	done

seed-sentry:
	@test -n "$(ORG_ID)" || (echo "ORG_ID required (e.g. ORG_ID=<uuid> make seed-sentry)" && exit 1)
	@BODY='{"id":"e-'$$$$'","level":"error","title":"Test fault","environment":"prod","tags":[["service","api"]]}'; \
	SIG=$$(printf '%s' "$$BODY" | openssl dgst -sha256 -hmac "$(SECRET)" -hex | awk '{ if (NF==1) print $$1; else print $$2 }'); \
	curl -sf -X POST "$(HOST)/v1/webhooks/sentry/$(ORG_ID)" \
	  -H "Content-Type: application/json" \
	  -H "Sentry-Hook-Signature: $$SIG" \
	  -d "$$BODY" && echo " ✓ posted"

help:
	@grep '^[a-z]' Makefile | awk -F: '{print $$1}' | sort -u
