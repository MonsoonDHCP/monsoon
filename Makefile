VERSION := $(shell git describe --tags --always --dirty 2> NUL || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build release test lint clean run

build:
	go build -ldflags "$(LDFLAGS)" -o bin/monsoon ./cmd/monsoon

release:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-linux-amd64 ./cmd/monsoon
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-linux-arm64 ./cmd/monsoon
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-darwin-amd64 ./cmd/monsoon
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-darwin-arm64 ./cmd/monsoon
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-windows-amd64.exe ./cmd/monsoon
	GOOS=freebsd GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-freebsd-amd64 ./cmd/monsoon

test:
	go test -race -cover ./...

lint:
	go vet ./...

run:
	go run ./cmd/monsoon --config ./configs/monsoon.yaml

clean:
	powershell -NoProfile -Command "if (Test-Path bin) { Remove-Item -Recurse -Force bin }"
