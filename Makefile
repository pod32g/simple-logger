# simple-logger developer tasks.
# Run `make help` for an overview.

GO        ?= go
PKG       ?= ./...
FUZZTIME  ?= 30s
COVERFILE ?= coverage.out

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: build-examples ## Build all packages, including nested example modules
	$(GO) build $(PKG)

.PHONY: build-examples
build-examples: ## Build examples that carry their own go.mod
	@find example -name go.mod -print0 | while IFS= read -r -d '' mod; do \
		dir=$$(dirname "$$mod"); \
		echo "  building $$dir"; \
		(cd "$$dir" && $(GO) build -o "$$(mktemp -d)/" ./... && $(GO) vet ./...) || exit 1; \
	done

.PHONY: test
test: ## Run the test suite
	$(GO) test $(PKG)

.PHONY: race
race: ## Run tests with the race detector
	$(GO) test -race $(PKG)

.PHONY: cover
cover: ## Run tests with coverage and print a summary
	$(GO) test -race -covermode=atomic -coverprofile=$(COVERFILE) $(PKG)
	$(GO) tool cover -func=$(COVERFILE) | tail -1

.PHONY: cover-html
cover-html: cover ## Generate an HTML coverage report
	$(GO) tool cover -html=$(COVERFILE)

.PHONY: bench
bench: ## Run benchmarks
	$(GO) test -bench=. -benchmem -run='^$$' $(PKG)

.PHONY: fuzz
fuzz: ## Run the JSON formatter fuzz target (FUZZTIME=30s by default)
	$(GO) test -run='^$$' -fuzz='^FuzzJSONFormatterFormat$$' -fuzztime=$(FUZZTIME) .

.PHONY: vet
vet: ## Run go vet
	$(GO) vet $(PKG)

.PHONY: fmt
fmt: ## Format the code
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if any file is not gofmt-ed
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Not gofmt-ed:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: lint
lint: ## Run golangci-lint (requires golangci-lint)
	golangci-lint run $(PKG)

.PHONY: staticcheck
staticcheck: ## Run staticcheck (requires staticcheck)
	staticcheck $(PKG)

.PHONY: tidy
tidy: ## Tidy and verify go modules
	$(GO) mod tidy
	$(GO) mod verify

.PHONY: check
check: fmt-check vet test ## Run the fast local checks (fmt, vet, test)

.PHONY: ci
ci: fmt-check vet lint staticcheck race ## Run the full check suite as CI would

.PHONY: clean
clean: ## Remove build and coverage artifacts
	$(GO) clean
	rm -f $(COVERFILE)
