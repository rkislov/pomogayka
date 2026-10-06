APP := pomogayka
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
OUT ?= dist
LDFLAGS := -s -w -X main.version=$(VERSION)
export CGO_ENABLED := 0

.PHONY: all build release clean test run linux-amd64 windows-amd64 raspberry-pi64 raspberry-pi32

all: release

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/server
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP)-migrate ./cmd/migrate-sqlite-to-postgres

release:
	OUT_DIR=$(OUT) VERSION=$(VERSION) ./scripts/build-release.sh

linux-amd64:
	mkdir -p $(OUT)
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(APP)-linux-amd64 ./cmd/server

windows-amd64:
	mkdir -p $(OUT)
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(APP)-windows-amd64.exe ./cmd/server

raspberry-pi64:
	mkdir -p $(OUT)
	GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(APP)-linux-arm64 ./cmd/server

raspberry-pi32:
	mkdir -p $(OUT)
	GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(APP)-linux-armv7 ./cmd/server

test:
	go test ./...

run:
	go run ./cmd/server

clean:
	rm -rf bin dist
