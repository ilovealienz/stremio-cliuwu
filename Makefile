BINARY  = stremio-cliuwu
VERSION ?= $(patsubst v%,%,$(shell git describe --tags --always --dirty 2>/dev/null))
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
