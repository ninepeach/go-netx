.PHONY: fmt test race vet check examples clean

EXAMPLE_DIR := build/examples
EXAMPLES := tcp-echo tcp-client udp-echo udp-client http-server socks5-server

fmt:
	gofmt -w .

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

check: test race vet

examples:
	@mkdir -p $(EXAMPLE_DIR)
	@for example in $(EXAMPLES); do \
		echo "building $(EXAMPLE_DIR)/$$example"; \
		go build -o "$(EXAMPLE_DIR)/$$example" "./examples/$$example" || exit 1; \
	done

clean:
	rm -rf build
