BINARY := edrive
VERSION := 0.4.0

.PHONY: build test darwin-arm64 clean

build:
	go build -trimpath -ldflags "-s -w" -o bin/$(BINARY) ./cmd/edrive

test:
	go test ./...

darwin-arm64:
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/$(BINARY)-darwin-arm64 ./cmd/edrive

clean:
	rm -rf bin
