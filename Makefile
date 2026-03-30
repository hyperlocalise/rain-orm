projectname?=rain-orm
version?=$(shell git describe --abbrev=0 --tags 2>/dev/null || echo dev)
golangci_lint_version?=v2.10.1
gobin?=$(shell go env GOPATH)/bin
golangci_lint_bin?=$(gobin)/golangci-lint
benchstat_bin?=$(gobin)/benchstat

.DEFAULT_GOAL := help

.PHONY: help
help: ## list makefile targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## build the library (verifies compilation)
	@go build ./...

.PHONY: test
test: ## run tests with coverage
	go test -race -coverprofile=coverage.out $$(go list ./... | grep -v '/examples/')
	go tool cover -func=coverage.out | sort -rnk3

.PHONY: test-integration-db
test-integration-db: ## run mysql and postgres integration tests against local docker services
	./scripts/test-integration-databases.sh

.PHONY: bench
bench: ## run sqlite benchmark suite with allocation metrics
	go test -run '^$$' -bench . -benchmem -count=3 ./pkg/rain

.PHONY: bench-report
bench-report: ## run sqlite benchmark suite and save an annotated report under artifacts/bench
	./scripts/bench-sqlite-report.sh "$(BENCH_FILTER)"

.PHONY: bench-ormshowdown
bench-ormshowdown: ## run ORM showdown benchmark suite with allocation metrics
	go test -run '^$$' -bench . -benchmem ./benchmarks/ormshowdown/...

.PHONY: benchstats
benchstats: ## run ORM showdown per-library benchmarks and compare them with benchstat
	BENCHSTAT_BIN="$(benchstat_bin)" ./scripts/bench-ormshowdown-report.sh "$(BENCH_FILTER)"

.PHONY: test-json
test-json: ## run tests with JSON output (for CI)
	go test -json -race -coverprofile=coverage.out $$(go list ./... | grep -v '/examples/') > test-report.jsonl

.PHONY: clean
clean: ## clean up test artifacts
	@rm -rf coverage.out test-report.jsonl

.PHONY: fmt
fmt: ## format go files
	go tool goimports -w .
	go tool gofumpt -w .

.PHONY: fmt-file
fmt-file: ## format a single file (usage: make fmt-file FILE=path/to/file.go)
	go tool goimports -w $(FILE)
	go tool gofumpt -w $(FILE)

.PHONY: lint
lint: ## lint go files
	go tool golangci-lint run

.PHONY: precommit
precommit: ## run local CI validation flow
	make fmt
	git diff --exit-code
	make lint
	make test
	make build

.PHONY: staticcheck
staticcheck: ## run staticcheck directly
	go tool staticcheck ./...

.PHONY: example-basic
example-basic: ## run basic usage example (placeholder)
	@echo "This example demonstrates basic CRUD operations."
	@echo "See examples/basic/main.go for implementation guidance."

.PHONY: example-schema
example-schema: ## run schema definition example (placeholder)
	@echo "This example demonstrates schema definition."
	@echo "See examples/schema/main.go for implementation guidance."

.PHONY: example-dialect
example-dialect: ## run dialect example (placeholder)
	@echo "This example demonstrates database dialects."
	@echo "See examples/dialect/main.go for implementation guidance."

.PHONY: bootstrap
bootstrap: ## download tool and module dependencies
	go mod download
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(golangci_lint_version)
	go install golang.org/x/perf/cmd/benchstat@latest
