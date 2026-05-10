.PHONY: build install check-build test test-race vet fmt fmt-check lint tidy clean api-openapi webui webui-size release-snapshot release-check precommit-install precommit-run

check-build:
	go build ./...

api-openapi:
	go run ./internal/api/cmd/gen-openapi

# Build the SPA bundle into internal/webui/web/dist/. Vite is invoked
# after api-openapi so the generator wired in package.json prebuild
# reads the freshest internal/api/openapi.json on every run, and the
# Go embed at internal/webui/embed.go picks up the rebuilt bundle.
webui: api-openapi
	cd internal/webui/web && npm ci && npm run build

# Bundle size CI gate. Sums the gzipped byte length of every file
# under internal/webui/web/dist/ and fails when the total exceeds
# the 600 KB ceiling defined in T6.8 of
# docs/specs/api-webui/2026-05-04-implementation.md.
webui-size: webui
	go run ./internal/webui/cmd/check-bundle-size

build: webui-size check-build
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

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

precommit-install:
	pre-commit install

precommit-run:
	pre-commit run --all-files

clean:
	rm -f bcc
	rm -rf dist

release-check:
	goreleaser check

release-snapshot:
	goreleaser release --snapshot --clean
