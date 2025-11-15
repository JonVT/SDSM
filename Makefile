# Simple helper targets

.PHONY: build lint lint-css test fmt fmt-check

build:
	go build ./...

# Aggregated lint target (expand as more linters are added)
lint: lint-css

lint-css:
	bash tools/check_unused_css.sh

test:
	go test ./...

# Format all Go files in-place
fmt:
	gofmt -s -w .

# Check that all Go files are gofmt'd; fails if any are not
fmt-check:
	@FILES=$$(gofmt -l .); \
	if [ -n "$$FILES" ]; then \
		echo "The following files are not gofmt'd:"; \
		echo "$$FILES"; \
		exit 1; \
	fi
