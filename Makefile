GO_DIRS := ./api/... ./cmd/... ./internal/... ./test/...
GO_SRC := cmd internal test

.PHONY: gen web build build-noweb test test-e2e fmt lint

gen:
	./scripts/gen.sh

web:
	if [ -f web/package.json ]; then cd web && npm ci && npm run build; fi

build: web
	go build -tags embedweb -o bin/ ./cmd/...

build-noweb:
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
