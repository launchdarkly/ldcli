.PHONY: vendor

build:
	go build -o ldcli

log:
	tail -f *.log

test:
	go test ./...

vendor:
	go mod tidy && go mod vendor
