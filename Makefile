VERSION ?= 0.1.3
GO ?= go
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: test vet race build clean
test:
	$(GO) test ./... -count=1
vet:
	$(GO) vet ./...
race:
	$(GO) test -race ./... -count=1
build:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags='$(LDFLAGS)' -o dist/tavito-guard-linux-amd64 ./cmd/tavito-guard
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags='$(LDFLAGS)' -o dist/tavito-guard-linux-arm64 ./cmd/tavito-guard
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o dist/tavito-release ./cmd/tavito-release
clean:
	$(GO) clean
