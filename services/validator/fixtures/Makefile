.PHONY: build
# Build the sandbox fixture image. The control-plane's validator service
# references this tag via NEXIS_VALIDATOR_IMAGE (defaults to
# nexis/validator-fixture:latest).
build:
	docker build -t nexis/validator-fixture:latest .
