.PHONY: build generate log test vendor

build:
	go build -o ldcli

generate:
	go generate ./...

install-hooks:
	cp -r git/hooks/* .git/hooks/

log:
	tail -f *.log

openapi-spec-check-updates:
	make openapi-spec-download
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
