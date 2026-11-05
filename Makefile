.PHONY: fmt fmtcheck test race vet bench testdoc doccheck cross stress fuzz check verify examples clean

PLATFORMS := linux/amd64 linux/386 darwin/arm64 windows/amd64 freebsd/amd64 js/wasm

EXAMPLE_DIR := build/examples
EXAMPLES := tcp-echo tcp-client udp-echo udp-client http-server socks5-server

fmt:
	gofmt -w .

fmtcheck:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

test:
	go test ./...

race:
	go test -race ./...
	go test -race -tags netxdebug ./...

vet:
	go vet ./...
	go vet -tags stress ./...

bench:
	go test -run '^$$' -bench . -benchmem ./...

testdoc:
	go run ./internal/tools/testdoc -w

doccheck:
	go run ./internal/tools/testdoc

cross:
	@for p in $(PLATFORMS); do \
		echo "vet $$p"; \
		GOOS=$${p%/*} GOARCH=$${p#*/} go vet ./... || exit 1; \
		GOOS=$${p%/*} GOARCH=$${p#*/} go vet -tags stress ./... || exit 1; \
	done

# Stress tests live behind the stress build tag. NETX_STRESS_SECONDS sets
# the long-run duration (default 3).
stress:
	go test -race -tags stress -count=1 -run Stress ./...
	go test -race -tags 'stress netxdebug' -count=1 -run Stress ./...

FUZZTIME ?= 30s
FUZZ_TARGETS := FuzzDropCount:./internal/sys FuzzTableOps:./flow FuzzRingOps:./queue \
	FuzzGetRelease:./buf FuzzReplyAndWriteTo:./udp

fuzz:
	@for t in $(FUZZ_TARGETS); do \
		echo "fuzz $${t%%:*} ($(FUZZTIME))"; \
		go test -run '^$$' -fuzz "^$${t%%:*}$$" -fuzztime $(FUZZTIME) $${t#*:} || exit 1; \
	done

check: test race vet

# verify is the required check before every push. See docs/CONTRIBUTING.md.
verify: fmtcheck vet doccheck race cross

examples:
	@mkdir -p $(EXAMPLE_DIR)
	@for example in $(EXAMPLES); do \
		echo "building $(EXAMPLE_DIR)/$$example"; \
		go build -o "$(EXAMPLE_DIR)/$$example" "./examples/$$example" || exit 1; \
	done

clean:
	rm -rf build
