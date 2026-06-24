##
#   Copyright 2025 TechDivision GmbH
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
##

BINARY   := valet
CMD      := ./cmd/valet
DIST     := dist
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w -X main.Version=$(VERSION)

.PHONY: build build-all install clean test lint help

## build: Build for the current OS/arch (output: dist/valet)
build:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(CMD)
	@echo "Built $(DIST)/$(BINARY)  ($(VERSION))"

## install: Build and copy to /usr/local/valet-sh/bin/valet
install: build
	@mkdir -p /usr/local/valet-sh/bin
	install -m 755 $(DIST)/$(BINARY) /usr/local/valet-sh/bin/$(BINARY)
	@echo "Installed /usr/local/valet-sh/bin/$(BINARY)"

## build-all: Cross-compile for all supported platforms
build-all:
	@mkdir -p $(DIST)
	GOOS=linux  GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-amd64  $(CMD)
	GOOS=linux  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-arm64  $(CMD)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-darwin-amd64 $(CMD)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-darwin-arm64 $(CMD)
	@echo "All binaries written to $(DIST)/"
	@ls -lh $(DIST)/$(BINARY)-*

## test: Run all tests with race detector
test:
	go test -race ./...

## test-coverage: Run tests with coverage report
test-coverage:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out

## fmt-check: Verify code formatting (CI quality gate)
fmt-check:
	@if [ -n "$(gofmt -l .)" ]; then \
		echo "The following files need formatting:"; \
		gofmt -l .; \
		echo "Run 'gofmt -w .' to fix"; \
		exit 1; \
	fi
	@echo "All files properly formatted"

## lint: Run golangci-lint (auto-installs if not present)
LINT_VERSION := v1.64.8
LINT_BIN := $(shell go env GOPATH)/bin/golangci-lint

lint: $(LINT_BIN)
	$(LINT_BIN) run

$(LINT_BIN):
	@echo "Installing golangci-lint $(LINT_VERSION)..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(LINT_VERSION)

## vet: Run go vet static analysis
vet:
	go vet ./...

## lint-ci: Run linter exactly as CI does (includes go mod download)
lint-ci: $(LINT_BIN)
	go mod download
	$(LINT_BIN) run --timeout=5m

## mod-verify: Verify go.mod is tidy
mod-verify:
	go mod tidy
	git diff --exit-code go.mod go.sum

## quality: Run all quality checks (fmt, vet, lint, mod-verify)
quality: fmt-check vet lint mod-verify

## clean: Remove build artifacts and coverage files
clean:
	rm -rf $(DIST) coverage.out

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
