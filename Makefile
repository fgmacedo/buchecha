.PHONY: build install check-build test test-race vet fmt fmt-check tidy clean

check-build:
	go build ./...

build: check-build
	go build -o bcc ./cmd/bcc

install: check-build
	go install ./cmd/bcc

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@diff=$$(gofmt -l .); if [ -n "$$diff" ]; then echo "$$diff"; exit 1; fi

tidy:
	go mod tidy

clean:
	rm -f bcc
