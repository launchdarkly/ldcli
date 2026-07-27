.PHONY: build generate log test test-docker sandbox-docker vendor

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

# Build the test image and run the full automated suite (Go + dev-server UI) in it.
test-docker:
	docker build -f Dockerfile.test -t ldcli-test .
	docker run --rm ldcli-test

# Interactive sandbox for manually walking `ldcli setup` in a throwaway Node project.
# Pass your token: make sandbox-docker LD_ACCESS_TOKEN=<token>
sandbox-docker:
	docker build -f Dockerfile.test -t ldcli-test .
	docker run --rm -it -e LD_ACCESS_TOKEN=$(LD_ACCESS_TOKEN) ldcli-test sandbox

vendor:
	go mod tidy && go mod vendor
