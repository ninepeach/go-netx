GO ?= go
GOFMT ?= gofmt

GOFILES := $(shell find . -name '*.go' -not -path './vendor/*')

.PHONY: all check ci fmt fmt-check test race vet windows-vet

all: check

fmt:
	@$(GOFMT) -w $(GOFILES)

fmt-check:
	@files="$$($(GOFMT) -l $(GOFILES))"; \
	if [ -n "$$files" ]; then \
		echo "Go files need formatting:"; \
		echo "$$files"; \
		exit 1; \
	fi

test:
	@$(GO) test ./...

race:
	@$(GO) test -race ./...

vet:
	@$(GO) vet ./...

windows-vet:
	@GOOS=windows GOARCH=amd64 $(GO) vet ./...

check: fmt-check vet test

ci: check race windows-vet
