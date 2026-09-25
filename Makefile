.PHONY: build generate log test vendor parity parity-capture

PARITY_GO_BIN := bin/ldcli-go
PARITY_RUST_BIN := rust/target/debug/ldcli
PARITY_HARNESS := rust/target/debug/parity
# The Go build injects its version with -X main.version; the Rust build reads
# the same label from the environment. Cases are recorded against "test".
PARITY_VERSION := test

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

parity:
	mkdir -p bin
	go build -ldflags "-X main.version=$(PARITY_VERSION)" -o $(PARITY_GO_BIN) .
	LDCLI_VERSION=$(PARITY_VERSION) cargo build --manifest-path rust/Cargo.toml -p ldcli -p parity
	go run ./parity/dump > bin/commands.txt
	$(PARITY_HARNESS) check --go-bin $(PARITY_GO_BIN) --rust-bin $(PARITY_RUST_BIN) --commands bin/commands.txt --cases parity/cases --exemptions parity/exemptions.toml --fixtures parity/fixtures

# Capture never runs the Rust binary: expectations come from the Go oracle.
parity-capture:
	mkdir -p bin
	go build -ldflags "-X main.version=$(PARITY_VERSION)" -o $(PARITY_GO_BIN) .
	cargo build --manifest-path rust/Cargo.toml -p parity
	go run ./parity/dump > bin/commands.txt
	$(PARITY_HARNESS) capture --go-bin $(PARITY_GO_BIN) --commands bin/commands.txt --cases parity/cases --exemptions parity/exemptions.toml --fixtures parity/fixtures --seed-missing-help
