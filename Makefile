VERSION ?= 0.1.1

.PHONY: build test vet app clean

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o bin/ ./cmd/...

test:
	go test -race ./...

vet:
	go vet ./...

# macOS: dist/K10s.app with the engine bundled inside.
app:
	VERSION=$(VERSION) macos/K10s/build-app.sh

clean:
	rm -rf bin dist macos/K10s/.build
