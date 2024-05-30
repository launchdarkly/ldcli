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

install-hooks:
	cp -r git/hooks/* .git/hooks/
