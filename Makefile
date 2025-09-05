.PHONY: lint

GO_FILES := $(shell find . -name '*.go' -not -path './vendor/*')

lint:
ifeq ($(GO_FILES),)
	@echo "lint: no go files yet, skipping"
else
	golangci-lint run ./...
endif
