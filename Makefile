# Simple helper targets

.PHONY: build lint lint-css

build:
	go build ./...

# Aggregated lint target (expand as more linters are added)
lint: lint-css

lint-css:
	bash tools/check_unused_css.sh
