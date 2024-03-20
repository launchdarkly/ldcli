.PHONY: vendor

build:
	go build -o ldcli

test:
	go test ./...

vendor:
	go mod tidy && go mod vendor
