.PHONY: fmt test race vet check examples

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
	go build ./examples/...
