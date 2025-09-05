.PHONY: build test vet lint run

GO_FILES := $(shell find . -name '*.go' -not -path './vendor/*')
BINARY   := bin/gateway
CMD      := ./cmd/gateway

build:
	go build -o $(BINARY) $(CMD)

test:
	go test -race -cover ./...

vet:
	go vet ./...

lint:
ifeq ($(GO_FILES),)
	@echo "lint: no go files yet, skipping"
else
	golangci-lint run ./...
endif

run: build
	$(BINARY)
