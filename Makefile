.PHONY: build generate log test vendor

build:
	go build -o ldcli

generate:
	go generate ./...

log:
	tail -f *.log

test:
	go test ./...

vendor:
	go mod tidy && go mod vendor
