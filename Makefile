GO ?= go
BIN_DIR ?= bin
BINARY ?= $(BIN_DIR)/kopia-server

.PHONY: all build test vet fmt fmt-check check demo clean

all: check

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BINARY) .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w $$(find . -type f -name '*.go' -not -path './vendor/*')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -type f -name '*.go' -not -path './vendor/*'))"

check: build test fmt-check

demo:
	$(GO) test ./server -run '^Test(ServerServesAPIAndFrontendFallback|ServerRequiresConfiguredBasicAuth)$$' -v

clean:
	rm -rf $(BIN_DIR)