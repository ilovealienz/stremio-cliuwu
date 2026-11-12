BINARY  = stremio-cliuwu
# The tag on a tagged commit, otherwise the newest release plus -dev-<commit>
# (release tags are on main, outside the working branch's history).
VERSION ?= $(patsubst v%,%,$(shell git describe --tags --exact-match --dirty 2>/dev/null || { t=$$(git describe --tags $$(git rev-list --tags --max-count=1) 2>/dev/null) && echo "$$t-dev-$$(git rev-parse --short HEAD)$$(git diff --quiet HEAD || echo -dirty)"; }))
ifeq ($(VERSION),)
VERSION = dev
endif

.PHONY: run build build-all build-linux build-windows install clean

run:
	go mod tidy
	go run .

build:
	go mod tidy
	CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BINARY) .
	@echo "built → ./$(BINARY)"

build-all: build-linux build-windows

build-linux:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o dist/$(BINARY)-linux .

build-windows:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o dist/$(BINARY)-windows.exe .

install: build
	install -m755 $(BINARY) /usr/local/bin/$(BINARY)
	@echo "installed → /usr/local/bin/$(BINARY)"

clean:
	rm -f $(BINARY)
	rm -rf dist/
