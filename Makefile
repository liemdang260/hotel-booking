GOCMD ?= go

.PHONY: build test fmt tidy check

build:
	$(GOCMD) build ./...

test:
	$(GOCMD) test ./...

fmt:
	$(GOCMD) fmt ./...

tidy:
	$(GOCMD) mod tidy

check: fmt test build
