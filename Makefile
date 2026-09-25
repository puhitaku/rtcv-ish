GO_DIRS := ./api/... ./cmd/... ./internal/... ./test/...
GO_SRC := cmd internal test

.PHONY: gen build test test-e2e fmt lint

gen:
	./scripts/gen.sh

build:
	go build -o bin/ ./cmd/...

test:
	go test ./...

test-e2e:
	go test -count=1 -v ./test/e2e/...

fmt:
	gofmt -w $(GO_SRC)
	goimports -w -local github.com/puhitaku/rtcv-ish $(GO_SRC)

lint:
	go vet $(GO_DIRS)
	if command -v golangci-lint >/dev/null; then golangci-lint run $(GO_DIRS); fi
