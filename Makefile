.PHONY: build install check-build test test-race vet fmt fmt-check tidy clean api-openapi webui

check-build:
	go build ./...

api-openapi:
	go run ./internal/api/cmd/gen-openapi

# Stub target: the real SPA bundle lands in P6 (see docs/specs/api-webui).
webui: api-openapi
	@echo "webui: stub (real bundle lands in P6)"

build: webui check-build
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
