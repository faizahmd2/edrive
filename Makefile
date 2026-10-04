BINARY := edrive
VERSION := 0.5.0
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test release clean

# edrive uses cgo for Touch ID and the Keychain, so it must be built on macOS.
build:
	CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/edrive

test:
	go test ./...

release:
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-darwin-arm64 ./cmd/edrive
	cd bin && shasum -a 256 $(BINARY)-darwin-arm64 > $(BINARY)-darwin-arm64.sha256
	@echo "Upload bin/$(BINARY)-darwin-arm64 and bin/$(BINARY)-darwin-arm64.sha256 to the v$(VERSION) release."

clean:
	rm -rf bin
