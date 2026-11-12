.PHONY: build generate log test vendor verify-dependency-pr

build:
	go build -o ldcli

generate:
	go generate ./...

install-hooks:
	pre-commit install

log:
	tail -f *.log

openapi-spec-check-updates:
	make openapi-spec-update
	./scripts/check-openapi-changed.sh

openapi-spec-download:
	curl -s -o ld-openapi.json https://app.launchdarkly.com/api/v2/openapi.json

openapi-spec-update:
	make openapi-spec-download
	make generate

test:
	go test ./...

vendor:
	go mod tidy && go mod vendor

verify-dependency-pr:
	scripts/dependency-pr/verify.sh $(ARGS)
