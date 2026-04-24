BINARY  = stremio-cliuwu
VERSION = 0.1.0

.PHONY: run build build-all build-linux build-windows install clean

run:
	go mod tidy
	go run .

build:
	go mod tidy
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BINARY) .
	@echo "built → ./$(BINARY)"

build-all: build-linux build-windows

build-linux:
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o dist/$(BINARY)-linux .

build-windows:
	@mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o dist/$(BINARY)-windows.exe .

install: build
	install -m755 $(BINARY) /usr/local/bin/$(BINARY)
	@echo "installed → /usr/local/bin/$(BINARY)"

clean:
	rm -f $(BINARY)
	rm -rf dist/
